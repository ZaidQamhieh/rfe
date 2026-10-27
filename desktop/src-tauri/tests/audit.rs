//! The audit and log screens' data, against a throwaway agent: events from a pairing, failed and
//! good sign-ins and a revoke arrive in order, paging works across a page boundary, an ordinary
//! (code-paired) device gets the forbidden state, and hostile text is cleaned and cut.

mod common;

use common::{free_port, Agent, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentError};
use rfe_desktop_lib::audit::{self, AuditEntry, AuditReply};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use std::path::Path;
use tempfile::TempDir;

const PW: &str = "pw-for-audit-tests";

fn store() -> Offloaded {
    Offloaded::new(MemoryStore::default())
}

fn hostile_name() -> String {
    format!("\u{202e}evil<script>alert(1)</script>\n{}", "A".repeat(600))
}

struct Setup {
    agent: Agent,
    admin: TempDir,
    admin_store: Offloaded,
    code_dir: TempDir,
    code_store: Offloaded,
}

/// Pair with a code (an ordinary device), fail a sign-in with a hostile name and a wrong password,
/// sign in as the owner, and revoke the code-paired device as the owner.
async fn setup() -> Setup {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let (code_dir, code_store) = (TempDir::new().unwrap(), store());
    let (admin, admin_store) = (TempDir::new().unwrap(), store());

    let paired = flows::pair(
        code_dir.path(),
        &a.host,
        &fp,
        &a.pair_code(),
        "Code Laptop",
        &code_store,
    )
    .await
    .unwrap();
    flows::login(
        admin.path(),
        &a.host,
        &fp,
        &hostile_name(),
        "nope",
        "Desk",
        &admin_store,
    )
    .await
    .unwrap_err();
    flows::login(
        admin.path(),
        &a.host,
        &fp,
        "owner",
        "wrong",
        "Desk",
        &admin_store,
    )
    .await
    .unwrap_err();
    let owner = flows::login(
        admin.path(),
        &a.host,
        &fp,
        "owner",
        PW,
        "Desk",
        &admin_store,
    )
    .await
    .unwrap();

    let raw = Raw::new(&a.host);
    let resp = raw
        .http
        .delete(format!("{}/devices/{}", raw.base, paired.device_id))
        .bearer_auth(&owner.token)
        .send()
        .await
        .unwrap();
    assert!(resp.status().is_success(), "revoke: {}", resp.status());
    Setup {
        agent: a,
        admin,
        admin_store,
        code_dir,
        code_store,
    }
}

async fn all_pages(dir: &Path, st: &Offloaded, limit: usize) -> (Vec<AuditReply>, Vec<AuditEntry>) {
    let (mut pages, mut all, mut before) = (vec![], vec![], None);
    loop {
        let p = audit::fetch_audit(dir, st, before, limit).await.unwrap();
        assert!(!p.forbidden);
        all.extend(p.entries.clone());
        before = p.next_before;
        pages.push(p);
        if before.is_none() {
            return (pages, all);
        }
        assert!(pages.len() < 50, "paging does not end");
    }
}

#[tokio::test]
async fn events_are_listed_newest_first_and_paging_crosses_the_page_boundary() {
    let s = setup().await;
    let (pages, all) = all_pages(s.admin.path(), &s.admin_store, 2).await;

    // Newest first, no repeats, and the pages join into the same list one big page returns.
    assert!(all.windows(2).all(|w| w[0].id > w[1].id), "{all:?}");
    let big = audit::fetch_audit(s.admin.path(), &s.admin_store, None, 100)
        .await
        .unwrap();
    assert_eq!(big.entries, all);
    assert_eq!(big.next_before, None, "a short page is the end");

    // The events the setup caused, in the order they happened (oldest first here).
    let actions: Vec<&str> = all.iter().rev().map(|e| e.action.as_str()).collect();
    let mut want = [
        "pair",
        "login_failed",
        "login_failed",
        "login",
        "device_revoked",
    ]
    .iter();
    let mut next = want.next();
    for a in &actions {
        if Some(a) == next {
            next = want.next();
        }
    }
    assert!(
        next.is_none(),
        "events missing or out of order: {actions:?}"
    );

    // At least three pages of two, every full page carries the cursor, the last does not.
    assert!(pages.len() >= 3, "{} pages", pages.len());
    for p in &pages[..pages.len() - 1] {
        assert_eq!(p.entries.len(), 2);
        assert_eq!(p.next_before, Some(p.entries[1].id));
    }
    assert_eq!(pages.last().unwrap().next_before, None);

    // The names and the revoke are attributed.
    let revoke = all.iter().find(|e| e.action == "device_revoked").unwrap();
    assert_eq!(revoke.actor, "Desk");
    let pair = all.iter().find(|e| e.action == "pair").unwrap();
    assert_eq!(pair.actor, "Code Laptop");
    assert!(chrono_like(&pair.at), "not an RFC 3339 time: {:?}", pair.at);
}

/// `2026-10-04T12:00:00+02:00`-shaped, without a date library.
fn chrono_like(s: &str) -> bool {
    s.len() >= 20 && s.as_bytes()[4] == b'-' && s.as_bytes()[10] == b'T'
}

