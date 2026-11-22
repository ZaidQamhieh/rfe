//! Push refresh: follows each saved computer's Server-Sent Events stream (GET /v1/events) and tells
//! the window, with one Tauri event per computer and kind, that something changed there, so the
//! window re-reads the open page at once instead of waiting for its poll.
//!
//! One background task per signed-in computer, over the same pinned TLS client and bearer login
//! every other call uses. The task reconnects with a growing wait, gives up quickly on an agent that
//! has no such route (404, 405, 501: the window keeps polling) or refuses this login, never reads a
//! line longer than 64 KiB, and ends when the computer is removed, signed out, or the window is
//! destroyed. An event carries only the computer's key and the kind; the login is never logged.

use crate::agent_client::{AgentClient, CLIENT_VERSION, CLIENT_VERSION_HEADER};
use crate::applog;
use serde::Serialize;
use std::collections::{HashMap, HashSet};
use std::sync::Mutex;
use std::time::{Duration, Instant};
use tauri::{AppHandle, Emitter, Manager};

/// The Tauri event the window listens for: `{host, kind}` for each change the agent announces.
pub const EVENT_NAME: &str = "agent-event";
/// The Tauri event that reports a stream's state: `{host, state}`, state one of "connected",
/// "disconnected" or "unsupported".
pub const STREAM_NAME: &str = "agent-stream";
/// The most one line (and one event's data) of the stream may be; a longer one ends the stream.
pub const MAX_LINE: usize = 64 * 1024;
/// The kinds the agent announces (protocol/openapi.yaml, /events). Anything else is ignored.
pub const KINDS: [&str; 6] = [
    "hello",
    "transfers",
    "files",
    "settings",
    "devices",
    "audit",
];

/// The first reconnect wait; it doubles after each failure up to `BACKOFF_MAX`.
const BACKOFF_START: Duration = Duration::from_secs(1);
const BACKOFF_MAX: Duration = Duration::from_secs(60);
/// How long to wait before asking again an agent that has no event route or refuses this login.
const RECHECK: Duration = Duration::from_secs(600);
/// The longest a "Retry-After" answer (503 too many streams) is honoured, in seconds.
const RETRY_AFTER_MAX: u64 = 300;
/// A stream with no byte for this long is dropped; the agent sends a ping every 20 s.
const IDLE_LIMIT: Duration = Duration::from_secs(60);
/// One connection lasts at most this long (the client's own limit is 30 s); a new one follows.
const STREAM_LIFE: Duration = Duration::from_secs(3600);
/// A stream that lived this long counts as healthy: the reconnect wait starts over.
const STABLE_AFTER: Duration = Duration::from_secs(30);

/// One event of the stream: its name (`event:` field, "message" when absent) and joined data lines.
#[derive(Debug, PartialEq, Eq)]
pub struct SseEvent {
    pub name: String,
    pub data: String,
}

/// A line or an event's data grew past `MAX_LINE`.
#[derive(Debug, PartialEq, Eq)]
pub struct TooLong;

/// Builds events out of the bytes of a text/event-stream body, which may arrive cut anywhere.
/// Understands `event:` and `data:` fields, comment lines and CRLF; the rest is ignored.
#[derive(Default)]
pub struct SseParser {
    line: Vec<u8>,
    name: String,
    data: String,
    has_data: bool,
}

impl SseParser {
    /// Adds the next bytes of the body and returns the events they completed (an event ends at an
    /// empty line). Fails once a line, or the data of one event, is longer than `MAX_LINE` bytes.
    pub fn feed(&mut self, bytes: &[u8]) -> Result<Vec<SseEvent>, TooLong> {
        let mut out = Vec::new();
        for &b in bytes {
            if b != b'\n' {
                if self.line.len() >= MAX_LINE {
                    return Err(TooLong);
                }
                self.line.push(b);
                continue;
            }
            let mut line = std::mem::take(&mut self.line);
            if line.last() == Some(&b'\r') {
                line.pop();
            }
            if let Some(ev) = self.take_line(&line)? {
                out.push(ev);
            }
        }
        Ok(out)
    }

