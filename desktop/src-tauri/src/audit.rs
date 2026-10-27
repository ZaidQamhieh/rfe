//! Admin screen data: the agent's audit trail (`GET /v1/audit`, paged newest first) and the tail of
//! its log (`GET /v1/logs`). Both routes are admin only; a login that is not an admin device gets
//! 403 from the agent, which is reported as `forbidden` (a state of its own) rather than an error.
//!
//! Everything here goes through the pinned client, like the device list. The text in an entry (the
//! actor is a username or device label chosen by whoever tried to sign in) is untrusted: it is
//! stripped of control and bidirectional-override characters and cut to a fixed length before it
//! reaches the window, which then shows it with `textContent` only.

use crate::agent_client::{AgentClient, AgentError};
use crate::applog;
use crate::flows;
use crate::secrets::Offloaded;
use serde::{Deserialize, Serialize};
use std::path::Path;

/// Entries asked for per request; the same page the phone uses.
pub const AUDIT_PAGE: usize = 100;
/// The most the agent returns in one page.
pub const AUDIT_PAGE_MAX: usize = 500;
/// The agent sends about this many log lines; more are dropped.
pub const MAX_LOG_LINES: usize = 200;
/// Characters kept of an actor, target, detail or timestamp.
pub const CAP_FIELD: usize = 200;
/// Characters kept of a log message.
pub const CAP_MESSAGE: usize = 1000;

/// One recorded event. `at` is the agent's RFC 3339 timestamp, kept as sent so the window can show
/// it in local time and keep the original in a tooltip.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct AuditEntry {
    pub id: i64,
    pub at: String,
    pub action: String,
    pub actor: String,
    pub target: String,
    pub detail: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct LogLine {
    pub ts: String,
    pub message: String,
}

#[derive(Deserialize, Default)]
#[serde(default)]
struct AuditBody {
    entries: Vec<AuditEntry>,
}

/// One page of the audit trail. `next_before` is the cursor for the next older page, `None` when
/// this page was short (nothing older).
#[derive(Debug, Clone, Default, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct AuditReply {
    pub forbidden: bool,
    pub entries: Vec<AuditEntry>,
    pub next_before: Option<i64>,
}

#[derive(Debug, Clone, Default, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct LogsReply {
    pub forbidden: bool,
    pub lines: Vec<LogLine>,
}

/// `s` without control characters and bidirectional overrides (they can reorder or hide text that
/// follows), cut to `max` characters with an ellipsis.
pub fn clean(s: &str, max: usize) -> String {
    let mut out = String::new();
    for (n, c) in s.chars().enumerate() {
        if n == max {
            out.push('\u{2026}');
            break;
        }
        out.push(if c.is_control() || is_bidi(c) { ' ' } else { c });
    }
    out
}

fn is_bidi(c: char) -> bool {
    matches!(c, '\u{061c}' | '\u{200e}' | '\u{200f}' | '\u{202a}'..='\u{202e}' | '\u{2066}'..='\u{2069}')
}

impl AuditEntry {
    fn cleaned(self) -> Self {
        Self {
            id: self.id,
            at: clean(&self.at, 64),
            action: clean(&self.action, 64),
            actor: clean(&self.actor, CAP_FIELD),
            target: clean(&self.target, CAP_FIELD),
            detail: clean(&self.detail, CAP_FIELD),
        }
    }
}

/// The agent's answer for a login that is not an admin device.
fn is_forbidden(e: &AgentError) -> bool {
    matches!(e, AgentError::Server { status: 403, code, .. } if code == "FORBIDDEN")
}

/// The pinned client and the saved token. Like the device list: a forgotten pin ends the session,
/// and a token the agent no longer accepts is dropped so the window returns to sign-in.
async fn session(dir: &Path, store: &Offloaded) -> Result<(AgentClient, String), AgentError> {
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
        .find(|p| flows::pin_key(&p.host) == key)
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                saved.host
            ))
        })?;
    Ok((AgentClient::pinned(&saved.host, &pinned)?, saved.token))
}

