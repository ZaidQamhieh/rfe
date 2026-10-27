//! Approve-on-PC pairing: the match code, polling, the one-time token, and what happens when it
//! cannot be stored. Run against a throwaway agent; the owner's answer is given with the CLI.

mod common;

use common::{free_port, Agent};
use rfe_desktop_lib::agent_client::{capture_fingerprint, known_message, match_code, AgentError};
use rfe_desktop_lib::flows::{self, PairProgress, PairWait};
use rfe_desktop_lib::secrets::{account, MemoryStore, Offloaded, SecretStore, UnavailableStore};
use std::sync::atomic::{AtomicUsize, Ordering};
use tempfile::TempDir;

async fn ask(
    a: &Agent,
    fp: &str,
    dir: &TempDir,
    store: &Offloaded,
) -> Result<PairWait, AgentError> {
    flows::request_pairing(dir.path(), &a.host, fp, "Desktop (PC approval)", store).await
}

fn saved(p: PairProgress) -> flows::Saved {
    match p {
        PairProgress::Approved(s) => s,
        other => panic!("expected approval, got {other:?}"),
    }
}

#[tokio::test]
async fn the_match_code_equals_the_one_the_agent_shows_the_owner() {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();

    let wait = ask(&a, &fp, &dir, &store).await.unwrap();
    assert_eq!(wait.expires_in_seconds, 120);
    assert_eq!(wait.match_code, a.waiting_match_code());

    // Control: the code depends on the certificate this app saw, so a relayed connection
    // (another certificate) yields another code and the user would see the mismatch.
    let other_cert = "ab".repeat(32);
    let relayed = match_code(&other_cert, &wait.client_nonce, &wait.request_id).unwrap();
    assert_ne!(relayed, a.waiting_match_code());
    assert_eq!(wait.match_code.len(), 9, "{}", wait.match_code);
}

#[tokio::test]
async fn pending_then_approved_stores_the_token_once() {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    let wait = ask(&a, &fp, &dir, &store).await.unwrap();

    assert!(matches!(
        flows::poll_pairing(dir.path(), &wait, &store)
            .await
            .unwrap(),
        PairProgress::Pending
    ));
    assert!(flows::load_saved(dir.path(), &store)
        .unwrap()
        .token
        .is_empty());

    a.answer_pair_request(true);
    let s = saved(
        flows::poll_pairing(dir.path(), &wait, &store)
            .await
            .unwrap(),
    );
    assert!(!s.token.is_empty() && s.username.is_empty());
    assert_eq!(
        flows::load_saved(dir.path(), &store).unwrap().token,
        s.token
    );
    assert_eq!(flows::list_pins(dir.path()).unwrap().len(), 1);
    let list = flows::list_devices(dir.path(), &store).await.unwrap();
    assert_eq!(
        list.len(),
        1,
        "an approved device is an ordinary one: {list:?}"
    );
    assert!(list[0].current && !list[0].via_login);

    // The agent hands the token out once. A client that threw the first answer away has nothing.
    assert!(matches!(
        flows::poll_pairing(dir.path(), &wait, &store)
            .await
            .unwrap(),
        PairProgress::Expired
    ));
}

#[tokio::test]
async fn a_rejected_request_stores_nothing() {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    let wait = ask(&a, &fp, &dir, &store).await.unwrap();

    a.answer_pair_request(false);
    assert!(matches!(
        flows::poll_pairing(dir.path(), &wait, &store)
            .await
            .unwrap(),
        PairProgress::Rejected
    ));
    assert!(flows::load_saved(dir.path(), &store)
        .unwrap()
        .token
        .is_empty());
    assert!(flows::list_pins(dir.path()).unwrap().is_empty());
}

