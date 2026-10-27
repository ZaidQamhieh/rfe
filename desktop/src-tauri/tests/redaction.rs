//! A `{:?}` of anything that carries a secret must not print it.

use rfe_desktop_lib::agent_client::LoginOk;
use rfe_desktop_lib::flows::{PairWait, Saved};

#[test]
fn debug_output_hides_tokens_and_nonces() {
    let saved = Saved {
        host: "pc:8765".into(),
        token: "tok-secret-1".into(),
        ..Default::default()
    };
    let ok = LoginOk {
        device_token: "tok-secret-2".into(),
        device_id: "dev1".into(),
        ..Default::default()
    };
    let wait = PairWait {
        host: "pc:8765".into(),
        fingerprint: "ab".repeat(32),
        request_id: "req1".into(),
        client_nonce: "nonce-secret-3".into(),
        match_code: "1234 5678".into(),
        expires_in_seconds: 60,
    };
    let text = format!("{saved:?} {ok:?} {wait:?}");
    for secret in ["tok-secret-1", "tok-secret-2", "nonce-secret-3"] {
        assert!(!text.contains(secret), "{secret} leaked: {text}");
    }
    assert!(text.contains("pc:8765") && text.contains("dev1"), "{text}");
}

/// The approval poll carries its secret as a query parameter; a connection error that quotes the
/// request URL would put it in text the window shows.
#[tokio::test]
async fn a_failed_approval_poll_does_not_quote_its_nonce() {
    use rfe_desktop_lib::agent_client::AgentClient;
    // Nothing listens on this port, so the request fails before any TLS.
    let client = AgentClient::pinned("127.0.0.1:1", &"ab".repeat(32)).unwrap();
    let err = client
        .poll_pair_request("req1", "nonce-secret-4")
        .await
        .unwrap_err();
    assert!(
        !err.to_string().contains("nonce-secret-4"),
        "nonce leaked: {err}"
    );
}
