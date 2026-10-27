//! Admin device actions against a throwaway agent: an account session changes the access of,
//! revokes and removes another paired device, each checked on the agent itself with the PC-side
//! CLI (`rfe devices`, `rfe audit`); an ordinary (code-paired) session is refused; the
//! computer's own device needs a confirmation. Run with RFE_AGENT_BIN set to a built agent.

mod common;

#[path = "support/ui.rs"]
mod ui;
use common::{free_port, Agent, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentClient, AgentError};
use rfe_desktop_lib::device_actions::{
    self as actions, explain, AccessPatch, GONE, LAUNCH_NEEDS_VIEW, NOTHING_CHANGED, NOT_ADMIN,
    SELF_UNCONFIRMED,
};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use serde_json::Value;
use std::path::Path;
use tempfile::TempDir;

struct Fixture {
    a: Agent,
    store: Offloaded,
    fp: String,
    admin_dir: TempDir,
    admin_id: String,
    admin_token: String,
}

async fn fixture() -> Fixture {
    let a = Agent::start(free_port());
    a.add_user("owner", "pw-for-device-actions");
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let admin_dir = TempDir::new().unwrap();
    let admin = flows::login(
        admin_dir.path(),
        &a.host,
        &fp,
        "owner",
        "pw-for-device-actions",
        "Admin PC",
        &store,
    )
    .await
    .unwrap();
    Fixture {
        a,
        store,
        fp,
        admin_dir,
        admin_id: admin.device_id,
        admin_token: admin.token,
    }
}

impl Fixture {
    /// Another computer, paired with a code: an ordinary device with its own saved login.
    async fn pair_other(&self) -> (TempDir, flows::Saved) {
        let dir = TempDir::new().unwrap();
        let saved = flows::pair(
            dir.path(),
            &self.a.host,
            &self.fp,
            &self.a.pair_code(),
            "Other PC",
            &self.store,
        )
        .await
        .unwrap();
        (dir, saved)
    }

    fn admin(&self) -> &Path {
        self.admin_dir.path()
    }

    /// The agent's own view of one device: its `rfe devices` line.
    fn cli_row(&self, id: &str) -> Option<String> {
        self.a
            .devices_cli()
            .lines()
            .find(|l| l.starts_with(&id[..8]))
            .map(str::to_string)
    }

    async fn roots(&self) -> Vec<String> {
        let v: Value = Raw::new(&self.a.host)
            .http
            .get(format!("https://{}/v1/settings", self.a.host))
            .bearer_auth(&self.admin_token)
            .send()
            .await
            .unwrap()
            .json()
            .await
            .unwrap();
        v["roots"]
            .as_array()
            .expect("the agent reports its roots")
            .iter()
            .map(|r| r.as_str().unwrap().to_string())
            .collect()
    }
}

fn status(e: &AgentError) -> u16 {
    match e {
        AgentError::Server { status, .. } => *status,
        other => panic!("not a server error: {other:?}"),
    }
}

