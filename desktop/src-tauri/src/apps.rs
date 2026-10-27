//! The host's app catalog: list the apps the agent's computer offers (`GET /apps`) and start one
//! (`POST /apps/{id}/launch`).
//!
//! The window only ever names an app by the opaque catalog id the agent handed out. It sends no
//! command, path, argument or environment, and this app starts no process: the agent looks the id
//! up in its own catalog and hands the entry to the host's native launcher. The id is checked
//! against the agent's pattern here, before it can enter a URL.

use crate::agent_client::{AgentClient, AgentError};
use crate::applog;
use crate::flows;
use crate::secrets::Offloaded;
use serde::{Deserialize, Serialize};
use std::path::Path;
use tauri::Manager;

/// One entry of the PC's catalog, as the window shows it. The agent sends no path or command.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct HostApp {
    pub id: String,
    pub name: String,
    /// The host can start this entry. When false the agent refuses a launch with 409.
    pub launchable: bool,
    pub description: String,
    pub icon: String,
    pub category: String,
}

/// What `GET /apps` returned.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct HostAppCatalog {
    pub platform: String,
    /// This computer holds the LaunchApps grant (the agent re-checks it on every launch).
    pub launch_allowed: bool,
    pub apps: Vec<HostApp>,
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct Started {
    status: String,
}

/// `app_` followed by 64 lowercase hex digits: the only shape the agent hands out and accepts.
pub fn validate_app_id(id: &str) -> Result<(), AgentError> {
    let ok = id.len() == 68
        && id
            .strip_prefix("app_")
            .is_some_and(|h| h.bytes().all(|b| matches!(b, b'0'..=b'9' | b'a'..=b'f')));
    if ok {
        Ok(())
    } else {
        Err(AgentError::Local(format!("unexpected app id {id:?}")))
    }
}

impl AgentClient {
    /// The host's catalog (`GET /apps`). Needs the ViewApps grant, which even an account sign-in
    /// does not imply.
    pub async fn host_apps(&self, token: &str) -> Result<HostAppCatalog, AgentError> {
        self.call_json(reqwest::Method::GET, "/apps", token).await
    }

    /// Asks the agent to start one catalog entry (`POST /apps/{id}/launch`). A success means the
    /// host's launcher accepted the request, not that the app finished starting.
    pub async fn launch_host_app(&self, token: &str, id: &str) -> Result<(), AgentError> {
        validate_app_id(id)?;
        let started: Started = self
            .call_json(reqwest::Method::POST, &format!("/apps/{id}/launch"), token)
            .await?;
        if started.status == "started" {
            Ok(())
        } else {
            Err(AgentError::Local(format!(
                "the agent answered with an unknown launch status {:?}",
                started.status
            )))
        }
    }
}

/// Every error code the two app endpoints return, each with its own wording and what to do next.
/// The guide has a row for each, with exactly this text (`tests/apps.rs` checks).
pub const APP_MESSAGES: &[(&str, &str)] = &[
    (
        "APP_VIEW_FORBIDDEN",
        "This computer is not allowed to see the PC's apps. On the PC's agent, turn on app viewing for this computer, then refresh.",
    ),
    (
        "APP_LAUNCH_FORBIDDEN",
        "This computer may see the PC's apps but not start them. On the PC's agent, turn on app launching for this computer.",
    ),
    (
        "APP_NOT_FOUND",
        "That app is no longer in the PC's catalog. It may have been uninstalled. Refresh the list.",
    ),
    (
        "APP_NOT_LAUNCHABLE",
        "The PC has no way to start that app. It is listed but cannot be launched from here.",
    ),
    (
        "APP_LAUNCH_BUSY",
        "The PC is already starting another app. Wait a moment, then try again.",
    ),
    (
        "NO_INTERACTIVE_SESSION",
        "Nobody is signed in to a graphical desktop on the host, so there is nowhere to open the app. Sign in at the PC, then try again.",
    ),
    (
        "APP_LAUNCH_UNAVAILABLE",
        "The PC's app launcher is not available (on Linux it needs the gio tool). Install it on the PC, then try again.",
    ),
    (
        "APP_LAUNCH_FAILED",
        "The PC tried to start the app and could not. Check that it still works on the PC.",
    ),
    (
        "APP_CATALOG_RATE_LIMITED",
        "Too many requests for the app list. Wait a minute, then refresh.",
    ),
    (
        "APP_LAUNCH_RATE_LIMITED",
        "Too many app launches. The agent allows only a few per minute. Wait a minute, then try again.",
    ),
    (
        "APP_CATALOG_UNSUPPORTED",
        "The PC's operating system does not support the app catalog yet.",
    ),
    (
        "BAD_APP_ID",
        "The agent refused the app id as malformed. This is a bug in the app.",
    ),
];

