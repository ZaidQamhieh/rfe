//! Spike and acceptance tests for the first slice, run against throwaway
//! agents (explicit -addr/-data/-roots, random loopback port, temp dirs).
//! Set RFE_AGENT_BIN to a built `rfe` binary.

use common::{free_port, Agent};
use rfe_desktop_lib::agent_client::{
    capture_fingerprint, normalize_fingerprint, validate_hostport, AgentClient, AgentError,
};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::identity::Identity;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded, UnavailableStore};
use tempfile::TempDir;

mod common;

#[tokio::test]
async fn captured_fingerprint_matches_the_agents_own_report() {
    let a = Agent::start(free_port());
    let seen = capture_fingerprint(&a.host).await.unwrap();
    assert_eq!(seen.len(), 64);
    assert_eq!(seen, a.status_fingerprint());
}

#[tokio::test]
async fn pinned_login_lists_the_full_device_list() {
    let store = Offloaded::new(MemoryStore::default());
    let a = Agent::start(free_port());
    a.add_user("owner", "correct horse battery");
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let state = TempDir::new().unwrap();

    let saved = flows::login(
        state.path(),
        &a.host,
        &fp,
        "owner",
        "correct horse battery",
        "Desktop A",
        &store,
    )
    .await
    .expect("login through the pin");
    assert!(!saved.token.is_empty());

    // A second login device (different identity) so "full list" is not just "me".
    let other = TempDir::new().unwrap();
    flows::login(
        other.path(),
        &a.host,
        &fp,
        "owner",
        "correct horse battery",
        "Desktop B",
        &store,
    )
    .await
    .unwrap();

    // The agent assigns the device row id; the client-sent id is its separate client id.
    assert!(Identity::load_or_create(state.path(), &store)
        .unwrap()
        .device_id()
        .starts_with("desktop-"));
    let list = flows::list_devices(state.path(), &store).await.unwrap();
    assert_eq!(
        list.len(),
        2,
        "admin session must see both devices: {list:?}"
    );
    let current: Vec<_> = list.iter().filter(|d| d.current).collect();
    assert_eq!(current.len(), 1);
    assert_eq!(current[0].id, saved.device_id);
    assert!(!saved.device_id.is_empty());
    assert!(list.iter().all(|d| d.via_login));
    assert!(list.iter().any(|d| d.label == "Desktop B"));
    assert!(
        a.devices_cli().contains("Desktop B"),
        "CLI ground truth agrees"
    );
}

#[tokio::test]
async fn wrong_pin_is_refused_and_sends_no_credentials() {
    let store = Offloaded::new(MemoryStore::default());
    let a = Agent::start(free_port());
    a.add_user("owner", "pw-for-wrong-pin-test");
    let state = TempDir::new().unwrap();
    let wrong = "0".repeat(64);

    let err = flows::login(
        state.path(),
        &a.host,
        &wrong,
        "owner",
        "pw-for-wrong-pin-test",
        "X",
        &store,
    )
    .await
    .expect_err("a wrong pin must fail");
    assert!(
        matches!(err, AgentError::Network(_)),
        "expected a TLS failure, got {err:?}"
    );
    assert!(err.to_string().contains("fingerprint mismatch"), "{err}");
    // The window shows this text: it must say what to do, not only that TLS failed.
    assert!(err.to_string().contains("not the one you trusted"), "{err}");

    // Nothing reached the server: no device row and no login/failed-login audit entry.
    assert!(
        a.devices_cli().contains("No paired devices"),
        "{}",
        a.devices_cli()
    );
    let audit = a.audit_cli();
    assert!(
        !audit.contains("login"),
        "agent saw a login attempt: {audit}"
    );

    // Negative control: the same agent and credentials succeed with the right pin.
    let fp = capture_fingerprint(&a.host).await.unwrap();
    flows::login(
        state.path(),
        &a.host,
        &fp,
        "owner",
        "pw-for-wrong-pin-test",
        "X",
        &store,
    )
    .await
    .expect("right pin must work");
    assert!(a.audit_cli().contains("login"));
}

