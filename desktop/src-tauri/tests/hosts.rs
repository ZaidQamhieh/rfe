//! Saved hosts: migration from a single-agent `state.json`, a separate login per host, switching,
//! renaming, and removal that wipes the token and the pin. The in-memory keystore stands in for the
//! OS one, as in the other tests; the last test uses two real throwaway agents.

mod common;

#[path = "support/ui.rs"]
mod ui;
use common::{free_port, Agent};
use rfe_desktop_lib::agent_client::capture_fingerprint;
use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::hosts;
use rfe_desktop_lib::secrets::{account, MemoryStore, Offloaded, SecretStore};
use std::path::Path;
use tempfile::TempDir;

const FP_A: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const FP_B: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
const A: &str = "10.0.0.5:8765";
const B: &str = "10.0.0.6:8765";

fn session(host: &str, fingerprint: &str, user: &str) -> Saved {
    Saved {
        host: host.into(),
        fingerprint: fingerprint.into(),
        token: format!("token-for-{host}"),
        username: user.into(),
        device_id: format!("device-on-{host}"),
    }
}

/// Signs in to A, then to B, the way two enrollments in a row would.
fn two_hosts(dir: &Path, store: &MemoryStore) {
    flows::save(dir, store, &session(A, FP_A, "alice")).unwrap();
    flows::save(dir, store, &session(B, FP_B, "bob")).unwrap();
}

#[test]
fn a_state_file_from_before_saved_hosts_becomes_one_host_without_losing_anything() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    // The oldest shape: one agent, no pin map, and the token still in the file.
    let old = format!(
        r#"{{"host":"Office.Local:8765","fingerprint":"{FP_A}","username":"owner","device_id":"d1","token":"old-plain-token"}}"#
    );
    std::fs::write(dir.path().join("state.json"), &old).unwrap();

    let list = hosts::list(dir.path(), &store).unwrap();
    assert_eq!(list.len(), 1, "{list:?}");
    let h = &list[0];
    assert_eq!(h.key, "office.local:8765");
    assert_eq!(h.host, "Office.Local:8765");
    assert_eq!(
        h.name, "Office.Local:8765",
        "the address is the name until renamed"
    );
    assert_eq!(h.fingerprint, FP_A);
    assert_eq!(h.username, "owner");
    assert!(h.active && h.signed_in);

    // The session is intact and the old plaintext token went to the keystore.
    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(saved.host, "Office.Local:8765");
    assert_eq!(saved.token, "old-plain-token");
    let state = std::fs::read_to_string(dir.path().join("state.json")).unwrap();
    assert!(!state.contains("old-plain-token"), "{state}");
    assert!(state.contains(FP_A) && state.contains("owner") && state.contains("d1"));

    // hosts.json is versioned, holds no secret and is private to the user.
    let file = std::fs::read_to_string(dir.path().join("hosts.json")).unwrap();
    assert!(file.contains(r#""version": 1"#), "{file}");
    assert!(
        !file.contains("old-plain-token") && !file.contains("token"),
        "{file}"
    );
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = std::fs::metadata(dir.path().join("hosts.json"))
            .unwrap()
            .permissions()
            .mode();
        assert_eq!(mode & 0o777, 0o600);
    }
    // Reading again changes nothing.
    assert_eq!(hosts::list(dir.path(), &store).unwrap(), list);
    assert_eq!(
        std::fs::read_to_string(dir.path().join("hosts.json")).unwrap(),
        file
    );
}

#[test]
fn migration_also_lists_every_other_trusted_agent_and_a_fresh_install_has_none() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    assert!(hosts::list(dir.path(), &store).unwrap().is_empty());
    assert!(
        !dir.path().join("hosts.json").exists(),
        "nothing is written when there is nothing to save"
    );

    std::fs::write(
        dir.path().join("state.json"),
        format!(
            r#"{{"host":"{A}","fingerprint":"{FP_A}","username":"owner","device_id":"d1","pins":{{"{A}":"{FP_A}","{B}":"{FP_B}"}}}}"#
        ),
    )
    .unwrap();
    let list = hosts::list(dir.path(), &store).unwrap();
    let keys: Vec<(&str, bool, &str)> = list
        .iter()
        .map(|h| (h.key.as_str(), h.active, h.fingerprint.as_str()))
        .collect();
    assert_eq!(keys, vec![(A, true, FP_A), (B, false, FP_B)]);
    assert!(!list[1].signed_in, "B was only trusted, never signed in to");
}