#[tokio::test]
async fn an_admin_session_changes_the_access_of_another_device_and_the_agent_agrees() {
    let f = fixture().await;
    let (_d, other) = f.pair_other().await;
    let id = other.device_id.as_str();

    // A new code-paired device starts browse-only, with no limits.
    let before = actions::device_access(f.admin(), &f.store, id)
        .await
        .unwrap();
    assert!(
        before.browse && !before.download && !before.upload,
        "{before:?}"
    );
    assert!(!before.read_only && before.jail_root.is_empty());
    assert!(f.cli_row(id).unwrap().contains("read-write"));

    // File grants and read-only, in one change.
    let after = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            download: Some(true),
            upload: Some(true),
            read_only: Some(true),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap();
    assert!(
        after.download && after.upload && after.read_only && after.browse,
        "{after:?}"
    );
    assert!(!after.modify && !after.delete && !after.share);
    let row = f.cli_row(id).unwrap();
    assert!(row.contains("read-only"), "the agent CLI shows it: {row}");
    let audit = f.a.audit_cli();
    assert!(
        audit.contains("device.updated") || audit.contains("updated"),
        "the change is audited: {audit}"
    );
    assert!(
        audit.contains("download=true") && audit.contains("readOnly=true"),
        "{audit}"
    );

    // Turning a grant off again leaves the others as they were.
    let back = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            download: Some(false),
            read_only: Some(false),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap();
    assert!(!back.download && !back.read_only && back.upload, "{back:?}");
    assert!(f.cli_row(id).unwrap().contains("read-write"));

    // The folder limit: set inside the agent's roots, read back, cleared.
    let root = f.roots().await.remove(0);
    let work = Path::new(&root).join("work");
    std::fs::create_dir_all(&work).unwrap();
    let jailed = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            jail_root: Some(work.to_str().unwrap().to_string()),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap();
    assert_eq!(
        std::fs::canonicalize(&jailed.jail_root).unwrap(),
        std::fs::canonicalize(&work).unwrap()
    );
    assert!(f.a.audit_cli().contains("jailRoot="), "audited");
    let read = actions::device_access(f.admin(), &f.store, id)
        .await
        .unwrap();
    assert_eq!(read.jail_root, jailed.jail_root, "a fresh read agrees");
    let cleared = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            jail_root: Some(String::new()),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap();
    assert!(cleared.jail_root.is_empty());

    // A limit outside the agent's folders is refused with the agent's reason, and changes nothing.
    let e = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            jail_root: Some("/definitely/not/inside/the/roots".into()),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap_err();
    assert_eq!(status(&e), 400, "{e:?}");
    assert!(
        explain(&e).starts_with("The agent refused that change:"),
        "{}",
        explain(&e)
    );
    assert!(actions::device_access(f.admin(), &f.store, id)
        .await
        .unwrap()
        .jail_root
        .is_empty());

    // App permissions: the agent's own rule (launch needs view) is checked before the call too.
    let e = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            view_apps: Some(false),
            launch_apps: Some(true),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap_err();
    assert_eq!(e.to_string(), LAUNCH_NEEDS_VIEW);
    let apps = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            view_apps: Some(true),
            launch_apps: Some(true),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap();
    assert!(apps.view_apps && apps.launch_apps);

    // An empty change is not sent.
    let e = actions::set_access(f.admin(), &f.store, id, &AccessPatch::default(), false)
        .await
        .unwrap_err();
    assert_eq!(e.to_string(), NOTHING_CHANGED);
}

#[tokio::test]
async fn an_admin_session_revokes_then_removes_another_device_and_the_agent_agrees() {
    let f = fixture().await;
    let (other_dir, other) = f.pair_other().await;
    let (_d2, bystander) = f.pair_other().await;
    let id = other.device_id.as_str();

    // Control: the device works, and the agent lists it active.
    assert!(flows::list_devices(other_dir.path(), &f.store)
        .await
        .is_ok());
    assert!(f.cli_row(id).unwrap().contains("active"));

    let done = actions::revoke(f.admin(), &f.store, id, false)
        .await
        .unwrap();
    assert!(!done.signed_out);
    let row = f.cli_row(id).unwrap();
    assert!(
        row.contains("revoked"),
        "the agent CLI shows it revoked: {row}"
    );
    // Its token stops working at once...
    match AgentClient::pinned(&f.a.host, &f.fp)
        .unwrap()
        .devices(&other.token)
        .await
    {
        Err(AgentError::Server { status: 401, .. }) => {}
        other => panic!("a revoked device must be refused, got {other:?}"),
    }
    // ...the row is still listed (as revoked), and nothing else was touched.
    let listed = flows::list_devices(f.admin(), &f.store).await.unwrap();
    let mine = listed.iter().find(|d| d.id == id).expect("still listed");
    assert!(mine.revoked);
    assert!(f.cli_row(&bystander.device_id).unwrap().contains("active"));
    assert!(f.cli_row(&f.admin_id).unwrap().contains("active"));
    // Revoking twice is harmless.
    actions::revoke(f.admin(), &f.store, id, false)
        .await
        .unwrap();

    actions::remove(f.admin(), &f.store, id, false)
        .await
        .unwrap();
    assert!(f.cli_row(id).is_none(), "the agent no longer has the row");
    let listed = flows::list_devices(f.admin(), &f.store).await.unwrap();
    assert!(listed.iter().all(|d| d.id != id));
    assert_eq!(
        listed.len(),
        2,
        "the admin and the bystander remain: {listed:?}"
    );
    let audit = f.a.audit_cli();
    assert!(
        audit.contains("revoked") && audit.contains("removed"),
        "{audit}"
    );

    // Removing an active device in one step blocks it too.
    actions::remove(f.admin(), &f.store, &bystander.device_id, false)
        .await
        .unwrap();
    assert!(f.cli_row(&bystander.device_id).is_none());
    assert!(AgentClient::pinned(&f.a.host, &f.fp)
        .unwrap()
        .devices(&bystander.token)
        .await
        .is_err());

    // A device that is already gone: changing its access is a 404 with its own message.
    let e = actions::set_access(
        f.admin(),
        &f.store,
        id,
        &AccessPatch {
            browse: Some(false),
            ..Default::default()
        },
        false,
    )
    .await
    .unwrap_err();
    assert_eq!(status(&e), 404, "{e:?}");
    assert_eq!(explain(&e), GONE);
    let e = actions::device_access(f.admin(), &f.store, id)
        .await
        .unwrap_err();
    assert_eq!(explain(&e), GONE);
}

