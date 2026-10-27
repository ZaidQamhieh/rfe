//! RFE desktop app, first slice: pin the agent's certificate, log in, list devices.
//! The device key and login token are kept in the OS keystore.

pub mod agent_client;
pub mod applog;
pub mod apps; // feature:app-catalog
pub mod audit; // feature:audit-logs
pub mod desktop;
pub mod device_actions;
pub mod discovery;
pub mod fileops; // feature:file-ops
pub mod files; // feature:file-browser
pub mod flows;
mod fsutil;
pub mod health;
pub mod hosts;
pub mod identity;
pub mod local; // feature:local-files
pub mod native;
pub mod pair_inbox; // feature:pair-inbox
pub mod pairing_codes; // feature:pairing-codes
pub mod secrets;
pub mod updates;
// ---- feature:transfers ----
pub mod transfers;

use agent_client::Device;
use flows::Saved;
use secrets::Offloaded;
use serde::Serialize;
use std::sync::Mutex;
use tauri::Manager;

fn keystore() -> Offloaded {
    Offloaded::os_keystore()
}

fn data_dir(app: &tauri::AppHandle) -> Result<std::path::PathBuf, String> {
    app.path().app_data_dir().map_err(|e| e.to_string())
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct SavedView {
    host: String,
    fingerprint: String,
    signed_in: bool,
    username: String,
    /// The agent's id for this computer; shown in Settings, not a secret.
    device_id: String,
}

impl From<Saved> for SavedView {
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

// Every command that touches the keystore is async and goes through `Offloaded`, so an open
// unlock prompt never blocks the main thread or a runtime worker.
#[tauri::command]
async fn saved_agent(app: tauri::AppHandle) -> Result<SavedView, String> {
    let dir = data_dir(&app)?;
    let saved = keystore().run(move |s| flows::load_saved(&dir, s)).await?;
    Ok(saved.into())
}

#[tauri::command]
async fn list_pins(app: tauri::AppHandle) -> Result<Vec<flows::PinView>, String> {
    let dir = data_dir(&app)?;
    keystore()
        .run(move |s| flows::list_pins_with_login(&dir, s))
        .await
}

#[tauri::command]
async fn forget_pin(app: tauri::AppHandle, host: String) -> Result<flows::ForgetPin, String> {
    let dir = data_dir(&app)?;
    keystore()
        .run(move |s| flows::forget_pin(&dir, s, &host))
        .await
}

#[tauri::command]
async fn probe_agent(app: tauri::AppHandle, host: String) -> Result<flows::Probe, String> {
    flows::probe(&data_dir(&app)?, &host)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
async fn login(
    app: tauri::AppHandle,
    host: String,
    fingerprint: String,
    username: String,
    password: String,
) -> Result<SavedView, String> {
    let dir = data_dir(&app)?;
    flows::login(
        &dir,
        &host,
        &fingerprint,
        &username,
        &password,
        "RFE Desktop",
        &keystore(),
    )
    .await
    .map(Into::into)
    .map_err(|e| e.to_string())
}

#[tauri::command]
async fn pair_with_code(
    app: tauri::AppHandle,
    host: String,
    fingerprint: String,
    code: String,
) -> Result<SavedView, String> {
    flows::pair(
        &data_dir(&app)?,
        &host,
        &fingerprint,
        &code,
        "RFE Desktop",
        &keystore(),
    )
    .await
    .map(Into::into)
    .map_err(|e| e.to_string())
}

/// The pairing request this window is waiting on. Only one at a time: asking again replaces it.
#[derive(Default)]
struct PendingPair(Mutex<Option<flows::PairWait>>);

impl PendingPair {
    /// Clears the slot only if it still holds `request_id`: a poll that finishes after the window asked
    /// for a newer request must not discard that one (audit F-141).
    fn take_if(&self, request_id: &str) {
        let mut slot = self.0.lock().unwrap();
        if slot.as_ref().is_some_and(|w| w.request_id == request_id) {
            slot.take();
        }
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct PairWaitView {
    match_code: String,
    expires_in_seconds: u64,
}

#[tauri::command]
async fn request_pairing(
    app: tauri::AppHandle,
    pending: tauri::State<'_, PendingPair>,
    host: String,
    fingerprint: String,
) -> Result<PairWaitView, String> {
    let wait = flows::request_pairing(
        &data_dir(&app)?,
        &host,
        &fingerprint,
        "RFE Desktop",
        &keystore(),
    )
    .await
    .map_err(|e| e.to_string())?;
    let view = PairWaitView {
        match_code: wait.match_code.clone(),
        expires_in_seconds: wait.expires_in_seconds,
    };
    *pending.0.lock().unwrap() = Some(wait);
    Ok(view)
}

#[derive(Serialize)]
struct PollView {
    /// "pending", "rejected", "expired" or "approved".
    status: &'static str,
    saved: Option<SavedView>,
}

#[tauri::command]
async fn poll_pairing(
    app: tauri::AppHandle,
    pending: tauri::State<'_, PendingPair>,
) -> Result<PollView, String> {
    let wait = pending
        .0
        .lock()
        .unwrap()
        .clone()
        .ok_or_else(|| "no pairing request is waiting".to_string())?;
    let progress = flows::poll_pairing(&data_dir(&app)?, &wait, &keystore()).await;
    let view = |status, saved| PollView { status, saved };
    match progress {
        Ok(flows::PairProgress::Pending) => Ok(view("pending", None)),
        Ok(flows::PairProgress::Rejected) => {
            pending.take_if(&wait.request_id);
            Ok(view("rejected", None))
        }
        Ok(flows::PairProgress::Expired) => {
            pending.take_if(&wait.request_id);
            Ok(view("expired", None))
        }
        Ok(flows::PairProgress::Approved(saved)) => {
            pending.take_if(&wait.request_id);
            Ok(view("approved", Some(saved.into())))
        }
        Err(e) => {
            // An error after the agent handed out the approval cannot be retried: the request
            // is spent. Keep waiting only for errors that left it untouched.
            if matches!(e, agent_client::AgentError::Local(_)) {
                pending.take_if(&wait.request_id);
            }
            Err(e.to_string())
        }
    }
}

#[tauri::command]
async fn reset_device_key(app: tauri::AppHandle) -> Result<(), String> {
    let dir = data_dir(&app)?;
    keystore()
        .run(move |s| flows::reset_device_key(&dir, s))
        .await
}

#[tauri::command]
fn cancel_pairing(pending: tauri::State<'_, PendingPair>) {
    pending.0.lock().unwrap().take();
}

#[tauri::command]
async fn list_devices(app: tauri::AppHandle, host: Option<String>) -> Result<Vec<Device>, String> {
    flows::list_devices(&data_dir(&app)?, &keystore().scoped(host))
        .await
        .map_err(|e| e.to_string())
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct SettingsView {
    log_level: &'static str,
    app_version: &'static str,
    client_version: &'static str,
    min_agent_for_approval: &'static str,
    data_dir: String,
    platform: &'static str,
}

#[tauri::command]
fn app_settings(app: tauri::AppHandle) -> Result<SettingsView, String> {
    let dir = data_dir(&app)?;
    Ok(SettingsView {
        log_level: flows::log_level(&dir)?.as_str(),
        app_version: env!("CARGO_PKG_VERSION"),
        client_version: agent_client::CLIENT_VERSION,
        min_agent_for_approval: flows::MIN_AGENT_FOR_APPROVAL,
        data_dir: dir.display().to_string(),
        platform: std::env::consts::OS,
    })
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct FoundView {
    #[serde(flatten)]
    found: discovery::Found,
    /// This app already trusts a certificate for that address.
    known: bool,
}

/// Looks for agents on the local network for a few seconds. The result is a list of addresses to
/// try, nothing more: no credential is sent and no agent is trusted by being listed.
#[tauri::command]
async fn discover_agents(app: tauri::AppHandle) -> Result<Vec<FoundView>, String> {
    let dir = data_dir(&app)?;
    let list = flows::discover(&dir, std::time::Duration::from_secs(3)).await?;
    Ok(list
        .into_iter()
        .map(|(found, known)| FoundView { found, known })
        .collect())
}

#[tauri::command]
fn diagnostics(app: tauri::AppHandle) -> Result<String, String> {
    Ok(flows::diagnostics(&data_dir(&app)?))
}

#[tauri::command]
fn set_log_level(app: tauri::AppHandle, level: String) -> Result<&'static str, String> {
    flows::set_log_level(&data_dir(&app)?, &level).map(|l| l.as_str())
}

#[tauri::command]
async fn check_keystore(app: tauri::AppHandle) -> Result<(), String> {
    let dir = data_dir(&app)?;
    keystore()
        .run(move |s| flows::check_keystore(&dir, s))
        .await
}

#[tauri::command]
async fn sign_out(app: tauri::AppHandle) -> Result<flows::SignOut, String> {
    flows::sign_out_and_revoke(&data_dir(&app)?, &keystore()).await
}

// ---- feature:health-metrics ----
/// One refresh of the health and metrics screen for the saved session. Takes no argument: the
/// address and certificate are the ones already trusted, and the three routes are fixed.
#[tauri::command]
async fn agent_health(
    app: tauri::AppHandle,
    host: Option<String>,
) -> Result<health::Snapshot, String> {
    health::agent_health(&data_dir(&app)?, &keystore().scoped(host))
        .await
        .map_err(|e| e.to_string())
}

// ---- feature:pairing-codes ----
/// Mints a one-time pairing code for a phone. Admin sessions only; any other session gets
/// `status: "forbidden"`. The code is never logged or saved.
#[tauri::command]
async fn generate_pairing_code(
    app: tauri::AppHandle,
    host: Option<String>,
    ttl_seconds: Option<u64>,
) -> Result<pairing_codes::CodeView, String> {
    pairing_codes::generate_for(
        &data_dir(&app)?,
        &keystore().scoped(host),
        ttl_seconds.unwrap_or(pairing_codes::TTL_SECONDS),
    )
    .await
    .map_err(|e| e.to_string())
}

// ---- feature:audit-logs ----
/// One page of the agent's audit trail (admin only). `before` is the cursor from the previous page.
#[tauri::command]
async fn audit_page(
    app: tauri::AppHandle,
    host: Option<String>,
    before: Option<i64>,
) -> Result<audit::AuditReply, String> {
    audit::fetch_audit(
        &data_dir(&app)?,
        &keystore().scoped(host),
        before,
        audit::AUDIT_PAGE,
    )
    .await
    .map_err(|e| e.to_string())
}

/// The tail of the agent's log (admin only).
#[tauri::command]
async fn agent_log(
    app: tauri::AppHandle,
    host: Option<String>,
) -> Result<audit::LogsReply, String> {
    audit::fetch_logs(&data_dir(&app)?, &keystore().scoped(host))
        .await
        .map_err(|e| e.to_string())
}

/// What to tell a user who starts the app with no graphical session (an SSH login, a console):
/// GTK would otherwise abort with a panic that names no cause they can act on.
pub fn no_display_message(var: impl Fn(&str) -> Option<String>) -> Option<&'static str> {
    let set = |k: &str| var(k).is_some_and(|v| !v.is_empty());
    if set("DISPLAY") || set("WAYLAND_DISPLAY") {
        None
    } else {
        Some(
            "RFE Desktop needs a graphical session, and neither DISPLAY nor WAYLAND_DISPLAY is set. \
             Start it from your desktop's application menu, not from a text-only login.",
        )
    }
}

pub fn run() {
    #[cfg(target_os = "linux")]
    if let Some(msg) = no_display_message(|k| std::env::var(k).ok()) {
        eprintln!("{msg}");
        std::process::exit(1);
    }
    tauri::Builder::default()
        // First, as the plugin requires: a second launch hands over to the running app, which
        // brings its window forward, and the second process exits.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            applog::info("a second launch was handed over to this window");
            desktop::show_for_second_start(app);
        }))
        // Remembers the window's size and position between runs (kept in the app's config folder;
        // it holds nothing but geometry).
        .plugin(tauri_plugin_window_state::Builder::default().build())
        .plugin(tauri_plugin_dialog::init())
        // Notifications are shown only by `desktop_notify`: the page has no permission on this plugin.
        .plugin(tauri_plugin_notification::init())
        .manage(desktop::Desktop::default())
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                desktop::on_close_requested(window, api);
            }
        })
        .manage(PendingPair::default())
        // ---- feature:transfers ----
        .manage(transfers::Transfers::default())
        .setup(|app| {
            if let Ok(dir) = app.path().app_data_dir() {
                flows::apply_log_level(&dir);
            }
            applog::info(&format!("started {}", agent_client::CLIENT_VERSION));
            desktop::setup_tray(app.handle());
            desktop::start_hidden_if_asked(app.handle());
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            // feature:file-browser
            files::files_roots,
            files::files_list,
            files::files_meta,
            files::files_create_folder,
            files::files_rename,
            files::files_trash,
            // feature:file-ops
            fileops::files_search,
            fileops::files_recent,
            fileops::files_copy,
            fileops::files_move,
            fileops::files_create_file,
            fileops::files_compress,
            fileops::files_extract,
            fileops::files_archive_list,
            fileops::files_checksum,
            fileops::files_checksums,
            fileops::files_chmod,
            fileops::share_mint,
            fileops::share_links,
            fileops::share_revoke_link,
            fileops::trash_items,
            fileops::trash_restore_items,
            fileops::trash_empty_items,
            fileops::files_read_text,
            fileops::files_write_text,
            fileops::files_thumb,
            fileops::wake_computer,
            saved_agent,
            list_pins,
            forget_pin,
            probe_agent,
            login,
            pair_with_code,
            request_pairing,
            poll_pairing,
            cancel_pairing,
            reset_device_key,
            list_devices,
            sign_out,
            app_settings,
            discover_agents,
            diagnostics,
            set_log_level,
            check_keystore,
            apps::list_host_apps,  // feature:app-catalog
            apps::launch_host_app, // feature:app-catalog
            agent_health,
            generate_pairing_code, // feature:pairing-codes
            audit_page,            // feature:audit-logs
            agent_log,
            // feature:device-actions
            device_actions::commands::device_access,
            device_actions::commands::device_access_all,
            device_actions::commands::set_device_access,
            device_actions::commands::revoke_device,
            device_actions::commands::remove_device,
            // feature:pair-inbox
            pair_inbox::list_pair_requests,
            pair_inbox::answer_pair_request,
            // feature:multi-hosts
            hosts::list_hosts,
            hosts::switch_host,
            hosts::rename_host,
            hosts::remove_host,
            // ---- feature:transfers ----
            transfers::transfer_download,
            transfers::transfer_upload,
            transfers::transfer_upload_tree,
            transfers::transfer_download_tree,
            local::local_places,
            local::local_list,
            local::local_search,
            local::local_create_folder,
            local::local_rename,
            local::local_delete,
            local::local_read_text,
            local::local_read_image,
            local::local_open,
            local::local_save_text,
            native::pick_files,
            native::pick_folder,
            transfers::transfer_list,
            transfers::transfer_cancel,
            transfers::transfer_pause,
            transfers::transfer_retry,
            transfers::transfer_set_prefs,
            transfers::transfer_resolve,
            transfers::transfer_clear_finished,
            transfers::transfer_folder,
            desktop::desktop_set_prefs,
            desktop::desktop_has_tray,
            desktop::desktop_set_scale,
            desktop::desktop_notify,
            desktop::desktop_autostart_state,
            desktop::desktop_set_autostart,
            updates::update_check,
            updates::update_download,
            transfers::choose_download_folder,
            transfers::reset_download_folder
        ])
        .run(tauri::generate_context!())
        .expect("error while running the RFE desktop app");
}

