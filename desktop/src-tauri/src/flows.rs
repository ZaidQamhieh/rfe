//! App-level flows shared by the Tauri commands and the integration tests.

use crate::agent_client::{
    capture_fingerprint, match_code, new_client_nonce, normalize_fingerprint, AgentClient,
    AgentError, Device, LoginOk, PairPoll,
};
use crate::applog::{self, Level};
use crate::fsutil::write_private;
use crate::identity::Identity;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::Path;

use crate::secrets::{account, Offloaded, SecretStore};

#[derive(Clone, Default)]
pub struct Saved {
    pub host: String,
    pub fingerprint: String,
    /// Lives in the OS keystore, never in `state.json`.
    pub token: String,
    pub username: String,
    /// The agent-assigned id of this computer's device row.
    pub device_id: String,
}

// Not derived: a `{:?}` in a log line or a test failure must not print the login token.
impl std::fmt::Debug for Saved {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Saved")
            .field("host", &self.host)
            .field("fingerprint", &self.fingerprint)
            .field("token", &"<redacted>")
            .field("username", &self.username)
            .field("device_id", &self.device_id)
            .finish()
    }
}

/// What `state.json` holds: no secrets. `token` is read only to migrate a pre-keystore file
/// and is never written.
#[derive(Default, Serialize, Deserialize)]
struct StateFile {
    #[serde(default)]
    host: String,
    #[serde(default)]
    fingerprint: String,
    #[serde(default)]
    username: String,
    #[serde(default)]
    device_id: String,
    #[serde(default, skip_serializing)]
    token: String,
    /// What the app records for the diagnostics report (see `applog`); empty means the default.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    log_level: String,
    /// Every certificate the user has trusted, by `pin_key`. `None` only for a file written before
    /// pins were kept per address; `read_state` folds that file's single host into it.
    #[serde(default)]
    pins: Option<BTreeMap<String, String>>,
}

/// The key a pin is stored under: the address as `host:port`, case-folded. The same host on
/// another port is another agent and has its own pin.
pub fn pin_key(host: &str) -> String {
    host.trim().to_ascii_lowercase()
}

impl StateFile {
    fn pins(&self) -> &BTreeMap<String, String> {
        static NONE: BTreeMap<String, String> = BTreeMap::new();
        self.pins.as_ref().unwrap_or(&NONE)
    }

    fn pins_mut(&mut self) -> &mut BTreeMap<String, String> {
        self.pins.get_or_insert_with(BTreeMap::new)
    }
}

impl StateFile {
    fn into_saved(self, token: String) -> Saved {
        Saved {
            host: self.host,
            fingerprint: self.fingerprint,
            username: self.username,
            device_id: self.device_id,
            token,
        }
    }
}

fn state_path(dir: &Path) -> std::path::PathBuf {
    dir.join("state.json")
}

