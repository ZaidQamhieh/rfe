//! Every message of the file tools and the dialogs (properties, share links, trash, compress and
//! extract, checksum, wake, keystore and device-key checks, local files) has a row in the user guide,
//! and is still in the app. A stem that is not in the app any more is a stale row.

use std::path::Path;

#[path = "support/ui.rs"]
mod ui;

const MESSAGES: &[&str] = &[
    "Connect to a server first.",
    "Open a connected server first to upload into it.",
    "Open a connected server in Files first.",
    "Select one file on a connected server in Files first.",
    "Pick a server first: switch the main view to a server.",
    "Copying between two servers isn’t supported.",
    "Nothing to transfer",
    "Open the folder in Files to extract.",
    "Trash is already empty",
    "Deleted forever:",
    "That folder can’t be opened:",
    "Could not start:",
    "This sign-in cannot change what a device may do.",
    "The MAC address of",
    "Wake signal sent to",
    "The keystore works: a test secret was saved, read back and removed.",
    "The keystore is not working.",
    "A new device key will be created the next time you sign in.",
    "Tick the box to confirm you compared the fingerprints.",
    "Describe the problem first",
    "The clipboard is not available here",
    "a search needs 1 to 256 characters",
    "choose between 1 and 1000 items",
    "the checksum must be sha256, sha1 or md5",
    "a permission mode is 3 or 4 octal digits, for example 0755",
    "this file is not text, or is too large to open here",
    "a share link lasts from 1 minute to 24 hours",
    "that is not a share link id",
    "a MAC address looks like aa:bb:cc:dd:ee:ff",
    "a path must be absolute, with no .. step",
    "this item cannot be renamed",
    "a drive or the root cannot be deleted",
    "This is not a file.",
    "This file is too large to show as text (over 1 MB).",
    "This file is not text.",
    "This picture is too large to preview (over 8 MB).",
    "This file would start a program, so it is not opened from here.",
    "That folder does not exist on this computer.",
    "Too many files with that name in the folder.",
    "pick a folder or a file, not the root",
    "that folder holds too many files to upload at once",
    "The agent did not confirm that the change was made.",
    "items failed:",
];

fn read(rel: &str) -> String {
    let p = Path::new(env!("CARGO_MANIFEST_DIR")).join(rel);
    std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("{}: {e}", p.display()))
}

#[test]
fn every_tool_message_is_in_the_guide_and_still_in_the_app() {
    let guide = read("../docs/user-guide.md");
    let source = [read("src/fileops.rs"), read("src/local.rs"), ui::js()].concat();
    for stem in MESSAGES {
        assert!(
            guide.contains(&format!("| {stem} |")),
            "user-guide.md does not explain {stem:?}"
        );
        assert!(source.contains(stem), "{stem:?} is no longer in the app");
    }
}
