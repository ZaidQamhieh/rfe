//! Saved hosts: several agents the user can switch between.
//!
//! The existing session code (`flows`) knows exactly one signed-in agent: `state.json` holds its
//! address, account name and device id plus the pin of every trusted agent, and the keystore holds
//! its login token. That stays the single "active session", so every existing flow (list devices,
//! sign out, pair, approve) keeps working unchanged. This module adds the rest:
//!
//! * `hosts.json` (version 1, written atomically, 0600, no secrets) lists the saved hosts with a
//!   display name, the address and the account name and device id to use for each. The pinned
//!   certificate stays in `state.json`; it is read from there, never copied, so the two cannot
//!   disagree.
//! * The login token of a host that is not the active one is parked in the keystore under its own
//!   entry (`host-token:<data dir>#<host:port>`). Switching parks the active session and unparks the
//!   target's, so each host keeps its own token and an old token is never sent to another agent.
//! * `flows::save` calls [`park_before_replace`] first, so signing in to a second agent through the
//!   ordinary connect flow parks the first agent's login instead of overwriting it.
//!
//! A `state.json` from before this module needs no conversion: its single agent and its pins become
//! hosts the first time the list is read, and `state.json` itself is not changed (an older build can
//! still read it). The app opens on the host used last because switching makes it the active
//! session, which is what `state.json` already remembers between runs.

use crate::applog;
use crate::flows::{self, pin_key, Saved};
use crate::fsutil::write_private;
use crate::secrets::{account, Offloaded, SecretStore};
use serde::{Deserialize, Serialize};
use std::cell::Cell;
use std::path::{Path, PathBuf};
use std::sync::{Mutex, MutexGuard};
use tauri::Manager;

/// The format of `hosts.json`. A file with a higher number is from a newer app and is refused
/// rather than rewritten, so a downgrade cannot drop what the newer app stored.
const VERSION: u32 = 1;
const MAX_NAME_CHARS: usize = 64;

#[derive(Clone, PartialEq, Serialize, Deserialize)]
struct Record {
    /// The pin key (`host:port`, lower case); what every command refers to a host by.
    key: String,
    /// What the user sees; the address until the user renames it.
    name: String,
    /// The address to connect to, as it was typed.
    host: String,
    #[serde(default)]
    username: String,
    #[serde(default)]
    device_id: String,
}

#[derive(Clone, Default, PartialEq, Serialize, Deserialize)]
struct HostsFile {
    #[serde(default)]
    version: u32,
    #[serde(default)]
    hosts: Vec<Record>,
}

fn hosts_path(dir: &Path) -> PathBuf {
    dir.join("hosts.json")
}

/// The keystore entry for the login of `key` while another host is the active one.
pub fn parked_account(dir: &Path, key: &str) -> String {
    format!("{}#{}", account("host-token", dir), pin_key(key))
}

