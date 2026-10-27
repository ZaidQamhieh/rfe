//! The pairing-request inbox against a throwaway agent: what an account session sees and can do,
//! what any other login gets instead, the limit of three, and requests that are already gone.
//! Requests are made the way a phone or another computer makes them (`flows::request_pairing`).

mod common;

#[path = "support/ui.rs"]
mod ui;
use common::{free_port, Agent};
use rfe_desktop_lib::agent_client::{capture_fingerprint, known_message, AgentError};
use rfe_desktop_lib::flows::{self, PairProgress, PairWait};
use rfe_desktop_lib::pair_inbox::{self, Answer, LIMIT, TTL_SECONDS};
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use std::path::Path;
use tempfile::TempDir;

const PW: &str = "pw-for-the-inbox-tests";

struct Setup {
    a: Agent,
    fp: String,
    store: Offloaded,
    /// The owner's window: an account session.
    admin: TempDir,
}

async fn setup() -> Option<Setup> {
    let a = Agent::start(free_port());
    if !a.require_pair_request().await {
        return None;
    }
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let admin = TempDir::new().unwrap();
    flows::login(
        admin.path(),
        &a.host,
        &fp,
        "owner",
        PW,
        "Owner window",
        &store,
    )
    .await
    .unwrap();
    Some(Setup {
        a,
        fp,
        store,
        admin,
    })
}

/// A computer asking to be paired, which keeps what it needs to collect the answer.
struct Asker {
    dir: TempDir,
    wait: PairWait,
}

async fn ask(s: &Setup, label: &str) -> Result<Asker, AgentError> {
    let dir = TempDir::new().unwrap();
    let wait = flows::request_pairing(dir.path(), &s.a.host, &s.fp, label, &s.store).await?;
    Ok(Asker { dir, wait })
}

async fn poll(s: &Setup, who: &Asker) -> PairProgress {
    flows::poll_pairing(who.dir.path(), &who.wait, &s.store)
        .await
        .unwrap()
}

async fn load(s: &Setup) -> pair_inbox::Inbox {
    pair_inbox::load(s.admin.path(), &s.store).await.unwrap()
}

fn server_code(e: &AgentError) -> (u16, &str) {
    match e {
        AgentError::Server { status, code, .. } => (*status, code),
        other => panic!("not a server error: {other:?}"),
    }
}

#[tokio::test]
async fn lists_waiting_requests_then_accepts_one_and_rejects_another() {
    let Some(s) = setup().await else { return };
    let empty = load(&s).await;
    assert!(!empty.forbidden && empty.requests.is_empty(), "{empty:?}");
    assert_eq!((empty.limit, empty.ttl_seconds), (LIMIT, TTL_SECONDS));

    let first = ask(&s, "Phone A").await.unwrap();
    let second = ask(&s, "Phone B").await.unwrap();

    let inbox = load(&s).await;
    assert!(!inbox.forbidden);
    assert_eq!(inbox.requests.len(), 2, "{inbox:?}");
    // Oldest first, each with the code the asking side computed for itself: the owner compares
    // the two, so they must be the same.
    let (a, b) = (&inbox.requests[0], &inbox.requests[1]);
    assert_eq!((a.label.as_str(), b.label.as_str()), ("Phone A", "Phone B"));
    assert_eq!(a.id, first.wait.request_id);
    assert_eq!(a.match_code, first.wait.match_code);
    assert_eq!(b.match_code, second.wait.match_code);
    assert_ne!(a.match_code, b.match_code);
    assert_eq!(a.address, "127.0.0.1");
    assert!(a.replaces.is_empty());
    assert!(
        (100..=120).contains(&a.expires_in_seconds)
            && a.age_seconds + a.expires_in_seconds == TTL_SECONDS,
        "{a:?}"
    );

    // Nobody has answered yet, so nothing has been granted.
    assert!(matches!(poll(&s, &first).await, PairProgress::Pending));

    assert_eq!(
        pair_inbox::answer(s.admin.path(), &s.store, &a.id, true)
            .await
            .unwrap(),
        Answer::Done
    );
    assert_eq!(
        pair_inbox::answer(s.admin.path(), &s.store, &b.id, false)
            .await
            .unwrap(),
        Answer::Done
    );

    // The end state, from the agent's side and from each asker's side.
    assert!(load(&s).await.requests.is_empty());
    assert!(s
        .a
        .pair_requests_cli()
        .contains("No pairing requests waiting"));
    match poll(&s, &first).await {
        PairProgress::Approved(saved) => assert!(!saved.token.is_empty()),
        other => panic!("Phone A should be approved, got {other:?}"),
    }
    assert!(matches!(poll(&s, &second).await, PairProgress::Rejected));
    let devices = flows::list_devices(s.admin.path(), &s.store).await.unwrap();
    assert!(devices.iter().any(|d| d.label == "Phone A"), "{devices:?}");
    assert!(!devices.iter().any(|d| d.label == "Phone B"), "{devices:?}");
    // Rejecting stored nothing for B, approving stored a login for A only.
    assert!(flows::load_saved(second.dir.path(), &s.store)
        .unwrap()
        .token
        .is_empty());
    assert!(!flows::load_saved(first.dir.path(), &s.store)
        .unwrap()
        .token
        .is_empty());
}

