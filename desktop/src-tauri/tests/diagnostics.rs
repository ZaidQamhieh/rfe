//! The diagnostics report and the secret masking behind it. The log is process-wide, so these
//! tests run one at a time; the test against a real agent is in log_secrets.rs.

use rfe_desktop_lib::applog::{self, Level};
use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::secrets::MemoryStore;
use std::sync::Mutex;
use tempfile::TempDir;

static ONE_AT_A_TIME: Mutex<()> = Mutex::new(());

fn exclusive() -> std::sync::MutexGuard<'static, ()> {
    let g = ONE_AT_A_TIME.lock().unwrap_or_else(|e| e.into_inner());
    applog::clear();
    applog::set_level(Level::Info);
    g
}

#[test]
fn a_fresh_install_still_gets_a_report() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    let r = flows::diagnostics(dir.path());
    assert!(r.starts_with("RFE Desktop diagnostics\n"), "{r}");
    assert!(
        r.contains(&format!("app version: {}", env!("CARGO_PKG_VERSION"))),
        "{r}"
    );
    assert!(r.contains("agent address: (none)"), "{r}");
    assert!(r.contains("saved login: none"), "{r}");
    assert!(r.contains("log level: info"), "{r}");
    assert!(r.contains("recent errors:\n  (none)"), "{r}");
    assert!(r.contains("not included"), "{r}");
}

#[test]
fn it_names_the_agent_and_its_pin_but_not_the_account() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    let fp = "ab".repeat(32);
    flows::save(
        dir.path(),
        &MemoryStore::default(),
        &Saved {
            host: "10.0.0.5:8765".into(),
            fingerprint: fp.clone(),
            token: "token-that-stays-out".into(),
            username: "alice-the-owner".into(),
            device_id: "dev-1".into(),
        },
    )
    .unwrap();
    let r = flows::diagnostics(dir.path());
    assert!(r.contains("agent address: 10.0.0.5:8765"), "{r}");
    assert!(r.contains(&format!("pinned fingerprint: {fp}")), "{r}");
    assert!(r.contains("trusted agents: 1"), "{r}");
    assert!(r.contains("saved login: account"), "{r}");
    assert!(
        !r.contains("alice-the-owner"),
        "the account name is left out:\n{r}"
    );
    assert!(!r.contains("token-that-stays-out"), "{r}");
}

#[test]
fn recent_errors_are_the_last_twenty_error_lines() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    for i in 0..30 {
        applog::error(&format!("failure number {i}"));
        applog::info("fine");
    }
    let r = flows::diagnostics(dir.path());
    let errors = r.split("log (").next().unwrap();
    assert!(
        !errors.contains("failure number 9\n"),
        "only the newest twenty:\n{errors}"
    );
    assert!(errors.contains("failure number 10\n") && errors.contains("failure number 29\n"));
    assert!(!errors.contains("fine"), "{errors}");
    assert!(r.contains("log (60 lines, oldest first):"), "{r}");
}

#[test]
fn a_damaged_state_file_is_reported_not_fatal() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    std::fs::write(dir.path().join("state.json"), b"{ not json").unwrap();
    let r = flows::diagnostics(dir.path());
    assert!(r.contains("state: unreadable"), "{r}");
    assert!(r.contains("app version"), "{r}");
}

#[test]
fn a_registered_secret_is_masked_in_the_log_and_the_report() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    applog::register_secret("s3cr3t-value-123");
    applog::error("the call failed with s3cr3t-value-123 inside");
    // A secret registered after the line was written is masked when the line is read.
    applog::info("late-registered-token-456 appeared");
    applog::register_secret("late-registered-token-456");
    // Too short to register: it would mask ordinary words.
    applog::register_secret("abc");
    applog::info("abc stays");

    let lines = applog::lines().join("\n");
    assert!(!lines.contains("s3cr3t-value-123"), "{lines}");
    assert!(!lines.contains("late-registered-token-456"), "{lines}");
    assert!(lines.contains("the call failed with *** inside"), "{lines}");
    assert!(lines.contains("abc stays"), "{lines}");
    let r = flows::diagnostics(dir.path());
    assert!(
        !r.contains("s3cr3t-value-123") && !r.contains("late-registered-token-456"),
        "{r}"
    );
}

#[test]
fn a_secret_cut_by_the_line_limit_is_not_left_half_showing() {
    let _x = exclusive();
    let secret = "K".repeat(40);
    applog::register_secret(&secret);
    // The secret straddles the 300-character limit.
    applog::info(&format!("{}{secret}", "x".repeat(285)));
    let line = applog::lines().pop().unwrap();
    assert!(!line.contains("KKKKK"), "{line}");
}