#[test]
fn two_hosts_keep_separate_tokens_and_switching_moves_each_one() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);

    // B is the one in use; A's login was parked, not overwritten.
    let list = hosts::list(dir.path(), &store).unwrap();
    assert_eq!(list.len(), 2, "{list:?}");
    let a = list.iter().find(|h| h.key == A).unwrap();
    let b = list.iter().find(|h| h.key == B).unwrap();
    assert!(!a.active && a.signed_in && a.username == "alice" && a.fingerprint == FP_A);
    assert!(b.active && b.signed_in && b.username == "bob" && b.fingerprint == FP_B);
    assert_eq!(
        store
            .get(&hosts::parked_account(dir.path(), A))
            .unwrap()
            .as_deref(),
        Some(format!("token-for-{A}").as_str())
    );
    assert_eq!(
        store.get(&account("token", dir.path())).unwrap().as_deref(),
        Some(format!("token-for-{B}").as_str())
    );

    let now_a = hosts::switch(dir.path(), &store, A).unwrap();
    assert_eq!(now_a.host, A);
    assert_eq!(now_a.token, format!("token-for-{A}"));
    assert_eq!(now_a.fingerprint, FP_A);
    assert_eq!(now_a.username, "alice");
    assert_eq!(now_a.device_id, format!("device-on-{A}"));
    // What the rest of the app sees is now A's session, and B's token is parked.
    let seen = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(
        (seen.host.as_str(), seen.token.as_str()),
        (A, format!("token-for-{A}").as_str())
    );
    assert_eq!(
        store
            .get(&hosts::parked_account(dir.path(), B))
            .unwrap()
            .as_deref(),
        Some(format!("token-for-{B}").as_str())
    );
    assert_eq!(
        store.get(&hosts::parked_account(dir.path(), A)).unwrap(),
        None
    );

    let back = hosts::switch(dir.path(), &store, B).unwrap();
    assert_eq!(
        (back.username.as_str(), back.token.as_str()),
        ("bob", format!("token-for-{B}").as_str())
    );
    // Switching to the host already in use changes nothing.
    assert_eq!(
        hosts::switch(dir.path(), &store, B).unwrap().token,
        back.token
    );
}

#[test]
fn a_refusal_from_a_host_that_was_switched_away_from_drops_only_its_own_login() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store); // B is active, A is parked
    let tok_a = format!("token-for-{A}");
    let tok_b = format!("token-for-{B}");

    // A call to A was in flight when the user switched to B; A answers 401 now.
    flows::drop_refused_token(dir.path(), &store, &tok_a).unwrap();
    assert_eq!(
        flows::load_saved(dir.path(), &store).unwrap().token,
        tok_b,
        "the active host keeps its login"
    );
    assert!(store
        .get(&hosts::parked_account(dir.path(), A))
        .unwrap()
        .is_none());

    // A token nobody holds changes nothing, and neither does an empty one.
    flows::drop_refused_token(dir.path(), &store, "token-of-nobody").unwrap();
    flows::drop_refused_token(dir.path(), &store, "").unwrap();
    assert_eq!(flows::load_saved(dir.path(), &store).unwrap().token, tok_b);

    // The refused login is the active one: it goes, and a parked login of another host stays.
    let dir2 = TempDir::new().unwrap();
    let store2 = MemoryStore::default();
    two_hosts(dir2.path(), &store2);
    flows::drop_refused_token(dir2.path(), &store2, &tok_b).unwrap();
    assert!(flows::load_saved(dir2.path(), &store2)
        .unwrap()
        .token
        .is_empty());
    assert_eq!(
        store2
            .get(&hosts::parked_account(dir2.path(), A))
            .unwrap()
            .as_deref(),
        Some(tok_a.as_str())
    );
}

#[test]
fn a_device_key_reset_leaves_no_parked_login_of_the_old_key() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    // Signed in to B: refused as before.
    assert!(flows::reset_device_key(dir.path(), &store).is_err());
    assert!(store
        .get(&hosts::parked_account(dir.path(), A))
        .unwrap()
        .is_some());

    flows::sign_out(dir.path(), &store).unwrap();
    flows::reset_device_key(dir.path(), &store).unwrap();
    for key in [A, B] {
        assert!(
            store
                .get(&hosts::parked_account(dir.path(), key))
                .unwrap()
                .is_none(),
            "{key}"
        );
    }
    let list = hosts::list(dir.path(), &store).unwrap();
    assert!(list.iter().all(|h| !h.signed_in), "{list:?}");
}