#[tokio::test]
async fn a_changed_certificate_after_pairing_is_refused() {
    let store = Offloaded::new(MemoryStore::default());
    let port = free_port();
    let first = Agent::start(port);
    first.add_user("owner", "pw-for-cert-change");
    let fp1 = capture_fingerprint(&first.host).await.unwrap();
    let state = TempDir::new().unwrap();
    flows::login(
        state.path(),
        &first.host,
        &fp1,
        "owner",
        "pw-for-cert-change",
        "Desktop",
        &store,
    )
    .await
    .unwrap();
    assert_eq!(
        flows::list_devices(state.path(), &store)
            .await
            .unwrap()
            .len(),
        1
    );
    let same = flows::probe(state.path(), &first.host).await.unwrap();
    assert!(
        !same.changed && same.previous == fp1,
        "unchanged cert must not warn: {same:?}"
    );
    let _old_data = first.stop();

    // Same address, new data dir = new certificate (an impostor or a reinstalled agent).
    let second = Agent::start(port);
    second.add_user("owner", "pw-for-cert-change");
    let fp2 = capture_fingerprint(&second.host).await.unwrap();
    assert_ne!(
        fp1, fp2,
        "control: the second agent really has a different certificate"
    );

    let probe = flows::probe(state.path(), &second.host).await.unwrap();
    assert!(
        probe.changed,
        "a different cert at the same address must be flagged"
    );
    assert_eq!(probe.previous, fp1);
    assert_eq!(probe.fingerprint, fp2);

    let err = flows::list_devices(state.path(), &store)
        .await
        .expect_err("saved pin must refuse the new cert");
    assert!(matches!(err, AgentError::Network(_)), "{err:?}");
    let err = flows::login(
        state.path(),
        &second.host,
        &fp1,
        "owner",
        "pw-for-cert-change",
        "Desktop",
        &store,
    )
    .await
    .expect_err("login must refuse the new cert");
    assert!(matches!(err, AgentError::Network(_)), "{err:?}");
    assert!(second.devices_cli().contains("No paired devices"));
    assert!(!second.audit_cli().contains("login"));
}

#[tokio::test]
async fn a_missing_or_locked_keystore_stops_login_before_any_network_traffic() {
    let a = Agent::start(free_port());
    a.add_user("owner", "pw-for-keystore-test");
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let state = TempDir::new().unwrap();

    let err = flows::login(
        state.path(),
        &a.host,
        &fp,
        "owner",
        "pw-for-keystore-test",
        "X",
        &Offloaded::new(UnavailableStore),
    )
    .await
    .expect_err("login must fail without a keystore");
    assert!(matches!(err, AgentError::Local(_)), "{err:?}");
    assert!(err.to_string().contains("OS keystore"), "{err}");
    assert!(
        a.devices_cli().contains("No paired devices"),
        "no device was enrolled"
    );
    assert!(
        !a.audit_cli().contains("login"),
        "the agent saw a login attempt"
    );
    let files: Vec<_> = std::fs::read_dir(state.path()).unwrap().collect();
    assert!(files.is_empty(), "no file fallback was written: {files:?}");

    // Negative control: the same agent and credentials succeed with a working keystore.
    let store = Offloaded::new(MemoryStore::default());
    flows::login(
        state.path(),
        &a.host,
        &fp,
        "owner",
        "pw-for-keystore-test",
        "X",
        &store,
    )
    .await
    .expect("a working keystore must succeed");
    assert!(a.audit_cli().contains("login"));
    // And a locked keystore also blocks reading the saved token afterwards.
    let err = flows::list_devices(state.path(), &Offloaded::new(UnavailableStore))
        .await
        .expect_err("a locked keystore must not yield a token");
    assert!(err.to_string().contains("OS keystore"), "{err}");
    assert_eq!(
        flows::list_devices(state.path(), &store)
            .await
            .unwrap()
            .len(),
        1
    );
}

#[tokio::test]
async fn wrong_password_surfaces_the_agents_error_code() {
    let store = Offloaded::new(MemoryStore::default());
    let a = Agent::start(free_port());
    a.add_user("owner", "the-right-one");
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let state = TempDir::new().unwrap();
    let err = flows::login(state.path(), &a.host, &fp, "owner", "not-it", "X", &store)
        .await
        .unwrap_err();
    match err {
        AgentError::Server { status, code, .. } => {
            assert_eq!(status, 401);
            assert_eq!(code, "INVALID_CREDENTIALS");
        }
        other => panic!("expected a server error, got {other:?}"),
    }
}

#[test]
fn host_and_fingerprint_inputs_are_validated() {
    for bad in [
        "evil.com/path:1",
        "user@host:8765",
        "https://h:1",
        "host",
        "host:99999",
        ":8765",
        "::1:8765",
        "a:b:c:80",
        "127.0.0.1:0",
        "[::1:8765",
        "[]:8765",
        "h[1]:80",
    ] {
        assert!(validate_hostport(bad).is_err(), "{bad} should be rejected");
    }
    assert!(validate_hostport("127.0.0.1:8765").is_ok());
    assert!(validate_hostport("[::1]:8765").is_ok());
    assert!(validate_hostport("my-pc.local:8765").is_ok());
    assert_eq!(normalize_fingerprint("AB:cd ef"), "abcdef");
    assert!(AgentClient::pinned("127.0.0.1:8765", "abc").is_err());
}

