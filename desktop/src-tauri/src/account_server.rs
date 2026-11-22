//! Account-server controls for one connected computer: read its account-server status, turn its own
//! account-server role on or off, and choose which other computer's account server it trusts.
//! All of it is the agent's `/settings/account-server` route (admin sessions only) plus the public
//! `GET /accounts/key` of the computer to be trusted, read before anything is pinned.
//!
//! Trusting a key is the one step that needs the owner: the fingerprint is worked out here from the
//! key itself (never taken from what the other computer claims), shown to the owner to compare with
//! the one on that computer's own screen, and sent back with the key so the agent can refuse a key
//! that changed in between. A session that an account server vouched for may not change the pin; the
//! agent answers 403 and `explain` says so in plain words.

use crate::agent_client::{self, normalize_fingerprint, AgentError, CODE_CAPABILITY_DENIED};
use crate::applog;
use crate::device_actions::{logged, session};
use crate::health::{STATUS_FORBIDDEN, STATUS_UNAUTHORIZED};
use crate::secrets::Offloaded;
use base64::Engine as _;
use reqwest::Method;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::path::Path;

/// The agent's own limit on an account server's display name (openapi `AccountServerSettingsUpdate`).
pub const MAX_NAME_CHARS: usize = 64;

/// An Ed25519 public key is 32 raw bytes.
const KEY_BYTES: usize = 32;

/// HTTP status the agent answers once an account's failed-password budget is spent.
const STATUS_TOO_MANY_REQUESTS: u16 = 429;

/// What a name falls back to when the other computer sends none that is fit to show.
const DEFAULT_NAME: &str = "Account server";

/// 403 on a change: a pairing code login, or a login an account server vouched for, may not do it.
pub const NOT_ALLOWED: &str =
    "This sign-in cannot change the account server. Sign in on this computer with its own account \
     password, not a pairing code or an account server.";

/// 403 ACCOUNT_SERVER_DISABLED when asking a computer for its key.
pub const DISABLED: &str =
    "That computer's account server is turned off. Turn it on there (Settings, Security, Account \
     server, or rfe accounts enable), then look up its key again.";

/// The route is missing: an agent from before the account server.
pub const TOO_OLD: &str = "This agent has no account server support. Update the agent first.";

/// The other computer answered with something that is not an Ed25519 public key.
pub const NOT_A_KEY: &str =
    "That computer sent something that is not an account-server key, so nothing was changed.";

/// The key to pin does not have the fingerprint the owner confirmed.
pub const KEY_CHANGED: &str =
    "The key no longer matches the fingerprint you confirmed, so nothing was trusted. Look the key \
     up again and compare it.";

/// 403 CAPABILITY_DENIED on a pin: the request carried no account password.
pub const PASSWORD_NEEDED: &str =
    "Trusting an account server needs this computer's account password. Enter the password of the \
     account you are signed in with.";

/// 401 INVALID_CREDENTIALS on a pin: the password is not the signed-in account's.
pub const WRONG_PASSWORD: &str =
    "That is not the password of the account this app is signed in with, so nothing was trusted. \
     Check it and try again.";

/// 429 on a pin: the account's failed-password budget is spent, even the right password is refused.
pub const TOO_MANY_PASSWORDS: &str =
    "Too many wrong passwords for this account in a short time. Wait a few minutes, then try \
     again.";

/// The one account server a computer accepts sign-ins from.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct TrustedServer {
    pub name: String,
    pub public_key: String,
    pub key_fingerprint: String,
}

/// `GET /settings/account-server`: this computer's account-server role and the server it trusts.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Settings {
    pub enabled: bool,
    pub name: String,
    /// This computer's own key; the agent sends it only while the role is on.
    pub key_fingerprint: Option<String>,
    pub trusted_server: Option<TrustedServer>,
}

/// The `accountServer` field of `/health`: what a computer offers as an account server.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct Offer {
    pub name: String,
    pub key_fingerprint: String,
}

