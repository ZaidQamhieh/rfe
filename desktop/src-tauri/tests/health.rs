//! The health and metrics screen's data: every field the window shows is read from a throwaway
//! agent, optional fields may be missing, a non-admin session gets its own state (the agent's 403)
//! instead of an error, and an agent that is down or silent ends in a clear message within the
//! timeout, never a wait.

mod common;

#[path = "support/ui.rs"]
mod ui;
use common::{free_port, Agent, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentClient, AgentError};
use rfe_desktop_lib::flows;
use rfe_desktop_lib::health::{self, AgentStatus, Health, Metrics};
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use std::net::TcpListener;
use std::path::Path;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tempfile::TempDir;

const PW: &str = "pw-for-health-tests";

async fn admin_session(a: &Agent) -> (TempDir, Offloaded, String) {
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    flows::login(dir.path(), &a.host, &fp, "owner", PW, "Admin", &store)
        .await
        .unwrap();
    (dir, store, fp)
}

async fn code_session(a: &Agent) -> (TempDir, Offloaded) {
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let dir = TempDir::new().unwrap();
    flows::pair(dir.path(), &a.host, &fp, &a.pair_code(), "Code", &store)
        .await
        .unwrap();
    (dir, store)
}

fn now_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as i64
}

#[tokio::test]
async fn an_admin_session_gets_every_field_the_screen_shows() {
    let a = Agent::start(free_port());
    let (dir, store, fp) = admin_session(&a).await;

    let s = health::agent_health(dir.path(), &store).await.unwrap();
    // What this computer is talking to.
    assert_eq!(s.host, a.host);
    assert_eq!(s.fingerprint, fp);
    assert_eq!(s.fingerprint, a.status_fingerprint());
    // /health
    assert_eq!(s.health.status, "ok");
    assert_eq!(s.health.name, "rfe-desktop-test");
    assert!(!s.health.version.is_empty(), "{:?}", s.health);
    assert_eq!(s.health.os, std::env::consts::OS);
    assert!(s.health.read_only.is_some(), "{:?}", s.health);
    // /status
    let st = s.status.as_ref().expect("status is read");
    assert!(st.uptime_seconds.unwrap() >= 0, "{st:?}");
    assert!(st.platform.contains('/'), "{st:?}");
    let (free, total) = (st.free_bytes.unwrap(), st.total_bytes.unwrap());
    assert!(total > 0 && free >= 0 && free <= total, "{st:?}");
    assert_eq!(st.version, s.health.version);
    assert!(s.status_note.is_empty());
    // /metrics
    assert!(!s.metrics_forbidden && s.metrics_note.is_empty(), "{s:?}");
    let m = s.metrics.as_ref().expect("an admin reads the metrics");
    assert!(
        m.rx_bytes.unwrap() >= 0 && m.tx_bytes.unwrap() >= 0,
        "{m:?}"
    );
    for pct in [m.cpu_percent.unwrap(), m.ram_percent.unwrap()] {
        assert!((0.0..=100.0).contains(&pct), "{m:?}");
    }
    let age = (now_ms() - m.ts_ms.unwrap()).abs();
    assert!(age < 60_000, "the agent's reading time is far off: {m:?}");

    // The window gets camelCase names and nothing it should not hold.
    let json = serde_json::to_value(&s).unwrap();
    for key in [
        "host",
        "fingerprint",
        "health",
        "status",
        "statusNote",
        "metrics",
        "metricsForbidden",
        "metricsNote",
    ] {
        assert!(json.get(key).is_some(), "missing {key}: {json}");
    }
    assert!(json["metrics"]["rxBytes"].is_i64() && json["metrics"]["tsMs"].is_i64());
    assert!(json["status"]["uptimeSeconds"].is_i64());
    assert!(json["health"]["readOnly"].is_boolean());
    assert!(!json.to_string().contains("token"), "{json}");

    // A second reading: the counters never go backwards and the agent's clock moves on.
    let again = health::agent_health(dir.path(), &store).await.unwrap();
    let m2 = again.metrics.unwrap();
    assert!(m2.rx_bytes >= m.rx_bytes && m2.tx_bytes >= m.tx_bytes);
    assert!(m2.ts_ms >= m.ts_ms);
}

