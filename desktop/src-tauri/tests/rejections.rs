//! Every rejection the agent sends on sign-in and pairing has its own message that says what to
//! do next. The codes are triggered against a throwaway agent, not just listed.

mod common;

use common::{free_port, identity, Agent, Raw};
use rfe_desktop_lib::agent_client::{known_message, AgentError, KNOWN_CODES};
use rfe_desktop_lib::identity::Identity;
use serde_json::{json, Value};
use std::collections::BTreeMap;

const PW: &str = "pw-for-rejection-tests";

fn login_body(id: &Identity, password: &str, nonce: &str, signed: &str) -> Value {
    json!({
        "username": "owner", "password": password, "deviceLabel": "T",
        "deviceId": id.device_id(), "devicePublicKey": id.public_key_b64(),
        "nonce": nonce, "signature": id.sign_b64(signed),
    })
}

fn code_of(e: &AgentError) -> &str {
    match e {
        AgentError::Server { code, .. } => code,
        other => panic!("not a server error: {other:?}"),
    }
}

#[test]
fn every_known_code_has_its_own_message_and_unknown_codes_stay_raw() {
    let mut seen = BTreeMap::new();
    for code in KNOWN_CODES {
        let text = known_message(code).unwrap_or_else(|| panic!("{code} has no message"));
        assert!(text.len() > 20, "{code}: too terse: {text}");
        assert!(!text.contains(code), "{code}: shows the code, not words");
        if let Some(other) = seen.insert(text, code) {
            panic!("{code} and {other} share the message {text:?}");
        }
    }

    let unknown = AgentError::Server {
        status: 418,
        code: "TEAPOT".into(),
        message: "short and stout".into(),
    };
    assert_eq!(unknown.to_string(), "TEAPOT: short and stout");
    assert!(known_message("TEAPOT").is_none());
}

#[tokio::test]
async fn each_rejection_the_agent_can_be_made_to_send_reads_differently() {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let raw = Raw::new(&a.host);
    let (id, _d) = identity();
    let (other, _d2) = identity();

    let mut refusals = vec![];
    // Wrong password with a valid device proof.
    let n = raw.nonce().await;
    refusals.push(
        raw.refused("/login", login_body(&id, "not-the-password", &n, &n))
            .await,
    );
    // A nonce the agent never issued.
    refusals.push(
        raw.refused(
            "/login",
            login_body(&id, PW, "00000000000000000000000000000000", "x"),
        )
        .await,
    );
    // A real nonce, signed over something else.
    let n = raw.nonce().await;
    refusals.push(
        raw.refused("/login", login_body(&id, PW, &n, "other"))
            .await,
    );
    // No device proof at all.
    refusals.push(
        raw.refused(
            "/login",
            json!({"username": "owner", "password": PW, "deviceLabel": "T", "deviceId": id.device_id()}),
        )
        .await,
    );
    // A pairing code that does not exist, with a valid device proof.
    let n = raw.nonce().await;
    refusals.push(
        raw.refused(
            "/pair",
            json!({
                "pairingCode": "000000", "deviceLabel": "T", "deviceId": id.device_id(),
                "devicePublicKey": id.public_key_b64(), "nonce": n,
                "signature": id.proof_b64(&a.status_fingerprint(), &n),
            }),
        )
        .await,
    );
    // The device id is bound to this key by a first login; a second key then claims it.
    let n = raw.nonce().await;
    raw.ok("/login", login_body(&id, PW, &n, &n)).await;
    let n = raw.nonce().await;
    let mut stolen = login_body(&other, PW, &n, &n);
    stolen["deviceId"] = json!(id.device_id());
    refusals.push(raw.refused("/login", stolen).await);

    let want = [
        "INVALID_CREDENTIALS",
        "INVALID_NONCE",
        "INVALID_SIGNATURE",
        "DEVICE_KEY_REQUIRED",
        "INVALID_CODE",
        "DEVICE_KEY_MISMATCH",
    ];
    let got: Vec<_> = refusals.iter().map(code_of).collect();
    assert_eq!(
        got, want,
        "the triggers did not produce the codes they target"
    );

    let mut texts = BTreeMap::new();
    for e in &refusals {
        let code = code_of(e);
        assert_eq!(e.to_string(), known_message(code).unwrap(), "{code}");
        if let AgentError::Server { message, .. } = e {
            assert_ne!(
                &e.to_string(),
                message,
                "{code}: still the agent's raw text"
            );
        }
        if let Some(prev) = texts.insert(e.to_string(), code.to_string()) {
            panic!("{code} and {prev} read the same");
        }
    }
}

#[tokio::test]
async fn too_many_attempts_is_reported_as_rate_limited_with_a_wait_hint() {
    let a = Agent::start(free_port());
    a.add_user("owner", PW);
    let raw = Raw::new(&a.host);
    let (id, _d) = identity();

    let mut limited = None;
    for _ in 0..15 {
        let n = raw.nonce().await;
        let e = raw
            .refused("/login", login_body(&id, "wrong", &n, &n))
            .await;
        if code_of(&e) == "RATE_LIMITED" {
            limited = Some(e);
            break;
        }
        assert_eq!(
            code_of(&e),
            "INVALID_CREDENTIALS",
            "control: before the limit"
        );
    }
    let e = limited.expect("the agent never rate-limited 15 failed logins");
    assert!(e.to_string().contains("Wait a minute"), "{e}");
}

/// Nothing listens on the address: the error the window shows says so and what to check, and
/// it is not mistaken for a certificate problem.
#[tokio::test]
async fn an_unreachable_agent_is_reported_with_what_to_check() {
    let port = {
        let l = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        l.local_addr().unwrap().port()
    };
    let err = rfe_desktop_lib::agent_client::capture_fingerprint(&format!("127.0.0.1:{port}"))
        .await
        .expect_err("nothing is listening");
    let text = err.to_string();
    assert!(text.contains("cannot reach the agent"), "{text}");
    assert!(text.contains("Check that the agent is running"), "{text}");
    assert!(!text.contains("not the one you trusted"), "{text}");
}
