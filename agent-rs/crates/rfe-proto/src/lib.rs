//! Wire protocol between the Go agent and its Rust sidecars. See `protocol/sidecar.md`.
//!
//! A frame is `u32 big-endian length | u8 kind | payload`, where the length counts the kind byte and the
//! payload. Kind 0 carries one JSON message, kind 1 raw bytes that belong to the JSON frame just before it
//! (the JSON says so with `"body": true`). A sender writes a JSON frame and its body under one lock, so a
//! receiver always sees them back to back.

use serde::{Deserialize, Serialize};
use std::io::{self, Read, Write};

/// Protocol version both sides must agree on in the handshake.
/// The version a sidecar announces: the release it was built for (`RFE_RELEASE_VERSION`, set by the release
/// workflow so every binary in an archive reports the same version), else the crate version.
pub const fn release_version(crate_version: &'static str) -> &'static str {
    match option_env!("RFE_RELEASE_VERSION") {
        Some(v) => v,
        None => crate_version,
    }
}

pub const PROTO_VERSION: u32 = 1;

pub const KIND_JSON: u8 = 0;
pub const KIND_BIN: u8 = 1;

/// Frames larger than this are refused before any allocation (a corrupt length must not eat memory).
pub const MAX_FRAME_BYTES: usize = 160 << 20;

/// Error codes a sidecar returns. Go maps them back to its own errors.
pub mod code {
    pub const BAD_REQUEST: &str = "BAD_REQUEST";
    pub const NOT_SUPPORTED: &str = "NOT_SUPPORTED";
    pub const TOO_LARGE: &str = "TOO_LARGE";
    pub const BUSY: &str = "BUSY";
    pub const CANCELED: &str = "CANCELED";
    pub const INTERNAL: &str = "INTERNAL";
}

#[derive(Debug)]
pub struct Frame {
    pub kind: u8,
    pub payload: Vec<u8>,
}

/// The first frame a sidecar sends.
#[derive(Debug, Serialize, Deserialize, PartialEq, Eq)]
pub struct Hello {
    pub kind: String,
    pub proto: u32,
    pub name: String,
    pub version: String,
}

impl Hello {
    pub fn new(name: &str, version: &str) -> Self {
        Hello {
            kind: "hello".into(),
            proto: PROTO_VERSION,
            name: name.into(),
            version: version.into(),
        }
    }
}

/// Fields every request carries; the rest of the JSON object is op-specific.
#[derive(Debug, Deserialize)]
pub struct Envelope {
    pub id: u64,
    pub op: String,
    /// A BIN frame follows this request.
    #[serde(default)]
    pub body: bool,
    /// For `op == "cancel"`: the request id to cancel.
    #[serde(default)]
    pub target: u64,
}

pub fn write_frame<W: Write>(w: &mut W, kind: u8, payload: &[u8]) -> io::Result<()> {
    let len = payload.len() + 1;
    if len > MAX_FRAME_BYTES {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "frame too large",
        ));
    }
    w.write_all(&(len as u32).to_be_bytes())?;
    w.write_all(&[kind])?;
    w.write_all(payload)
}

/// Reads one frame. `Ok(None)` is a clean end of stream (the peer closed between frames).
pub fn read_frame<R: Read>(r: &mut R) -> io::Result<Option<Frame>> {
    let mut len_buf = [0u8; 4];
    match r.read_exact(&mut len_buf) {
        Ok(()) => {}
        Err(e) if e.kind() == io::ErrorKind::UnexpectedEof => return Ok(None),
        Err(e) => return Err(e),
    }
    let len = u32::from_be_bytes(len_buf) as usize;
    if len == 0 || len > MAX_FRAME_BYTES {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            format!("bad frame length {len}"),
        ));
    }
    let mut kind = [0u8; 1];
    r.read_exact(&mut kind)?;
    // Grow with the bytes that actually arrive instead of trusting the declared length for the allocation.
    let want = len - 1;
    let mut payload = Vec::with_capacity(want.min(1 << 20));
    r.take(want as u64).read_to_end(&mut payload)?;
    if payload.len() != want {
        return Err(io::Error::new(
            io::ErrorKind::UnexpectedEof,
            "frame truncated",
        ));
    }
    Ok(Some(Frame {
        kind: kind[0],
        payload,
    }))
}

/// Builds an error response for request `id`.
pub fn error_response(id: u64, code: &str, message: &str) -> serde_json::Value {
    serde_json::json!({ "id": id, "ok": false, "code": code, "message": message })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Cursor;

    #[test]
    fn round_trips_json_and_binary_frames() {
        let mut buf = Vec::new();
        write_frame(&mut buf, KIND_JSON, br#"{"id":1}"#).unwrap();
        write_frame(&mut buf, KIND_BIN, &[1, 2, 3, 0, 255]).unwrap();
        let mut r = Cursor::new(buf);
        let a = read_frame(&mut r).unwrap().unwrap();
        assert_eq!(
            (a.kind, a.payload.as_slice()),
            (KIND_JSON, br#"{"id":1}"# as &[u8])
        );
        let b = read_frame(&mut r).unwrap().unwrap();
        assert_eq!((b.kind, b.payload), (KIND_BIN, vec![1, 2, 3, 0, 255]));
        assert!(
            read_frame(&mut r).unwrap().is_none(),
            "clean EOF between frames"
        );
    }

    #[test]
    fn refuses_oversized_and_zero_length_frames_without_allocating() {
        let huge = ((MAX_FRAME_BYTES + 1) as u32).to_be_bytes();
        assert!(read_frame(&mut Cursor::new(huge.to_vec())).is_err());
        assert!(read_frame(&mut Cursor::new(vec![0, 0, 0, 0])).is_err());
    }

    #[test]
    fn truncated_frame_is_an_error_not_a_clean_eof() {
        let mut buf = Vec::new();
        write_frame(&mut buf, KIND_JSON, b"hello world").unwrap();
        buf.truncate(buf.len() - 3);
        assert!(read_frame(&mut Cursor::new(buf)).is_err());
    }

    #[test]
    fn declared_length_alone_does_not_allocate_or_succeed() {
        let mut buf = (MAX_FRAME_BYTES as u32).to_be_bytes().to_vec();
        buf.push(KIND_BIN);
        buf.extend_from_slice(&[7; 16]);
        let err = read_frame(&mut Cursor::new(buf)).unwrap_err();
        assert_eq!(err.kind(), io::ErrorKind::UnexpectedEof);
    }

    #[test]
    fn hello_carries_the_protocol_version() {
        let h = Hello::new("rfe-indexd", "0.1.0");
        let json = serde_json::to_string(&h).unwrap();
        assert_eq!(serde_json::from_str::<Hello>(&json).unwrap(), h);
        assert_eq!(h.proto, PROTO_VERSION);
    }
}
