//! The account-server controls of the desktop app core: the fingerprint is worked out from the key,
//! the plain-words errors, and a trust request whose confirmed fingerprint is wrong is refused before
//! any session is opened. Against a throwaway agent (set RFE_AGENT_BIN to a built one): pinning a
//! server needs the account password, turning the role on or off and unpinning do not.

mod common;

use common::{free_port, Agent};
use rfe_desktop_lib::account_server::{self, clean_name, key_fingerprint};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentError};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use tempfile::TempDir;

/// 32 zero bytes, standard base64.
const ZERO_KEY: &str = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
/// SHA-256 of 32 zero bytes.
const ZERO_FP: &str = "66687aadf862bd776c8fc18b8e9f8e20089714856ee233b3902a591d0d5f2925";

#[test]
fn the_fingerprint_is_the_sha256_of_the_raw_key() {
    assert_eq!(key_fingerprint(ZERO_KEY).unwrap(), ZERO_FP);
    assert_eq!(key_fingerprint(&format!(" {ZERO_KEY}\n")).unwrap(), ZERO_FP);
}

#[test]
fn something_that_is_not_a_32_byte_key_is_refused() {
    for bad in ["", "not base64!", "Zm9v", "AAAA"] {
        let e = key_fingerprint(bad).unwrap_err();
        assert!(
            e.to_string().contains(account_server::NOT_A_KEY),
            "{bad:?}: {e}"
        );
    }
}

#[test]
fn a_name_from_another_computer_is_made_safe_to_show() {
    assert_eq!(clean_name("  Office PC ", "x"), "Office PC");
    assert_eq!(clean_name("", "fallback"), "fallback");
    assert_eq!(clean_name("bad\u{7}name", "fallback"), "fallback");
    assert_eq!(clean_name(&"a".repeat(65), "fallback"), "fallback");
    assert_eq!(clean_name(&"a".repeat(64), "fallback").len(), 64);
}

fn server(status: u16, code: &str) -> AgentError {
    AgentError::Server {
        status,
        code: code.into(),
        message: "m".into(),
    }
}

#[test]
fn refusals_are_said_in_plain_words() {
    assert_eq!(
        account_server::explain(&server(403, "FORBIDDEN")),
        account_server::NOT_ALLOWED
    );
    assert_eq!(
        account_server::explain(&server(403, "ACCOUNT_SERVER_DISABLED")),
        account_server::DISABLED
    );
    assert_eq!(
        account_server::explain(&server(403, "CAPABILITY_DENIED")),
        account_server::PASSWORD_NEEDED
    );
    assert_eq!(
        account_server::explain(&server(401, "INVALID_CREDENTIALS")),
        account_server::WRONG_PASSWORD
    );
    assert_eq!(
        account_server::explain(&server(429, "RATE_LIMITED")),
        account_server::TOO_MANY_PASSWORDS
    );
    assert_eq!(
        account_server::explain(&server(404, "HTTP_404")),
        account_server::TOO_OLD
    );
    assert_eq!(
        account_server::explain(&server(400, "BAD_REQUEST")),
        "The agent refused that change: m"
    );
}

#[tokio::test]
async fn trusting_a_key_with_another_fingerprint_sends_nothing() {
    let dir = std::env::temp_dir().join("rfe-account-server-test-never-created");
    let store = Offloaded::new(MemoryStore::default());
    let wrong = "0f".repeat(32);
    let e = account_server::trust(&dir, &store, "Office PC", ZERO_KEY, &wrong, "pw")
        .await
        .unwrap_err();
    assert!(e.to_string().contains(account_server::KEY_CHANGED), "{e}");
    assert!(!dir.exists(), "no session or file was touched");
}

const OWNER_PASSWORD: &str = "pw-for-account-server-test";

/// A throwaway agent with one account, and a desktop login to it as that account.
async fn signed_in() -> (Agent, Offloaded, TempDir) {
    let a = Agent::start(free_port());
    a.add_user("owner", OWNER_PASSWORD);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    flows::login(
        dir.path(),
        &a.host,
        &fp,
        "owner",
        OWNER_PASSWORD,
        "Admin PC",
        &store,
    )
    .await
    .unwrap();
    (a, store, dir)
}

#[tokio::test]
async fn trusting_a_server_needs_the_account_password_and_with_it_succeeds() {
    if !common::modern("account server controls") {
        return;
    }
    let (_a, store, dir) = signed_in().await;

    let e = account_server::trust(dir.path(), &store, "Office PC", ZERO_KEY, ZERO_FP, "")
        .await
        .unwrap_err();
    assert_eq!(
        account_server::explain(&e),
        account_server::PASSWORD_NEEDED,
        "{e}"
    );

    let e = account_server::trust(dir.path(), &store, "Office PC", ZERO_KEY, ZERO_FP, "not it")
        .await
        .unwrap_err();
    assert_eq!(
        account_server::explain(&e),
        account_server::WRONG_PASSWORD,
        "{e}"
    );

    let before = account_server::status(dir.path(), &store).await.unwrap();
    assert!(
        before.settings.trusted_server.is_none(),
        "nothing was pinned by the refusals"
    );

    let after = account_server::trust(
        dir.path(),
        &store,
        "Office PC",
        ZERO_KEY,
        ZERO_FP,
        OWNER_PASSWORD,
    )
    .await
    .unwrap();
    let pinned = after.settings.trusted_server.expect("the key is pinned");
    assert_eq!(pinned.key_fingerprint, ZERO_FP);
    assert_eq!(pinned.name, "Office PC");
}

#[tokio::test]
async fn turning_the_role_on_and_unpinning_need_no_password() {
    if !common::modern("account server controls") {
        return;
    }
    let (_a, store, dir) = signed_in().await;
    let on = account_server::set_enabled(dir.path(), &store, true)
        .await
        .unwrap();
    assert!(on.settings.enabled);
    account_server::trust(
        dir.path(),
        &store,
        "Office PC",
        ZERO_KEY,
        ZERO_FP,
        OWNER_PASSWORD,
    )
    .await
    .unwrap();
    let off = account_server::untrust(dir.path(), &store).await.unwrap();
    assert!(off.settings.trusted_server.is_none());
}

#[tokio::test]
async fn too_many_wrong_passwords_are_refused_in_words_even_for_the_right_one() {
    if !common::modern("account server controls") {
        return;
    }
    let (_a, store, dir) = signed_in().await;
    let mut refused = None;
    for _ in 0..40 {
        let e = account_server::trust(dir.path(), &store, "Office PC", ZERO_KEY, ZERO_FP, "wrong")
            .await
            .unwrap_err();
        if let AgentError::Server { status: 429, .. } = e {
            refused = Some(e);
            break;
        }
    }
    let e = refused.expect("the agent stops checking passwords after a few wrong ones");
    assert_eq!(
        account_server::explain(&e),
        account_server::TOO_MANY_PASSWORDS
    );
    let e = account_server::trust(
        dir.path(),
        &store,
        "Office PC",
        ZERO_KEY,
        ZERO_FP,
        OWNER_PASSWORD,
    )
    .await
    .unwrap_err();
    assert_eq!(
        account_server::explain(&e),
        account_server::TOO_MANY_PASSWORDS,
        "{e}"
    );
}