#[tokio::test]
async fn an_ordinary_session_is_refused_and_nothing_changes() {
    let f = fixture().await;
    let (ordinary_dir, ordinary) = f.pair_other().await;
    let (_d, target) = f.pair_other().await;
    let ord = ordinary_dir.path();
    let tid = target.device_id.as_str();

    let patch = AccessPatch {
        download: Some(true),
        ..Default::default()
    };
    let e = actions::set_access(ord, &f.store, tid, &patch, false)
        .await
        .unwrap_err();
    assert_eq!(status(&e), 403, "{e:?}");
    assert_eq!(explain(&e), NOT_ADMIN);
    // Not even for itself: access limits are for admins only.
    let e = actions::set_access(ord, &f.store, &ordinary.device_id, &patch, true)
        .await
        .unwrap_err();
    assert_eq!(explain(&e), NOT_ADMIN);
    let e = actions::revoke(ord, &f.store, tid, false)
        .await
        .unwrap_err();
    assert_eq!((status(&e), explain(&e)), (403, NOT_ADMIN.to_string()));
    let e = actions::remove(ord, &f.store, tid, false)
        .await
        .unwrap_err();
    assert_eq!((status(&e), explain(&e)), (403, NOT_ADMIN.to_string()));
    let e = actions::device_access(ord, &f.store, tid)
        .await
        .unwrap_err();
    assert_eq!(
        explain(&e),
        GONE,
        "it only ever sees itself, so another device is simply not there"
    );

    // The agent still has the target exactly as it was.
    let row = f.cli_row(tid).unwrap();
    assert!(
        row.contains("active") && row.contains("read-write"),
        "{row}"
    );
    assert!(
        !actions::device_access(f.admin(), &f.store, tid)
            .await
            .unwrap()
            .download,
        "the refused change was not applied"
    );

    // Control: the same calls from the account session work, so the 403s were about the session.
    actions::set_access(f.admin(), &f.store, tid, &patch, false)
        .await
        .unwrap();
    actions::revoke(f.admin(), &f.store, tid, false)
        .await
        .unwrap();
}

#[tokio::test]
async fn acting_on_this_computers_own_device_needs_a_confirmation_and_signs_it_out() {
    let f = fixture().await;
    let (_d, other) = f.pair_other().await;

    for call in 0..3 {
        let e = match call {
            0 => actions::revoke(f.admin(), &f.store, &f.admin_id, false)
                .await
                .map(|_| ()),
            1 => actions::remove(f.admin(), &f.store, &f.admin_id, false)
                .await
                .map(|_| ()),
            _ => actions::set_access(
                f.admin(),
                &f.store,
                &f.admin_id,
                &AccessPatch {
                    read_only: Some(true),
                    ..Default::default()
                },
                false,
            )
            .await
            .map(|_| ()),
        }
        .unwrap_err();
        assert_eq!(e.to_string(), SELF_UNCONFIRMED, "call {call}");
    }
    assert!(
        f.cli_row(&f.admin_id).unwrap().contains("active"),
        "nothing reached the agent"
    );
    assert!(!flows::load_saved(f.admin(), &f.store)
        .unwrap()
        .token
        .is_empty());

    // Confirmed: the agent revokes it, and the login here is cleared (the token is dead anyway).
    let done = actions::revoke(f.admin(), &f.store, &f.admin_id, true)
        .await
        .unwrap();
    assert!(done.signed_out);
    assert!(f.cli_row(&f.admin_id).unwrap().contains("revoked"));
    assert!(flows::load_saved(f.admin(), &f.store)
        .unwrap()
        .token
        .is_empty());
    let e = flows::list_devices(f.admin(), &f.store).await.unwrap_err();
    assert!(e.to_string().contains("not signed in"), "{e}");
    assert!(
        f.cli_row(&other.device_id).unwrap().contains("active"),
        "others untouched"
    );
}

