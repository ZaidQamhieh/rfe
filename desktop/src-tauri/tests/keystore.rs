//! Keystore behaviour: where the device key and login token live, migration of the old
//! 0600 files, and what happens when the keystore is missing, locked or lossy.

use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::identity::Identity;
use rfe_desktop_lib::secrets::{account, MemoryStore, OsKeystore, SecretStore, UnavailableStore};
use std::path::Path;
use tempfile::TempDir;

const OLD_KEY_B64: &str = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="; // 32 bytes of 0x01

fn write_legacy(dir: &Path) {
    std::fs::write(
        dir.join("identity.json"),
        format!(r#"{{"device_id":"desktop-old","private_key":"{OLD_KEY_B64}"}}"#),
    )
    .unwrap();
    std::fs::write(
        dir.join("state.json"),
        r#"{"host":"h:1","fingerprint":"ab","token":"old-token","username":"owner","device_id":"d1"}"#,
    )
    .unwrap();
}

fn listing(dir: &Path) -> Vec<String> {
    let mut v: Vec<_> = std::fs::read_dir(dir)
        .unwrap()
        .map(|e| e.unwrap().file_name().to_string_lossy().to_string())
        .collect();
    v.sort();
    v
}

fn all_file_text(dir: &Path) -> String {
    listing(dir)
        .iter()
        .map(|n| std::fs::read_to_string(dir.join(n)).unwrap_or_default())
        .collect()
}

#[test]
fn key_and_token_go_to_the_keystore_and_never_to_files() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    let id = Identity::load_or_create(dir.path(), &store).unwrap();
    flows::save(
        dir.path(),
        &store,
        &Saved {
            host: "h:1".into(),
            token: "tok-123".into(),
            ..Default::default()
        },
    )
    .unwrap();

    assert_eq!(listing(dir.path()), ["state.json"], "no identity file");
    let text = all_file_text(dir.path());
    assert!(!text.contains("tok-123"), "token leaked to disk: {text}");
    assert!(!text.contains("private_key"), "key leaked to disk: {text}");
    let in_store = store
        .get(&account("identity", dir.path()))
        .unwrap()
        .expect("key in the keystore");
    assert!(in_store.contains("private_key"));
    assert_eq!(
        flows::load_saved(dir.path(), &store).unwrap().token,
        "tok-123"
    );
    // The identity is stable across loads.
    let again = Identity::load_or_create(dir.path(), &store).unwrap();
    assert_eq!(again.device_id(), id.device_id());
    assert_eq!(again.public_key_b64(), id.public_key_b64());
}

#[test]
fn old_0600_files_are_migrated_into_the_keystore_and_removed() {
    let dir = TempDir::new().unwrap();
    write_legacy(dir.path());
    let store = MemoryStore::default();

    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(saved.token, "old-token");
    assert_eq!(
        (saved.host.as_str(), saved.device_id.as_str()),
        ("h:1", "d1")
    );
    let id = Identity::load_or_create(dir.path(), &store).unwrap();
    assert_eq!(id.device_id(), "desktop-old");

    assert_eq!(listing(dir.path()), ["state.json"], "identity.json removed");
    assert!(!all_file_text(dir.path()).contains("old-token"));
    assert!(store
        .get(&account("identity", dir.path()))
        .unwrap()
        .unwrap()
        .contains(OLD_KEY_B64));
    // Migrating again is a no-op.
    assert_eq!(
        flows::load_saved(dir.path(), &store).unwrap().token,
        "old-token"
    );
}

#[test]
fn a_failed_migration_leaves_the_old_files_untouched() {
    let dir = TempDir::new().unwrap();
    write_legacy(dir.path());
    let before = all_file_text(dir.path());

    assert!(flows::load_saved(dir.path(), &UnavailableStore).is_err());
    assert!(Identity::load_or_create(dir.path(), &UnavailableStore).is_err());
    assert_eq!(all_file_text(dir.path()), before);
    assert_eq!(listing(dir.path()), ["identity.json", "state.json"]);

    // Control: with a working keystore the same files migrate.
    let store = MemoryStore::default();
    assert!(flows::load_saved(dir.path(), &store).is_ok());
    assert!(Identity::load_or_create(dir.path(), &store).is_ok());
    assert_eq!(listing(dir.path()), ["state.json"]);
}

#[test]
fn missing_or_locked_keystore_is_a_clear_error_and_never_a_file_fallback() {
    let dir = TempDir::new().unwrap();
    let err = Identity::load_or_create(dir.path(), &UnavailableStore)
        .err()
        .expect("must fail");
    assert!(err.contains("OS keystore"), "{err}");
    assert!(err.contains("never saved to files"), "{err}");
    let err = flows::save(
        dir.path(),
        &UnavailableStore,
        &Saved {
            token: "tok".into(),
            ..Default::default()
        },
    )
    .unwrap_err();
    assert!(err.contains("OS keystore"), "{err}");
    assert!(
        listing(dir.path()).is_empty(),
        "nothing was written: {:?}",
        listing(dir.path())
    );

    // Negative control: the same calls succeed with a working keystore.
    let store = MemoryStore::default();
    Identity::load_or_create(dir.path(), &store).unwrap();
    flows::save(
        dir.path(),
        &store,
        &Saved {
            token: "tok".into(),
            ..Default::default()
        },
    )
    .unwrap();
    assert_eq!(listing(dir.path()), ["state.json"]);
}

