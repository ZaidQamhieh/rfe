//! The device proof is bound to the agent it is made for. The app signs the agent's certificate
//! fingerprint with the nonce, so an agent that relays a nonce fetched from a second agent gets a
//! signature the second agent refuses; and an agent that only knows the bare-nonce proof is refused
//! before any password is sent. Set RFE_AGENT_BIN to a built agent.

mod common;

use common::{free_port, identity, Agent, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentClient, ERR_AGENT_NO_BOUND_PROOF};
use serde_json::{json, Value};

const PW: &str = "pw-for-bound-proof-tests";

async fn advertises_v2(raw: &Raw) -> bool {
    let v: Value = raw
        .http
        .post(format!("{}/auth/challenge", raw.base))
        .send()
        .await
        .unwrap()
        .json()
        .await
        .unwrap();
    v["proof"] == "v2"
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn a_proof_made_for_another_agent_is_refused_and_one_for_this_agent_is_accepted() {
    let agent = Agent::start(free_port());
    agent.add_user("owner", PW);
    let raw = Raw::new(&agent.host);
    if !advertises_v2(&raw).await {
        assert!(
            std::env::var_os("RFE_ALLOW_OLD_AGENT").is_some(),
            "this agent does not offer the bound proof; set RFE_ALLOW_OLD_AGENT=1 to test an older release"
        );
        eprintln!("skipped: agent has no bound proof");
        return;
    }
    let (id, _dir) = identity();
    let body = |nonce: &str, signature: String| {
        json!({
            "username": "owner", "password": PW, "deviceLabel": "T",
            "deviceId": id.device_id(), "devicePublicKey": id.public_key_b64(),
            "nonce": nonce, "signature": signature,
        })
    };

    // A relay: the nonce came from this agent, the signature names some other agent's certificate.
    let n = raw.nonce().await;
    let other = "ab".repeat(32);
    let e = raw
        .refused("/login", body(&n, id.proof_b64(&other, &n)))
        .await;
    assert!(
        format!("{e:?}").contains("INVALID_SIGNATURE"),
        "a proof for another certificate must be refused: {e:?}"
    );

    // The same request with this agent's own fingerprint signs in.
    let n = raw.nonce().await;
    let mine = agent.status_fingerprint();
    let resp = raw
        .http
        .post(format!("{}/login", raw.base))
        .json(&body(&n, id.proof_b64(&mine, &n)))
        .send()
        .await
        .unwrap();
    assert!(resp.status().is_success(), "{}", resp.status());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn an_agent_without_the_bound_proof_is_refused_before_a_password_is_sent() {
    let agent = Agent::start(free_port());
    agent.add_user("owner", PW);
    let raw = Raw::new(&agent.host);
    if advertises_v2(&raw).await {
        return; // every other test signs in through the bound proof
    }
    assert!(
        std::env::var_os("RFE_ALLOW_OLD_AGENT").is_some(),
        "this agent does not offer the bound proof; set RFE_ALLOW_OLD_AGENT=1 to test an older release"
    );
    let fp = capture_fingerprint(&agent.host).await.unwrap();
    let client = AgentClient::pinned(&agent.host, &fp).unwrap();
    let (id, _dir) = identity();
    let e = client.login(&id, "owner", PW, "T").await.unwrap_err();
    assert_eq!(e.to_string(), ERR_AGENT_NO_BOUND_PROOF);
}

#[test]
fn the_proof_signs_the_fingerprint_and_the_nonce_and_not_the_bare_nonce() {
    use base64::{engine::general_purpose::STANDARD, Engine};
    use ed25519_dalek::{Signature, Verifier, VerifyingKey};
    let (id, _dir) = identity();
    let key = VerifyingKey::from_bytes(
        &STANDARD
            .decode(id.public_key_b64())
            .unwrap()
            .try_into()
            .unwrap(),
    )
    .unwrap();
    let sig = |s: &str| Signature::from_slice(&STANDARD.decode(s).unwrap()).unwrap();
    let fp = "AB".repeat(32);

    let proof = sig(&id.proof_b64(&fp, "nonce-1"));
    let message = format!("rfe-device-proof-v2\n{}\nnonce-1", "ab".repeat(32));
    assert!(key.verify(message.as_bytes(), &proof).is_ok());
    assert!(
        key.verify(b"nonce-1", &proof).is_err(),
        "a bare-nonce signature would be good at every agent"
    );
}

/// The app has one way to answer a challenge. Nothing in the code that talks to an agent may sign
/// a bare nonce, or an agent could trade a relayed nonce for a signature that works elsewhere.
#[test]
fn no_app_code_signs_a_bare_nonce() {
    let src = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("src");
    for entry in std::fs::read_dir(src).unwrap() {
        let path = entry.unwrap().path();
        if path.extension().is_some_and(|e| e == "rs") && !path.ends_with("identity.rs") {
            let text = std::fs::read_to_string(&path).unwrap();
            assert!(
                !text.contains("sign_b64("),
                "{} signs a bare message; use Identity::proof_b64",
                path.display()
            );
        }
    }
}