/// Missing file means "never configured". Any other read or parse failure is an
/// error, so a damaged state file cannot silently drop the pin and token.
fn read_state(dir: &Path) -> Result<StateFile, String> {
    let path = state_path(dir);
    match std::fs::read(&path) {
        Ok(bytes) => {
            let mut f: StateFile = serde_json::from_slice(&bytes).map_err(|e| {
                format!(
                    "{} is damaged ({e}); delete it to start over",
                    path.display()
                )
            })?;
            if f.pins.is_none() {
                let legacy = (!f.host.is_empty() && !f.fingerprint.is_empty())
                    .then(|| (pin_key(&f.host), f.fingerprint.clone()));
                f.pins = Some(legacy.into_iter().collect());
            }
            Ok(f)
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(StateFile::default()),
        Err(e) => Err(format!("read {}: {e}", path.display())),
    }
}

fn write_state(dir: &Path, f: &StateFile) -> Result<(), String> {
    let json = serde_json::to_vec_pretty(f).map_err(|e| e.to_string())?;
    write_private(&state_path(dir), &json)
}

/// The saved log level, or the default (info) when none was chosen.
pub fn log_level(dir: &Path) -> Result<Level, String> {
    let f = read_state(dir)?;
    Ok(Level::parse(&f.log_level).unwrap_or(Level::Info))
}

/// Saves the log level in `state.json` and applies it now.
pub fn set_log_level(dir: &Path, level: &str) -> Result<Level, String> {
    let parsed = Level::parse(level)
        .ok_or_else(|| format!("unknown log level {level:?}; use off, error, info or debug"))?;
    let mut f = read_state(dir)?;
    f.log_level = parsed.as_str().to_string();
    write_state(dir, &f)?;
    applog::set_level(parsed);
    applog::info(&format!("log level set to {}", parsed.as_str()));
    Ok(parsed)
}

/// Applies the saved log level; run once at start. A damaged state file leaves the default.
pub fn apply_log_level(dir: &Path) {
    if let Ok(level) = log_level(dir) {
        applog::set_level(level);
    }
}

/// Saves a throwaway secret, reads it back and removes it: whether the keystore works right now,
/// including a locked one asking to be unlocked.
pub fn check_keystore(dir: &Path, store: &dyn SecretStore) -> Result<(), String> {
    let acct = account("check", dir);
    let outcome = (|| {
        store.set(&acct, "ok")?;
        match store.get(&acct)? {
            Some(v) if v == "ok" => Ok(()),
            _ => Err("the OS keystore did not return what was saved".to_string()),
        }
    })();
    let cleanup = store.delete(&acct);
    let result = outcome.and(cleanup);
    match &result {
        Ok(()) => applog::info("keystore check passed"),
        Err(e) => applog::error(&format!("keystore check failed: {e}")),
    }
    result
}

/// Agents advertising on the local network, each with whether this app already trusts a
/// certificate for its address. Listing an agent trusts nothing: the caller still runs [`probe`]
/// and the user still compares the fingerprint.
pub async fn discover(
    dir: &Path,
    window: std::time::Duration,
) -> Result<Vec<(crate::discovery::Found, bool)>, String> {
    let pins: Vec<String> = read_state(dir)?.pins().keys().cloned().collect();
    let r = crate::discovery::discover(window).await;
    match &r {
        Ok(list) => applog::info(&format!("found {} agents on the network", list.len())),
        Err(e) => applog::error(&format!("network discovery failed: {e}")),
    }
    Ok(r?
        .into_iter()
        .map(|f| {
            let known = pins.contains(&pin_key(&f.hostport));
            (f, known)
        })
        .collect())
}

/// A report the user can paste into a bug report: versions, the agent's address and pinned
/// fingerprint, the recent errors and the in-memory log. It is assembled from fixed fields, never
/// from the keystore or the token, and the log lines are masked ([`applog`]). It does name the
/// agent's address, so it says to read it before posting it in public.
pub fn diagnostics(dir: &Path) -> String {
    use std::fmt::Write;
    let mut out = String::new();
    let line = |out: &mut String, text: String| {
        out.push_str(&text);
        out.push('\n');
    };
    let secs = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    line(&mut out, "RFE Desktop diagnostics".into());
    line(&mut out, format!("generated (unix time): {secs}"));
    line(
        &mut out,
        format!("app version: {}", env!("CARGO_PKG_VERSION")),
    );
    line(
        &mut out,
        format!("sent to agents as: {}", crate::agent_client::CLIENT_VERSION),
    );
    line(
        &mut out,
        format!(
            "system: {} {}",
            std::env::consts::OS,
            std::env::consts::ARCH
        ),
    );
    line(
        &mut out,
        format!("approval on the PC needs: {MIN_AGENT_FOR_APPROVAL} or newer"),
    );
    line(&mut out, format!("log level: {}", applog::level().as_str()));
    match read_state(dir) {
        Ok(f) => {
            let none = |v: &str| {
                if v.is_empty() {
                    "(none)".to_string()
                } else {
                    v.to_string()
                }
            };
            line(&mut out, format!("agent address: {}", none(&f.host)));
            line(
                &mut out,
                format!("pinned fingerprint: {}", none(&f.fingerprint)),
            );
            line(&mut out, format!("trusted agents: {}", f.pins().len()));
            let login = if !f.username.is_empty() {
                "account"
            } else if !f.device_id.is_empty() {
                "pairing code or approval"
            } else {
                "none"
            };
            line(&mut out, format!("saved login: {login}"));
        }
        Err(e) => line(&mut out, format!("state: unreadable ({e})")),
    }
    line(
        &mut out,
        "device key and login token: in the OS keystore, not included".into(),
    );
    let lines = applog::lines();
    let errors: Vec<&String> = lines.iter().filter(|l| l.contains(" ERROR ")).collect();
    out.push_str("\nrecent errors:\n");
    if errors.is_empty() {
        out.push_str("  (none)\n");
    }
    for e in errors.iter().rev().take(20).rev() {
        let _ = writeln!(out, "  {e}");
    }
    let _ = writeln!(out, "\nlog ({} lines, oldest first):", lines.len());
    for l in &lines {
        let _ = writeln!(out, "  {l}");
    }
    out.push_str(
        "\nThis report holds no password, token or key. It names the agent's address; read it \
         before you post it in public.\n",
    );
    out
}

/// Logs the outcome of one flow: `what` succeeded, or failed with the error text (which never
/// holds a credential).
fn logged<T>(what: &str, r: Result<T, AgentError>) -> Result<T, AgentError> {
    match &r {
        Ok(_) => applog::info(&format!("{what}: ok")),
        Err(e) => applog::error(&format!("{what} failed: {e}")),
    }
    r
}

/// Loads the saved state and the token from the keystore. A token left in `state.json` by an
/// earlier version is moved into the keystore first, then removed from the file; if the
/// keystore refuses, this fails and the file is left as it was.
pub fn load_saved(dir: &Path, store: &dyn SecretStore) -> Result<Saved, String> {
    match store.scope_host() {
        Some(host) => crate::hosts::saved_for(dir, store, host),
        None => load_active(dir, store),
    }
}

/// The active session, whatever host `store` is scoped to.
pub fn load_active(dir: &Path, store: &dyn SecretStore) -> Result<Saved, String> {
    // Under the saved-hosts lock: a switch in progress has changed the token and not yet the
    // address (or the reverse), and this must never pair one agent's address with another's token.
    let _held = crate::hosts::lock();
    let mut f = read_state(dir)?;
    let acct = account("token", dir);
    if !f.token.is_empty() {
        store.set(&acct, &f.token)?;
        if store.get(&acct)?.as_deref() != Some(f.token.as_str()) {
            return Err("the OS keystore did not keep the login token".into());
        }
        f.token.clear();
        write_state(dir, &f)?;
    }
    let token = store.get(&acct)?.unwrap_or_default();
    applog::register_secret(&token);
    Ok(f.into_saved(token))
}

/// Stores the token in the keystore (or deletes it when empty), then the rest in `state.json`.
pub fn save(dir: &Path, store: &dyn SecretStore, s: &Saved) -> Result<(), String> {
    let _held = crate::hosts::lock();
    // A different agent replacing the active session must not destroy its login (saved hosts).
    // If the saved-hosts file cannot be used (damaged, or from a newer app) a fresh sign-in still goes
    // through: an approval token is single-use and would be lost, and the old login was never
    // recoverable from that file anyway. The file is left as it is.
    if let Err(e) = crate::hosts::park_before_replace(dir, store, &s.host) {
        if s.token.is_empty() {
            return Err(e);
        }
        applog::error("the previous login could not be kept in the saved hosts");
    }
    let acct = account("token", dir);
    // Keep every other trusted address; this session's address is trusted at this fingerprint.
    // Read before the token changes: a state file that cannot be read must leave both as they were.
    let mut f = read_state(dir)?;
    if !s.host.is_empty() && !s.fingerprint.is_empty() {
        f.pins_mut().insert(pin_key(&s.host), s.fingerprint.clone());
    }
    f.host = s.host.clone();
    f.fingerprint = s.fingerprint.clone();
    f.username = s.username.clone();
    f.device_id = s.device_id.clone();
    f.token.clear();
    // A keystore that cannot be read must stop the save before anything changes: reading it as 'no old
    // token' would make the rollback below delete the real one (audit F-160).
    let previous = store.get(&acct)?;
    if s.token.is_empty() {
        store.delete(&acct)?;
    } else {
        store.set(&acct, &s.token)?;
    }
    if let Err(e) = write_state(dir, &f) {
        // The token and the address must stay a pair: put the old token back beside the old address.
        let _ = match previous {
            Some(t) => store.set(&acct, &t),
            None => store.delete(&acct),
        };
        return Err(e);
    }
    Ok(())
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Probe {
    pub fingerprint: String,
    /// The fingerprint trusted earlier for this same address, or empty.
    pub previous: String,
    /// True when `previous` is set and differs from what the agent presents now.
    pub changed: bool,
}

pub async fn probe(dir: &Path, host: &str) -> Result<Probe, AgentError> {
    let saved = read_state(dir).map_err(AgentError::Local)?;
    let fingerprint = logged(
        &format!("read the certificate of {host}"),
        capture_fingerprint(host).await,
    )?;
    let previous = saved
        .pins()
        .get(&pin_key(host))
        .cloned()
        .unwrap_or_default();
    let changed = !previous.is_empty() && previous != fingerprint;
    Ok(Probe {
        fingerprint,
        previous,
        changed,
    })
}

/// Loads or creates this computer's device key. Runs before any network traffic, so a missing or
/// locked keystore stops enrollment before the agent sees anything.
async fn device_identity(dir: &Path, store: &Offloaded) -> Result<Identity, AgentError> {
    let dir = dir.to_path_buf();
    store
        .run(move |s| Identity::load_or_create(&dir, s))
        .await
        .map_err(AgentError::Local)
}

/// Stores what an enrollment returned: the pin and device id in `state.json`, the token in the
/// keystore.
async fn remember(
    dir: &Path,
    host: &str,
    fingerprint: &str,
    username: &str,
    ok: LoginOk,
    store: &Offloaded,
) -> Result<Saved, AgentError> {
    let saved = Saved {
        host: host.to_string(),
        fingerprint: normalize_fingerprint(fingerprint),
        token: ok.device_token,
        username: username.to_string(),
        device_id: ok.device_id,
    };
    applog::register_secret(&saved.token);
    let (dir, to_save) = (dir.to_path_buf(), saved.clone());
    store
        .run(move |s| save(&dir, s, &to_save))
        .await
        .map_err(AgentError::Local)?;
    Ok(saved)
}

/// Pins `fingerprint` (the one the user confirmed), logs in, and stores the pin and token.
pub async fn login(
    dir: &Path,
    host: &str,
    fingerprint: &str,
    username: &str,
    password: &str,
    label: &str,
    store: &Offloaded,
) -> Result<Saved, AgentError> {
    applog::register_secret(password);
    let what = format!("sign-in to {host} as {username}");
    let result = async {
        let identity = device_identity(dir, store).await?;
        let client = AgentClient::pinned(host, fingerprint)?;
        let ok = client.login(&identity, username, password, label).await?;
        remember(dir, host, fingerprint, username, ok, store).await
    }
    .await;
    logged(&what, result)
}

/// Pins `fingerprint`, enrolls with a one-time pairing code, and stores the pin and token. The
/// result is an ordinary device (no account name), which the agent lets list and manage only itself.
pub async fn pair(
    dir: &Path,
    host: &str,
    fingerprint: &str,
    code: &str,
    label: &str,
    store: &Offloaded,
) -> Result<Saved, AgentError> {
    let code = code.trim();
    if code.is_empty() {
        return Err(AgentError::Local("enter the pairing code".into()));
    }
    applog::register_secret(code);
    let what = format!("pairing with a code on {host}");
    let result = async {
        let identity = device_identity(dir, store).await?;
        let client = AgentClient::pinned(host, fingerprint)?;
        let ok = client.pair(&identity, code, label).await?;
        remember(dir, host, fingerprint, "", ok, store).await
    }
    .await;
    logged(&what, result)
}

/// The first agent release that has `POST /pair/request`.
pub const MIN_AGENT_FOR_APPROVAL: &str = "agent-v1.0.0";

/// A pairing request waiting for the owner to answer at the PC. `client_nonce` is the secret that
/// authorises polling, so it stays on the Rust side and is not part of what the window sees.
#[derive(Clone)]
pub struct PairWait {
    pub host: String,
    pub fingerprint: String,
    pub request_id: String,
    pub client_nonce: String,
    /// What the owner must see on the PC too, before approving.
    pub match_code: String,
    pub expires_in_seconds: u64,
}

// Not derived: the client nonce is the secret that authorises polling.
impl std::fmt::Debug for PairWait {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PairWait")
            .field("host", &self.host)
            .field("fingerprint", &self.fingerprint)
            .field("request_id", &self.request_id)
            .field("client_nonce", &"<redacted>")
            .field("match_code", &self.match_code)
            .field("expires_in_seconds", &self.expires_in_seconds)
            .finish()
    }
}

/// Asks the agent to have the owner approve this computer at the PC. Nothing is stored until
/// the owner approves; the returned match code is for the user to compare with the PC.
pub async fn request_pairing(
    dir: &Path,
    host: &str,
    fingerprint: &str,
    label: &str,
    store: &Offloaded,
) -> Result<PairWait, AgentError> {
    let what = format!("asking {host} to approve this computer on the PC");
    logged(
        &what,
        request_pairing_inner(dir, host, fingerprint, label, store).await,
    )
}

async fn request_pairing_inner(
    dir: &Path,
    host: &str,
    fingerprint: &str,
    label: &str,
    store: &Offloaded,
) -> Result<PairWait, AgentError> {
    let identity = device_identity(dir, store).await?;
    let client = AgentClient::pinned(host, fingerprint)?;
    let client_nonce = new_client_nonce()?;
    applog::register_secret(&client_nonce);
    let started = client
        .pair_request(&identity, label, &client_nonce)
        .await
        .map_err(|e| match e {
            // An agent without the endpoint answers a bare-text 404, not the JSON error body.
            AgentError::Server { status: 404, code, .. } if code.starts_with("HTTP_") => {
                AgentError::Local(format!(
                    "This agent is too old to approve a new computer from the PC (it needs {MIN_AGENT_FOR_APPROVAL} or newer). \
                     Pair with a code or sign in with an account instead."
                ))
            }
            // An agent from before the commit-then-reveal nonce rejects the request body.
            AgentError::Server { status: 400, code, .. } if code == "BAD_REQUEST" => {
                AgentError::Local(
                    "This agent is too old for this version of the app: update the agent on that computer. \
                     Meanwhile, pair with a code or sign in with an account instead."
                        .into(),
                )
            }
            other => other,
        })?;
    client
        .reveal_pair_request(&started.request_id, &client_nonce)
        .await?;
    let fingerprint = normalize_fingerprint(fingerprint);
    Ok(PairWait {
        match_code: match_code(&fingerprint, &client_nonce, &started.request_id)?,
        host: host.to_string(),
        fingerprint,
        request_id: started.request_id,
        client_nonce,
        expires_in_seconds: started.expires_in_seconds,
    })
}

#[derive(Debug)]
pub enum PairProgress {
    Pending,
    Rejected,
    /// The request is gone: it expired, or its approval was already collected.
    Expired,
    Approved(Saved),
}

/// Checks the request once. On approval the token is stored right away, because the agent hands
/// it out only once: if storing fails the approval is lost and the user must ask again.
pub async fn poll_pairing(
    dir: &Path,
    wait: &PairWait,
    store: &Offloaded,
) -> Result<PairProgress, AgentError> {
    let r = poll_pairing_inner(dir, wait, store).await;
    let host = &wait.host;
    match &r {
        Ok(PairProgress::Pending) => applog::debug(&format!("approval on {host} still pending")),
        Ok(PairProgress::Rejected) => applog::info(&format!("approval on {host} was rejected")),
        Ok(PairProgress::Expired) => applog::info(&format!("approval request on {host} expired")),
        Ok(PairProgress::Approved(_)) => applog::info(&format!("approval on {host}: approved")),
        Err(e) => applog::error(&format!("approval poll on {host} failed: {e}")),
    }
    r
}

async fn poll_pairing_inner(
    dir: &Path,
    wait: &PairWait,
    store: &Offloaded,
) -> Result<PairProgress, AgentError> {
    let client = AgentClient::pinned(&wait.host, &wait.fingerprint)?;
    match client
        .poll_pair_request(&wait.request_id, &wait.client_nonce)
        .await
    {
        Ok(PairPoll::Pending) => Ok(PairProgress::Pending),
        Ok(PairPoll::Rejected) => Ok(PairProgress::Rejected),
        Ok(PairPoll::Approved(ok)) => {
            match remember(dir, &wait.host, &wait.fingerprint, "", ok, store).await {
                Ok(saved) => Ok(PairProgress::Approved(saved)),
                Err(e) => Err(AgentError::Local(format!(
                    "The PC approved this computer, but saving the login failed ({e}). The \
                     approval can be collected only once and is now used up. Ask again."
                ))),
            }
        }
        Err(AgentError::Server { status: 404, .. }) => Ok(PairProgress::Expired),
        Err(e) => Err(e),
    }
}

pub async fn list_devices(dir: &Path, store: &Offloaded) -> Result<Vec<Device>, AgentError> {
    let r = list_devices_inner(dir, store).await;
    match &r {
        Ok(list) => applog::debug(&format!("listed {} devices", list.len())),
        Err(e) => applog::error(&format!("listing devices failed: {e}")),
    }
    r
}

async fn list_devices_inner(dir: &Path, store: &Offloaded) -> Result<Vec<Device>, AgentError> {
    let s = {
        let dir = dir.to_path_buf();
        store
            .run(move |s| load_saved(&dir, s))
            .await
            .map_err(AgentError::Local)?
    };
    if s.token.is_empty() || s.host.is_empty() {
        return Err(AgentError::Local("not signed in".into()));
    }
    // The pin map is the record of what is trusted: a forgotten pin ends the session here.
    let pinned = read_state(dir)
        .map_err(AgentError::Local)?
        .pins()
        .get(&pin_key(&s.host))
        .cloned()
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                s.host
            ))
        })?;
    let listed = AgentClient::pinned(&s.host, &pinned)?
        .devices(&s.token)
        .await;
    if let Err(AgentError::Server { status: 401, .. }) = &listed {
        // The agent refuses this token: it was revoked or removed there. Drop the dead token so
        // the window goes back to sign-in, and keep the pin, the account name and the device key.
        let dir = dir.to_path_buf();
        let token = s.token.clone();
        store
            .run(move |st| drop_refused_token(&dir, st, &token))
            .await
            .map_err(AgentError::Local)?;
    }
    listed
}

