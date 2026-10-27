//! The window's script source, for the tests that check every message the app can show against
//! `docs/user-guide.md`. The window is several files under `ui/` (no build step), so a message can
//! live in any of them.

use std::path::Path;

/// Every `ui/*.js`, in name order, joined.
pub fn js() -> String {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("../ui");
    let mut files: Vec<_> = std::fs::read_dir(&dir)
        .unwrap_or_else(|e| panic!("{}: {e}", dir.display()))
        .filter_map(|e| e.ok().map(|e| e.path()))
        .filter(|p| p.extension().is_some_and(|x| x == "js"))
        .collect();
    files.sort();
    files
        .iter()
        .map(|p| std::fs::read_to_string(p).unwrap_or_else(|e| panic!("{}: {e}", p.display())))
        .collect::<Vec<_>>()
        .join("\n")
}