#[test]
fn reading_the_session_while_hosts_switch_never_pairs_an_address_with_the_other_token() {
    let dir = TempDir::new().unwrap();
    let store = std::sync::Arc::new(MemoryStore::default());
    two_hosts(dir.path(), &store);
    let stop = std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false));
    let switcher = {
        let (dir, store, stop) = (dir.path().to_path_buf(), store.clone(), stop.clone());
        std::thread::spawn(move || {
            let mut n = 0;
            while !stop.load(std::sync::atomic::Ordering::Relaxed) && n < 400 {
                hosts::switch(&dir, &*store, if n % 2 == 0 { A } else { B }).unwrap();
                n += 1;
            }
        })
    };
    for _ in 0..2000 {
        let s = flows::load_saved(dir.path(), &*store).unwrap();
        assert_eq!(s.token, format!("token-for-{}", s.host), "{}", s.host);
    }
    stop.store(true, std::sync::atomic::Ordering::Relaxed);
    switcher.join().unwrap();
}

#[test]
fn only_a_session_with_a_login_is_marked_signed_in_in_the_trusted_list() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    let pins = flows::list_pins_with_login(dir.path(), &store).unwrap();
    assert_eq!(pins.iter().filter(|p| p.active).count(), 1, "{pins:?}");
    assert!(pins.iter().find(|p| p.active).unwrap().host == B);

    // Signed out: the address is still remembered, but nobody is signed in to it.
    flows::sign_out(dir.path(), &store).unwrap();
    let pins = flows::list_pins_with_login(dir.path(), &store).unwrap();
    assert_eq!(pins.len(), 2);
    assert!(pins.iter().all(|p| !p.active), "{pins:?}");
}

#[test]
fn the_app_opens_on_the_host_used_last() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    hosts::switch(dir.path(), &store, A).unwrap();
    // A new run reads only the files and the keystore.
    let opened = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(opened.host, A);
    assert_eq!(opened.token, format!("token-for-{A}"));
    assert_eq!(
        hosts::list(dir.path(), &store)
            .unwrap()
            .iter()
            .filter(|h| h.active)
            .count(),
        1
    );
}

#[test]
fn a_host_that_was_signed_out_stays_in_the_list_and_switches_without_a_token() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    // Signing out of B (the active one) clears its token only.
    flows::sign_out(dir.path(), &store).unwrap();
    hosts::switch(dir.path(), &store, A).unwrap();
    let list = hosts::list(dir.path(), &store).unwrap();
    let b = list.iter().find(|h| h.key == B).unwrap();
    assert!(!b.signed_in && b.username == "bob");
    let to_b = hosts::switch(dir.path(), &store, B).unwrap();
    assert!(to_b.token.is_empty(), "B has no login to restore");
    assert_eq!(to_b.username, "bob");
    // And A's login was parked on the way out.
    assert_eq!(
        hosts::switch(dir.path(), &store, A).unwrap().token,
        format!("token-for-{A}")
    );
}

#[test]
fn removing_a_parked_host_wipes_its_token_and_pin_and_leaves_the_other_host() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    assert!(store
        .get(&hosts::parked_account(dir.path(), A))
        .unwrap()
        .is_some());

    let out = hosts::remove(dir.path(), &store, A).unwrap();
    assert!(!out.was_active);
    assert_eq!(
        store.get(&hosts::parked_account(dir.path(), A)).unwrap(),
        None
    );
    let pins = flows::list_pins(dir.path()).unwrap();
    assert_eq!(pins.len(), 1);
    assert_eq!(pins[0].host, B);
    let list = hosts::list(dir.path(), &store).unwrap();
    assert_eq!(list.len(), 1);
    assert_eq!(list[0].key, B);
    // B's login is untouched.
    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(saved.token, format!("token-for-{B}"));
    // The removed host cannot be switched back to.
    let err = hosts::switch(dir.path(), &store, A).unwrap_err();
    assert!(err.contains("not in the saved list"), "{err}");
}