#[tokio::test]
async fn a_page_that_is_exactly_full_offers_the_next_one_and_it_is_empty() {
    let s = setup().await;
    let total = audit::fetch_audit(s.admin.path(), &s.admin_store, None, 100)
        .await
        .unwrap()
        .entries
        .len();
    // A page size equal to the number of entries is "full", so the window asks once more and gets
    // nothing: that is the end, not an error.
    let p = audit::fetch_audit(s.admin.path(), &s.admin_store, None, total)
        .await
        .unwrap();
    let cursor = p.next_before.expect("a full page carries a cursor");
    let rest = audit::fetch_audit(s.admin.path(), &s.admin_store, Some(cursor), total)
        .await
        .unwrap();
    assert!(rest.entries.is_empty() && rest.next_before.is_none() && !rest.forbidden);
}

#[tokio::test]
async fn hostile_text_is_cleaned_and_cut() {
    let s = setup().await;
    let (_, all) = all_pages(s.admin.path(), &s.admin_store, 100).await;
    let bad = all
        .iter()
        .find(|e| e.actor.contains("evil"))
        .expect("the hostile sign-in is listed");
    assert_eq!(bad.action, "login_failed");
    assert!(
        bad.actor.chars().count() <= audit::CAP_FIELD + 1,
        "{}",
        bad.actor.len()
    );
    assert!(bad.actor.ends_with('\u{2026}'));
    assert!(!bad.actor.chars().any(|c| c.is_control() || c == '\u{202e}'));
    // Markup is not removed (the window shows text only); it is just text.
    assert!(bad.actor.contains("<script>"));
}

#[test]
fn clean_strips_controls_and_overrides_and_cuts_by_characters() {
    assert_eq!(audit::clean("a\u{202e}b\nc\u{0}d", 10), "a b c d");
    assert_eq!(audit::clean("abcdef", 3), "abc\u{2026}");
    assert_eq!(audit::clean("abc", 3), "abc");
    // Multi-byte characters are counted as one each and never split.
    assert_eq!(
        audit::clean("\u{1f600}\u{1f600}\u{1f600}", 2),
        "\u{1f600}\u{1f600}\u{2026}"
    );
    assert_eq!(audit::clean("", 5), "");
}

#[tokio::test]
async fn an_ordinary_device_gets_the_forbidden_state_for_both_routes() {
    let a = Agent::start(free_port());
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let (dir, st) = (TempDir::new().unwrap(), store());
    flows::pair(dir.path(), &a.host, &fp, &a.pair_code(), "Phone", &st)
        .await
        .unwrap();

    let p = audit::fetch_audit(dir.path(), &st, None, 100)
        .await
        .unwrap();
    assert!(
        p.forbidden && p.entries.is_empty() && p.next_before.is_none(),
        "{p:?}"
    );
    let l = audit::fetch_logs(dir.path(), &st).await.unwrap();
    assert!(l.forbidden && l.lines.is_empty(), "{l:?}");
    // A refusal for rights is not a dead login: the token stays.
    assert!(!flows::load_saved(dir.path(), &st).unwrap().token.is_empty());
}

#[tokio::test]
async fn the_agent_log_is_listed_for_an_admin_and_empty_without_a_journal() {
    let s = setup().await;
    let l = audit::fetch_logs(s.admin.path(), &s.admin_store)
        .await
        .unwrap();
    // The test agent is not a systemd unit (and has no journalctl on its PATH): the agent answers
    // with an empty list, which is a normal answer.
    assert!(!l.forbidden);
    assert!(l.lines.len() <= audit::MAX_LOG_LINES);
}

#[tokio::test]
async fn a_revoked_login_is_an_error_and_its_token_is_dropped() {
    let s = setup().await;
    // The code-paired device was revoked in `setup`.
    let e = audit::fetch_audit(s.code_dir.path(), &s.code_store, None, 100)
        .await
        .unwrap_err();
    assert!(matches!(e, AgentError::Server { status: 401, .. }), "{e:?}");
    assert!(flows::load_saved(s.code_dir.path(), &s.code_store)
        .unwrap()
        .token
        .is_empty());
    let again = audit::fetch_logs(s.code_dir.path(), &s.code_store)
        .await
        .unwrap_err();
    assert!(again.to_string().contains("not signed in"), "{again}");
    drop(s.agent);
}

#[tokio::test]
async fn bad_input_is_refused_before_it_reaches_the_agent() {
    let s = setup().await;
    for (before, limit) in [(Some(0), 10), (Some(-5), 10), (None, 0), (None, 501)] {
        let e = audit::fetch_audit(s.admin.path(), &s.admin_store, before, limit)
            .await
            .unwrap_err();
        assert!(
            matches!(e, AgentError::Local(_)),
            "{before:?} {limit}: {e:?}"
        );
    }
    let nobody = TempDir::new().unwrap();
    let e = audit::fetch_audit(nobody.path(), &store(), None, 10)
        .await
        .unwrap_err();
    assert_eq!(e.to_string(), "not signed in");
}