async fn drop_dead_token(
    dir: &Path,
    store: &Offloaded,
    token: &str,
    e: &AgentError,
) -> Result<(), AgentError> {
    if let AgentError::Server { status: 401, .. } = e {
        let dir = dir.to_path_buf();
        let token = token.to_string();
        store
            .run(move |st| flows::drop_refused_token(&dir, st, &token))
            .await
            .map_err(AgentError::Local)?;
    }
    Ok(())
}

/// One page of the audit trail: the newest `limit` entries, or the `limit` before id `before`.
pub async fn fetch_audit(
    dir: &Path,
    store: &Offloaded,
    before: Option<i64>,
    limit: usize,
) -> Result<AuditReply, AgentError> {
    let r = fetch_audit_inner(dir, store, before, limit).await;
    match &r {
        Ok(p) if p.forbidden => applog::info("the audit log was refused: not an admin login"),
        Ok(p) => applog::debug(&format!("read {} audit entries", p.entries.len())),
        Err(e) => applog::error(&format!("reading the audit log failed: {e}")),
    }
    r
}

async fn fetch_audit_inner(
    dir: &Path,
    store: &Offloaded,
    before: Option<i64>,
    limit: usize,
) -> Result<AuditReply, AgentError> {
    if limit == 0 || limit > AUDIT_PAGE_MAX {
        return Err(AgentError::Local(format!(
            "the page size must be 1 to {AUDIT_PAGE_MAX}"
        )));
    }
    if before.is_some_and(|b| b <= 0) {
        return Err(AgentError::Local("unexpected audit cursor".into()));
    }
    let (client, token) = session(dir, store).await?;
    let mut query = vec![("limit", limit.to_string())];
    if let Some(b) = before {
        query.push(("before", b.to_string()));
    }
    match client
        .get_json_at::<AuditBody>(&token, "/audit", &query)
        .await
    {
        Ok(body) => {
            let got = body.entries.len();
            let entries: Vec<AuditEntry> =
                body.entries.into_iter().map(AuditEntry::cleaned).collect();
            // A full page may have more behind it; a short one is the end. The cursor is the id
            // of the last (oldest) entry, as the agent defines it.
            let next_before = if got >= limit {
                entries.last().map(|e| e.id).filter(|id| *id > 0)
            } else {
                None
            };
            Ok(AuditReply {
                forbidden: false,
                entries,
                next_before,
            })
        }
        Err(e) if is_forbidden(&e) => Ok(AuditReply {
            forbidden: true,
            ..Default::default()
        }),
        Err(e) => {
            drop_dead_token(dir, store, &token, &e).await?;
            Err(e)
        }
    }
}

/// The tail of the agent's log (about the last 200 lines, oldest first, as the agent sends them).
pub async fn fetch_logs(dir: &Path, store: &Offloaded) -> Result<LogsReply, AgentError> {
    let r = fetch_logs_inner(dir, store).await;
    match &r {
        Ok(p) if p.forbidden => applog::info("the agent log was refused: not an admin login"),
        Ok(p) => applog::debug(&format!("read {} agent log lines", p.lines.len())),
        Err(e) => applog::error(&format!("reading the agent log failed: {e}")),
    }
    r
}

async fn fetch_logs_inner(dir: &Path, store: &Offloaded) -> Result<LogsReply, AgentError> {
    let (client, token) = session(dir, store).await?;
    match client
        .get_json_at::<Vec<LogLine>>(&token, "/logs", &[])
        .await
    {
        Ok(lines) => {
            let skip = lines.len().saturating_sub(MAX_LOG_LINES);
            Ok(LogsReply {
                forbidden: false,
                lines: lines
                    .into_iter()
                    .skip(skip)
                    .map(|l| LogLine {
                        ts: clean(&l.ts, 64),
                        message: clean(&l.message, CAP_MESSAGE),
                    })
                    .collect(),
            })
        }
        Err(e) if is_forbidden(&e) => Ok(LogsReply {
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
mod bidi_tests {
    use super::*;

    #[test]
    fn arabic_letter_mark_is_treated_as_bidi() {
        assert!(is_bidi('\u{061c}'));
        assert_eq!(clean("a\u{061c}b", 16), "a b");
    }
}