#[test]
fn a_keystore_that_loses_writes_is_caught() {
    struct Lossy;
    impl SecretStore for Lossy {
        fn get(&self, _: &str) -> Result<Option<String>, String> {
            Ok(None)
        }
        fn set(&self, _: &str, _: &str) -> Result<(), String> {
            Ok(())
        }
        fn delete(&self, _: &str) -> Result<(), String> {
            Ok(())
        }
    }
    let dir = TempDir::new().unwrap();
    write_legacy(dir.path());
    assert!(flows::load_saved(dir.path(), &Lossy).is_err());
    assert!(Identity::load_or_create(dir.path(), &Lossy).is_err());
    assert_eq!(
        listing(dir.path()),
        ["identity.json", "state.json"],
        "old files kept when the keystore cannot be trusted"
    );
}

#[test]
fn a_legacy_key_that_differs_from_the_keystore_is_never_deleted() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    Identity::load_or_create(dir.path(), &store).unwrap(); // a different, fresh key
    write_legacy(dir.path());
    let err = Identity::load_or_create(dir.path(), &store)
        .err()
        .expect("conflict must fail");
    assert!(err.contains("different key"), "{err}");
    assert!(dir.path().join("identity.json").exists());
}

#[test]
fn sign_out_removes_the_token_everywhere_even_when_the_keystore_is_locked() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::save(
        dir.path(),
        &store,
        &Saved {
            host: "h:1".into(),
            token: "tok".into(),
            ..Default::default()
        },
    )
    .unwrap();
    flows::sign_out(dir.path(), &store).unwrap();
    let after = flows::load_saved(dir.path(), &store).unwrap();
    assert!(after.token.is_empty());
    assert_eq!(after.host, "h:1", "the pin and host stay");

    // A pre-keystore file with a token: a locked keystore errors, but the plaintext is gone.
    let old = TempDir::new().unwrap();
    write_legacy(old.path());
    assert!(flows::sign_out(old.path(), &UnavailableStore).is_err());
    assert!(!all_file_text(old.path()).contains("old-token"));
}

/// The real OS backend with no reachable keystore. The child re-runs this test binary with an
/// unreachable session bus, which is what a machine without a Secret Service looks like.
#[cfg(target_os = "linux")]
#[test]
fn the_real_backend_reports_an_unreachable_keystore() {
    if std::env::var("RFE_KEYSTORE_CHILD").is_ok() {
        let err = OsKeystore::with_service("rfe-desktop-test")
            .get("probe")
            .expect_err("no keystore is reachable");
        assert!(err.contains("OS keystore"), "{err}");
        // Names the cause and the fix, not only "unavailable".
        assert!(err.contains("no OS keystore answered"), "{err}");
        assert!(err.contains("Secret Service provider"), "{err}");
        let dir = TempDir::new().unwrap();
        assert!(Identity::load_or_create(dir.path(), &OsKeystore::new()).is_err());
        assert!(listing(dir.path()).is_empty(), "no file fallback");
        return;
    }
    let out = std::process::Command::new(std::env::current_exe().unwrap())
        .args([
            "--exact",
            "the_real_backend_reports_an_unreachable_keystore",
            "--nocapture",
        ])
        .env("RFE_KEYSTORE_CHILD", "1")
        .env("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent/rfe-bus")
        .env_remove("DBUS_STARTER_ADDRESS")
        .output()
        .unwrap();
    assert!(
        out.status.success() && String::from_utf8_lossy(&out.stdout).contains("1 passed"),
        "child failed:\n{}{}",
        String::from_utf8_lossy(&out.stdout),
        String::from_utf8_lossy(&out.stderr)
    );
}

/// Needs a running, unlocked Secret Service: `cargo test -- --ignored`.
#[test]
#[ignore = "needs an unlocked Secret Service"]
fn the_real_secret_service_round_trips() {
    let store = OsKeystore::with_service("rfe-desktop-test");
    let dir = TempDir::new().unwrap();
    let acct = account("token", dir.path());
    assert_eq!(store.get(&acct).unwrap(), None);
    store.set(&acct, "s3cret").unwrap();
    assert_eq!(store.get(&acct).unwrap().as_deref(), Some("s3cret"));
    let id = Identity::load_or_create(dir.path(), &store).unwrap();
    assert_eq!(
        Identity::load_or_create(dir.path(), &store)
            .unwrap()
            .device_id(),
        id.device_id()
    );
    assert!(listing(dir.path()).is_empty(), "nothing on disk");
    store.delete(&acct).unwrap();
    store.delete(&account("identity", dir.path())).unwrap();
    assert_eq!(store.get(&acct).unwrap(), None);
    store.delete(&acct).unwrap(); // deleting a missing secret is fine
}

/// A sign-in and a pairing starting together on a fresh install must share one device key.
#[test]
fn two_first_uses_at_once_end_with_one_device_key() {
    let dir = TempDir::new().unwrap();
    let store = std::sync::Arc::new(MemoryStore::default());
    let start = std::sync::Arc::new(std::sync::Barrier::new(8));
    let ids: Vec<String> = (0..8)
        .map(|_| {
            let (dir, store, start) = (dir.path().to_path_buf(), store.clone(), start.clone());
            std::thread::spawn(move || {
                start.wait();
                Identity::load_or_create(&dir, &*store)
                    .unwrap()
                    .device_id()
                    .to_string()
            })
        })
        .collect::<Vec<_>>()
        .into_iter()
        .map(|h| h.join().unwrap())
        .collect();
    assert!(ids.iter().all(|i| *i == ids[0]), "{ids:?}");
    let after = Identity::load_or_create(dir.path(), &*store).unwrap();
    assert_eq!(after.device_id(), ids[0]);
}