/// Missing file means "no hosts saved yet". A file that cannot be read, is damaged or comes from a
/// newer app is an error, never a silent reset.
fn read_file(dir: &Path) -> Result<HostsFile, String> {
    let path = hosts_path(dir);
    match std::fs::read(&path) {
        Ok(bytes) => {
            let f: HostsFile = serde_json::from_slice(&bytes).map_err(|e| {
                format!(
                    "{} is damaged ({e}); delete it to start over",
                    path.display()
                )
            })?;
            if f.version > VERSION {
                return Err(format!(
                    "{} was written by a newer version of this app; update the app, or delete \
                     it to start over",
                    path.display()
                ));
            }
            Ok(f)
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(HostsFile::default()),
        Err(e) => Err(format!("read {}: {e}", path.display())),
    }
}

fn write_file(dir: &Path, f: &HostsFile) -> Result<(), String> {
    let out = HostsFile {
        version: VERSION,
        hosts: f.hosts.clone(),
    };
    let json = serde_json::to_vec_pretty(&out).map_err(|e| e.to_string())?;
    write_private(&hosts_path(dir), &json)
}

thread_local! {
    static HELD: Cell<bool> = const { Cell::new(false) };
}
static LOCK: Mutex<()> = Mutex::new(());

/// Serialises the host operations. Re-entrant on one thread, because `switch` calls `flows::save`,
/// which calls back into [`park_before_replace`].
pub(crate) struct Held(Option<MutexGuard<'static, ()>>);

impl Drop for Held {
    fn drop(&mut self) {
        if self.0.take().is_some() {
            HELD.with(|h| h.set(false));
        }
    }
}

pub(crate) fn lock() -> Held {
    if HELD.with(|h| h.get()) {
        return Held(None);
    }
    let guard = LOCK.lock().unwrap_or_else(|e| e.into_inner());
    HELD.with(|h| h.set(true));
    Held(Some(guard))
}

fn upsert(file: &mut HostsFile, host: &str, username: &str, device_id: &str) {
    let key = pin_key(host);
    match file.hosts.iter_mut().find(|r| r.key == key) {
        Some(r) => {
            r.host = host.trim().to_string();
            r.username = username.to_string();
            r.device_id = device_id.to_string();
        }
        None => file.hosts.push(Record {
            name: host.trim().to_string(),
            host: host.trim().to_string(),
            key,
            username: username.to_string(),
            device_id: device_id.to_string(),
        }),
    }
}

/// Parks the active session's login so `incoming` can replace it. Called first thing by
/// `flows::save`. Nothing happens when the same host signs in again or when there is no session.
/// If the keystore or the file refuses, this fails and the active session is left as it was.
pub fn park_before_replace(
    dir: &Path,
    store: &dyn SecretStore,
    incoming: &str,
) -> Result<(), String> {
    let _held = lock();
    let cur = flows::load_saved(dir, store)?;
    if cur.host.is_empty() || pin_key(&cur.host) == pin_key(incoming) {
        return Ok(());
    }
    let key = pin_key(&cur.host);
    let acct = parked_account(dir, &key);
    if cur.token.is_empty() {
        store.delete(&acct)?;
    } else {
        store.set(&acct, &cur.token)?;
        if store.get(&acct)?.as_deref() != Some(cur.token.as_str()) {
            return Err("the OS keystore did not keep the login token".into());
        }
    }
    // The record keeps the account name and device id that the active session is about to lose.
    let mut file = read_file(dir)?;
    upsert(&mut file, &cur.host, &cur.username, &cur.device_id);
    write_file(dir, &file)
}

/// The address, pin and login of the saved host `key` without making it the active one: the
/// active session when it is that host, otherwise the parked login. A host that is not saved, or
/// is no longer trusted, is an error; a host with no login gives an empty token.
pub fn saved_for(dir: &Path, store: &dyn SecretStore, key: &str) -> Result<Saved, String> {
    let _held = lock();
    let key = pin_key(key);
    let active = flows::load_active(dir, store)?;
    if !active.host.is_empty() && pin_key(&active.host) == key {
        return Ok(active);
    }
    let file = read_file(dir)?;
    let rec = file
        .hosts
        .iter()
        .find(|r| r.key == key)
        .ok_or_else(|| format!("{key} is not a saved host"))?;
    let fingerprint = flows::list_pins(dir)?
        .into_iter()
        .find(|p| p.host == key)
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            format!("{key} is no longer a trusted agent; connect and compare its fingerprint again")
        })?;
    let token = store.get(&parked_account(dir, &key))?.unwrap_or_default();
    crate::applog::register_secret(&token);
    Ok(Saved {
        host: rec.host.clone(),
        fingerprint,
        token,
        username: rec.username.clone(),
        device_id: rec.device_id.clone(),
    })
}

/// Removes every parked login, for every saved host. A reset device key retires the identity those
/// logins were made with, so none of them may outlive it.
pub fn forget_all_parked(dir: &Path, store: &dyn SecretStore) -> Result<(), String> {
    let _held = lock();
    for r in &read_file(dir)?.hosts {
        store.delete(&parked_account(dir, &r.key))?;
    }
    Ok(())
}

/// Removes the parked login that equals `token`, if any: the agent refused it after the user had
/// already switched to another host. A token is valid at one agent only, so equal means the same login.
pub fn forget_parked_token(dir: &Path, store: &dyn SecretStore, token: &str) -> Result<(), String> {
    let _held = lock();
    for r in &read_file(dir)?.hosts {
        let acct = parked_account(dir, &r.key);
        if store.get(&acct)?.as_deref() == Some(token) {
            store.delete(&acct)?;
        }
    }
    Ok(())
}