#[test]
fn removing_the_active_host_wipes_the_token_in_use_and_signs_out() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);

    let out = hosts::remove(dir.path(), &store, B).unwrap();
    assert!(out.was_active);
    assert_eq!(store.get(&account("token", dir.path())).unwrap(), None);
    assert_eq!(
        store.get(&hosts::parked_account(dir.path(), B)).unwrap(),
        None
    );
    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert!(saved.host.is_empty() && saved.token.is_empty(), "{saved:?}");
    let list = hosts::list(dir.path(), &store).unwrap();
    assert_eq!(
        list.iter().map(|h| h.key.as_str()).collect::<Vec<_>>(),
        vec![A]
    );
    assert!(
        list[0].signed_in && !list[0].active,
        "A keeps its parked login"
    );
}

#[test]
fn a_removed_host_leaves_no_secret_anywhere_in_the_data_folder() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    hosts::remove(dir.path(), &store, A).unwrap();
    for name in ["state.json", "hosts.json"] {
        let text = std::fs::read_to_string(dir.path().join(name)).unwrap();
        assert!(!text.contains("token-for-"), "{name}: {text}");
        assert!(
            !text.contains(FP_A),
            "{name} still has the removed pin: {text}"
        );
    }
}

#[test]
fn forgetting_a_pin_from_the_trusted_agents_list_also_wipes_a_parked_login() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    flows::forget_pin(dir.path(), &store, A).unwrap();
    assert_eq!(
        store.get(&hosts::parked_account(dir.path(), A)).unwrap(),
        None
    );
    // The stale entry leaves the list the next time it is read.
    let list = hosts::list(dir.path(), &store).unwrap();
    assert_eq!(
        list.iter().map(|h| h.key.as_str()).collect::<Vec<_>>(),
        vec![B]
    );
}

#[test]
fn a_host_whose_pin_was_forgotten_cannot_be_switched_to_with_its_old_login() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    // Forget B's pin while A is the active host: the record for B goes with it.
    hosts::switch(dir.path(), &store, A).unwrap();
    flows::forget_pin(dir.path(), &store, B).unwrap();
    let err = hosts::switch(dir.path(), &store, B).unwrap_err();
    assert!(err.contains("not in the saved list"), "{err}");
    assert_eq!(flows::load_saved(dir.path(), &store).unwrap().host, A);
}

#[test]
fn renaming_changes_only_the_display_name_and_survives_switching() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    hosts::rename(dir.path(), &store, A, "  Office PC ").unwrap();
    hosts::switch(dir.path(), &store, A).unwrap();
    hosts::switch(dir.path(), &store, B).unwrap();
    let list = hosts::list(dir.path(), &store).unwrap();
    let a = list.iter().find(|h| h.key == A).unwrap();
    assert_eq!(a.name, "Office PC");
    assert_eq!(a.host, A);
    assert_eq!(a.fingerprint, FP_A);
    assert!(a.signed_in);
    assert_eq!(list.iter().find(|h| h.key == B).unwrap().name, B);
}

#[test]
fn a_bad_name_or_an_unknown_host_is_refused_and_changes_nothing() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    hosts::list(dir.path(), &store).unwrap();
    let before = std::fs::read(dir.path().join("hosts.json")).unwrap();
    for (name, want) in [
        ("   ", "enter a name for the host"),
        (&"x".repeat(65), "at most 64 characters"),
        ("two\nlines", "control characters"),
    ] {
        let err = hosts::rename(dir.path(), &store, A, name).unwrap_err();
        assert!(err.contains(want), "{err}");
    }
    let err = hosts::rename(dir.path(), &store, "9.9.9.9:1", "x").unwrap_err();
    assert!(err.contains("not in the saved list"), "{err}");
    assert!(hosts::remove(dir.path(), &store, "9.9.9.9:1").is_err());
    assert!(hosts::switch(dir.path(), &store, "9.9.9.9:1").is_err());
    assert_eq!(
        std::fs::read(dir.path().join("hosts.json")).unwrap(),
        before
    );
}