/// Everything the dialog shows about one computer.
#[derive(Debug, Clone, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Status {
    #[serde(flatten)]
    pub settings: Settings,
    /// What the computer's health report says it offers (`None` while the role is off).
    pub offered: Option<Offer>,
}

/// An account server's key as another computer sent it, with the fingerprint worked out here.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct KeyView {
    pub host_port: String,
    pub name: String,
    pub public_key: String,
    pub key_fingerprint: String,
}

/// The SHA-256 of a raw Ed25519 public key (given as standard base64), as 64 lowercase hex
/// characters: the fingerprint both computers show. Anything that is not a 32-byte key is refused.
pub fn key_fingerprint(public_key_b64: &str) -> Result<String, AgentError> {
    let raw = base64::engine::general_purpose::STANDARD
        .decode(public_key_b64.trim())
        .map_err(|_| AgentError::Local(NOT_A_KEY.into()))?;
    if raw.len() != KEY_BYTES {
        return Err(AgentError::Local(NOT_A_KEY.into()));
    }
    Ok(hex::encode(Sha256::digest(&raw)))
}

/// A display name another computer chose, made safe to show: trimmed, and `fallback` when it is
/// empty, longer than the agent accepts, or holds a control character.
pub fn clean_name(name: &str, fallback: &str) -> String {
    let name = name.trim();
    if name.is_empty()
        || name.chars().count() > MAX_NAME_CHARS
        || name.chars().any(char::is_control)
    {
        return fallback.to_string();
    }
    name.to_string()
}

fn parse<T: serde::de::DeserializeOwned>(v: serde_json::Value) -> Result<T, AgentError> {
    serde_json::from_value(v).map_err(|e| AgentError::Local(format!("unexpected response: {e}")))
}

/// Reads the settings and, best effort, what the health report offers, on an open session. A health
/// report that fails leaves `offered` empty: the settings already say everything that matters.
async fn read_status(s: &crate::device_actions::Session) -> Result<Status, AgentError> {
    let settings: Settings = parse(
        s.client
            .call_device_json(Method::GET, "/settings/account-server", &s.token, None)
            .await?,
    )?;
    let offered = match s
        .client
        .call_device_json(Method::GET, "/health", &s.token, None)
        .await
    {
        Ok(h) => h
            .get("accountServer")
            .and_then(|a| serde_json::from_value::<Offer>(a.clone()).ok()),
        Err(e) => {
            applog::info(&format!("the health report gave no account server: {e}"));
            None
        }
    };
    Ok(Status { settings, offered })
}

/// This computer's account-server role and trusted server, as the agent has them now.
pub async fn status(dir: &Path, store: &Offloaded) -> Result<Status, AgentError> {
    let r = async { read_status(&session(dir, store).await?).await }.await;
    logged("reading the account server settings", r)
}

/// Asks the computer at `host:port` for its account-server key without trusting it, and works out
/// the fingerprint from the key. Nothing is changed anywhere.
pub async fn lookup_key(host_port: &str) -> Result<KeyView, AgentError> {
    let host_port = host_port.trim();
    let r = async {
        let reply = agent_client::fetch_account_key(host_port).await?;
        let key_fingerprint = key_fingerprint(&reply.public_key)?;
        Ok(KeyView {
            host_port: host_port.to_string(),
            name: clean_name(&reply.name, host_port),
            public_key: reply.public_key.trim().to_string(),
            key_fingerprint,
        })
    }
    .await;
    logged(
        &format!("looking up the account server key of {host_port}"),
        r,
    )
}

/// Sends one change (`PUT` or `DELETE /settings/account-server`) and returns the status afterwards.
async fn change(
    dir: &Path,
    store: &Offloaded,
    method: Method,
    body: Option<serde_json::Value>,
) -> Result<Status, AgentError> {
    let s = session(dir, store).await?;
    s.client
        .call_device_json(method, "/settings/account-server", &s.token, body.as_ref())
        .await?;
    read_status(&s).await
}