/// Drops the login an agent refused (401), but only the login that was refused. The caller sent
/// `token` some time ago; the user may have switched to another host since, or signed in again,
/// and that session must not lose its token to a refusal about another one. A token is valid at
/// one agent only, so a match on the token is a match on the login: the active one is deleted when
/// it is that token, otherwise the parked copy of it (the host was switched away from).
pub fn drop_refused_token(dir: &Path, store: &dyn SecretStore, token: &str) -> Result<(), String> {
    if token.is_empty() {
        return Ok(());
    }
    let _held = crate::hosts::lock();
    if load_active(dir, store)?.token == token {
        return store.delete(&account("token", dir));
    }
    crate::hosts::forget_parked_token(dir, store, token)
}

/// Creates a new identity for this computer on the user's request: the next sign-in enrolls a
/// new device row. Refused while signed in, because the saved login belongs to the old key's
/// device and would be left behind on the agent.
pub fn reset_device_key(dir: &Path, store: &dyn SecretStore) -> Result<(), String> {
    let _held = crate::hosts::lock();
    if !load_saved(dir, store)?.token.is_empty() {
        return Err("Sign out first; the saved login belongs to the current device key.".into());
    }
    // The logins parked for the other saved hosts were made with the key being retired too.
    crate::hosts::forget_all_parked(dir, store)?;
    Identity::reset(dir, store)?;
    applog::info("device key reset");
    Ok(())
}

