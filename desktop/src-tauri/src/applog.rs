//! A small in-memory log for the diagnostics report: the last few hundred events, filtered by the
//! level the user chose in Settings. It is never written to disk. Callers pass event words and
//! error text, never credentials. As a second line of defence, every secret the app handles is
//! registered here the moment it exists (`register_secret`), and any registered value that still
//! ends up in a line is replaced by `***`, both when the line is recorded and when it is read.

use std::collections::VecDeque;
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Level {
    Off,
    Error,
    Info,
    Debug,
}

impl Level {
    pub fn as_str(self) -> &'static str {
        match self {
            Level::Off => "off",
            Level::Error => "error",
            Level::Info => "info",
            Level::Debug => "debug",
        }
    }

    pub fn parse(s: &str) -> Option<Level> {
        Some(match s {
            "off" => Level::Off,
            "error" => Level::Error,
            "info" => Level::Info,
            "debug" => Level::Debug,
            _ => return None,
        })
    }
}

/// Lines kept; the oldest go first.
pub const CAPACITY: usize = 500;
/// Characters kept of one message.
const MAX_LINE: usize = 300;

struct Log {
    level: Level,
    lines: VecDeque<String>,
    /// Values to mask. Bounded; the oldest are dropped first.
    secrets: Vec<String>,
}

/// Shorter values are not registered: they would mask ordinary words.
const MIN_SECRET: usize = 6;
const MAX_SECRETS: usize = 256;

static LOG: Mutex<Log> = Mutex::new(Log {
    level: Level::Info,
    lines: VecDeque::new(),
    secrets: Vec::new(),
});

fn log() -> std::sync::MutexGuard<'static, Log> {
    LOG.lock().unwrap_or_else(|e| e.into_inner())
}

pub fn set_level(level: Level) {
    log().level = level;
}

pub fn level() -> Level {
    log().level
}

/// Registers a secret value (a password, token, nonce, pairing code or key) to be masked in the log.
pub fn register_secret(secret: &str) {
    if secret.len() < MIN_SECRET {
        return;
    }
    let mut l = log();
    // A secret registered again is the one in use now: move it to the newest place so it is the last to be
    // evicted, instead of keeping it where it was first registered (audit F-172).
    if let Some(i) = l.secrets.iter().position(|s| s == secret) {
        let s = l.secrets.remove(i);
        l.secrets.push(s);
        return;
    }
    if l.secrets.len() == MAX_SECRETS {
        l.secrets.remove(0);
    }
    l.secrets.push(secret.to_string());
}

fn mask(secrets: &[String], text: &str) -> String {
    let mut out = text.to_string();
    // Longest first, so a secret that contains another is masked whole.
    let mut ordered: Vec<&String> = secrets.iter().collect();
    ordered.sort_by_key(|s| std::cmp::Reverse(s.len()));
    for s in ordered {
        out = out.replace(s.as_str(), "***");
    }
    out
}

/// Records `msg` if `at` is within the chosen level. One line: control characters become spaces.
pub fn record(at: Level, msg: &str) {
    let mut l = log();
    if at == Level::Off || at > l.level {
        return;
    }
    // Mask before cutting the line short, so a secret cannot be left half showing.
    let clean: String = mask(&l.secrets, msg)
        .chars()
        .map(|c| if c.is_control() { ' ' } else { c })
        .take(MAX_LINE)
        .collect();
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    if l.lines.len() == CAPACITY {
        l.lines.pop_front();
    }
    l.lines
        .push_back(format!("{secs} {} {clean}", at.as_str().to_uppercase()));
}

pub fn error(msg: &str) {
    record(Level::Error, msg);
}

pub fn info(msg: &str) {
    record(Level::Info, msg);
}

pub fn debug(msg: &str) {
    record(Level::Debug, msg);
}

pub fn lines() -> Vec<String> {
    let l = log();
    l.lines.iter().map(|line| mask(&l.secrets, line)).collect()
}

pub fn clear() {
    log().lines.clear();
}

#[cfg(test)]
mod secret_order_tests {
    use super::*;

    #[test]
    fn a_secret_registered_again_is_the_last_to_be_evicted() {
        register_secret("keep-this-secret-0");
        for i in 0..MAX_SECRETS - 1 {
            register_secret(&format!("filler-secret-{i:04}"));
        }
        register_secret("keep-this-secret-0"); // in use again: moves to the newest place
        register_secret("one-more-secret-xx"); // would evict the oldest
        assert!(log().secrets.iter().any(|s| s == "keep-this-secret-0"));
    }
}
