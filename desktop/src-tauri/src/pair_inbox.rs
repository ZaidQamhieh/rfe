//! The pairing-request inbox: the owner's screen for computers and phones that asked to be paired
//! with "approve on the PC". It lists what the agent is holding (`GET /pair/requests`) and
//! answers one request at a time (approve or reject). Only an account session can do either; any
//! other login gets 403 from the agent, which the listing turns into `forbidden` rather than an
//! error, so the window can explain it.
//!
//! Every request goes through the pinned client with the saved login, exactly like the device
//! list. The window never sees the token and cannot name a URL: it passes a request id, which is
//! checked before it enters the path.

use crate::agent_client::{AgentClient, AgentError, WaitingPairRequest};
use crate::applog;
use crate::flows::{self, MIN_AGENT_FOR_APPROVAL};
use crate::secrets::Offloaded;
use serde::Serialize;
use std::path::Path;

/// How long the agent keeps a request waiting.
pub const TTL_SECONDS: u64 = 120;
/// How many requests the agent holds at once; a further one is refused with `PAIR_BUSY`.
pub const LIMIT: usize = 3;

/// One waiting request, ready to show. `label` and `address` were sent by whoever asked, so the
/// window must render them as text only.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InboxRequest {
    pub id: String,
    pub label: String,
    /// The code the agent derives for this request; the person asking shows the same one.
    pub match_code: String,
    /// The address the request came from, as the agent saw it.
    pub address: String,
    /// Label of the paired device an approval would take over; empty for a new computer.
    pub replaces: String,
    pub age_seconds: u64,
    pub expires_in_seconds: u64,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Inbox {
    /// This login is not an account session, so the agent will not list or answer requests.
    pub forbidden: bool,
    pub requests: Vec<InboxRequest>,
    pub limit: usize,
    pub ttl_seconds: u64,
}

/// What answering did. `Gone` is not a failure: the request ran out or was answered on the PC
/// (or from another window) in the meantime.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Answer {
    Done,
    Gone,
}

fn digits(s: &str, from: usize, to: usize) -> Option<i64> {
    let part = s.get(from..to)?;
    if part.is_empty() || !part.bytes().all(|b| b.is_ascii_digit()) {
        return None;
    }
    part.parse().ok()
}