#[tokio::test]
async fn a_pairing_code_session_gets_the_forbidden_state_and_still_sees_the_health() {
    let a = Agent::start(free_port());
    let (dir, store) = code_session(&a).await;

    let s = health::agent_health(dir.path(), &store)
        .await
        .expect("a 403 on the metrics is a state of the screen, not an error");
    assert!(s.metrics_forbidden, "{s:?}");
    assert!(s.metrics.is_none() && s.metrics_note.is_empty(), "{s:?}");
    // Everything an ordinary device may read is still shown.
    assert_eq!(s.health.name, "rfe-desktop-test");
    assert!(s.status.is_some(), "{s:?}");
    assert_eq!(serde_json::to_value(&s).unwrap()["metricsForbidden"], true);

    // Control: the same agent answers an admin session, so the 403 was about the session.
    a.add_user("owner", PW);
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let admin_dir = TempDir::new().unwrap();
    flows::login(admin_dir.path(), &a.host, &fp, "owner", PW, "Admin", &store)
        .await
        .unwrap();
    let admin = health::agent_health(admin_dir.path(), &store)
        .await
        .unwrap();
    assert!(!admin.metrics_forbidden && admin.metrics.is_some());
}

#[tokio::test]
async fn an_agent_that_is_down_is_reported_at_once_with_what_to_check() {
    let a = Agent::start(free_port());
    let (dir, store, _fp) = admin_session(&a).await;
    drop(a); // kills the agent; its port now refuses connections

    let started = Instant::now();
    let err = health::agent_health(dir.path(), &store)
        .await
        .expect_err("nothing is listening any more");
    let text = err.to_string();
    assert!(text.contains("cannot reach the agent"), "{text}");
    assert!(text.contains("Check that the agent is running"), "{text}");
    assert!(
        started.elapsed() < health::REQUEST_TIMEOUT + Duration::from_secs(2),
        "took {:?}",
        started.elapsed()
    );
    // The login survives: an agent that is off is not a revoked login.
    assert!(!flows::load_saved(dir.path(), &store)
        .unwrap()
        .token
        .is_empty());
}

#[tokio::test]
async fn an_agent_that_answers_nothing_times_out_with_a_clear_message() {
    // Accepts the connection and then says nothing, as a hung or half-asleep PC does.
    let silent = TcpListener::bind("127.0.0.1:0").unwrap();
    let host = format!("127.0.0.1:{}", silent.local_addr().unwrap().port());
    let hold = std::thread::spawn(move || {
        let mut held = vec![];
        silent.set_nonblocking(true).unwrap();
        let until = Instant::now() + Duration::from_secs(15);
        while Instant::now() < until {
            if let Ok((s, _)) = silent.accept() {
                held.push(s);
            }
            std::thread::sleep(Duration::from_millis(20));
        }
    });

    let client = AgentClient::pinned(&host, &"ab".repeat(32)).unwrap();
    let started = Instant::now();
    let err = health::snapshot(
        &client,
        &host,
        &"ab".repeat(32),
        "token",
        Duration::from_secs(1),
    )
    .await
    .expect_err("a silent agent must not hang the screen");
    let took = started.elapsed();
    let text = err.to_string();
    assert!(
        text.contains("The agent did not answer within 1 second."),
        "{text}"
    );
    assert!(text.contains("check that it is running"), "{text}");
    assert!(
        took >= Duration::from_millis(900) && took < Duration::from_secs(4),
        "the three requests share one timeout window, took {took:?}"
    );
    drop(hold);
}

#[tokio::test]
async fn a_revoked_login_is_reported_and_its_dead_token_is_dropped() {
    let a = Agent::start(free_port());
    let (dir, store, _fp) = admin_session(&a).await;
    let device = flows::load_saved(dir.path(), &store).unwrap().device_id;
    a.revoke_cli(&device);

    let err = health::agent_health(dir.path(), &store)
        .await
        .expect_err("the agent refuses a revoked token");
    assert!(
        matches!(err, AgentError::Server { status: 401, .. }),
        "{err:?}"
    );
    assert!(
        err.to_string().contains("no longer accepts this login"),
        "{err}"
    );
    let saved = flows::load_saved(dir.path(), &store).unwrap();
    assert!(
        saved.token.is_empty() && !saved.host.is_empty(),
        "the dead token goes, the address stays: {saved:?}"
    );
}

