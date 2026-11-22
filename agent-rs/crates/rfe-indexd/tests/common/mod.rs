//! Helpers shared by the rfe-indexd integration tests.
#![allow(dead_code)]

use rfe_indexd::index::{Index, Limits};
use std::fs;
use std::path::Path;
use std::sync::atomic::AtomicBool;

/// The budget every test index is built with: large enough that no test tree is truncated.
pub const TEST_LIMITS: Limits = Limits {
    max_entries: 1_000_000,
    max_bytes: 1 << 30,
};

/// Builds an index of `root` with `TEST_LIMITS`.
pub fn build(root: &Path) -> Index {
    Index::build(
        &[root.to_str().unwrap().to_string()],
        TEST_LIMITS,
        &AtomicBool::new(false),
    )
}

/// Writes the file `x` (creating its parent directories).
pub fn touch(p: &Path) {
    touch_body(p, "x");
}

/// Writes `body` to the file `p`, creating its parent directories.
pub fn touch_body(p: &Path, body: &str) {
    fs::create_dir_all(p.parent().unwrap()).unwrap();
    fs::write(p, body).unwrap();
}