/// Removes the token from `state.json` first (always possible), then from the keystore, so a
/// locked keystore can never leave a plaintext copy behind.
pub fn sign_out(dir: &Path, store: &dyn SecretStore) -> Result<(), String> {
    let _held = crate::hosts::lock();
    let mut f = read_state(dir)?;
    if !f.token.is_empty() {
        f.token.clear();
        write_state(dir, &f)?;
    }
    store.delete(&account("token", dir))
}

/// What signing out did beyond clearing this computer.
#[derive(Debug, Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SignOut {
    /// The agent no longer accepts the old token.
    pub revoked: bool,
    /// Set when the agent could not be told; says what is still valid and how to fix it.
    pub note: String,
}

/// Signs out: revokes this computer's device on the agent so the old token stops working, then
/// clears it locally ([`sign_out`]). The local part always runs, so an unreachable agent or a
/// changed certificate never leaves the token on this computer; the result says when the agent
/// could not be told.
pub async fn sign_out_and_revoke(dir: &Path, store: &Offloaded) -> Result<SignOut, String> {
    // One snapshot of address, device and token under the saved-hosts lock: a switch to another agent
    // between separate reads would send that agent's login to this one.
    let saved = {
        let owned = dir.to_path_buf();
        match store.run(move |s| load_saved(&owned, s)).await {
            Ok(saved) => saved,
            // A keystore that is locked still lets a pre-keystore file token be revoked.
            Err(_) => {
                let f = read_state(dir)?;
                let token = f.token.clone();
                f.into_saved(token)
            }
        }
    };
    let state = read_state(dir)?;
    let token = saved.token.clone();
    let mut out = SignOut::default();
    if !token.is_empty() && !saved.host.is_empty() && saved.device_id.is_empty() {
        out.note = "Signed out on this computer only. This computer's device is not known, so the \
                    agent was not told and this login still works until it is revoked from the \
                    agent's device list or with `rfe revoke` on the PC."
            .into();
    }
    if !token.is_empty() && !saved.host.is_empty() && !saved.device_id.is_empty() {
        let pinned = state.pins().get(&pin_key(&saved.host)).cloned();
        let revoked = match pinned
            .ok_or_else(|| AgentError::Local("this agent is no longer trusted".into()))
            .and_then(|fp| AgentClient::pinned(&saved.host, &fp))
        {
            Ok(c) => c.revoke_own_device(&token, &saved.device_id).await,
            Err(e) => Err(e),
        };
        match revoked {
            Ok(()) => out.revoked = true,
            // The agent already refuses this token, which is what sign-out is for.
            Err(AgentError::Server { status: 401, .. }) => out.revoked = true,
            Err(e) => {
                out.note = format!(
                    "Signed out on this computer only. The agent could not be told ({e}), so this \
                     computer's login still works until it is revoked from the agent's device list \
                     or with `rfe revoke` on the PC."
                );
            }
        }
    }
    // Clear the login that was revoked, not whichever one is active by now: the user may have
    // switched to another agent during the request, and that login stays.
    let dir = dir.to_path_buf();
    store
        .run(move |s| {
            let _held = crate::hosts::lock();
            let still_active =
                token.is_empty() || load_saved(&dir, s).map_or(true, |l| l.token == token);
            if still_active {
                sign_out(&dir, s)
            } else {
                crate::hosts::forget_parked_token(&dir, s, &token)
            }
        })
        .await?;
    applog::info(&format!(
        "signed out (agent told: {})",
        if out.revoked { "yes" } else { "no" }
    ));
    Ok(out)
}

