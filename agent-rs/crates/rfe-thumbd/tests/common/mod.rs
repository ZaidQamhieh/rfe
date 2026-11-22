//! Helpers shared by the rfe-thumbd integration tests.
#![allow(dead_code)]

/// Reads a fixture from `tests/data`.
pub fn data(name: &str) -> Vec<u8> {
    std::fs::read(format!("{}/tests/data/{name}", env!("CARGO_MANIFEST_DIR"))).unwrap()
}
