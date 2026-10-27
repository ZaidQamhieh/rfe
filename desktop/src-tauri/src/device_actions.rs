//! Admin device actions: revoke or remove a device, and change what it may do (per-device
//! permission grants, read-only, folder limit, app permissions). All of it is `DELETE
//! /devices/{id}` and `PATCH /devices/{id}` on the pinned connection; the agent decides who may
//! ("admin" means a session that signed in with the account) and answers 403 to anyone else.
//!
//! The agent has no call that renames a device, so this app offers no rename: a device's name is
//! whatever it gave when it signed in.
//!
//! Acting on this computer's own device (revoking or removing it signs this window out) needs the
//! caller to say it knows: `confirm_self`. The window asks twice and shows a stronger warning;
//! the core refuses without the flag so a script or a bug cannot do it by accident.

use crate::agent_client::{AgentClient, AgentError};
use crate::applog;
use crate::flows;
use crate::secrets::Offloaded;
use reqwest::Method;
use serde::{Deserialize, Serialize};
use std::path::Path;

/// What the window shows when the agent refuses a device action with 403.
pub const NOT_ADMIN: &str =
    "This login is not an admin session, so the agent will not change other devices. Sign in \
     with the account (not a pairing code or an approval on the PC) to revoke, remove or change \
     access.";

/// 404 NOT_FOUND on a device action.
pub const GONE: &str =
    "That device is no longer on the agent (it may already have been removed). The list has been \
     refreshed.";

/// The agent has no such route (an agent older than the device controls).
pub const TOO_OLD: &str =
    "This agent does not support that device action. Update the agent, or use rfe on the PC.";

/// The caller did not confirm an action on this computer's own device.
pub const SELF_UNCONFIRMED: &str =
    "This is the computer you are using. Revoking or removing it signs you out here, and \
     changing its own access can lock it out. Confirm that you mean it.";

/// The saved login has no device id and the agent did not say which device this computer is.
pub const UNKNOWN_SELF: &str =
    "Could not tell which device is this computer, so nothing was changed. Refresh the list and try again.";

/// Turning on launching apps without viewing them (the agent refuses it too).
pub const LAUNCH_NEEDS_VIEW: &str =
    "Allowing a device to launch apps needs it to be allowed to view apps as well.";

/// A save with no setting changed.
pub const NOTHING_CHANGED: &str = "No setting was changed, so there is nothing to save.";

/// The jail path is empty (no limit) or text the agent can read as a path.
pub const BAD_JAIL: &str = "The folder limit must be one path, up to 4096 characters, or empty.";

/// One device with everything the agent says about its access. Only an admin session gets these
/// fields for other devices; an ordinary one sees just itself, without them.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase", default)]
pub struct DeviceAccess {
    pub id: String,
    pub label: String,
    pub revoked: bool,
    pub current: bool,
    pub via_login: bool,
    pub jail_root: String,
    pub read_only: bool,
    pub view_apps: bool,
    pub launch_apps: bool,
    pub browse: bool,
    pub download: bool,
    pub upload: bool,
    pub modify: bool,
    pub delete: bool,
    pub share: bool,
}

/// The settings to change: only what is present is sent, so an unchanged folder limit is not
/// re-checked by the agent (it may have gone stale since it was set) and nothing is reset by
/// accident.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase", default)]
pub struct AccessPatch {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub jail_root: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub read_only: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub view_apps: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub launch_apps: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub browse: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub download: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub upload: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub modify: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub delete: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub share: Option<bool>,
}

impl AccessPatch {
    fn is_empty(&self) -> bool {
        *self == AccessPatch::default()
    }
}

/// What a revoke or remove did.
#[derive(Debug, Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Done {
    /// The action was on this computer's own device: the saved login here was cleared too.
    pub signed_out: bool,
}

/// The saved login, ready to call the agent. Not `Debug`-derived: it holds the token.
struct Session {
    client: AgentClient,
    token: String,
    device_id: String,
}

/// A device id is what the agent made (letters, digits, `-`, `_`); anything else never enters a URL.
fn check_id(id: &str) -> Result<(), AgentError> {
    let ok = !id.is_empty()
        && id.len() <= 128
        && id
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_'));
    if ok {
        Ok(())
    } else {
        Err(AgentError::Local(format!("unexpected device id {id:?}")))
    }
}

