//! The real Secret Service with a default keyring that exists and is locked, and nobody to type
//! the password. Needs `desktop/scripts/with-keyring.sh --locked`:
//!
//!   desktop/scripts/with-keyring.sh --locked cargo test --locked --test keystore_locked -- --ignored

use rfe_desktop_lib::flows;
use rfe_desktop_lib::secrets::{Offloaded, OsKeystore, SecretStore};
use tempfile::TempDir;

fn require_private_session() {
    let private = |name: &str| {
        std::env::var(name)
            .map(|v| v.starts_with(std::env::temp_dir().to_str().unwrap()))
            .unwrap_or(false)
    };
    assert!(
        private("XDG_DATA_HOME") && private("XDG_RUNTIME_DIR"),
        "run this through desktop/scripts/with-keyring.sh --locked, never against a real keyring"
    );
}

#[test]
#[ignore = "needs desktop/scripts/with-keyring.sh --locked"]
fn a_locked_keystore_refuses_and_says_how_to_unlock() {
    require_private_session();
    let store = OsKeystore::with_service("rfe-desktop-locked-test");
    for result in [
        store.set("probe", "x").err(),
        store.get("probe").err(),
        store.delete("probe").err(),
    ] {
        let err = result.expect("a locked keystore must not answer");
        assert!(err.contains("OS keystore refused access"), "{err}");
        assert!(err.contains("Unlock it"), "{err}");
        assert!(err.contains("never saved to files"), "{err}");
    }
}

#[tokio::test]
#[ignore = "needs desktop/scripts/with-keyring.sh --locked"]
async fn a_locked_keystore_stops_sign_in_before_any_network_traffic() {
    require_private_session();
    let dir = TempDir::new().unwrap();
    // Port 1 has nothing behind it: reaching the network would give a "cannot reach" error.
    let err = flows::login(
        dir.path(),
        "127.0.0.1:1",
        &"ab".repeat(32),
        "user",
        "password",
        "RFE Desktop",
        &Offloaded::new(OsKeystore::new()),
    )
    .await
    .expect_err("no device key can be made while the keystore is locked");
    let text = err.to_string();
    assert!(text.contains("OS keystore refused access"), "{text}");
    assert!(!text.contains("cannot reach"), "{text}");
    assert_eq!(
        std::fs::read_dir(dir.path()).unwrap().count(),
        0,
        "no file fallback"
    );
}
