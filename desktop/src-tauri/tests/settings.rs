//! What Settings reads and writes: the log level (kept in `state.json`), the keystore check, and
//! the in-memory log. The log is process-wide, so the tests that use it run one at a time.

use rfe_desktop_lib::applog::{self, Level};
use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::secrets::{MemoryStore, UnavailableStore};
use std::sync::Mutex;
use tempfile::TempDir;

static ONE_AT_A_TIME: Mutex<()> = Mutex::new(());

fn exclusive() -> std::sync::MutexGuard<'static, ()> {
    ONE_AT_A_TIME.lock().unwrap_or_else(|e| e.into_inner())
}

#[test]
fn the_log_level_defaults_to_info_and_is_kept_in_state_json() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    assert_eq!(flows::log_level(dir.path()).unwrap(), Level::Info);

    flows::set_log_level(dir.path(), "debug").unwrap();
    assert_eq!(flows::log_level(dir.path()).unwrap(), Level::Debug);
    assert_eq!(applog::level(), Level::Debug, "applied at once");
    let file = std::fs::read_to_string(dir.path().join("state.json")).unwrap();
    assert!(file.contains("\"log_level\": \"debug\""), "{file}");

    // Saving a login later keeps the level.
    let store = MemoryStore::default();
    flows::save(
        dir.path(),
        &store,
        &Saved {
            host: "h:1".into(),
            fingerprint: "ab".repeat(32),
            token: "tok".into(),
            username: "u".into(),
            device_id: "d".into(),
        },
    )
    .unwrap();
    assert_eq!(flows::log_level(dir.path()).unwrap(), Level::Debug);
    assert!(!std::fs::read_to_string(dir.path().join("state.json"))
        .unwrap()
        .contains("tok\""));

    // A restart applies it.
    applog::set_level(Level::Info);
    flows::apply_log_level(dir.path());
    assert_eq!(applog::level(), Level::Debug);
    flows::set_log_level(dir.path(), "info").unwrap();
}

#[test]
fn an_unknown_level_is_refused_and_changes_nothing() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    flows::set_log_level(dir.path(), "error").unwrap();
    let err = flows::set_log_level(dir.path(), "verbose").unwrap_err();
    assert!(err.contains("off, error, info or debug"), "{err}");
    assert_eq!(flows::log_level(dir.path()).unwrap(), Level::Error);
    flows::set_log_level(dir.path(), "info").unwrap();
}

#[test]
fn the_level_filters_what_is_recorded() {
    let _x = exclusive();
    applog::clear();
    applog::set_level(Level::Error);
    applog::debug("d");
    applog::info("i");
    applog::error("e");
    assert_eq!(applog::lines().len(), 1);
    applog::set_level(Level::Off);
    applog::error("nothing");
    assert_eq!(applog::lines().len(), 1);
    applog::set_level(Level::Debug);
    applog::debug("d2");
    assert_eq!(applog::lines().len(), 2);
    applog::set_level(Level::Info);
}

#[test]
fn the_log_is_bounded_and_one_line_per_event() {
    let _x = exclusive();
    applog::clear();
    applog::set_level(Level::Info);
    for i in 0..applog::CAPACITY + 50 {
        applog::info(&format!("event {i}"));
    }
    let lines = applog::lines();
    assert_eq!(lines.len(), applog::CAPACITY);
    assert!(lines[0].ends_with("event 50"), "the oldest went first");
    applog::info("two\nlines\tand a very long tail ".repeat(40).as_str());
    let last = applog::lines().pop().unwrap();
    assert!(!last.contains('\n') && !last.contains('\t'), "{last:?}");
    assert!(last.len() < 400, "{}", last.len());
    applog::clear();
}

#[test]
fn the_keystore_check_passes_on_a_working_store_and_leaves_nothing_behind() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::check_keystore(dir.path(), &store).unwrap();
    // Run again: the throwaway entry was removed, so this is not a read of a leftover.
    flows::check_keystore(dir.path(), &store).unwrap();
}

#[test]
fn the_keystore_check_reports_a_locked_store_with_the_fix() {
    let _x = exclusive();
    let dir = TempDir::new().unwrap();
    let err = flows::check_keystore(dir.path(), &UnavailableStore).unwrap_err();
    assert!(err.contains("OS keystore refused access"), "{err}");
    assert!(err.contains("Unlock it"), "{err}");
}