#[test]
fn a_damaged_or_newer_hosts_file_is_an_error_and_is_never_overwritten() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);

    std::fs::write(dir.path().join("hosts.json"), "{ not json").unwrap();
    let err = hosts::list(dir.path(), &store).unwrap_err();
    assert!(
        err.contains("hosts.json is damaged") && err.contains("delete it to start over"),
        "{err}"
    );
    assert_eq!(
        std::fs::read_to_string(dir.path().join("hosts.json")).unwrap(),
        "{ not json"
    );

    let newer = r#"{"version":2,"hosts":[]}"#;
    std::fs::write(dir.path().join("hosts.json"), newer).unwrap();
    let err = hosts::switch(dir.path(), &store, A).unwrap_err();
    assert!(
        err.contains("written by a newer version of this app"),
        "{err}"
    );
    assert_eq!(
        std::fs::read_to_string(dir.path().join("hosts.json")).unwrap(),
        newer
    );
    // The login itself still works: only the list is unavailable.
    assert_eq!(flows::load_saved(dir.path(), &store).unwrap().host, B);

    // Deleting the file starts over from the trusted agents.
    std::fs::remove_file(dir.path().join("hosts.json")).unwrap();
    assert_eq!(hosts::list(dir.path(), &store).unwrap().len(), 2);
}

#[test]
fn signing_in_to_the_same_host_again_parks_nothing() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::save(dir.path(), &store, &session(A, FP_A, "alice")).unwrap();
    flows::save(dir.path(), &store, &session(A, FP_A, "alice")).unwrap();
    assert_eq!(
        store.get(&hosts::parked_account(dir.path(), A)).unwrap(),
        None
    );
    assert_eq!(hosts::list(dir.path(), &store).unwrap().len(), 1);
}

/// Two real agents, two real logins through the ordinary sign-in flow, each token used only against
/// its own agent.
#[tokio::test]
async fn two_real_agents_each_answer_only_to_their_own_login() {
    let a = Agent::start(free_port());
    let b = Agent::start(free_port());
    a.add_user("alice", "pw-alice-1");
    b.add_user("bob", "pw-bob-1");
    let fp_a = capture_fingerprint(&a.host).await.unwrap();
    let fp_b = capture_fingerprint(&b.host).await.unwrap();
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());

    flows::login(
        dir.path(),
        &a.host,
        &fp_a,
        "alice",
        "pw-alice-1",
        "Desktop",
        &store,
    )
    .await
    .unwrap();
    flows::login(
        dir.path(),
        &b.host,
        &fp_b,
        "bob",
        "pw-bob-1",
        "Desktop",
        &store,
    )
    .await
    .unwrap();

    // B is active.
    let on_b = flows::list_devices(dir.path(), &store).await.unwrap();
    assert!(on_b.iter().any(|d| d.current), "{on_b:?}");

    let to_a = hosts::switch(dir.path(), &store, &a.host).unwrap();
    assert_eq!(to_a.username, "alice");
    assert!(!to_a.token.is_empty());
    let on_a = flows::list_devices(dir.path(), &store).await.unwrap();
    assert!(on_a.iter().any(|d| d.current), "{on_a:?}");

    let to_b = hosts::switch(dir.path(), &store, &b.host).unwrap();
    assert_eq!(to_b.username, "bob");
    assert_ne!(to_a.token, to_b.token);
    assert!(flows::list_devices(dir.path(), &store).await.is_ok());

    // Removing A wipes its login here; B keeps working.
    hosts::remove(dir.path(), &store, &a.host).unwrap();
    assert!(flows::list_devices(dir.path(), &store).await.is_ok());
    assert_eq!(hosts::list(dir.path(), &store).unwrap().len(), 1);
}

/// The messages this feature adds are explained in the user guide, and are really in the source.
#[test]
fn the_new_messages_are_explained_in_the_guide() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let guide = std::fs::read_to_string(root.join("../docs/user-guide.md")).unwrap();
    let mut code = std::fs::read_to_string(root.join("src/hosts.rs")).unwrap();
    code.push_str(&ui::js());
    let html = std::fs::read_to_string(root.join("../ui/index.html")).unwrap();
    let flat: String = code
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .replace("\\ ", "")
        .replace("\" \"", "");
    for stem in [
        "this server is not in the saved list",
        "enter a name for the host",
        "the name can be at most",
        "the name cannot contain control characters",
        "was written by a newer version of this app",
        "Signed out on this server. Sign in to continue.",
        "Type the new server's address. Your current login stays saved.",
    ] {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
        assert!(
            flat.contains(stem) || code.contains(stem) || html.contains(stem),
            "{stem:?} is not in the source"
        );
    }
    assert!(guide.contains("`hosts.json` is damaged"));
    assert!(
        guide.contains("hosts.json"),
        "the kept-files table names hosts.json"
    );
}

