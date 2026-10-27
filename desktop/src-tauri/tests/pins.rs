//! Trusted certificates are kept per `host:port`, can be reviewed, and forgetting one ends the
//! login that rested on it.

use rfe_desktop_lib::agent_client::AgentError;
use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::secrets::{account, MemoryStore, Offloaded, SecretStore};
use tempfile::TempDir;

const FP_A: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const FP_B: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

fn session(host: &str, fingerprint: &str) -> Saved {
    Saved {
        host: host.into(),
        fingerprint: fingerprint.into(),
        token: format!("token-for-{host}"),
        username: "owner".into(),
        device_id: "d1".into(),
    }
}

fn hosts(dir: &std::path::Path) -> Vec<(String, String, bool)> {
    flows::list_pins(dir)
        .unwrap()
        .into_iter()
        .map(|p| (p.host, p.fingerprint, p.active))
        .collect()
}

#[test]
fn the_same_host_on_two_ports_keeps_two_pins() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::save(dir.path(), &store, &session("10.0.0.5:8765", FP_A)).unwrap();
    flows::save(dir.path(), &store, &session("10.0.0.5:9000", FP_B)).unwrap();

    assert_eq!(
        hosts(dir.path()),
        vec![
            ("10.0.0.5:8765".to_string(), FP_A.to_string(), false),
            ("10.0.0.5:9000".to_string(), FP_B.to_string(), true),
        ],
        "signing in to the second agent must not overwrite the first agent's pin"
    );
}

#[test]
fn a_host_name_is_one_pin_whatever_its_case() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::save(dir.path(), &store, &session("Pc.Local:8765", FP_A)).unwrap();
    flows::save(dir.path(), &store, &session("pc.local:8765", FP_B)).unwrap();
    assert_eq!(
        hosts(dir.path()),
        vec![("pc.local:8765".to_string(), FP_B.to_string(), true)]
    );
}

#[test]
fn forgetting_one_pin_leaves_the_other_pin_and_the_login() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::save(dir.path(), &store, &session("10.0.0.5:8765", FP_A)).unwrap();
    flows::save(dir.path(), &store, &session("10.0.0.5:9000", FP_B)).unwrap();

    let out = flows::forget_pin(dir.path(), &store, "10.0.0.5:8765").unwrap();
    assert!(out.was_pinned && !out.signed_out, "{out:?}");
    assert_eq!(hosts(dir.path()).len(), 1);
    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(saved.host, "10.0.0.5:9000");
    assert_eq!(saved.token, "token-for-10.0.0.5:9000");
}

/// Negative control for the test above: forgetting the pin of the agent in use does sign out.
#[test]
fn forgetting_the_pin_of_the_agent_in_use_clears_the_login_too() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::save(dir.path(), &store, &session("10.0.0.5:9000", FP_B)).unwrap();

    let out = flows::forget_pin(dir.path(), &store, "10.0.0.5:9000").unwrap();
    assert!(out.was_pinned && out.signed_out, "{out:?}");
    assert!(hosts(dir.path()).is_empty());
    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert!(saved.token.is_empty() && saved.host.is_empty(), "{saved:?}");
    assert_eq!(store.get(&account("token", dir.path())).unwrap(), None);
}

#[test]
fn forgetting_a_host_that_was_never_trusted_changes_nothing() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    let out = flows::forget_pin(dir.path(), &store, "10.0.0.9:1").unwrap();
    assert!(!out.was_pinned && !out.signed_out);
    assert!(
        !dir.path().join("state.json").exists(),
        "no state file is created for a no-op"
    );
}

#[test]
fn a_state_file_from_before_pins_keeps_its_one_trusted_agent_and_forgetting_sticks() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    std::fs::write(
        dir.path().join("state.json"),
        format!(r#"{{"host":"10.0.0.5:8765","fingerprint":"{FP_A}","username":"owner","device_id":"d1"}}"#),
    )
    .unwrap();
    assert_eq!(
        hosts(dir.path()),
        vec![("10.0.0.5:8765".to_string(), FP_A.to_string(), true)]
    );

    flows::forget_pin(dir.path(), &store, "10.0.0.5:8765").unwrap();
    assert!(
        hosts(dir.path()).is_empty(),
        "a forgotten legacy pin must not come back from the old host/fingerprint fields"
    );
}

#[tokio::test]
async fn a_session_whose_pin_is_gone_is_refused_before_any_network_traffic() {
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());
    // state.json says "signed in to 127.0.0.1:1" but the pin map does not list it.
    std::fs::write(
        dir.path().join("state.json"),
        r#"{"host":"127.0.0.1:1","fingerprint":"","username":"owner","device_id":"d1","pins":{}}"#,
    )
    .unwrap();
    store.set(&account("token", dir.path()), "a-token").unwrap();
    let err = flows::list_devices(dir.path(), &store).await.unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m.contains("no longer a trusted agent")),
        "{err:?}"
    );
}

/// Negative control: with the pin present the same session gets as far as the network.
#[tokio::test]
async fn control_a_session_with_its_pin_reaches_the_network() {
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());
    std::fs::write(
        dir.path().join("state.json"),
        format!(
            r#"{{"host":"127.0.0.1:1","fingerprint":"{FP_A}","username":"owner","device_id":"d1","pins":{{"127.0.0.1:1":"{FP_A}"}}}}"#
        ),
    )
    .unwrap();
    store.set(&account("token", dir.path()), "a-token").unwrap();
    let err = flows::list_devices(dir.path(), &store).await.unwrap_err();
    assert!(matches!(err, AgentError::Network(_)), "{err:?}");
}
