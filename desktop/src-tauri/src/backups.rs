//! Admin screen data: the agent's backup and launcher status (`GET /v1/system/backups`, admin only).
//! A login that is not an admin device gets 403, reported as `forbidden` rather than an error. Every
//! text field is untrusted agent output: control and bidi characters are stripped and the length cut
//! before it reaches the window, which shows it with `textContent` only.

use crate::agent_client::{AgentError, CODE_FORBIDDEN, STATUS_FORBIDDEN};
use crate::applog;
use crate::audit::clean;
use crate::files::{drop_dead_token, session};
use crate::secrets::Offloaded;
use serde::{Deserialize, Serialize};
use std::path::Path;

const CAP: usize = 300;

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct LastBackup {
    pub time: String,
    pub label: String,
    pub path: String,
    pub size: i64,
    pub ok: bool,
    pub error: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct LastVerification {
    pub time: String,
    pub ok: bool,
    pub archive: String,
    pub error: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct Offsite {
    pub configured: bool,
    pub last_time: String,
    pub ok: bool,
    pub error: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct Scheduler {
    pub enabled: bool,
    pub last_run: String,
    pub last_error: String,
    pub pending: Vec<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct Launcher {
    pub current: String,
    pub previous: String,
    pub pending: String,
    pub bad: Vec<String>,
    pub updated: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Body {
    last_backup: Option<LastBackup>,
    last_verification: Option<LastVerification>,
    offsite: Offsite,
    scheduler: Scheduler,
    launcher: Option<Launcher>,
    launcher_error: String,
}

#[derive(Debug, Clone, Default, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct BackupsReply {
    pub forbidden: bool,
    pub last_backup: Option<LastBackup>,
    pub last_verification: Option<LastVerification>,
    pub offsite: Offsite,
    pub scheduler: Scheduler,
    pub launcher: Option<Launcher>,
    pub launcher_error: String,
}

fn c(s: &str) -> String {
    clean(s, CAP)
}

fn cleaned(b: Body) -> BackupsReply {
    BackupsReply {
        forbidden: false,
        last_backup: b.last_backup.map(|x| LastBackup {
            time: c(&x.time),
            label: c(&x.label),
            path: c(&x.path),
            size: x.size,
            ok: x.ok,
            error: c(&x.error),
        }),
        last_verification: b.last_verification.map(|x| LastVerification {
            time: c(&x.time),
            ok: x.ok,
            archive: c(&x.archive),
            error: c(&x.error),
        }),
        offsite: Offsite {
            configured: b.offsite.configured,
            last_time: c(&b.offsite.last_time),
            ok: b.offsite.ok,
            error: c(&b.offsite.error),
        },
        scheduler: Scheduler {
            enabled: b.scheduler.enabled,
            last_run: c(&b.scheduler.last_run),
            last_error: c(&b.scheduler.last_error),
            pending: b.scheduler.pending.iter().take(20).map(|s| c(s)).collect(),
        },
        launcher: b.launcher.map(|x| Launcher {
            current: c(&x.current),
            previous: c(&x.previous),
            pending: c(&x.pending),
            bad: x.bad.iter().take(20).map(|s| c(s)).collect(),
            updated: c(&x.updated),
        }),
        launcher_error: c(&b.launcher_error),
    }
}

fn is_forbidden(e: &AgentError) -> bool {
    matches!(e, AgentError::Server { status: STATUS_FORBIDDEN, code, .. } if code == CODE_FORBIDDEN)
}

/// The agent's backup and launcher status.
pub async fn fetch_backups(dir: &Path, store: &Offloaded) -> Result<BackupsReply, AgentError> {
    let r = inner(dir, store).await;
    match &r {
        Ok(p) if p.forbidden => applog::info("the backup status was refused: not an admin login"),
        Ok(_) => applog::debug("read the backup status"),
        Err(e) => applog::error(&format!("reading the backup status failed: {e}")),
    }
    r
}

async fn inner(dir: &Path, store: &Offloaded) -> Result<BackupsReply, AgentError> {
    let (client, token) = session(dir, store).await?;
    match client
        .get_json_at::<Body>(&token, "/system/backups", &[])
        .await
    {
        Ok(b) => Ok(cleaned(b)),
        Err(e) if is_forbidden(&e) => Ok(BackupsReply {
            forbidden: true,
            ..Default::default()
        }),
        Err(e) => {
            drop_dead_token(dir, store, &token, &e).await?;
            Err(e)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn text_is_cleaned_and_cut() {
        let b: Body = serde_json::from_str(
            r#"{"last_backup":{"time":"t","label":"a\u202eb","path":"/x","size":5,"ok":false,"error":"bad\u0007"},
                "scheduler":{"enabled":true,"pending":["a"]},"launcher":{"current":"1.0.0","bad":["0.9"]}}"#,
        )
        .unwrap();
        let r = cleaned(b);
        let lb = r.last_backup.unwrap();
        assert_eq!(lb.label, "a b");
        assert_eq!(lb.error, "bad ");
        assert!(r.scheduler.enabled && r.launcher.unwrap().bad == vec!["0.9"]);
    }
}
