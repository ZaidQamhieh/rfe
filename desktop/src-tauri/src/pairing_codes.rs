//! Generate a one-time pairing code for a phone (`POST /pairing/generate`). Only an admin session
//! (one signed in with the account) may do it; any other session gets a plain "not allowed"
//! state instead of an error. The code goes to the window once, to be shown; this app never
//! writes it to `state.json`, the keystore or the log, and keeps no copy after answering.

use crate::agent_client::{AgentClient, AgentError};
use crate::applog;
use crate::flows;
use crate::secrets::Offloaded;
use serde::Serialize;
use std::path::Path;

/// How long a code made here stays valid. Shorter than the agent's default hour, because the
/// code sits on a screen; "Generate a new code" makes another.
pub const TTL_SECONDS: u64 = 600;

/// What the window gets: a code and its lifetime, or the fact that this login may not make one.
#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CodeView {
    /// `"ok"` or `"forbidden"`.
    pub status: &'static str,
    /// Empty unless `status` is `"ok"`.
    pub code: String,
    pub expires_in_seconds: u64,
    /// The text the phone's QR scanner reads (JSON); carries the live code, so it is redacted in
    /// `Debug` and never logged. Empty unless `status` is `"ok"`.
    pub qr: String,
}

// Not derived: a `{:?}` in a log line or a test failure must not print a live pairing code.
impl std::fmt::Debug for CodeView {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("CodeView")
            .field("status", &self.status)
            .field(
                "code",
                &if self.code.is_empty() {
                    ""
                } else {
                    "<redacted>"
                },
            )
            .field("expires_in_seconds", &self.expires_in_seconds)
            .field("qr", &if self.qr.is_empty() { "" } else { "<redacted>" })
            .finish()
    }
}

/// Mints a code with the saved login. A refusal because the session is not an admin one is a
/// result (`forbidden`), not an error, so the window can explain it; every other failure is an
/// error whose text is shown as it is.
pub async fn generate(dir: &Path, store: &Offloaded) -> Result<CodeView, AgentError> {
    generate_for(dir, store, TTL_SECONDS).await
}

/// The lifetimes the settings offer, in seconds: two, five and ten minutes.
pub const LIFETIMES: [u64; 3] = [120, 300, 600];

/// Like `generate`, with a lifetime from [`LIFETIMES`]; anything else is the default.
pub async fn generate_for(
    dir: &Path,
    store: &Offloaded,
    ttl_seconds: u64,
) -> Result<CodeView, AgentError> {
    let ttl = if LIFETIMES.contains(&ttl_seconds) {
        ttl_seconds
    } else {
        TTL_SECONDS
    };
    let r = generate_inner(dir, store, ttl).await;
    match &r {
        Ok(v) if v.status == "ok" => applog::info("pairing code generation: ok"),
        Ok(_) => {
            applog::info("pairing code generation: refused, this login is not an admin session")
        }
        Err(e) => applog::error(&format!("pairing code generation failed: {e}")),
    }
    r
}

async fn generate_inner(
    dir: &Path,
    store: &Offloaded,
    ttl_seconds: u64,
) -> Result<CodeView, AgentError> {
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
    // The pin list is the record of what is trusted: a forgotten pin ends the session here.
    let pinned = flows::list_pins(dir)
        .map_err(AgentError::Local)?
        .into_iter()
        .find(|p| p.host == flows::pin_key(&saved.host))
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                saved.host
            ))
        })?;
    let made = AgentClient::pinned(&saved.host, &pinned)?
        .generate_pairing_code(&saved.token, ttl_seconds)
        .await;
    match made {
        Ok(g) if g.pairing_code.is_empty() => Err(AgentError::Local(
            "unexpected response: the agent sent no pairing code".into(),
        )),
        Ok(g) => {
            applog::register_secret(&g.pairing_code);
            let qr = if g.qr_payload.is_null() {
                String::new()
            } else {
                g.qr_payload.to_string()
            };
            applog::register_secret(&qr);
            Ok(CodeView {
                status: "ok",
                code: g.pairing_code,
                expires_in_seconds: g.expires_in_seconds,
                qr,
            })
        }
        Err(AgentError::Server { status: 403, .. }) => Ok(CodeView {
            status: "forbidden",
            code: String::new(),
            expires_in_seconds: 0,
            qr: String::new(),
        }),
        Err(e) => {
            if let AgentError::Server { status: 401, .. } = &e {
                // The agent refuses this token: drop the dead login so the window goes back to
                // sign-in, as for the device list.
                let dir = dir.to_path_buf();
                let token = saved.token.clone();
                store
                    .run(move |st| flows::drop_refused_token(&dir, st, &token))
                    .await
                    .map_err(AgentError::Local)?;
            }
            Err(e)
        }
    }
}