/// Turns this computer's own account-server role on or off.
pub async fn set_enabled(
    dir: &Path,
    store: &Offloaded,
    enabled: bool,
) -> Result<Status, AgentError> {
    let body = serde_json::json!({ "enabled": enabled });
    let what = if enabled {
        "turning the account server on"
    } else {
        "turning the account server off"
    };
    logged(what, change(dir, store, Method::PUT, Some(body)).await)
}

/// Pins `public_key` as the one account server this computer trusts. `confirmed_fingerprint` is what
/// the owner compared on screen; the key must still have it, or nothing is sent. Pinning a different
/// key ends every sign-in the previous server vouched for. `password` is the signed-in account's
/// password, which the agent checks before it pins anything; it is only sent, never kept or logged.
pub async fn trust(
    dir: &Path,
    store: &Offloaded,
    name: &str,
    public_key: &str,
    confirmed_fingerprint: &str,
    password: &str,
) -> Result<Status, AgentError> {
    let fp = key_fingerprint(public_key)?;
    if normalize_fingerprint(confirmed_fingerprint) != fp {
        return Err(AgentError::Local(KEY_CHANGED.into()));
    }
    let body = serde_json::json!({ "trustedServer": {
        "name": clean_name(name, DEFAULT_NAME),
        "publicKey": public_key.trim(),
        "keyFingerprint": fp,
    }, "password": password });
    logged(
        &format!("trusting the account server with key {fp}"),
        change(dir, store, Method::PUT, Some(body)).await,
    )
}

/// Stops trusting the pinned account server; every sign-in it vouched for ends.
pub async fn untrust(dir: &Path, store: &Offloaded) -> Result<Status, AgentError> {
    logged(
        "no longer trusting the account server",
        change(dir, store, Method::DELETE, None).await,
    )
}

/// The text the window shows for a failed account-server call: a missing, wrong or rate-limited
/// password on a pin, the refusal of a session that may not change the pin, a computer whose account server is off and an agent without the feature each get
/// their own sentence; everything else is the agent client's usual wording.
pub fn explain(e: &AgentError) -> String {
    match e {
        AgentError::Server {
            status: STATUS_FORBIDDEN,
            code,
            ..
        } if code == "ACCOUNT_SERVER_DISABLED" => DISABLED.to_string(),
        AgentError::Server {
            status: STATUS_FORBIDDEN,
            code,
            ..
        } if code == CODE_CAPABILITY_DENIED => PASSWORD_NEEDED.to_string(),
        AgentError::Server {
            status: STATUS_UNAUTHORIZED,
            code,
            ..
        } if code == "INVALID_CREDENTIALS" => WRONG_PASSWORD.to_string(),
        AgentError::Server {
            status: STATUS_TOO_MANY_REQUESTS,
            ..
        } => TOO_MANY_PASSWORDS.to_string(),
        AgentError::Server {
            status: STATUS_FORBIDDEN,
            ..
        } => NOT_ALLOWED.to_string(),
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
    pub async fn account_server_status(
        app: tauri::AppHandle,
        host: Option<String>,
    ) -> Result<Status, String> {
        super::status(&dir(&app)?, &keystore().scoped(host))
            .await
            .map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn account_server_key(host_port: String) -> Result<KeyView, String> {
        lookup_key(&host_port).await.map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn account_server_enable(
        app: tauri::AppHandle,
        host: Option<String>,
        enabled: bool,
    ) -> Result<Status, String> {
        set_enabled(&dir(&app)?, &keystore().scoped(host), enabled)
            .await
            .map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn account_server_trust(
        app: tauri::AppHandle,
        host: Option<String>,
        name: String,
        public_key: String,
        key_fingerprint: String,
        password: String,
    ) -> Result<Status, String> {
        trust(
            &dir(&app)?,
            &keystore().scoped(host),
            &name,
            &public_key,
            &key_fingerprint,
            &password,
        )
        .await
        .map_err(|e| explain(&e))
    }

    #[tauri::command]
    pub async fn account_server_untrust(
        app: tauri::AppHandle,
        host: Option<String>,
    ) -> Result<Status, String> {
        untrust(&dir(&app)?, &keystore().scoped(host))
            .await
            .map_err(|e| explain(&e))
    }
}