/// Removes a parked login. Called when a pin is forgotten, so no token outlives its trust.
pub fn forget_parked(dir: &Path, store: &dyn SecretStore, key: &str) -> Result<(), String> {
    store.delete(&parked_account(dir, key))
}

struct Synced {
    file: HostsFile,
    active: Saved,
    active_key: Option<String>,
    pins: Vec<flows::PinView>,
}

/// Brings the list in line with what `state.json` and the keystore say now: the active session and
/// every trusted agent have a record (this is the migration from a single-agent `state.json`), a
/// record whose agent is no longer trusted is dropped together with its parked login, and the
/// active host has no parked copy of its login.
fn sync(dir: &Path, store: &dyn SecretStore) -> Result<Synced, String> {
    let mut file = read_file(dir)?;
    let before = file.hosts.clone();
    let active = flows::load_saved(dir, store)?;
    let pins = flows::list_pins(dir)?;
    let active_key = (!active.host.is_empty()).then(|| pin_key(&active.host));

    if !active.host.is_empty() {
        upsert(&mut file, &active.host, &active.username, &active.device_id);
    }
    for p in &pins {
        if !file.hosts.iter().any(|r| r.key == p.host) {
            file.hosts.push(Record {
                key: p.host.clone(),
                name: p.host.clone(),
                host: p.host.clone(),
                username: String::new(),
                device_id: String::new(),
            });
        }
    }
    let kept =
        |r: &Record| Some(&r.key) == active_key.as_ref() || pins.iter().any(|p| p.host == r.key);
    let dropped: Vec<String> = file
        .hosts
        .iter()
        .filter(|r| !kept(r))
        .map(|r| r.key.clone())
        .collect();
    for key in &dropped {
        store.delete(&parked_account(dir, key))?;
    }
    file.hosts.retain(|r| kept(r));
    if let Some(key) = &active_key {
        store.delete(&parked_account(dir, key))?;
    }
    if file.hosts != before {
        write_file(dir, &file)?;
    }
    Ok(Synced {
        file,
        active,
        active_key,
        pins,
    })
}

/// One saved host, for the list. Holds no secret.
#[derive(Debug, Clone, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct HostView {
    pub key: String,
    pub name: String,
    pub host: String,
    /// The pinned certificate's fingerprint, empty if none is trusted.
    pub fingerprint: String,
    pub username: String,
    /// A login for this host is saved in the keystore.
    pub signed_in: bool,
    /// This is the host the window is using now.
    pub active: bool,
}

pub fn list(dir: &Path, store: &dyn SecretStore) -> Result<Vec<HostView>, String> {
    let _held = lock();
    let s = sync(dir, store)?;
    let mut out = Vec::new();
    for r in &s.file.hosts {
        let active = Some(&r.key) == s.active_key.as_ref();
        let signed_in = if active {
            !s.active.token.is_empty()
        } else {
            store.get(&parked_account(dir, &r.key))?.is_some()
        };
        out.push(HostView {
            fingerprint: s
                .pins
                .iter()
                .find(|p| p.host == r.key)
                .map(|p| p.fingerprint.clone())
                .unwrap_or_default(),
            key: r.key.clone(),
            name: r.name.clone(),
            host: r.host.clone(),
            username: r.username.clone(),
            signed_in,
            active,
        });
    }
    Ok(out)
}

const NOT_SAVED: &str = "this server is not in the saved list";

/// Makes `key` the active host: its pin, account name, device id and login become the active
/// session. The login of the host that was active is parked first, and nothing is lost if that
/// fails. A host whose certificate is no longer trusted cannot be switched to.
pub fn switch(dir: &Path, store: &dyn SecretStore, key: &str) -> Result<Saved, String> {
    let _held = lock();
    let s = sync(dir, store)?;
    let key = pin_key(key);
    let rec = s
        .file
        .hosts
        .iter()
        .find(|r| r.key == key)
        .ok_or_else(|| NOT_SAVED.to_string())?;
    if s.active_key.as_deref() == Some(key.as_str()) {
        return Ok(s.active);
    }
    let fingerprint = s
        .pins
        .iter()
        .find(|p| p.host == key)
        .map(|p| p.fingerprint.clone())
        .ok_or_else(|| {
            format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                rec.host
            )
        })?;
    let token = store.get(&parked_account(dir, &key))?.unwrap_or_default();
    applog::register_secret(&token);
    let target = Saved {
        host: rec.host.clone(),
        fingerprint,
        token,
        username: rec.username.clone(),
        device_id: rec.device_id.clone(),
    };
    // Parks the current session, then makes the target the active one.
    flows::save(dir, store, &target)?;
    store.delete(&parked_account(dir, &key))?;
    applog::info(&format!("switched to {key}"));
    Ok(target)
}