#[cfg(test)]
mod display_tests {
    use super::no_display_message;

    fn env(pairs: &'static [(&'static str, &'static str)]) -> impl Fn(&str) -> Option<String> {
        move |k| {
            pairs
                .iter()
                .find(|(n, _)| *n == k)
                .map(|(_, v)| v.to_string())
        }
    }

    #[test]
    fn a_session_with_x11_or_wayland_starts_and_one_with_neither_says_why() {
        assert!(no_display_message(env(&[("DISPLAY", ":0")])).is_none());
        assert!(no_display_message(env(&[("WAYLAND_DISPLAY", "wayland-0")])).is_none());
        assert!(no_display_message(env(&[]))
            .unwrap()
            .contains("graphical session"));
        assert!(
            no_display_message(env(&[("DISPLAY", ""), ("WAYLAND_DISPLAY", "")])).is_some(),
            "empty values are not a display"
        );
    }
}

#[cfg(test)]
mod pending_pair_tests {
    use super::*;

    fn wait(id: &str) -> flows::PairWait {
        flows::PairWait {
            host: "h".into(),
            fingerprint: "f".into(),
            request_id: id.into(),
            client_nonce: "n".into(),
            match_code: "123456".into(),
            expires_in_seconds: 60,
        }
    }

    #[test]
    fn a_finished_poll_does_not_clear_a_newer_request() {
        let p = PendingPair::default();
        *p.0.lock().unwrap() = Some(wait("new"));
        p.take_if("old");
        assert!(p.0.lock().unwrap().is_some(), "the newer request survives");
        p.take_if("new");
        assert!(p.0.lock().unwrap().is_none());
    }
}