/// One trusted agent, for the "trusted agents" list.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct PinView {
    pub host: String,
    pub fingerprint: String,
    /// This computer is signed in to this agent now.
    pub active: bool,
}

pub fn list_pins(dir: &Path) -> Result<Vec<PinView>, String> {
    let f = read_state(dir)?;
    let active = (!f.host.is_empty()).then(|| pin_key(&f.host));
    Ok(f.pins()
        .iter()
        .map(|(host, fingerprint)| PinView {
            host: host.clone(),
            fingerprint: fingerprint.clone(),
            active: active.as_deref() == Some(host.as_str()),
        })
        .collect())
}

/// [`list_pins`] with `active` meaning what it says: the saved session for that agent still has a
/// login. After signing out the address stays in `state.json` (the next sign-in starts there), but
/// the list must not call that agent "signed in".
pub fn list_pins_with_login(dir: &Path, store: &dyn SecretStore) -> Result<Vec<PinView>, String> {
    let signed_in = !load_saved(dir, store)?.token.is_empty();
    let mut pins = list_pins(dir)?;
    for p in &mut pins {
        p.active &= signed_in;
    }
    Ok(pins)
}

/// What forgetting a pin did.
#[derive(Debug, Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ForgetPin {
    pub was_pinned: bool,
    /// The pin belonged to the agent this computer was signed in to, so the saved login was
    /// cleared here too. The device stays valid on the agent until it is revoked there.
    pub signed_out: bool,
}