#[tokio::test]
async fn an_answered_or_unknown_request_is_gone_not_an_error() {
    let Some(s) = setup().await else { return };
    let one = ask(&s, "Phone A").await.unwrap();
    let id = one.wait.request_id.clone();

    assert_eq!(
        pair_inbox::answer(s.admin.path(), &s.store, &id, false)
            .await
            .unwrap(),
        Answer::Done
    );
    // The same request again, from a window that had not refreshed yet, and an id nobody made.
    for (approve, id) in [(true, id.as_str()), (false, id.as_str())] {
        assert_eq!(
            pair_inbox::answer(s.admin.path(), &s.store, id, approve)
                .await
                .unwrap(),
            Answer::Gone
        );
    }
    let unknown = "deadbeefdeadbeefdeadbeefdeadbeef";
    assert_eq!(
        pair_inbox::answer(s.admin.path(), &s.store, unknown, true)
            .await
            .unwrap(),
        Answer::Gone
    );
    // The answer given first stands: it was a rejection, and approving later did not undo it.
    assert!(matches!(poll(&s, &one).await, PairProgress::Rejected));
}

#[tokio::test]
async fn an_id_that_is_not_an_id_never_reaches_the_agent() {
    let Some(s) = setup().await else { return };
    let waiting = ask(&s, "Phone A").await.unwrap();
    for bad in [
        "",
        "../devices",
        "a/b",
        "a b",
        "abc?x=1",
        "ab\u{e9}",
        &"a".repeat(65),
    ] {
        let err = pair_inbox::answer(s.admin.path(), &s.store, bad, true)
            .await
            .unwrap_err();
        assert!(
            matches!(&err, AgentError::Local(m) if m.contains("unexpected request id")),
            "{bad:?}: {err:?}"
        );
    }
    // Control: nothing was answered by any of those.
    assert_eq!(load(&s).await.requests.len(), 1);
    assert!(matches!(poll(&s, &waiting).await, PairProgress::Pending));
}

#[tokio::test]
async fn a_login_that_is_not_an_account_gets_the_forbidden_state_and_can_answer_nothing() {
    let Some(s) = setup().await else { return };
    let waiting = ask(&s, "Phone A").await.unwrap();
    let id = waiting.wait.request_id.clone();

    // An ordinary device, paired with a code.
    let guest = TempDir::new().unwrap();
    flows::pair(
        guest.path(),
        &s.a.host,
        &s.fp,
        &s.a.pair_code(),
        "Guest",
        &s.store,
    )
    .await
    .unwrap();

    let inbox = pair_inbox::load(guest.path(), &s.store).await.unwrap();
    assert!(inbox.forbidden, "{inbox:?}");
    assert!(inbox.requests.is_empty(), "a guest must see no requests");

    let err = pair_inbox::answer(guest.path(), &s.store, &id, true)
        .await
        .unwrap_err();
    assert_eq!(server_code(&err), (403, "FORBIDDEN"), "{err:?}");
    assert_eq!(err.to_string(), known_message("FORBIDDEN").unwrap());

    // Control: the request is still waiting, so the refusal was about the session, and the owner
    // can answer it.
    assert!(matches!(poll(&s, &waiting).await, PairProgress::Pending));
    assert_eq!(load(&s).await.requests.len(), 1);
    assert_eq!(
        pair_inbox::answer(s.admin.path(), &s.store, &id, true)
            .await
            .unwrap(),
        Answer::Done
    );
}

