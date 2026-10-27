//! A token the agent no longer accepts ends the session without losing the pin or the device
//! key, and the key is only ever replaced when the user asks. Run against a throwaway agent.

mod common;

use common::{free_port, Agent};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentError};
use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::identity::Identity;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use tempfile::TempDir;

const PW: &str = "pw-for-recovery-tests";

struct Setup {
    agent: Agent,
    fp: String,
    dir: TempDir,
    store: Offloaded,
    saved: Saved,
}

async fn signed_in() -> Setup {
    let agent = Agent::start(free_port());
    agent.add_user("owner", PW);
    let fp = capture_fingerprint(&agent.host).await.unwrap();
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let saved = flows::login(dir.path(), &agent.host, &fp, "owner", PW, "Desktop", &store)
        .await
        .unwrap();
    Setup {
        agent,
        fp,
        dir,
        store,
        saved,
    }
}

fn device_key_id(s: &Setup) -> String {
    Identity::load_or_create(s.dir.path(), &s.store)
        .unwrap()
        .device_id()
        .to_string()
}

fn assert_session_ended_but_trust_kept(s: &Setup, key_before: &str) {
    let after = flows::load_saved(s.dir.path(), &s.store).unwrap();
    assert!(after.token.is_empty(), "the dead token was kept");
    assert_eq!(after.host, s.agent.host, "the host was forgotten");
    assert_eq!(after.username, "owner", "the account name was forgotten");
    assert_eq!(
        flows::list_pins(s.dir.path()).unwrap().len(),
        1,
        "the pin was lost"
    );
    assert_eq!(device_key_id(s), key_before, "the device key changed");
}

#[tokio::test]
async fn a_revoked_device_ends_the_session_and_keeps_the_pin_name_and_key() {
    let s = signed_in().await;
    let key = device_key_id(&s);
    flows::list_devices(s.dir.path(), &s.store)
        .await
        .expect("control: the token works before the owner revokes it");
    assert!(!flows::load_saved(s.dir.path(), &s.store)
        .unwrap()
        .token
        .is_empty());

    s.agent.revoke_cli(&s.saved.device_id);
    let err = flows::list_devices(s.dir.path(), &s.store)
        .await
        .unwrap_err();
    assert!(
        matches!(err, AgentError::Server { status: 401, .. }),
        "{err:?}"
    );
    assert_session_ended_but_trust_kept(&s, &key);

    // Signing in again with the same key works.
    let again = flows::login(
        s.dir.path(),
        &s.agent.host,
        &s.fp,
        "owner",
        PW,
        "Desktop",
        &s.store,
    )
    .await
    .expect("sign in again after the revoke");
    assert!(!again.token.is_empty());
    assert_eq!(device_key_id(&s), key);
}

#[tokio::test]
async fn a_removed_device_ends_the_session_the_same_way() {
    let s = signed_in().await;
    let key = device_key_id(&s);
    s.agent.remove_cli(&s.saved.device_id);
    let err = flows::list_devices(s.dir.path(), &s.store)
        .await
        .unwrap_err();
    assert!(
        matches!(err, AgentError::Server { status: 401, .. }),
        "{err:?}"
    );
    assert_session_ended_but_trust_kept(&s, &key);
}

/// Negative control: an agent that is merely unreachable says nothing about the token.
#[tokio::test]
async fn an_unreachable_agent_does_not_end_the_session() {
    let s = signed_in().await;
    let token = s.saved.token.clone();
    let Setup {
        agent,
        fp: _,
        dir,
        store,
        saved: _,
    } = s;
    drop(agent.stop());

    let err = flows::list_devices(dir.path(), &store).await.unwrap_err();
    assert!(matches!(err, AgentError::Network(_)), "{err:?}");
    assert_eq!(flows::load_saved(dir.path(), &store).unwrap().token, token);
}

#[tokio::test]
async fn a_refused_sign_in_never_replaces_the_device_key() {
    let agent = Agent::start(free_port());
    agent.add_user("owner", PW);
    let fp = capture_fingerprint(&agent.host).await.unwrap();
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let before = Identity::load_or_create(dir.path(), &store)
        .unwrap()
        .device_id()
        .to_string();

    let err = flows::login(dir.path(), &agent.host, &fp, "owner", "wrong", "D", &store)
        .await
        .unwrap_err();
    assert!(matches!(&err, AgentError::Server { code, .. } if code == "INVALID_CREDENTIALS"));
    let after = Identity::load_or_create(dir.path(), &store)
        .unwrap()
        .device_id()
        .to_string();
    assert_eq!(before, after, "a refused sign-in must leave the key alone");
}

#[tokio::test]
async fn resetting_the_device_key_is_refused_while_signed_in_and_enrolls_anew_after() {
    let s = signed_in().await;
    let old_key = device_key_id(&s);

    let err = flows::reset_device_key(s.dir.path(), &s.store).unwrap_err();
    assert!(err.contains("Sign out first"), "{err}");
    assert_eq!(device_key_id(&s), old_key, "a refused reset kept the key");

    flows::sign_out_and_revoke(s.dir.path(), &s.store)
        .await
        .unwrap();
    flows::reset_device_key(s.dir.path(), &s.store).unwrap();
    let again = flows::login(
        s.dir.path(),
        &s.agent.host,
        &s.fp,
        "owner",
        PW,
        "Desktop",
        &s.store,
    )
    .await
    .unwrap();
    let new_key = device_key_id(&s);
    assert_ne!(new_key, old_key, "the reset made a new key and device id");
    assert_ne!(
        again.device_id, s.saved.device_id,
        "a new device row was enrolled"
    );
    let rows = s.agent.devices_cli();
    assert_eq!(
        rows.matches("Desktop").count(),
        2,
        "old row stays until removed: {rows}"
    );
}

#[tokio::test]
async fn resetting_also_removes_a_leftover_plaintext_identity_file() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    std::fs::write(
        dir.path().join("identity.json"),
        r#"{"device_id":"desktop-old","private_key":"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="}"#,
    )
    .unwrap();
    flows::reset_device_key(dir.path(), &store).unwrap();
    assert!(!dir.path().join("identity.json").exists());
    let fresh = Identity::load_or_create(dir.path(), &store).unwrap();
    assert_ne!(fresh.device_id(), "desktop-old");
}
