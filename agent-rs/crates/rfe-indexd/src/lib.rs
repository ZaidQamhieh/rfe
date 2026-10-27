//! rfe-indexd: the search index and recent-files scanner for the RFE agent, run as a child process of the Go agent.
//!
//! The Go agent owns authentication, device roots and jails; this process only walks the roots it is handed (through
//! rooted, no-follow directory handles) and answers queries from a compact in-memory index.

pub mod entry;
pub mod filter;
pub mod glob;
pub mod index;
pub mod live;
pub mod recents;
pub mod server;
pub mod walk;