#[tokio::test]
async fn the_agent_holds_three_and_a_fourth_waits_until_one_is_answered() {
    let Some(s) = setup().await else { return };
    let mut askers = Vec::new();
    for label in ["One", "Two", "Three"] {
        askers.push(ask(&s, label).await.expect("under the limit"));
    }
    let inbox = load(&s).await;
    assert_eq!(inbox.requests.len(), LIMIT, "{inbox:?}");

    let err = ask(&s, "Four").await.err().expect("a fourth is refused");
    assert_eq!(server_code(&err), (429, "PAIR_BUSY"), "{err:?}");

    // Answering one makes room.
    pair_inbox::answer(s.admin.path(), &s.store, &inbox.requests[0].id, false)
        .await
        .unwrap();
    ask(&s, "Four").await.expect("room again after an answer");
    let labels: Vec<_> = load(&s)
        .await
        .requests
        .into_iter()
        .map(|r| r.label)
        .collect();
    assert_eq!(labels, ["Two", "Three", "Four"]);
}

#[tokio::test]
async fn without_a_login_or_after_it_is_revoked_the_inbox_says_so() {
    let Some(s) = setup().await else { return };

    // Never signed in on this folder.
    let nobody = TempDir::new().unwrap();
    let err = pair_inbox::load(nobody.path(), &s.store).await.unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m == "not signed in"),
        "{err:?}"
    );
    let err = pair_inbox::answer(nobody.path(), &s.store, "abc", true)
        .await
        .unwrap_err();
    assert!(matches!(&err, AgentError::Local(m) if m == "not signed in"));

    // The owner revokes the window's login at the PC: the agent refuses the token, and the dead
    // token is dropped so the window goes back to sign-in.
    let saved = flows::load_saved(s.admin.path(), &s.store).unwrap();
    s.a.revoke_cli(&saved.device_id);
    let err = pair_inbox::load(s.admin.path(), &s.store)
        .await
        .unwrap_err();
    assert_eq!(server_code(&err).0, 401, "{err:?}");
    assert!(flows::load_saved(s.admin.path(), &s.store)
        .unwrap()
        .token
        .is_empty());
}

/// Every message of the inbox is explained in the user guide, and is really in the app.
#[test]
fn every_inbox_message_is_in_the_user_guide_and_in_the_app() {
    const STEMS: &[&str] = &[
        "This agent is too old to list pairing requests",
        "This login cannot answer pairing requests.",
        "Accept only if the code matches",
        "The agent holds at most 3 waiting requests.",
        "That request already expired or was answered on the PC.",
        "Accepted ",
        "Rejected ",
    ];
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let guide = std::fs::read_to_string(root.join("../docs/user-guide.md")).unwrap();
    let mut code = std::fs::read_to_string(root.join("src/pair_inbox.rs")).unwrap();
    code.push_str(&ui::js());
    code.push_str(&std::fs::read_to_string(root.join("../ui/index.html")).unwrap());
    let flat: String = code.split_whitespace().collect::<Vec<_>>().join(" ");
    let flat = flat.replace("\\ ", "").replace("\" \"", "");
    for stem in STEMS {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
        assert!(
            flat.contains(stem) || code.contains(stem),
            "{stem:?} is listed but no longer in the app"
        );
    }
}