/// Seconds since 1970 for `YYYY-MM-DDTHH:MM:SSZ`, the only form the agent writes.
fn parse_utc(s: &str) -> Option<i64> {
    let b = s.as_bytes();
    if b.len() != 20
        || b[4] != b'-'
        || b[7] != b'-'
        || b[10] != b'T'
        || b[13] != b':'
        || b[16] != b':'
        || b[19] != b'Z'
    {
        return None;
    }
    let (y, m, d) = (digits(s, 0, 4)?, digits(s, 5, 7)?, digits(s, 8, 10)?);
    let (hh, mm, ss) = (digits(s, 11, 13)?, digits(s, 14, 16)?, digits(s, 17, 19)?);
    if !(1..=12).contains(&m) || !(1..=31).contains(&d) || hh > 23 || mm > 59 || ss > 60 {
        return None;
    }
    // Days from 1970-01-01 (the proleptic Gregorian calendar, March-based years).
    let y = if m <= 2 { y - 1 } else { y };
    let era = y.div_euclid(400);
    let yoe = y.rem_euclid(400);
    let doy = (153 * ((m + 9) % 12) + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    let days = era * 146_097 + doe - 719_468;
    Some(days * 86_400 + hh * 3_600 + mm * 60 + ss)
}

/// How a listed request reads at `now` (unix seconds). The agent gives only the expiry, so the
/// age is the time since the request was made, `TTL_SECONDS` before it. An expiry that cannot be
/// read leaves the request looking brand new rather than expired, so a good request is never
/// hidden by a format surprise.
pub fn describe(r: &WaitingPairRequest, now: i64) -> InboxRequest {
    let left = parse_utc(&r.expires_at)
        .map(|exp| (exp - now).clamp(0, TTL_SECONDS as i64) as u64)
        .unwrap_or(TTL_SECONDS);
    InboxRequest {
        id: r.id.clone(),
        label: r.label.clone(),
        match_code: r.match_code.clone(),
        address: r.remote_ip.clone(),
        replaces: r.replaces.clone(),
        age_seconds: TTL_SECONDS - left,
        expires_in_seconds: left,
    }
}

fn now_unix() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

fn inbox_of(list: &[WaitingPairRequest], now: i64) -> Inbox {
    Inbox {
        forbidden: false,
        requests: list.iter().map(|r| describe(r, now)).collect(),
        limit: LIMIT,
        ttl_seconds: TTL_SECONDS,
    }
}

/// The pinned client and the saved token, as the device list gets them. A pin that was forgotten
/// ends the session here.
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

/// The agent refused the token (revoked or removed there): drop the dead token so the window goes
/// back to sign-in, and keep the pin, the account name and the device key.
async fn forget_dead_token<T>(
    dir: &Path,
    store: &Offloaded,
    token: &str,
    r: Result<T, AgentError>,
) -> Result<T, AgentError> {
    if let Err(AgentError::Server { status: 401, .. }) = &r {
        let dir = dir.to_path_buf();
        let token = token.to_string();
        store
            .run(move |st| flows::drop_refused_token(&dir, st, &token))
            .await
            .map_err(AgentError::Local)?;
    }
    r
}

/// An agent without these routes answers a bare-text 404, not the JSON error body.
fn too_old(e: AgentError) -> AgentError {
    match e {
        AgentError::Server {
            status: 404, code, ..
        } if code.starts_with("HTTP_") => AgentError::Local(format!(
            "This agent is too old to list pairing requests (it needs {MIN_AGENT_FOR_APPROVAL} or newer)."
        )),
        other => other,
    }
}

/// The requests waiting at the agent. A login that is not an account session gets
/// `forbidden: true` and no requests.
pub async fn load(dir: &Path, store: &Offloaded) -> Result<Inbox, AgentError> {
    let r = load_inner(dir, store).await;
    match &r {
        Ok(i) if i.forbidden => applog::info("pairing requests: this login may not list them"),
        Ok(i) => applog::debug(&format!("{} pairing requests waiting", i.requests.len())),
        Err(e) => applog::error(&format!("listing pairing requests failed: {e}")),
    }
    r
}

async fn load_inner(dir: &Path, store: &Offloaded) -> Result<Inbox, AgentError> {
    let (client, token) = session(dir, store).await?;
    let listed = client.waiting_pair_requests(&token).await;
    match listed {
        Ok(list) => Ok(inbox_of(&list, now_unix())),
        Err(AgentError::Server { status: 403, .. }) => Ok(Inbox {
            forbidden: true,
            requests: Vec::new(),
            limit: LIMIT,
            ttl_seconds: TTL_SECONDS,
        }),
        Err(e) => forget_dead_token(dir, store, &token, Err(too_old(e))).await,
    }
}

/// Approves (`approve`) or rejects one waiting request. Approving lets whoever asked collect a
/// login: it starts browse-only, like any approved device.
pub async fn answer(
    dir: &Path,
    store: &Offloaded,
    request_id: &str,
    approve: bool,
) -> Result<Answer, AgentError> {
    let what = if approve { "approving" } else { "rejecting" };
    let r = answer_inner(dir, store, request_id, approve).await;
    match &r {
        Ok(Answer::Done) => applog::info(&format!("{what} pairing request {request_id}: ok")),
        Ok(Answer::Gone) => applog::info(&format!(
            "{what} pairing request {request_id}: it was already gone"
        )),
        Err(e) => applog::error(&format!("{what} pairing request {request_id} failed: {e}")),
    }
    r
}

async fn answer_inner(
    dir: &Path,
    store: &Offloaded,
    request_id: &str,
    approve: bool,
) -> Result<Answer, AgentError> {
    let (client, token) = session(dir, store).await?;
    match client
        .answer_pair_request(&token, request_id, approve)
        .await
    {
        Ok(()) => Ok(Answer::Done),
        Err(AgentError::Server {
            status: 404, code, ..
        }) if code == "NOT_FOUND" => Ok(Answer::Gone),
        Err(e) => forget_dead_token(dir, store, &token, Err(too_old(e))).await,
    }
}

#[tauri::command]
pub async fn list_pair_requests(
    app: tauri::AppHandle,
    host: Option<String>,
) -> Result<Inbox, String> {
    let dir = crate::data_dir(&app)?;
    load(&dir, &crate::keystore().scoped(host))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn answer_pair_request(
    app: tauri::AppHandle,
    host: Option<String>,
    id: String,
    approve: bool,
) -> Result<Answer, String> {
    let dir = crate::data_dir(&app)?;
    answer(&dir, &crate::keystore().scoped(host), &id, approve)
        .await
        .map_err(|e| e.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn waiting(expires_at: &str) -> WaitingPairRequest {
        WaitingPairRequest {
            id: "abc123".into(),
            label: "Pixel".into(),
            match_code: "1234 5678".into(),
            remote_ip: "192.168.1.5".into(),
            replaces: String::new(),
            expires_at: expires_at.into(),
        }
    }

    #[test]
    fn reads_the_agents_timestamps() {
        assert_eq!(parse_utc("1970-01-01T00:00:00Z"), Some(0));
        assert_eq!(parse_utc("1970-01-02T00:00:01Z"), Some(86_401));
        // 2000-03-01 is after a leap day in a century year that is a leap year.
        assert_eq!(parse_utc("2000-03-01T00:00:00Z"), Some(951_868_800));
        assert_eq!(parse_utc("2026-10-04T12:34:56Z"), Some(1_791_117_296));
        for bad in [
            "",
            "2026-10-04 12:34:56Z",
            "2026-10-04T12:34:56+00:00",
            "2026-13-04T12:34:56Z",
            "2026-10-04T24:34:56Z",
            "+026-10-04T12:34:56Z",
            "2026-10-04T12:34:5éZ",
        ] {
            assert_eq!(parse_utc(bad), None, "{bad:?}");
        }
    }

    #[test]
    fn age_and_time_left_add_up_to_the_lifetime() {
        let now = parse_utc("2026-10-04T12:00:00Z").unwrap();
        let r = describe(&waiting("2026-10-04T12:01:25Z"), now);
        assert_eq!((r.expires_in_seconds, r.age_seconds), (85, 35));
        assert_eq!(r.address, "192.168.1.5");
        assert_eq!(r.match_code, "1234 5678");
    }

    #[test]
    fn a_clock_that_disagrees_is_clamped_not_wrapped() {
        let now = parse_utc("2026-10-04T12:00:00Z").unwrap();
        // Already past its expiry on this clock, or further away than the lifetime allows.
        let past = describe(&waiting("2026-10-04T11:00:00Z"), now);
        assert_eq!((past.expires_in_seconds, past.age_seconds), (0, 120));
        let far = describe(&waiting("2026-10-04T13:00:00Z"), now);
        assert_eq!((far.expires_in_seconds, far.age_seconds), (120, 0));
        let unreadable = describe(&waiting("soon"), now);
        assert_eq!(unreadable.expires_in_seconds, 120);
    }
}