#[test]
fn damaged_state_and_identity_files_are_errors_not_resets() {
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    std::fs::write(dir.path().join("state.json"), b"{not json").unwrap();
    assert!(flows::load_saved(dir.path(), &store).is_err());
    assert_eq!(
        std::fs::read(dir.path().join("state.json")).unwrap(),
        b"{not json"
    );
    // Control: a missing file is simply "not configured".
    assert!(flows::load_saved(TempDir::new().unwrap().path(), &store)
        .unwrap()
        .host
        .is_empty());

    // An unreadable identity (a directory where the file should be) must not mint a new key.
    let idir = TempDir::new().unwrap();
    std::fs::create_dir(idir.path().join("identity.json")).unwrap();
    assert!(Identity::load_or_create(idir.path(), &store).is_err());
    assert!(
        idir.path().join("identity.json").is_dir(),
        "the existing entry was left alone"
    );
    // Control: a fresh dir creates one, and loading it again returns the same device id.
    let fresh = TempDir::new().unwrap();
    let first = Identity::load_or_create(fresh.path(), &store).unwrap();
    let again = Identity::load_or_create(fresh.path(), &store).unwrap();
    assert_eq!(first.device_id(), again.device_id());
    assert_eq!(first.public_key_b64(), again.public_key_b64());
}

#[test]
fn private_writes_replace_atomically_stay_0600_and_ignore_loose_leftovers() {
    let store = Offloaded::new(MemoryStore::default());
    use rfe_desktop_lib::flows::{load_saved, save, Saved};
    let dir = TempDir::new().unwrap();
    // A loose-permission file at the old fixed temp name must not be reused.
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let stale = dir.path().join("state.tmp");
        std::fs::write(&stale, b"old").unwrap();
        std::fs::set_permissions(&stale, std::fs::Permissions::from_mode(0o644)).unwrap();
    }
    let s = Saved {
        host: "h:1".into(),
        token: "secret".into(),
        ..Default::default()
    };
    save(dir.path(), &store, &s).unwrap();
    save(dir.path(), &store, &s).unwrap();
    assert_eq!(load_saved(dir.path(), &store).unwrap().token, "secret");
    let on_disk = std::fs::read_to_string(dir.path().join("state.json")).unwrap();
    assert!(
        !on_disk.contains("secret") && !on_disk.contains("token"),
        "the token must not be written to state.json: {on_disk}"
    );
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = std::fs::metadata(dir.path().join("state.json"))
            .unwrap()
            .permissions()
            .mode()
            & 0o777;
        assert_eq!(mode, 0o600, "state.json must be owner-only");
        let leftovers: Vec<_> = std::fs::read_dir(dir.path())
            .unwrap()
            .filter_map(|e| e.ok())
            .map(|e| e.file_name().to_string_lossy().to_string())
            .filter(|n| n.contains(".tmp."))
            .collect();
        assert!(
            leftovers.is_empty(),
            "temp files left behind: {leftovers:?}"
        );
    }
}

const SIGN_OUT_PW: &str = "pw-for-sign-out-test";