    /// Applies one complete line; an empty line closes the event being built.
    fn take_line(&mut self, line: &[u8]) -> Result<Option<SseEvent>, TooLong> {
        if line.is_empty() {
            let name = std::mem::take(&mut self.name);
            let data = std::mem::take(&mut self.data);
            let had = std::mem::take(&mut self.has_data);
            if name.is_empty() && !had {
                return Ok(None);
            }
            let name = if name.is_empty() {
                "message".to_string()
            } else {
                name
            };
            return Ok(Some(SseEvent { name, data }));
        }
        if line[0] == b':' {
            return Ok(None);
        }
        let text = String::from_utf8_lossy(line);
        let (field, value) = match text.split_once(':') {
            Some((f, v)) => (f, v.strip_prefix(' ').unwrap_or(v)),
            None => (&*text, ""),
        };
        match field {
            "event" => self.name = value.to_string(),
            "data" => {
                if self.data.len() + value.len() + 1 > MAX_LINE {
                    return Err(TooLong);
                }
                if self.has_data {
                    self.data.push('\n');
                }
                self.data.push_str(value);
                self.has_data = true;
            }
            _ => {}
        }
        Ok(None)
    }
}

/// The known kind an event announces, or none: the `event:` name when it is a known kind, else
/// the `kind` in a JSON data line. Never returns text taken from the stream.
pub fn kind_of(ev: &SseEvent) -> Option<&'static str> {
    let find = |s: &str| KINDS.iter().copied().find(|k| *k == s);
    if let Some(k) = find(&ev.name) {
        return Some(k);
    }
    if ev.name != "message" {
        return None;
    }
    let v: serde_json::Value = serde_json::from_str(&ev.data).ok()?;
    find(v.get("kind")?.as_str()?)
}

/// What the agent's answer to the stream request means for the client.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Verdict {
    /// 200 with an event stream: read it.
    Stream,
    /// The agent has no such route (404, 405, 501) or answered something that is not a stream.
    Unsupported,
    /// 401 or 403: this login may not subscribe.
    Denied,
    /// 503 or 429: too many streams open; wait as told.
    Busy,
    /// Anything else.
    Failed,
}

/// Classifies the answer to `GET /v1/events` by its status and Content-Type.
pub fn verdict(status: u16, content_type: &str) -> Verdict {
    match status {
        200 if content_type
            .trim()
            .to_ascii_lowercase()
            .starts_with("text/event-stream") =>
        {
            Verdict::Stream
        }
        200 | 404 | 405 | 501 => Verdict::Unsupported,
        401 | 403 => Verdict::Denied,
        429 | 503 => Verdict::Busy,
        _ => Verdict::Failed,
    }
}

/// The wait before reconnect number `attempt` (0 first): 1 s, 2 s, 4 s ... up to 60 s.
pub fn backoff(attempt: u32) -> Duration {
    BACKOFF_START
        .saturating_mul(1u32 << attempt.min(10))
        .min(BACKOFF_MAX)
}

/// How long to wait after an attempt that ended as `v`. `retry_after` is the agent's Retry-After
/// header; it can lengthen the wait of a busy agent (up to 5 minutes), never shorten it.
pub fn next_delay(v: Verdict, attempt: u32, retry_after: Option<&str>) -> Duration {
    match v {
        Verdict::Unsupported | Verdict::Denied => RECHECK,
        Verdict::Busy => {
            let told = retry_after
                .and_then(|s| s.trim().parse::<u64>().ok())
                .map(|s| Duration::from_secs(s.min(RETRY_AFTER_MAX)))
                .unwrap_or_default();
            backoff(attempt).max(told)
        }
        Verdict::Stream | Verdict::Failed => backoff(attempt),
    }
}