#[tokio::test]
async fn a_login_saved_without_its_device_id_still_needs_the_self_confirmation() {
    let f = fixture().await;
    // An older state file: the login is there, the device id is not.
    let state = f.admin().join("state.json");
    let text = std::fs::read_to_string(&state).unwrap();
    assert!(text.contains(&f.admin_id), "{text}");
    std::fs::write(&state, text.replace(&f.admin_id, "")).unwrap();
    assert!(flows::load_saved(f.admin(), &f.store)
        .unwrap()
        .device_id
        .is_empty());

    let e = actions::revoke(f.admin(), &f.store, &f.admin_id, false)
        .await
        .unwrap_err();
    assert_eq!(e.to_string(), SELF_UNCONFIRMED);
    assert!(
        f.cli_row(&f.admin_id).unwrap().contains("active"),
        "nothing reached the agent"
    );

    // Another device is still no trouble, and needs no confirmation.
    let (_dir, other) = f.pair_other().await;
    actions::revoke(f.admin(), &f.store, &other.device_id, false)
        .await
        .unwrap();
    assert!(f.cli_row(&other.device_id).unwrap().contains("revoked"));
}

#[tokio::test]
async fn an_ordinary_session_may_still_remove_itself_when_it_confirms() {
    let f = fixture().await;
    let (dir, me) = f.pair_other().await;
    let done = actions::remove(dir.path(), &f.store, &me.device_id, true)
        .await
        .unwrap();
    assert!(done.signed_out);
    assert!(f.cli_row(&me.device_id).is_none());
    assert!(flows::load_saved(dir.path(), &f.store)
        .unwrap()
        .token
        .is_empty());
}

#[tokio::test]
async fn ids_that_could_change_the_url_are_refused_before_anything_is_sent() {
    let f = fixture().await;
    for bad in ["", "..", "../settings", "a/b", "a?purge=true", "a b", "ä"] {
        let e = actions::revoke(f.admin(), &f.store, bad, true)
            .await
            .unwrap_err();
        assert!(
            matches!(&e, AgentError::Local(m) if m.contains("unexpected device id")),
            "{bad:?}: {e:?}"
        );
        let e = actions::remove(f.admin(), &f.store, bad, true)
            .await
            .unwrap_err();
        assert!(
            matches!(&e, AgentError::Local(m) if m.contains("unexpected device id")),
            "{bad:?}"
        );
    }
    assert!(f.cli_row(&f.admin_id).unwrap().contains("active"));
}

#[tokio::test]
async fn without_a_login_the_actions_say_so() {
    let f = fixture().await;
    let nobody = TempDir::new().unwrap();
    let e = actions::revoke(nobody.path(), &f.store, "abc", false)
        .await
        .unwrap_err();
    assert!(
        matches!(&e, AgentError::Local(m) if m == "not signed in"),
        "{e:?}"
    );
}

/// The guide explains every message this feature makes up, and the source still has it.
#[test]
fn every_message_of_this_feature_is_in_the_guide() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let guide = std::fs::read_to_string(root.join("../docs/user-guide.md")).unwrap();
    let source = std::fs::read_to_string(root.join("src/device_actions.rs")).unwrap() + &ui::js();
    let flat = |s: &str| s.split_whitespace().collect::<Vec<_>>().join(" ");
    let (guide, source) = (
        flat(&guide),
        flat(&source).replace("\\ ", "").replace("\" \"", ""),
    );
    for stem in [
        "This login is not an admin session",
        "That device is no longer on the agent",
        "This agent does not support that device action",
        "This is the computer you are using",
        "Allowing a device to launch apps needs",
        "No setting was changed",
        "The folder limit must be one path",
        "The agent refused that change:",
        "Revoked",
        "Removed",
        "Saved the access of",
        "This computer is signed out; sign in again to continue.",
        "The list could not be refreshed",
        "This is the sign-in this app uses",
    ] {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
        assert!(
            source.contains(stem),
            "{stem:?} is listed but no longer in the source"
        );
    }
}
