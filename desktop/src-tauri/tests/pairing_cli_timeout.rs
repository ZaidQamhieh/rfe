//! A hung agent program must not freeze the pairing dialog: the program is given 10 seconds, then
//! stopped by its process id and the window gets a plain error. The stand-in agent is a shell script
//! that records its pid and sleeps far longer than the limit, so this runs where /bin/sh does (Unix);
//! a Windows build is covered by the same code path (std's child kill), not by this script.
//! Its own test binary: it sets RFE_AGENT_BIN for the whole process.
#![cfg(unix)]

use rfe_desktop_lib::pairing_codes;
use std::os::unix::fs::PermissionsExt;
use std::time::{Duration, Instant};
use tempfile::TempDir;

/// How long the stand-in sleeps: well past the 10 s limit, so only a timeout can end the call early.
const HANG_SECONDS: u64 = 25;

/// True while a process with this id exists (signal 0 only checks).
fn alive(pid: &str) -> bool {
    std::process::Command::new("kill")
        .args(["-0", pid])
        .stderr(std::process::Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

#[tokio::test]
async fn pairing_cli_that_hangs_is_stopped_after_the_timeout_and_its_pid_is_gone() {
    let dir = TempDir::new().unwrap();
    let pids = dir.path().join("pids");
    let script = dir.path().join("rfe-stub");
    std::fs::write(
        &script,
        format!(
            "#!/bin/sh\necho $$ >> '{}'\nexec sleep {HANG_SECONDS}\n",
            pids.display()
        ),
    )
    .unwrap();
    std::fs::set_permissions(&script, std::fs::Permissions::from_mode(0o755)).unwrap();
    std::env::set_var("RFE_AGENT_BIN", &script);

    let t = Instant::now();
    let (admin, code) = tokio::join!(
        pairing_codes::local_admin(vec!["devices".into(), "-json".into()]),
        pairing_codes::generate_local(60)
    );
    let took = t.elapsed();

    assert!(
        took < Duration::from_secs(HANG_SECONDS - 5),
        "the calls waited {took:?} for a hung program"
    );
    for (what, r) in [
        ("local_admin", admin.map(|_| ())),
        ("generate_local", code.map(|_| ())),
    ] {
        let e = r.expect_err(&format!("{what} answered although the program hung"));
        assert!(
            format!("{e:?}").contains("did not answer within"),
            "{what}: {e:?}"
        );
    }
    let recorded = std::fs::read_to_string(&pids).unwrap();
    let pids: Vec<&str> = recorded.split_whitespace().collect();
    assert_eq!(
        pids.len(),
        2,
        "both calls started the program: {recorded:?}"
    );
    for p in pids {
        assert!(!alive(p), "the hung program {p} was left running");
    }
}
