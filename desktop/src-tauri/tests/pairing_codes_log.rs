//! A generated pairing code must never be written to the in-memory log (the text a user may paste
//! into a bug report). Its own test binary: the log is process-wide and this test is the only one
//! in the process. The log masks any secret it knows as `***`, so the check is twofold: the code
//! is not in the text, and nothing in it was masked, which would mean the app tried to log one.

mod common;

use common::{free_port, Agent};
use rfe_desktop_lib::agent_client::capture_fingerprint;
use rfe_desktop_lib::applog::{self, Level};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::pairing_codes;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use tempfile::TempDir;

#[tokio::test]
async fn the_log_holds_no_pairing_code_after_generating_one_or_being_refused() {
    applog::clear();
    applog::set_level(Level::Debug);
    const PW: &str = "pw-for-the-pairing-code-log-test";
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());

    let admin = TempDir::new().unwrap();
    flows::login(admin.path(), &a.host, &fp, "owner", PW, "Admin", &store)
        .await
        .unwrap();
    let made = pairing_codes::generate(admin.path(), &store).await.unwrap();
    let again = pairing_codes::generate(admin.path(), &store).await.unwrap();
    assert!(made.status == "ok" && again.status == "ok");

    let ordinary = TempDir::new().unwrap();
    flows::pair(ordinary.path(), &a.host, &fp, &made.code, "Phone", &store)
        .await
        .unwrap();
    let refused = pairing_codes::generate(ordinary.path(), &store)
        .await
        .unwrap();
    assert_eq!(refused.status, "forbidden");

    let text = applog::lines().join("\n");
    assert!(text.contains("pairing code generation: ok"), "{text}");
    assert!(text.contains("refused"), "{text}");
    for code in [&made.code, &again.code] {
        assert!(
            !text.contains(code.as_str()),
            "a code is in the log: {text}"
        );
    }
    assert!(
        !text.contains("***"),
        "something the app knows is secret was logged (and masked): {text}"
    );
}
