//! Pairing with a one-time code: an ordinary (non-admin) device, a code that is spent once, and
//! failures that must not burn a valid code. Run against a throwaway agent.

mod common;

use common::{free_port, identity, Agent, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentClient, AgentError};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded, UnavailableStore};
use serde_json::json;
use tempfile::TempDir;

fn code_of(e: &AgentError) -> &str {
    match e {
        AgentError::Server { code, .. } => code,
        other => panic!("not a server error: {other:?}"),
    }
}

async fn pair(
    a: &Agent,
    fp: &str,
    dir: &TempDir,
    code: &str,
    store: &Offloaded,
) -> Result<flows::Saved, AgentError> {
    flows::pair(dir.path(), &a.host, fp, code, "Desktop (code)", store).await
}

#[tokio::test]
async fn pairing_with_a_code_enrolls_an_ordinary_device_that_sees_only_itself() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();

    let saved = pair(&a, &fp, &dir, &a.pair_code(), &store).await.unwrap();
    assert!(!saved.token.is_empty() && !saved.device_id.is_empty());
    assert!(saved.username.is_empty(), "a code has no account name");

    // The token and pin are in the keystore and state, as for a password login.
    let again = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(again.token, saved.token);
    assert_eq!(flows::list_pins(dir.path()).unwrap().len(), 1);

    let list = flows::list_devices(dir.path(), &store).await.unwrap();
    assert_eq!(
        list.len(),
        1,
        "an ordinary device lists only itself: {list:?}"
    );
    assert!(list[0].current && !list[0].via_login, "{list:?}");
    assert!(a.devices_cli().contains("Desktop (code)"));
}

#[tokio::test]
async fn a_used_code_is_refused_the_second_time() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let code = a.pair_code();

    pair(&a, &fp, &TempDir::new().unwrap(), &code, &store)
        .await
        .expect("control: the first use works");
    let err = pair(&a, &fp, &TempDir::new().unwrap(), &code, &store)
        .await
        .unwrap_err();
    assert_eq!(code_of(&err), "INVALID_CODE", "{err:?}");
    assert!(err.to_string().contains("already used"), "{err}");
}

#[tokio::test]
async fn a_wrong_code_does_not_burn_the_valid_one() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    let code = a.pair_code();

    let err = pair(&a, &fp, &dir, "WRONGCODE", &store).await.unwrap_err();
    assert_eq!(code_of(&err), "INVALID_CODE");
    assert!(
        flows::load_saved(dir.path(), &store)
            .unwrap()
            .token
            .is_empty(),
        "a refused code stores nothing"
    );
    pair(&a, &fp, &dir, &code, &store)
        .await
        .expect("the valid code still works after a wrong guess");
}

#[tokio::test]
async fn a_bad_device_proof_does_not_burn_the_valid_code() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let code = a.pair_code();
    let raw = Raw::new(&a.host);
    let (id, _d) = identity();

    // The right code, but the signature is over something other than the nonce.
    let n = raw.nonce().await;
    let e = raw
        .refused(
            "/pair",
            json!({
                "pairingCode": code, "deviceLabel": "T", "deviceId": id.device_id(),
                "devicePublicKey": id.public_key_b64(), "nonce": n, "signature": id.sign_b64("other"),
            }),
        )
        .await;
    assert_eq!(code_of(&e), "INVALID_SIGNATURE");

    pair(&a, &fp, &TempDir::new().unwrap(), &code, &store)
        .await
        .expect("the code was not spent by the refused proof");
}

#[tokio::test]
async fn a_locked_keystore_stops_pairing_before_the_code_is_sent() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let code = a.pair_code();
    let dir = TempDir::new().unwrap();

    let err = pair(&a, &fp, &dir, &code, &Offloaded::new(UnavailableStore))
        .await
        .unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m.contains("OS keystore")),
        "{err:?}"
    );
    // Control: the code reached no one, so it still works with a usable keystore.
    pair(
        &a,
        &fp,
        &dir,
        &code,
        &Offloaded::new(MemoryStore::default()),
    )
    .await
    .expect("the code was not spent by the failed attempt");
}

#[tokio::test]
async fn signing_out_of_a_code_pairing_revokes_it_and_an_empty_code_never_leaves_the_app() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();

    let err = pair(&a, &fp, &dir, "   ", &store).await.unwrap_err();
    assert!(matches!(&err, AgentError::Local(m) if m.contains("enter the pairing code")));

    let saved = pair(&a, &fp, &dir, &a.pair_code(), &store).await.unwrap();
    let out = flows::sign_out_and_revoke(dir.path(), &store)
        .await
        .unwrap();
    assert!(out.revoked, "{out:?}");
    match AgentClient::pinned(&a.host, &fp)
        .unwrap()
        .devices(&saved.token)
        .await
    {
        Err(AgentError::Server { status: 401, .. }) => {}
        other => panic!("the old token must be rejected, got {other:?}"),
    }
}

#[tokio::test]
async fn a_code_paired_device_is_told_plainly_that_admin_actions_are_not_for_it() {
    let a = Agent::start(free_port());
    a.add_user("owner", "pw-for-403-test");
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let raw = Raw::new(&a.host);

    // An ordinary device, paired with a code, asks for an admin-only action.
    let ordinary_dir = TempDir::new().unwrap();
    let ordinary = pair(&a, &fp, &ordinary_dir, &a.pair_code(), &store)
        .await
        .unwrap();
    let e = raw
        .refused_as(&ordinary.token, "/pairing/generate", json!({}))
        .await;
    assert_eq!(code_of(&e), "FORBIDDEN", "{e:?}");
    assert!(e.to_string().contains("Sign in with the account"), "{e}");

    // Control: an account session is allowed the same call, so the 403 was about the session.
    let admin_dir = TempDir::new().unwrap();
    let admin = flows::login(
        admin_dir.path(),
        &a.host,
        &fp,
        "owner",
        "pw-for-403-test",
        "Admin",
        &store,
    )
    .await
    .unwrap();
    let resp = raw
        .http
        .post(format!("{}/pairing/generate", raw.base))
        .bearer_auth(&admin.token)
        .json(&json!({}))
        .send()
        .await
        .unwrap();
    assert!(resp.status().is_success(), "{}", resp.status());

    // And the two sessions see different lists of the same agent.
    let seen_by_ordinary = flows::list_devices(ordinary_dir.path(), &store)
        .await
        .unwrap();
    let seen_by_admin = flows::list_devices(admin_dir.path(), &store).await.unwrap();
    assert_eq!(seen_by_ordinary.len(), 1, "{seen_by_ordinary:?}");
    assert_eq!(seen_by_admin.len(), 2, "{seen_by_admin:?}");
}