async fn session(dir: &Path, store: &Offloaded) -> Result<Session, AgentError> {
    let saved = {
        let dir = dir.to_path_buf();
        store
            .run(move |s| flows::load_saved(&dir, s))
            .await
            .map_err(AgentError::Local)?
    };
    if saved.token.is_empty() || saved.host.is_empty() {
        return Err(AgentError::Local("not signed in".into()));
    }
    let key = flows::pin_key(&saved.host);
    let pinned = flows::list_pins(dir)
        .map_err(AgentError::Local)?
        .into_iter()
        .find(|p| p.host == key)
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                saved.host
            ))
        })?;
    Ok(Session {
        client: AgentClient::pinned(&saved.host, &pinned)?,
        token: saved.token,
        device_id: saved.device_id,
    })
}

fn logged<T>(what: &str, r: Result<T, AgentError>) -> Result<T, AgentError> {
    match &r {
        Ok(_) => applog::info(&format!("{what}: ok")),
        Err(e) => applog::error(&format!("{what} failed: {e}")),
    }
    r
}

/// This computer's own device id. The saved one is used when there is one; a login saved without
/// it (an older state file) asks the agent which listed device is the current one. When that cannot
/// be answered the action is refused: guessing "not mine" could revoke the device this window runs on.
async fn own_device_id(s: &Session) -> Result<String, AgentError> {
    if !s.device_id.is_empty() {
        return Ok(s.device_id.clone());
    }
    let list = s
        .client
        .call_device_json(Method::GET, "/devices", &s.token, None)
        .await?;
    let all: Vec<DeviceAccess> = serde_json::from_value(list)
        .map_err(|e| AgentError::Local(format!("unexpected response: {e}")))?;
    all.into_iter()
        .find(|d| d.current && !d.id.is_empty())
        .map(|d| d.id)
        .ok_or_else(|| AgentError::Local(UNKNOWN_SELF.into()))
}

/// Refuses an action on this computer's own device unless the caller confirmed it.
async fn guard_self(s: &Session, id: &str, confirm_self: bool) -> Result<bool, AgentError> {
    let is_self = own_device_id(s).await? == id;
    if is_self && !confirm_self {
        return Err(AgentError::Local(SELF_UNCONFIRMED.into()));
    }
    Ok(is_self)
}

fn parse_access(v: serde_json::Value) -> Result<DeviceAccess, AgentError> {
    serde_json::from_value(v).map_err(|e| AgentError::Local(format!("unexpected response: {e}")))
}

/// The current access of one device, read fresh from the agent (an admin session lists every
/// device; there is no call for a single one).
pub async fn device_access(
    dir: &Path,
    store: &Offloaded,
    id: &str,
) -> Result<DeviceAccess, AgentError> {
    check_id(id)?;
    let r = async {
        let s = session(dir, store).await?;
        let list = s
            .client
            .call_device_json(Method::GET, "/devices", &s.token, None)
            .await?;
        let all: Vec<DeviceAccess> = serde_json::from_value(list)
            .map_err(|e| AgentError::Local(format!("unexpected response: {e}")))?;
        all.into_iter()
            .find(|d| d.id == id)
            .ok_or_else(|| AgentError::Server {
                status: 404,
                code: "NOT_FOUND".into(),
                message: "no such device".into(),
            })
    }
    .await;
    logged(&format!("reading the access of device {id}"), r)
}

/// Every device with its access, in one read. An ordinary (non-admin) session gets only itself.
pub async fn device_access_all(
    dir: &Path,
    store: &Offloaded,
) -> Result<Vec<DeviceAccess>, AgentError> {
    let r = async {
        let s = session(dir, store).await?;
        let list = s
            .client
            .call_device_json(Method::GET, "/devices", &s.token, None)
            .await?;
        parse_access_list(list)
    }
    .await;
    logged("reading the access of all devices", r)
}

fn parse_access_list(v: serde_json::Value) -> Result<Vec<DeviceAccess>, AgentError> {
    serde_json::from_value(v).map_err(|e| AgentError::Local(format!("unexpected response: {e}")))
}

/// Changes a device's access (`PATCH /devices/{id}`) and returns the device as the agent now has it.
pub async fn set_access(
    dir: &Path,
    store: &Offloaded,
    id: &str,
    patch: &AccessPatch,
    confirm_self: bool,
) -> Result<DeviceAccess, AgentError> {
    check_id(id)?;
    if patch.is_empty() {
        return Err(AgentError::Local(NOTHING_CHANGED.into()));
    }
    if patch.launch_apps == Some(true) && patch.view_apps == Some(false) {
        return Err(AgentError::Local(LAUNCH_NEEDS_VIEW.into()));
    }
    if let Some(j) = &patch.jail_root {
        if j.len() > 4096 || j.chars().any(char::is_control) {
            return Err(AgentError::Local(BAD_JAIL.into()));
        }
    }
    let r = async {
        let s = session(dir, store).await?;
        guard_self(&s, id, confirm_self).await?;
        let body = serde_json::to_value(patch)
            .map_err(|e| AgentError::Local(format!("unexpected request: {e}")))?;
        let v = s
            .client
            .call_device_json(
                Method::PATCH,
                &format!("/devices/{id}"),
                &s.token,
                Some(&body),
            )
            .await?;
        parse_access(v)
    }
    .await;
    logged(&format!("changing the access of device {id}"), r)
}

