//! Generating a pairing code for a phone from the app: an account (admin) session gets a usable
//! one-time code, any other session gets the "not allowed" state, and a code never reaches
//! `state.json` or a `{:?}`. Run against a throwaway agent. The log is checked in its own test
//! binary (`pairing_codes_log.rs`), because the log is process-wide.

mod common;

#[path = "support/ui.rs"]
mod ui;
use common::{free_port, Agent};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentError};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::pairing_codes::{self, TTL_SECONDS};
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use std::path::Path;
use tempfile::TempDir;

const PW: &str = "pw-for-pairing-codes-test";
/// The characters the agent's codes are made of (agent/internal/pairing).
const ALPHABET: &str = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ";

async fn admin(a: &Agent, fp: &str, dir: &Path, store: &Offloaded) -> flows::Saved {
    flows::login(dir, &a.host, fp, "owner", PW, "Admin", store)
        .await
        .unwrap()
}

#[tokio::test]
async fn an_account_session_gets_a_code_that_pairs_a_device_exactly_once() {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    admin(&a, &fp, dir.path(), &store).await;

    let made = pairing_codes::generate(dir.path(), &store).await.unwrap();
    assert_eq!(made.status, "ok", "{made:?}");
    assert_eq!(made.code.len(), 8, "{made:?}");
    assert!(made.code.chars().all(|c| ALPHABET.contains(c)), "{made:?}");
    assert_eq!(made.expires_in_seconds, TTL_SECONDS);

    // The code is a real one: a phone (here, another app data folder) pairs with it, once.
    let phone = TempDir::new().unwrap();
    let paired = flows::pair(phone.path(), &a.host, &fp, &made.code, "Phone", &store)
        .await
        .expect("the generated code pairs a device");
    assert!(!paired.token.is_empty());
    assert!(a.devices_cli().contains("Phone"));
    let again = flows::pair(phone.path(), &a.host, &fp, &made.code, "Phone 2", &store)
        .await
        .unwrap_err();
    assert!(
        matches!(&again, AgentError::Server { code, .. } if code == "INVALID_CODE"),
        "{again:?}"
    );
}

#[tokio::test]
async fn generating_again_makes_a_different_code_and_the_agent_accepts_it() {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    admin(&a, &fp, dir.path(), &store).await;

    let first = pairing_codes::generate(dir.path(), &store).await.unwrap();
    let second = pairing_codes::generate(dir.path(), &store).await.unwrap();
    assert_ne!(first.code, second.code);
    flows::pair(
        TempDir::new().unwrap().path(),
        &a.host,
        &fp,
        &second.code,
        "Phone",
        &store,
    )
    .await
    .expect("the newest code works");
}

#[tokio::test]
async fn a_code_paired_session_gets_the_not_allowed_state_and_keeps_its_login() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    let ordinary = flows::pair(dir.path(), &a.host, &fp, &a.pair_code(), "Desktop", &store)
        .await
        .unwrap();

    let made = pairing_codes::generate(dir.path(), &store).await.unwrap();
    assert_eq!(made.status, "forbidden", "{made:?}");
    assert!(
        made.code.is_empty() && made.expires_in_seconds == 0,
        "{made:?}"
    );

    // Being refused is not being signed out: the login is still there and still works.
    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert_eq!(saved.token, ordinary.token);
    assert_eq!(
        flows::list_devices(dir.path(), &store).await.unwrap().len(),
        1
    );
}

#[tokio::test]
async fn without_a_login_nothing_is_sent() {
    let a = Agent::start(free_port());
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    let err = pairing_codes::generate(dir.path(), &store)
        .await
        .unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m == "not signed in"),
        "{err:?}"
    );
    assert!(
        !a.devices_cli().contains("Admin"),
        "the agent was never asked"
    );
}

#[tokio::test]
async fn a_revoked_login_is_refused_and_dropped_so_the_window_returns_to_sign_in() {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    let saved = admin(&a, &fp, dir.path(), &store).await;
    a.revoke_cli(&saved.device_id);

    let err = pairing_codes::generate(dir.path(), &store)
        .await
        .unwrap_err();
    assert!(
        matches!(&err, AgentError::Server { status: 401, .. }),
        "{err:?}"
    );
    let after = flows::load_saved(dir.path(), &store).unwrap();
    assert!(after.token.is_empty(), "the dead token was dropped");
    assert_eq!(after.host, a.host, "the pin and address are kept");
}

#[tokio::test]
async fn a_code_is_in_no_file_and_no_debug_output() {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    admin(&a, &fp, dir.path(), &store).await;

    let made = pairing_codes::generate(dir.path(), &store).await.unwrap();
    assert_eq!(made.status, "ok");

    let debug = format!("{made:?}");
    assert!(!debug.contains(&made.code), "{debug}");

    // Nothing in the app's data folder holds it (state.json is the only file the app writes there;
    // look at every file anyway).
    let mut seen = 0;
    for entry in std::fs::read_dir(dir.path()).unwrap() {
        let path = entry.unwrap().path();
        if path.is_file() {
            seen += 1;
            let text = String::from_utf8_lossy(&std::fs::read(&path).unwrap()).into_owned();
            assert!(
                !text.contains(&made.code),
                "{} holds the code",
                path.display()
            );
        }
    }
    assert!(seen > 0, "state.json should exist");
    assert!(dir.path().join("state.json").is_file());
}

/// Every message the window can show for this feature is in the guide, and still in the app.
#[test]
fn every_message_of_this_feature_is_explained_and_still_in_the_app() {
    const STEMS: &[&str] = &[
        "This login cannot create pairing codes",
        "This pairing code has expired",
        "unexpected response: the agent sent no pairing code",
    ];
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let guide = std::fs::read_to_string(root.join("../docs/user-guide.md")).unwrap();
    let mut code = String::new();
    code.push_str(&ui::js());
    for f in ["../ui/index.html", "src/pairing_codes.rs"] {
        code.push_str(&std::fs::read_to_string(root.join(f)).unwrap());
    }
    let flat = code.split_whitespace().collect::<Vec<_>>().join(" ");
    for stem in STEMS {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
        assert!(
            code.contains(stem) || flat.contains(stem),
            "{stem:?} is listed but no longer in the app"
        );
    }
}

#[tokio::test]
async fn the_lifetime_from_the_settings_is_what_the_agent_gives_the_code() {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    admin(&a, &fp, dir.path(), &store).await;

    for (asked, got) in [
        (120, 120),
        (300, 300),
        (600, 600),
        (7, TTL_SECONDS),
        (86_400, TTL_SECONDS),
    ] {
        let made = pairing_codes::generate_for(dir.path(), &store, asked)
            .await
            .unwrap();
        assert_eq!(made.status, "ok", "{made:?}");
        assert_eq!(made.expires_in_seconds, got, "asked for {asked}");
    }
}