/// Shown when the agent has no `/apps` route (an agent older than the app catalog answers with a
/// bare 404 that has no JSON body).
pub const NO_CATALOG_MESSAGE: &str =
    "This agent has no app catalog. Update the agent on the PC, then try again.";

/// The message for an error from either app endpoint. Codes other than the ones above (a lost
/// login, a certificate problem, the agent being unreachable) keep the wording the rest of the
/// app gives them.
pub fn describe(e: &AgentError) -> String {
    if let AgentError::Server { status, code, .. } = e {
        if let Some((_, text)) = APP_MESSAGES.iter().find(|(c, _)| c == code) {
            return (*text).to_string();
        }
        if *status == 404 && code == "HTTP_404" {
            return NO_CATALOG_MESSAGE.to_string();
        }
    }
    e.to_string()
}

/// The saved login and the pinned client for it. A forgotten pin ends the session, as in
/// `flows::list_devices`.
async fn session(dir: &Path, store: &Offloaded) -> Result<(AgentClient, String), AgentError> {
    let s = {
        let dir = dir.to_path_buf();
        store
            .run(move |s| flows::load_saved(&dir, s))
            .await
            .map_err(AgentError::Local)?
    };
    if s.token.is_empty() || s.host.is_empty() {
        return Err(AgentError::Local("not signed in".into()));
    }
    let key = flows::pin_key(&s.host);
    let pinned = flows::list_pins(dir)
        .map_err(AgentError::Local)?
        .into_iter()
        .find(|p| p.host == key)
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                s.host
            ))
        })?;
    Ok((AgentClient::pinned(&s.host, &pinned)?, s.token))
}

/// A 401 means the agent no longer accepts this token: drop it so the window goes back to sign-in
/// the next time it checks, keeping the pin, the account name and the device key.
async fn drop_dead_token<T>(
    dir: &Path,
    store: &Offloaded,
    token: &str,
    r: &Result<T, AgentError>,
) -> Result<(), AgentError> {
    if let Err(AgentError::Server { status: 401, .. }) = r {
        let dir = dir.to_path_buf();
        let token = token.to_string();
        store
            .run(move |st| flows::drop_refused_token(&dir, st, &token))
            .await
            .map_err(AgentError::Local)?;
    }
    Ok(())
}

pub async fn list(dir: &Path, store: &Offloaded) -> Result<HostAppCatalog, AgentError> {
    let (client, token) = session(dir, store).await?;
    let r = client.host_apps(&token).await;
    drop_dead_token(dir, store, &token, &r).await?;
    match &r {
        Ok(c) => applog::debug(&format!("listed {} host apps", c.apps.len())),
        Err(e) => applog::error(&format!("listing host apps failed: {e}")),
    }
    r
}

/// Starts one catalog entry. The id is validated before anything is sent.
pub async fn launch(dir: &Path, store: &Offloaded, id: &str) -> Result<(), AgentError> {
    validate_app_id(id)?;
    let (client, token) = session(dir, store).await?;
    let r = client.launch_host_app(&token, id).await;
    drop_dead_token(dir, store, &token, &r).await?;
    match &r {
        Ok(()) => applog::info(&format!("asked the host to start {id}")),
        Err(e) => applog::error(&format!("starting {id} failed: {e}")),
    }
    r
}

// The commands the window calls. Neither takes a URL, a path or a command: `list_host_apps` takes
// nothing and `launch_host_app` takes a catalog id, which is validated before it is used. Errors
// come back as the sentence the window shows.
fn app_dir(app: &tauri::AppHandle) -> Result<std::path::PathBuf, String> {
    app.path().app_data_dir().map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn list_host_apps(
    app: tauri::AppHandle,
    host: Option<String>,
) -> Result<HostAppCatalog, String> {
    let dir = app_dir(&app)?;
    list(&dir, &Offloaded::os_keystore().scoped(host))
        .await
        .map_err(|e| describe(&e))
}

#[tauri::command]
pub async fn launch_host_app(
    app: tauri::AppHandle,
    host: Option<String>,
    id: String,
) -> Result<(), String> {
    let dir = app_dir(&app)?;
    launch(&dir, &Offloaded::os_keystore().scoped(host), &id)
        .await
        .map_err(|e| describe(&e))
}