#[test]
fn a_damaged_hosts_file_does_not_stop_a_new_sign_in_from_being_saved() {
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    two_hosts(dir.path(), &store);
    std::fs::write(dir.path().join("hosts.json"), "{ not json").unwrap();

    // The active agent is B; signing in to C must keep C's token even though A/B cannot be parked.
    let c = "c.example:8765";
    let mut saved = flows::load_saved(dir.path(), &store).unwrap();
    saved.host = c.into();
    saved.fingerprint = "cd".repeat(32);
    saved.token = "token-for-c".into();
    flows::save(dir.path(), &store, &saved).expect("the sign-in is saved");

    let now = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(now.host, c);
    assert_eq!(now.token, "token-for-c");
    assert_eq!(
        std::fs::read_to_string(dir.path().join("hosts.json")).unwrap(),
        "{ not json",
        "the damaged file is never overwritten"
    );
}

/// A state file that cannot be written must not leave the new login beside the old address.
#[cfg(unix)]
#[test]
fn a_failed_save_keeps_the_old_address_and_its_own_token() {
    use std::os::unix::fs::PermissionsExt;
    let dir = TempDir::new().unwrap();
    let store = MemoryStore::default();
    flows::save(dir.path(), &store, &session(A, FP_A, "alice")).unwrap();

    std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o500)).unwrap();
    let res = flows::save(dir.path(), &store, &session(B, FP_B, "bob"));
    std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700)).unwrap();

    assert!(res.is_err(), "writing into a read-only folder must fail");
    let now = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(now.host, A);
    assert_eq!(now.token, format!("token-for-{A}"), "{now:?}");
}

/// A keystore that refuses to delete the active login (parked ones still go).
struct DeniesDelete(MemoryStore);

impl SecretStore for DeniesDelete {
    fn get(&self, account: &str) -> Result<Option<String>, String> {
        self.0.get(account)
    }
    fn set(&self, account: &str, secret: &str) -> Result<(), String> {
        self.0.set(account, secret)
    }
    fn delete(&self, account: &str) -> Result<(), String> {
        if account.starts_with("token:") {
            return Err("the keystore refused".into());
        }
        self.0.delete(account)
    }
}

#[test]
fn forgetting_the_active_agent_with_a_refusing_keystore_changes_nothing() {
    let dir = TempDir::new().unwrap();
    let store = DeniesDelete(MemoryStore::default());
    flows::save(dir.path(), &store, &session(A, FP_A, "alice")).unwrap();

    assert!(flows::forget_pin(dir.path(), &store, A).is_err());

    let now = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(now.host, A, "the address stays with its token: {now:?}");
    assert_eq!(now.token, format!("token-for-{A}"));
}

/// A keystore whose reads of the active login fail (writes and deletes still work).
struct UnreadableToken(MemoryStore);

impl SecretStore for UnreadableToken {
    fn get(&self, account: &str) -> Result<Option<String>, String> {
        if account.starts_with("token:") {
            return Err("the keystore could not be read".into());
        }
        self.0.get(account)
    }
    fn set(&self, account: &str, secret: &str) -> Result<(), String> {
        self.0.set(account, secret)
    }
    fn delete(&self, account: &str) -> Result<(), String> {
        self.0.delete(account)
    }
}

/// Audit F-160: a save that cannot read the old login must stop before touching anything, not treat it as
/// "no old login" and delete it if a later step fails.
#[test]
fn a_save_that_cannot_read_the_old_login_changes_nothing() {
    let dir = TempDir::new().unwrap();
    let mem = MemoryStore::default();
    flows::save(dir.path(), &mem, &session(A, FP_A, "alice")).unwrap();
    let store = UnreadableToken(mem);

    assert!(flows::save(dir.path(), &store, &session(B, FP_B, "bob")).is_err());

    let now = flows::load_saved(dir.path(), &store.0).unwrap();
    assert_eq!(now.host, A, "{now:?}");
    assert_eq!(now.token, format!("token-for-{A}"));
}
