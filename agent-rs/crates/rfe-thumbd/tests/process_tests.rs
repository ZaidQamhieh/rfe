//! The real binary, sandboxed, driven over the protocol.

mod common;

use common::data;
use rfe_proto::{code, read_frame, write_frame, KIND_BIN, KIND_JSON, PROTO_VERSION};
use serde_json::{json, Value};
use std::io::{BufReader, BufWriter, Write};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};

struct Thumbd {
    child: Child,
    stdin: BufWriter<ChildStdin>,
    stdout: BufReader<ChildStdout>,
    next: u64,
}

impl Thumbd {
    fn start() -> Thumbd {
        Thumbd::start_with(Stdio::inherit())
    }

    /// Starts the binary with the given stderr and checks its hello.
    fn start_with(stderr: Stdio) -> Thumbd {
        let mut child = Command::new(env!("CARGO_BIN_EXE_rfe-thumbd"))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(stderr)
            .spawn()
            .unwrap();
        let mut t = Thumbd {
            stdin: BufWriter::new(child.stdin.take().unwrap()),
            stdout: BufReader::new(child.stdout.take().unwrap()),
            child,
            next: 0,
        };
        let hello = read_frame(&mut t.stdout).unwrap().expect("hello");
        let v: Value = serde_json::from_slice(&hello.payload).unwrap();
        assert_eq!(
            (v["kind"].as_str(), v["name"].as_str(), v["proto"].as_u64()),
            (
                Some("hello"),
                Some(rfe_thumbd::server::NAME),
                Some(u64::from(PROTO_VERSION))
            )
        );
        t
    }

    /// Writes a request (and its body, if any) without reading the answer; returns the request id.
    fn send_only(&mut self, op: &str, mut req: Value, body: Option<&[u8]>) -> u64 {
        self.next += 1;
        let id = self.next;
        req["id"] = json!(id);
        req["op"] = json!(op);
        if body.is_some() {
            req["body"] = json!(true);
        }
        write_frame(
            &mut self.stdin,
            KIND_JSON,
            &serde_json::to_vec(&req).unwrap(),
        )
        .unwrap();
        if let Some(b) = body {
            write_frame(&mut self.stdin, KIND_BIN, b).unwrap();
        }
        self.stdin.flush().unwrap();
        id
    }

    /// Sends a request (with an optional body) and returns the response and its body, if any.
    fn call(&mut self, op: &str, req: Value, body: Option<&[u8]>) -> (Value, Option<Vec<u8>>) {
        let id = self.send_only(op, req, body);
        loop {
            let f = read_frame(&mut self.stdout).unwrap().expect("closed");
            let v: Value = serde_json::from_slice(&f.payload).unwrap();
            if v["id"] != json!(id) {
                continue;
            }
            let body = (v["body"] == true).then(|| {
                let b = read_frame(&mut self.stdout).unwrap().unwrap();
                assert_eq!(b.kind, KIND_BIN);
                b.payload
            });
            return (v, body);
        }
    }
}

impl Drop for Thumbd {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

#[test]
fn renders_under_the_sandbox() {
    let mut t = Thumbd::start();
    for (name, w, h) in [
        ("photo.jpg", 128, 96),
        ("orient6.jpg", 96, 128),
        ("alpha.webp", 128, 64),
        ("anim.gif", 120, 80),
        ("photo.tiff", 128, 96),
        ("deflate.tiff", 128, 96),
        ("photo.bmp", 128, 96),
        ("pal.bmp", 128, 96),
    ] {
        let (v, body) = t.call("thumb.render", json!({"maxSize": 128}), Some(&data(name)));
        assert_eq!(v["ok"], true, "{name}: {v}");
        assert_eq!(
            (v["width"].as_u64(), v["height"].as_u64()),
            (Some(w), Some(h)),
            "{name}"
        );
        let jpeg = body.expect("a jpeg body");
        assert!(jpeg.starts_with(&[0xff, 0xd8]), "{name}");
    }
}

#[test]
fn errors_use_the_documented_codes() {
    let mut t = Thumbd::start();
    let (v, _) = t.call(
        "thumb.render",
        json!({"maxSize": 128}),
        Some(&data("notimage.bin")),
    );
    assert_eq!(
        (v["ok"].clone(), v["code"].as_str()),
        (json!(false), Some(code::NOT_SUPPORTED))
    );
    let (v, _) = t.call("thumb.render", json!({"maxSize": 128}), None);
    assert_eq!(v["code"], code::BAD_REQUEST, "a render without a body");
    let (v, _) = t.call("nope", json!({}), Some(b"ignored body"));
    assert_eq!(v["code"], code::BAD_REQUEST, "unknown op, body consumed");
    let (v, _) = t.call("ping", json!({}), None);
    assert_eq!(v["ok"], true, "still in sync after the errors");
}

#[test]
fn many_concurrent_renders_all_answer() {
    let mut t = Thumbd::start();
    let photo = data("photo.jpg");
    let ids: Vec<u64> = (0..24)
        .map(|_| t.send_only("thumb.render", json!({"maxSize": 64}), Some(&photo)))
        .collect();
    let mut seen = std::collections::HashSet::new();
    while seen.len() < ids.len() {
        let f = read_frame(&mut t.stdout).unwrap().unwrap();
        let v: Value = serde_json::from_slice(&f.payload).unwrap();
        assert_eq!(v["ok"], true, "{v}");
        let b = read_frame(&mut t.stdout).unwrap().unwrap();
        assert_eq!(b.kind, KIND_BIN);
        assert!(seen.insert(v["id"].as_u64().unwrap()));
    }
}

#[cfg(target_os = "linux")]
#[test]
fn the_sandbox_refuses_files_sockets_and_new_processes() {
    let out = Command::new(env!("CARGO_BIN_EXE_rfe-thumbd"))
        .arg("--sandbox-selftest")
        .output()
        .unwrap();
    let text = String::from_utf8_lossy(&out.stdout).to_string();
    // ENOSYS is errno 38.
    for line in ["open=38", "socket=38", "exec=38", "fork=38", "render=1x1"] {
        assert!(
            text.lines().any(|l| l == line),
            "missing {line:?} in:\n{text}"
        );
    }
    assert!(out.status.success(), "{text}");
}

/// The sandbox sets a zero file-size limit, and stderr may be a log file: the process must keep answering, not die
/// with SIGXFSZ on its first write to that file.
#[test]
fn stderr_on_a_regular_file_does_not_kill_it() {
    let path = format!("{}/stderr-file", env!("CARGO_TARGET_TMPDIR"));
    let err = std::fs::File::create(&path).unwrap();
    // Thumbd's Drop kills the process at the end of the test, also when an assertion fails.
    let _t = Thumbd::start_with(Stdio::from(err));
}
