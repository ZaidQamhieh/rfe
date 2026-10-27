//! The audit and logs screen's messages are explained in the user guide, and are really in the app.

#[path = "support/ui.rs"]
mod ui;
use std::path::Path;

/// Stems of what this feature shows that the agent did not word, from `src/audit.rs` and `ui/*.js`.
const MESSAGES: &[&str] = &[
    "This login cannot read the audit log or the agent log.",
    "The audit log is empty.",
    "No loaded event matches these filters.",
    "No log line matches these filters.",
    "The agent's log is empty.",
    "unexpected audit cursor",
    "the page size must be",
];

fn read(rel: &str) -> String {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join(rel);
    std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("{}: {e}", path.display()))
}

#[test]
fn every_message_of_the_audit_screen_is_in_the_guide_and_in_the_app() {
    let guide = read("../docs/user-guide.md");
    let code = format!("{}{}", read("src/audit.rs"), ui::js());
    for stem in MESSAGES {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
        assert!(code.contains(stem), "{stem:?} is no longer in the app");
    }
}