#[tokio::test]
async fn without_a_login_nothing_is_sent() {
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let err = health::agent_health(dir.path(), &store).await.unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m.contains("not signed in")),
        "{err:?}"
    );
}

#[tokio::test]
async fn a_forgotten_pin_ends_the_session_instead_of_trusting_the_old_certificate() {
    let a = Agent::start(free_port());
    let (dir, store, _fp) = admin_session(&a).await;
    // Forget the trust while a login for that address is still saved (a leftover state).
    let state = dir.path().join("state.json");
    let mut v: serde_json::Value = serde_json::from_slice(&std::fs::read(&state).unwrap()).unwrap();
    v["pins"] = serde_json::json!({});
    std::fs::write(&state, serde_json::to_vec(&v).unwrap()).unwrap();

    let err = health::agent_health(dir.path(), &store).await.unwrap_err();
    assert!(
        err.to_string()
            .contains("is no longer a trusted agent; connect and compare its fingerprint again"),
        "{err}"
    );
}

#[test]
fn missing_optional_fields_and_unknown_ones_are_tolerated() {
    // All a caller without a valid token gets from /health.
    let h: Health = serde_json::from_str(r#"{"status":"ok"}"#).unwrap();
    assert_eq!(h.status, "ok");
    assert!(h.name.is_empty() && h.version.is_empty() && h.read_only.is_none());
    assert!(h.address.is_empty() && h.tailscale_address.is_empty() && h.mac_address.is_empty());

    // The full answer, with a field this app has never heard of.
    let h: Health = serde_json::from_str(
        r#"{"status":"ok","name":"pc","version":"1.43.0","os":"linux","readOnly":true,
            "address":"192.168.1.20:8765","tailscaleAddress":"100.1.2.3:8765",
            "macAddress":"aa:bb:cc:dd:ee:ff","sidecars":{"indexd":"up"}}"#,
    )
    .unwrap();
    assert_eq!(h.read_only, Some(true));
    assert_eq!(h.tailscale_address, "100.1.2.3:8765");
    assert_eq!(h.mac_address, "aa:bb:cc:dd:ee:ff");

    // Absent is not zero: a missing counter is not shown as 0 bytes.
    let m: Metrics = serde_json::from_str("{}").unwrap();
    assert!(m.rx_bytes.is_none() && m.cpu_percent.is_none() && m.ts_ms.is_none());
    let m: Metrics =
        serde_json::from_str(r#"{"rxBytes":0,"txBytes":1536,"cpuPercent":12.5,"tsMs":1}"#).unwrap();
    assert_eq!((m.rx_bytes, m.tx_bytes), (Some(0), Some(1536)));
    assert_eq!((m.cpu_percent, m.ram_percent), (Some(12.5), None));

    let s: AgentStatus = serde_json::from_str(r#"{"uptimeSeconds":90}"#).unwrap();
    assert_eq!(s.uptime_seconds, Some(90));
    assert!(s.free_bytes.is_none() && s.platform.is_empty());
}

#[tokio::test]
async fn the_bare_answer_a_stranger_gets_from_the_real_agent_parses() {
    let a = Agent::start(free_port());
    let raw = Raw::new(&a.host);
    let h: Health = raw
        .http
        .get(format!("{}/health", raw.base))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    assert_eq!(h.status, "ok");
    assert!(h.name.is_empty(), "no token, no detail: {h:?}");
}

/// The app's own messages on this screen are in the user guide, and really are in the app.
#[test]
fn every_message_of_the_screen_is_explained_in_the_guide() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let guide = std::fs::read_to_string(root.join("../docs/user-guide.md")).unwrap();
    let mut code = std::fs::read_to_string(root.join("src/agent_client.rs")).unwrap();
    code.push_str(&std::fs::read_to_string(root.join("src/health.rs")).unwrap());
    code.push_str(&ui::js());
    let flat: String = code
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .replace("\\ ", "")
        .replace("\" \"", "");
    for stem in [
        "The agent did not answer within",
        "unexpected response: the agent's health answer does not say ok",
        "Metrics are for administrators",
        "No metrics were reported",
        "not reported",
    ] {
        assert!(guide.contains(stem), "the guide does not explain {stem:?}");
        assert!(
            flat.contains(stem) || code.contains(stem),
            "{stem:?} is no longer in the app"
        );
    }
}