#[tokio::test]
async fn sign_out_revokes_the_token_on_the_agent() {
    let store = Offloaded::new(MemoryStore::default());
    let a = Agent::start(free_port());
    a.add_user("owner", SIGN_OUT_PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let state = TempDir::new().unwrap();
    let saved = flows::login(
        state.path(),
        &a.host,
        &fp,
        "owner",
        SIGN_OUT_PW,
        "Desktop",
        &store,
    )
    .await
    .unwrap();
    let client = AgentClient::pinned(&a.host, &fp).unwrap();
    client
        .devices(&saved.token)
        .await
        .expect("the token works before sign-out");

    let out = flows::sign_out_and_revoke(state.path(), &store)
        .await
        .unwrap();
    assert!(out.revoked && out.note.is_empty(), "{out:?}");

    match client.devices(&saved.token).await {
        Err(AgentError::Server { status: 401, .. }) => {}
        other => panic!("the old token must be rejected with 401, got {other:?}"),
    }
    assert!(flows::load_saved(state.path(), &store)
        .unwrap()
        .token
        .is_empty());
    assert!(
        a.devices_cli().to_lowercase().contains("revoked"),
        "the CLI shows the device as revoked: {}",
        a.devices_cli()
    );
}

/// Negative control: clearing only the local copy, as sign-out did before, leaves the token valid.
#[tokio::test]
async fn control_a_local_only_sign_out_leaves_the_token_valid_on_the_agent() {
    let store = Offloaded::new(MemoryStore::default());
    let a = Agent::start(free_port());
    a.add_user("owner", SIGN_OUT_PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let state = TempDir::new().unwrap();
    let saved = flows::login(
        state.path(),
        &a.host,
        &fp,
        "owner",
        SIGN_OUT_PW,
        "Desktop",
        &store,
    )
    .await
    .unwrap();

    flows::sign_out(state.path(), &store).unwrap();

    AgentClient::pinned(&a.host, &fp)
        .unwrap()
        .devices(&saved.token)
        .await
        .expect("a local-only sign-out does not revoke anything");
}

#[tokio::test]
async fn sign_out_clears_the_local_login_even_when_the_agent_is_unreachable() {
    let store = Offloaded::new(MemoryStore::default());
    let a = Agent::start(free_port());
    a.add_user("owner", SIGN_OUT_PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let state = TempDir::new().unwrap();
    flows::login(
        state.path(),
        &a.host,
        &fp,
        "owner",
        SIGN_OUT_PW,
        "Desktop",
        &store,
    )
    .await
    .unwrap();
    drop(a.stop()); // the agent goes away

    let out = flows::sign_out_and_revoke(state.path(), &store)
        .await
        .unwrap();
    assert!(!out.revoked);
    assert!(
        out.note.contains("could not be told") && out.note.contains("rfe revoke"),
        "{}",
        out.note
    );
    assert!(flows::load_saved(state.path(), &store)
        .unwrap()
        .token
        .is_empty());
}

#[tokio::test]
async fn signing_in_again_after_a_revoking_sign_out_works() {
    let store = Offloaded::new(MemoryStore::default());
    let a = Agent::start(free_port());
    a.add_user("owner", SIGN_OUT_PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let state = TempDir::new().unwrap();
    let login = || {
        flows::login(
            state.path(),
            &a.host,
            &fp,
            "owner",
            SIGN_OUT_PW,
            "Desktop",
            &store,
        )
    };
    let first = login().await.unwrap();
    flows::sign_out_and_revoke(state.path(), &store)
        .await
        .unwrap();

    let second = login()
        .await
        .expect("the same computer can sign in again after sign-out");
    assert_ne!(first.token, second.token);
    let list = flows::list_devices(state.path(), &store).await.unwrap();
    assert!(
        list.iter().any(|d| d.current && !d.revoked),
        "the new login is an active device: {list:?}"
    );
}

#[tokio::test]
async fn each_agent_keeps_its_own_pin_and_a_changed_certificate_is_flagged_for_the_right_one() {
    let store = Offloaded::new(MemoryStore::default());
    let port_a = free_port();
    let a = Agent::start(port_a);
    let b = Agent::start(free_port());
    a.add_user("owner", "pw-for-two-agents");
    b.add_user("owner", "pw-for-two-agents");
    let fp_a = capture_fingerprint(&a.host).await.unwrap();
    let fp_b = capture_fingerprint(&b.host).await.unwrap();
    let state = TempDir::new().unwrap();
    for (host, fp) in [(&a.host, &fp_a), (&b.host, &fp_b)] {
        flows::login(
            state.path(),
            host,
            fp,
            "owner",
            "pw-for-two-agents",
            "Desktop",
            &store,
        )
        .await
        .unwrap();
    }

    // The session now belongs to B, yet A is still a trusted address with its own pin.
    let probe_a = flows::probe(state.path(), &a.host).await.unwrap();
    assert!(
        !probe_a.changed && probe_a.previous == fp_a,
        "the first agent's pin was lost: {probe_a:?}"
    );

    let _old = a.stop();
    let impostor = Agent::start(port_a);
    let probe_a = flows::probe(state.path(), &impostor.host).await.unwrap();
    assert!(
        probe_a.changed && probe_a.previous == fp_a,
        "a new certificate at A's address must be flagged: {probe_a:?}"
    );
    let probe_b = flows::probe(state.path(), &b.host).await.unwrap();
    assert!(
        !probe_b.changed && probe_b.previous == fp_b,
        "control: B's own pin is untouched: {probe_b:?}"
    );
}