fn clean_name(name: &str) -> Result<String, String> {
    let name = name.trim();
    if name.is_empty() {
        return Err("enter a name for the host".into());
    }
    if name.chars().count() > MAX_NAME_CHARS {
        return Err(format!(
            "the name can be at most {MAX_NAME_CHARS} characters"
        ));
    }
    if name.chars().any(char::is_control) {
        return Err("the name cannot contain control characters".into());
    }
    Ok(name.to_string())
}

/// Changes only the display name; the address, pin and login are untouched.
pub fn rename(dir: &Path, store: &dyn SecretStore, key: &str, name: &str) -> Result<(), String> {
    let name = clean_name(name)?;
    let _held = lock();
    let mut s = sync(dir, store)?;
    let key = pin_key(key);
    let rec = s
        .file
        .hosts
        .iter_mut()
        .find(|r| r.key == key)
        .ok_or_else(|| NOT_SAVED.to_string())?;
    rec.name = name;
    write_file(dir, &s.file)
}

/// What removing a host did.
#[derive(Debug, Clone, Default, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Removed {
    /// It was the active host, so this computer is no longer signed in to anything.
    pub was_active: bool,
}

/// Forgets a host completely: its login token leaves the keystore, its pin is forgotten (so its
/// fingerprint has to be compared again) and its entry leaves the list. The device stays registered
/// on the agent until it is revoked there; signing out first does that.
pub fn remove(dir: &Path, store: &dyn SecretStore, key: &str) -> Result<Removed, String> {
    let _held = lock();
    let s = sync(dir, store)?;
    let key = pin_key(key);
    let rec = s
        .file
        .hosts
        .iter()
        .find(|r| r.key == key)
        .ok_or_else(|| NOT_SAVED.to_string())?;
    // The token first: if the keystore refuses, nothing else has changed.
    store.delete(&parked_account(dir, &key))?;
    let forgot = flows::forget_pin(dir, store, &rec.host)?;
    let mut file = s.file.clone();
    file.hosts.retain(|r| r.key != key);
    write_file(dir, &file)?;
    applog::info(&format!("removed the saved host {key}"));
    Ok(Removed {
        was_active: forgot.signed_out,
    })
}

// ---- Tauri commands ----

/// The active session as the window shows it; the same fields as the saved-login view.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ActiveView {
    host: String,
    fingerprint: String,
    signed_in: bool,
    username: String,
    device_id: String,
}

impl From<Saved> for ActiveView {
    fn from(s: Saved) -> Self {
        Self {
            signed_in: !s.token.is_empty(),
            host: s.host,
            fingerprint: s.fingerprint,
            username: s.username,
            device_id: s.device_id,
        }
    }
}

fn keystore() -> Offloaded {
    Offloaded::os_keystore()
}

fn data_dir(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    app.path().app_data_dir().map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn list_hosts(app: tauri::AppHandle) -> Result<Vec<HostView>, String> {
    let dir = data_dir(&app)?;
    keystore().run(move |s| list(&dir, s)).await
}

#[tauri::command]
pub async fn switch_host(app: tauri::AppHandle, key: String) -> Result<ActiveView, String> {
    let dir = data_dir(&app)?;
    keystore()
        .run(move |s| switch(&dir, s, &key))
        .await
        .map(Into::into)
}

#[tauri::command]
pub async fn rename_host(app: tauri::AppHandle, key: String, name: String) -> Result<(), String> {
    let dir = data_dir(&app)?;
    keystore().run(move |s| rename(&dir, s, &key, &name)).await
}

#[tauri::command]
pub async fn remove_host(app: tauri::AppHandle, key: String) -> Result<Removed, String> {
    let dir = data_dir(&app)?;
    keystore().run(move |s| remove(&dir, s, &key)).await
}