#[tokio::test]
async fn an_unknown_request_or_a_wrong_nonce_reads_as_expired() {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    let wait = ask(&a, &fp, &dir, &store).await.unwrap();

    let wrong_nonce = PairWait {
        client_nonce: "00".repeat(16),
        ..wait.clone()
    };
    let unknown_id = PairWait {
        request_id: "deadbeefdeadbeefdeadbeefdeadbeef".into(),
        ..wait.clone()
    };
    for w in [wrong_nonce, unknown_id] {
        assert!(matches!(
            flows::poll_pairing(dir.path(), &w, &store).await.unwrap(),
            PairProgress::Expired
        ));
    }
    // Control: the real request is still waiting, so those were refusals, not a dead request.
    assert!(matches!(
        flows::poll_pairing(dir.path(), &wait, &store)
            .await
            .unwrap(),
        PairProgress::Pending
    ));
}

#[tokio::test]
async fn a_locked_keystore_stops_the_request_before_the_agent_hears_of_it() {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let dir = TempDir::new().unwrap();

    let err = ask(&a, &fp, &dir, &Offloaded::new(UnavailableStore))
        .await
        .unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m.contains("OS keystore")),
        "{err:?}"
    );
    assert!(
        a.pair_requests_cli()
            .contains("No pairing requests waiting"),
        "the agent saw a request"
    );
    ask(&a, &fp, &dir, &Offloaded::new(MemoryStore::default()))
        .await
        .expect("control: with a usable keystore the request goes through");
    assert!(a.pair_requests_cli().contains("match code"));
}

/// Stores the first secret (the device key) and then refuses, like a keystore that locks while the
/// owner is away approving.
struct LocksAfterFirstWrite {
    inner: MemoryStore,
    sets: AtomicUsize,
}

impl SecretStore for LocksAfterFirstWrite {
    fn get(&self, a: &str) -> Result<Option<String>, String> {
        self.inner.get(a)
    }
    fn set(&self, a: &str, s: &str) -> Result<(), String> {
        if self.sets.fetch_add(1, Ordering::SeqCst) >= 1 {
            return Err("the OS keystore is locked".into());
        }
        self.inner.set(a, s)
    }
    fn delete(&self, a: &str) -> Result<(), String> {
        self.inner.delete(a)
    }
}

#[tokio::test]
async fn an_approval_that_cannot_be_stored_says_it_is_used_up() {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(LocksAfterFirstWrite {
        inner: MemoryStore::default(),
        sets: AtomicUsize::new(0),
    });
    let wait = ask(&a, &fp, &dir, &store).await.unwrap();
    a.answer_pair_request(true);

    let err = flows::poll_pairing(dir.path(), &wait, &store)
        .await
        .unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m.contains("used up") && m.contains("Ask again")),
        "{err:?}"
    );
    assert_eq!(store.get(&account("token", dir.path())).unwrap(), None);
    // The approval really was spent on the agent's side, which is why the message says so.
    assert!(matches!(
        flows::poll_pairing(dir.path(), &wait, &store)
            .await
            .unwrap(),
        PairProgress::Expired
    ));
}

#[tokio::test]
async fn a_fourth_waiting_request_is_told_the_pc_is_busy() {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dirs: Vec<_> = (0..4).map(|_| TempDir::new().unwrap()).collect();
    for d in &dirs[..3] {
        ask(&a, &fp, d, &store).await.expect("under the limit");
    }
    let err = ask(&a, &fp, &dirs[3], &store).await.unwrap_err();
    match &err {
        AgentError::Server { code, .. } => assert_eq!(code, "PAIR_BUSY"),
        other => panic!("{other:?}"),
    }
    assert_eq!(err.to_string(), known_message("PAIR_BUSY").unwrap());
}

/// Only meaningful against an agent without the endpoint (the previous-release CI job): the app
/// must say so in words and point to the other ways to pair, not show "HTTP_404".
#[tokio::test]
async fn an_agent_without_approval_is_told_so_in_words() {
    let a = Agent::start(free_port());
    if a.supports_pair_request().await {
        eprintln!("skipped: this agent supports approval");
        return;
    }
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let err = ask(&a, &fp, &TempDir::new().unwrap(), &store)
        .await
        .unwrap_err();
    let text = err.to_string();
    assert!(text.contains("too old"), "{text}");
    assert!(
        !text.contains("HTTP_404") && !text.contains("BAD_REQUEST"),
        "{text}"
    );
}
