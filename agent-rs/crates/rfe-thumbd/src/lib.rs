//! rfe-thumbd: thumbnail rendering for the RFE agent, run as a sandboxed child process of the Go agent.
//!
//! The agent opens the file inside its own jail and hands over the bytes; this process never sees a path. It
//! decodes (JPEG with DCT scaling, PNG, GIF, WebP), applies the EXIF orientation, resizes with a Lanczos filter and
//! encodes a JPEG, all under strict limits.

pub mod decode;
pub mod exif;
pub mod render;
pub mod sandbox;
pub mod server;