/// Forgets the trust in one address. Connecting to it again shows the fingerprint to compare as
/// for a new agent. If this computer was signed in to it, the saved login is cleared too: the
/// session rested on that trust. That cannot revoke the device, because the agent is no longer
/// trusted enough to talk to.
pub fn forget_pin(dir: &Path, store: &dyn SecretStore, host: &str) -> Result<ForgetPin, String> {
    let _held = crate::hosts::lock();
    let key = pin_key(host);
    // No login parked for this agent outlives the trust in it (saved hosts).
    crate::hosts::forget_parked(dir, store, &key)?;
    let mut f = read_state(dir)?;
    let mut out = ForgetPin {
        was_pinned: f.pins_mut().remove(&key).is_some(),
        signed_out: false,
    };
    if !f.host.is_empty() && pin_key(&f.host) == key {
        out.signed_out = true;
        f.host.clear();
        f.fingerprint.clear();
        f.username.clear();
        f.device_id.clear();
        f.token.clear();
        // The login first: if the keystore refuses, nothing changed and the user can try again,
        // instead of an empty address beside a token that still counts as signed in.
        store.delete(&account("token", dir))?;
        write_state(dir, &f)?;
    } else if out.was_pinned {
        write_state(dir, &f)?;
    }
    applog::info(&format!(
        "forgot the trust in {key} (pinned: {}, signed out: {})",
        out.was_pinned, out.signed_out
    ));
    Ok(out)
}