/// Why reading a stream ended.
#[derive(Debug, PartialEq, Eq, Clone, Copy)]
pub enum Ended {
    /// The agent closed it.
    Closed,
    /// No byte arrived for the idle limit.
    Idle,
    /// A line was longer than `MAX_LINE`.
    LineTooLong,
    /// The connection failed while reading.
    ReadFailed,
}

/// Reads the body of an event-stream answer until it ends, calling `on_kind` for each known kind in
/// order. `idle` is how long it waits for the next bytes.
pub async fn pump(
    mut resp: reqwest::Response,
    idle: Duration,
    mut on_kind: impl FnMut(&'static str),
) -> Ended {
    let mut parser = SseParser::default();
    loop {
        match tokio::time::timeout(idle, resp.chunk()).await {
            Err(_) => return Ended::Idle,
            Ok(Err(_)) => return Ended::ReadFailed,
            Ok(Ok(None)) => return Ended::Closed,
            Ok(Ok(Some(bytes))) => match parser.feed(&bytes) {
                Err(TooLong) => return Ended::LineTooLong,
                Ok(events) => {
                    for ev in &events {
                        if let Some(k) = kind_of(ev) {
                            on_kind(k);
                        }
                    }
                }
            },
        }
    }
}

/// Which streams to start and which to stop so that exactly `wanted` run, given those that `running`.
/// Both lists are sorted.
pub fn plan(running: &[String], wanted: &[String]) -> (Vec<String>, Vec<String>) {
    let have: HashSet<&String> = running.iter().collect();
    let want: HashSet<&String> = wanted.iter().collect();
    let mut start: Vec<String> = want.difference(&have).map(|s| (*s).clone()).collect();
    let mut stop: Vec<String> = have.difference(&want).map(|s| (*s).clone()).collect();
    start.sort();
    stop.sort();
    (start, stop)
}

#[derive(Serialize, Clone)]
struct Notice<'a> {
    host: &'a str,
    kind: &'a str,
}

#[derive(Serialize, Clone)]
struct StreamState<'a> {
    host: &'a str,
    state: &'a str,
}

/// The running streams by computer key, kept as app state so a command can start and stop them.
#[derive(Default)]
pub struct Events {
    tasks: Mutex<HashMap<String, tauri::async_runtime::JoinHandle<()>>>,
}

/// Tells the window a stream's state. A failed send only means the window is gone.
fn say_state(app: &AppHandle, host: &str, state: &str) {
    let _ = app.emit(STREAM_NAME, StreamState { host, state });
}

