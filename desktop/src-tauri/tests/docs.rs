//! The troubleshooting list covers every message the app can show. The agent's error codes are
//! checked against the real wording, so a changed or added code fails here until the guide says so.
//! Messages that come from the app itself are listed below by a stem the guide must contain: when
//! you add a message the user can see, add its stem here and a row to the guide.

#[path = "support/ui.rs"]
mod ui;
use rfe_desktop_lib::agent_client::{known_message, KNOWN_CODES};
use std::path::Path;

fn doc(name: &str) -> String {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../docs")
        .join(name);
    std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()))
}

#[test]
fn every_agent_error_code_is_in_the_guide_with_its_exact_wording() {
    let guide = doc("user-guide.md");
    for code in KNOWN_CODES {
        let text = known_message(code).expect("a known code has a message");
        assert!(
            guide.contains(&format!("| {text} | `{code}` |")),
            "user-guide.md has no troubleshooting row for {code} with the text {text:?}"
        );
    }
}

/// Stems of the messages the app itself produces, from `src/` and `ui/*.js`.
const APP_MESSAGES: &[&str] = &[
    "agent address must be host:port",
    "cannot reach the agent securely",
    "This agent's certificate is not the one you trusted",
    "the agent presented no certificate",
    "the agent reports a different fingerprint than the pinned one",
    "fingerprint must be 64 hex characters",
    "unexpected response",
    "unexpected request id",
    "unexpected device id",
    "unknown pairing status",
    "enter the pairing code",
    "This agent is too old to approve a new computer from the PC",
    "The request was rejected on the PC.",
    "The request expired or was already used. Ask again.",
    "The request timed out. Ask again.",
    "The PC approved this computer, but saving the login failed",
    "not signed in",
    "is no longer a trusted agent; connect and compare its fingerprint again",
    "Sign out first; the saved login belongs to the current device key.",
    "Signed out on this computer only",
    "unknown log level",
    "cannot start network discovery",
    "network discovery stopped",
    "; delete it to start over",
    "delete it to create a new device identity",
    "tls config",
];

/// Stems of the keystore messages, which keystore.md explains.
const KEYSTORE_MESSAGES: &[&str] = &[
    "no OS keystore answered",
    "the OS keystore refused access",
    "the OS keystore did not answer within",
    "the OS keystore did not keep the login token",
    "the OS keystore did not return what was saved",
    "the OS keystore call failed to run",
    "the keystore identity is damaged",
    "holds a different key than the keystore",
];

#[test]
fn every_message_the_app_makes_up_is_explained() {
    let guide = doc("user-guide.md");
    for stem in APP_MESSAGES {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
    }
    let keystore = doc("keystore.md");
    for stem in KEYSTORE_MESSAGES {
        assert!(
            keystore.contains(stem),
            "keystore.md does not explain {stem:?}"
        );
    }
}

/// The stems really are in the app, so the list above cannot rot into checking nothing.
#[test]
fn the_listed_messages_still_exist_in_the_app() {
    let src = Path::new(env!("CARGO_MANIFEST_DIR"));
    let mut code = String::new();
    for f in [
        "agent_client.rs",
        "discovery.rs",
        "flows.rs",
        "identity.rs",
        "secrets.rs",
    ] {
        code.push_str(&std::fs::read_to_string(src.join("src").join(f)).unwrap());
    }
    code.push_str(&ui::js());
    // Source literals wrap long strings across lines; compare with the wrapping removed.
    let flat: String = code
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .replace("\\ ", "")
        .replace("\" \"", "");
    for stem in APP_MESSAGES.iter().chain(KEYSTORE_MESSAGES) {
        assert!(
            flat.contains(stem) || code.contains(stem),
            "{stem:?} is listed but no longer in the app's source"
        );
    }
}