async fn delete(
    dir: &Path,
    store: &Offloaded,
    id: &str,
    purge: bool,
    confirm_self: bool,
) -> Result<Done, AgentError> {
    check_id(id)?;
    let s = session(dir, store).await?;
    let is_self = guard_self(&s, id, confirm_self).await?;
    let path = if purge {
        format!("/devices/{id}?purge=true")
    } else {
        format!("/devices/{id}")
    };
    s.client
        .call_device_json(Method::DELETE, &path, &s.token, None)
        .await?;
    if is_self {
        // The agent now refuses this token; drop it here too, as signing out does. Only that token:
        // the user may have switched to another host while the call ran.
        let dir = dir.to_path_buf();
        let token = s.token.clone();
        store
            .run(move |st| flows::drop_refused_token(&dir, st, &token))
            .await
            .map_err(AgentError::Local)?;
    }
    Ok(Done {
        signed_out: is_self,
    })
}

/// Blocks a device: the agent rejects its token from now on and the row stays, marked revoked.
pub async fn revoke(
    dir: &Path,
    store: &Offloaded,
    id: &str,
    confirm_self: bool,
) -> Result<Done, AgentError> {
    logged(
        &format!("revoking device {id}"),
        delete(dir, store, id, false, confirm_self).await,
    )
}

/// Deletes a device's row for good (`DELETE /devices/{id}?purge=true`). A device that was still
/// active is blocked at the same time, because its token is gone with the row.
pub async fn remove(
    dir: &Path,
    store: &Offloaded,
    id: &str,
    confirm_self: bool,
) -> Result<Done, AgentError> {
    logged(
        &format!("removing device {id}"),
        delete(dir, store, id, true, confirm_self).await,
    )
}

/// The text the window shows for a failed device action. 403 and 404 each get their own state;
/// everything else is the agent client's usual wording.
pub fn explain(e: &AgentError) -> String {
    match e {
        AgentError::Server { status: 403, .. } => NOT_ADMIN.to_string(),
        AgentError::Server {
            status: 404, code, ..
        } if code == "NOT_FOUND" => GONE.to_string(),
        AgentError::Server {
            status: 404 | 405,
            code,
            ..
        } if code.starts_with("HTTP_") => TOO_OLD.to_string(),
        AgentError::Server {
            status: 400,
            message,
            ..
        } => format!("The agent refused that change: {message}"),
        other => other.to_string(),
    }
}

// The Tauri commands: thin wrappers so the tests drive the same functions the window does.
pub mod commands {
    use super::*;

    fn dir(app: &tauri::AppHandle) -> Result<std::path::PathBuf, String> {
        use tauri::Manager;
        app.path().app_data_dir().map_err(|e| e.to_string())
    }

    fn keystore() -> Offloaded {
        Offloaded::os_keystore()
    }

    #[tauri::command]
    pub async fn device_access(
        app: tauri::AppHandle,
        host: Option<String>,
        id: String,
    ) -> Result<DeviceAccess, String> {
        super::device_access(&dir(&app)?, &keystore().scoped(host), &id)
            .await
            .map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn device_access_all(
        app: tauri::AppHandle,
        host: Option<String>,
    ) -> Result<Vec<DeviceAccess>, String> {
        super::device_access_all(&dir(&app)?, &keystore().scoped(host))
            .await
            .map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn set_device_access(
        app: tauri::AppHandle,
        host: Option<String>,
        id: String,
        patch: AccessPatch,
        confirm_self: bool,
    ) -> Result<DeviceAccess, String> {
        set_access(
            &dir(&app)?,
            &keystore().scoped(host),
            &id,
            &patch,
            confirm_self,
        )
        .await
        .map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn revoke_device(
        app: tauri::AppHandle,
        host: Option<String>,
        id: String,
        confirm_self: bool,
    ) -> Result<Done, String> {
        revoke(&dir(&app)?, &keystore().scoped(host), &id, confirm_self)
            .await
            .map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn remove_device(
        app: tauri::AppHandle,
        host: Option<String>,
        id: String,
        confirm_self: bool,
    ) -> Result<Done, String> {
        remove(&dir(&app)?, &keystore().scoped(host), &id, confirm_self)
            .await
            .map_err(|e| explain(&e))
    }
}