/// One connection to `host`'s event stream: loads the saved login and pin, connects, reads until
/// it ends, and reports what happened, how long it lasted, and any Retry-After the agent sent.
async fn connect_once(app: &AppHandle, key: &str) -> (Verdict, Option<String>, Duration) {
    let began = Instant::now();
    let failed = |v| (v, None, began.elapsed());
    let Ok(dir) = crate::data_dir(app) else {
        return failed(Verdict::Failed);
    };
    let wanted = key.to_string();
    let saved = match crate::keystore()
        .run(move |s| crate::hosts::saved_for(&dir, s, &wanted))
        .await
    {
        Ok(s) => s,
        Err(_) => return failed(Verdict::Failed),
    };
    if saved.token.is_empty() {
        return failed(Verdict::Denied);
    }
    let Ok(client) = AgentClient::pinned(&saved.host, &saved.fingerprint) else {
        return failed(Verdict::Failed);
    };
    let (http, base) = client.transport();
    let sent = http
        .get(format!("{base}/events"))
        .bearer_auth(&saved.token)
        .header(CLIENT_VERSION_HEADER, CLIENT_VERSION)
        .header(reqwest::header::ACCEPT, "text/event-stream")
        .timeout(STREAM_LIFE)
        .send()
        .await;
    let Ok(resp) = sent else {
        return failed(Verdict::Failed);
    };
    let ctype = resp
        .headers()
        .get(reqwest::header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    let v = verdict(resp.status().as_u16(), ctype);
    let retry = resp
        .headers()
        .get(reqwest::header::RETRY_AFTER)
        .and_then(|v| v.to_str().ok())
        .map(str::to_string);
    if v != Verdict::Stream {
        say_state(
            app,
            key,
            if matches!(v, Verdict::Unsupported | Verdict::Denied) {
                "unsupported"
            } else {
                "disconnected"
            },
        );
        return (v, retry, began.elapsed());
    }
    say_state(app, key, "connected");
    applog::info(&format!("event stream open for {key}"));
    let ended = pump(resp, IDLE_LIMIT, |kind| {
        let _ = app.emit(EVENT_NAME, Notice { host: key, kind });
    })
    .await;
    say_state(app, key, "disconnected");
    applog::info(&format!("event stream for {key} ended: {ended:?}"));
    (v, retry, began.elapsed())
}

/// The task for one computer: connects, waits as the last answer says, and tries again for as long
/// as it is not aborted.
async fn follow(app: AppHandle, key: String) {
    let mut attempt = 0u32;
    loop {
        let (v, retry, lived) = connect_once(&app, &key).await;
        let delay = next_delay(v, attempt, retry.as_deref());
        applog::debug(&format!(
            "event stream for {key}: {v:?}, next try in {delay:?}"
        ));
        attempt = if v == Verdict::Stream && lived >= STABLE_AFTER {
            0
        } else {
            attempt.saturating_add(1)
        };
        tokio::time::sleep(delay).await;
    }
}

/// Makes the running streams exactly the ones for `wanted` computers: starts the missing, aborts the rest.
fn apply(app: &AppHandle, wanted: &[String]) {
    let state = app.state::<Events>();
    let mut tasks = state.tasks.lock().unwrap_or_else(|e| e.into_inner());
    let running: Vec<String> = tasks.keys().cloned().collect();
    let (start, stop) = plan(&running, wanted);
    for k in stop {
        if let Some(h) = tasks.remove(&k) {
            h.abort();
        }
    }
    for k in start {
        let h = tauri::async_runtime::spawn(follow(app.clone(), k.clone()));
        tasks.insert(k, h);
    }
}

/// Stops the stream of one computer (it was removed or signed out).
pub fn stop(app: &AppHandle, key: &str) {
    let state = app.state::<Events>();
    let mut tasks = state.tasks.lock().unwrap_or_else(|e| e.into_inner());
    if let Some(h) = tasks.remove(key) {
        h.abort();
    }
}

/// Stops every stream (the window was destroyed).
pub fn stop_all(app: &AppHandle) {
    let state = app.state::<Events>();
    let mut tasks = state.tasks.lock().unwrap_or_else(|e| e.into_inner());
    for (_, h) in tasks.drain() {
        h.abort();
    }
}

/// Starts a stream for each saved computer that has a login and stops those that no longer do.
/// The window calls it at start and whenever the set of signed-in computers changes.
#[tauri::command]
pub async fn events_sync(app: AppHandle) -> Result<(), String> {
    let dir = crate::data_dir(&app)?;
    let hosts = crate::keystore()
        .run(move |s| crate::hosts::list(&dir, s))
        .await?;
    let wanted: Vec<String> = hosts
        .into_iter()
        .filter(|h| h.signed_in && !h.fingerprint.is_empty())
        .map(|h| h.key)
        .collect();
    apply(&app, &wanted);
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    fn feed_all(p: &mut SseParser, chunks: &[&[u8]]) -> Vec<SseEvent> {
        chunks.iter().flat_map(|c| p.feed(c).unwrap()).collect()
    }

    #[test]
    fn events_are_built_from_bytes_cut_anywhere_and_comments_and_crlf_are_handled() {
        let wire = b": ping\n\nid: 7\nevent: files\ndata: {\"kind\":\"files\",\"seq\":7}\n\nevent: audit\r\ndata: a\r\ndata: b\r\n\r\nretry: 5\n\n";
        let whole = feed_all(&mut SseParser::default(), &[wire]);
        let bytewise: Vec<SseEvent> = {
            let mut p = SseParser::default();
            wire.iter().flat_map(|b| p.feed(&[*b]).unwrap()).collect()
        };
        assert_eq!(whole, bytewise, "cutting the body anywhere changes nothing");
        assert_eq!(
            whole.len(),
            2,
            "a comment and a bare retry field are not events"
        );
        assert_eq!(whole[0].name, "files");
        assert_eq!(whole[0].data, "{\"kind\":\"files\",\"seq\":7}");
        assert_eq!(
            (whole[1].name.as_str(), whole[1].data.as_str()),
            ("audit", "a\nb")
        );
    }

    #[test]
    fn an_event_is_not_delivered_before_its_blank_line() {
        let mut p = SseParser::default();
        assert!(p.feed(b"event: files\ndata: {}\n").unwrap().is_empty());
        assert_eq!(p.feed(b"\n").unwrap().len(), 1);
    }

    #[test]
    fn a_line_at_the_cap_is_read_and_one_byte_more_ends_the_stream() {
        let line = |n: usize| format!("data: {}\n", "x".repeat(n - "data: ".len()));
        let mut p = SseParser::default();
        assert!(p.feed(line(MAX_LINE).as_bytes()).is_ok());
        let mut p = SseParser::default();
        assert_eq!(p.feed(line(MAX_LINE + 1).as_bytes()), Err(TooLong));
        let mut p = SseParser::default();
        assert_eq!(
            p.feed(&vec![b'x'; MAX_LINE + 1]),
            Err(TooLong),
            "also with no newline in sight"
        );
    }

    #[test]
    fn many_short_data_lines_cannot_pile_up_past_the_cap() {
        let mut p = SseParser::default();
        let chunk = format!("data: {}\n", "y".repeat(1000));
        let mut failed = false;
        for _ in 0..100 {
            if p.feed(chunk.as_bytes()).is_err() {
                failed = true;
                break;
            }
        }
        assert!(failed);
    }

    #[test]
    fn only_a_known_kind_is_reported_and_never_text_from_the_stream() {
        let ev = |n: &str, d: &str| SseEvent {
            name: n.into(),
            data: d.into(),
        };
        for k in KINDS {
            assert_eq!(kind_of(&ev(k, "")), Some(k));
        }
        assert_eq!(kind_of(&ev("sudo", "")), None);
        assert_eq!(
            kind_of(&ev("message", "{\"kind\":\"devices\",\"seq\":1}")),
            Some("devices")
        );
        assert_eq!(kind_of(&ev("message", "{\"kind\":\"nope\"}")), None);
        assert_eq!(kind_of(&ev("message", "not json")), None);
        assert_eq!(
            kind_of(&ev("files", "{\"kind\":\"audit\"}")),
            Some("files"),
            "the event name wins"
        );
    }

    #[test]
    fn the_answer_decides_whether_to_stream_give_up_or_wait() {
        assert_eq!(verdict(200, "text/event-stream"), Verdict::Stream);
        assert_eq!(
            verdict(200, "Text/Event-Stream; charset=utf-8"),
            Verdict::Stream
        );
        assert_eq!(
            verdict(200, "text/html"),
            Verdict::Unsupported,
            "a proxy's page is not a stream"
        );
        for s in [404, 405, 501] {
            assert_eq!(verdict(s, ""), Verdict::Unsupported, "{s}");
        }
        assert_eq!(verdict(401, ""), Verdict::Denied);
        assert_eq!(verdict(403, ""), Verdict::Denied);
        assert_eq!(verdict(503, ""), Verdict::Busy);
        assert_eq!(verdict(500, ""), Verdict::Failed);
    }

    #[test]
    fn the_reconnect_wait_doubles_to_a_minute_and_stays_there() {
        let secs: Vec<u64> = (0..9).map(|a| backoff(a).as_secs()).collect();
        assert_eq!(secs, [1, 2, 4, 8, 16, 32, 60, 60, 60]);
        assert_eq!(
            backoff(u32::MAX).as_secs(),
            60,
            "a huge count does not overflow"
        );
    }

    #[test]
    fn an_old_agent_is_asked_rarely_and_a_busy_one_is_waited_for_as_told_within_limits() {
        assert_eq!(next_delay(Verdict::Unsupported, 0, None), RECHECK);
        assert_eq!(next_delay(Verdict::Denied, 9, None), RECHECK);
        assert_eq!(next_delay(Verdict::Failed, 2, None), backoff(2));
        assert_eq!(next_delay(Verdict::Busy, 0, Some("30")).as_secs(), 30);
        assert_eq!(
            next_delay(Verdict::Busy, 6, Some("30")).as_secs(),
            60,
            "never shorter than the backoff"
        );
        assert_eq!(next_delay(Verdict::Busy, 0, Some("99999")).as_secs(), 300);
        assert_eq!(next_delay(Verdict::Busy, 0, Some("soon")), backoff(0));
    }

    #[test]
    fn streams_follow_the_signed_in_computers_only() {
        let s = |v: &[&str]| v.iter().map(|x| x.to_string()).collect::<Vec<_>>();
        let (start, stop) = plan(&s(&["a:1", "b:2"]), &s(&["b:2", "c:3", "d:4"]));
        assert_eq!(start, s(&["c:3", "d:4"]));
        assert_eq!(stop, s(&["a:1"]));
        assert_eq!(plan(&s(&["a:1"]), &s(&["a:1"])), (vec![], vec![]));
        assert_eq!(
            plan(&s(&["a:1"]), &[]).1,
            s(&["a:1"]),
            "a removed computer's stream is stopped"
        );
    }

    /// A plain-HTTP server that answers once: `head`, then `body`, then either closes or holds the
    /// connection open (`hold`) until the test ends.
    async fn serve(body: Vec<u8>, hold: bool) -> String {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let (mut sock, _) = listener.accept().await.unwrap();
            let mut buf = [0u8; 1024];
            let _ = sock.read(&mut buf).await;
            let head =
                "HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\nconnection: close\r\n\r\n";
            let _ = sock.write_all(head.as_bytes()).await;
            let _ = sock.write_all(&body).await;
            let _ = sock.flush().await;
            if hold {
                tokio::time::sleep(Duration::from_secs(30)).await;
            }
        });
        format!("http://{addr}/")
    }

    #[tokio::test]
    async fn a_stream_is_read_to_its_end_and_reports_each_known_kind_in_order() {
        let body = b"id: 1\nevent: hello\ndata: {\"kind\":\"hello\",\"seq\":1}\n\n: ping\n\nevent: bogus\ndata: {}\n\nid: 2\nevent: files\ndata: {\"kind\":\"files\",\"seq\":2}\n\n".to_vec();
        let resp = reqwest::get(serve(body, false).await).await.unwrap();
        let mut seen = Vec::new();
        let ended = pump(resp, Duration::from_secs(5), |k| seen.push(k)).await;
        assert_eq!(ended, Ended::Closed);
        assert_eq!(seen, ["hello", "files"]);
    }

    #[tokio::test]
    async fn a_silent_stream_is_dropped_after_the_idle_limit() {
        let resp = reqwest::get(serve(b": ping\n\n".to_vec(), true).await)
            .await
            .unwrap();
        let ended = pump(resp, Duration::from_millis(150), |_| {}).await;
        assert_eq!(ended, Ended::Idle);
    }

    #[tokio::test]
    async fn an_endless_line_ends_the_stream_instead_of_filling_memory() {
        let resp = reqwest::get(serve(vec![b'x'; MAX_LINE * 2], true).await)
            .await
            .unwrap();
        let ended = pump(resp, Duration::from_secs(5), |_| {}).await;
        assert_eq!(ended, Ended::LineTooLong);
    }
}
