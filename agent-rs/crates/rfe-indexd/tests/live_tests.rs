//! End to end: the real rfe-indexd binary, real file system events, answers checked against a fresh build.

mod common;

use common::{build, touch, TEST_LIMITS};
use rfe_indexd::filter::{Compiled, Filters};
use rfe_proto::{read_frame, write_frame, KIND_JSON};
use serde_json::{json, Value};
use std::fs;
use std::io::{BufReader, BufWriter, Write};
use std::path::Path;
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::time::{Duration, Instant};

struct Sidecar {
    child: Child,
    stdin: BufWriter<ChildStdin>,
    stdout: BufReader<ChildStdout>,
    next: u64,
}

impl Sidecar {
    fn start() -> Sidecar {
        let mut child = Command::new(env!("CARGO_BIN_EXE_rfe-indexd"))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap();
        let mut s = Sidecar {
            stdin: BufWriter::new(child.stdin.take().unwrap()),
            stdout: BufReader::new(child.stdout.take().unwrap()),
            child,
            next: 0,
        };
        let hello = read_frame(&mut s.stdout).unwrap().unwrap();
        assert_eq!(
            serde_json::from_slice::<Value>(&hello.payload).unwrap()["kind"],
            "hello"
        );
        s
    }

    fn call(&mut self, op: &str, mut body: Value) -> Value {
        self.next += 1;
        body["id"] = json!(self.next);
        body["op"] = json!(op);
        write_frame(
            &mut self.stdin,
            KIND_JSON,
            &serde_json::to_vec(&body).unwrap(),
        )
        .unwrap();
        self.stdin.flush().unwrap();
        loop {
            let f = read_frame(&mut self.stdout)
                .unwrap()
                .expect("sidecar closed");
            let v: Value = serde_json::from_slice(&f.payload).unwrap();
            if v["id"] == json!(self.next) {
                assert_eq!(v["ok"], true, "{op}: {v}");
                return v;
            }
        }
    }

    fn paths(&mut self, root: &Path) -> Vec<String> {
        let v = self.call(
            "index.query",
            json!({"filters": {}, "roots": [root.to_str().unwrap()], "limit": 100000}),
        );
        v["entries"]
            .as_array()
            .unwrap()
            .iter()
            .map(|e| e["path"].as_str().unwrap().to_string())
            .collect()
    }
}

impl Drop for Sidecar {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn fresh_paths(root: &Path) -> Vec<String> {
    let idx = build(root);
    idx.query(
        &Compiled::new(Filters::default()),
        &[root.to_str().unwrap().to_string()],
        usize::MAX,
    )
    .0
    .into_iter()
    .map(|e| e.path)
    .collect()
}

fn wait_until(what: &str, mut cond: impl FnMut() -> bool) {
    let deadline = Instant::now() + Duration::from_secs(10);
    while !cond() {
        assert!(Instant::now() < deadline, "timed out waiting for {what}");
        std::thread::sleep(Duration::from_millis(50));
    }
}

/// Waits until the live index reports its watches installed; a watch error fails at once.
fn wait_for_watch(sc: &mut Sidecar) {
    wait_until("the watches to be installed", || {
        let live = sc.call("index.stats", json!({}))["live"].clone();
        assert_eq!(live["error"], "", "{live}");
        live["watching"] == true
    });
}

fn has_suffix(paths: &[String], suffix: &str) -> bool {
    paths.iter().any(|p| p.ends_with(suffix))
}

#[test]
fn changes_on_disk_reach_queries_without_a_rebuild() {
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    touch(&root.join("a.txt"));
    touch(&root.join("docs/readme.md"));
    touch(&root.join("docs/deep/file.bin"));

    let mut sc = Sidecar::start();
    let r = root.to_str().unwrap();
    sc.call(
        "index.build",
        json!({"roots": [r], "maxEntries": TEST_LIMITS.max_entries, "maxBytes": TEST_LIMITS.max_bytes}),
    );
    wait_for_watch(&mut sc);

    touch(&root.join("docs/created.txt"));
    wait_until("a created file", || {
        has_suffix(&sc.paths(&root), "docs/created.txt")
    });

    fs::rename(root.join("docs"), root.join("manuals")).unwrap();
    wait_until("a renamed directory", || {
        let p = sc.paths(&root);
        has_suffix(&p, "manuals/deep/file.bin") && !p.iter().any(|p| p.contains("/docs"))
    });

    // A directory created after the build gets watched too.
    touch(&root.join("fresh/inner/new.txt"));
    wait_until("a new subtree", || {
        has_suffix(&sc.paths(&root), "fresh/inner/new.txt")
    });
    touch(&root.join("fresh/inner/later.txt"));
    wait_until(
        "a file in a directory that appeared after the build",
        || has_suffix(&sc.paths(&root), "fresh/inner/later.txt"),
    );

    fs::remove_dir_all(root.join("manuals")).unwrap();
    fs::remove_file(root.join("a.txt")).unwrap();
    wait_until("deletions", || {
        let p = sc.paths(&root);
        !p.iter().any(|p| p.contains("manuals")) && !has_suffix(&p, "/a.txt")
    });

    let stats = sc.call("index.stats", json!({}));
    assert!(stats["live"]["updates"].as_u64().unwrap() >= 1, "{stats}");
    assert_eq!(
        stats["live"]["rebuilds"], 0,
        "no rebuild was needed: {stats}"
    );
    assert_eq!(
        sc.paths(&root),
        fresh_paths(&root),
        "live index equals a fresh build"
    );
}

#[test]
fn a_burst_of_changes_is_batched_and_correct() {
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    touch(&root.join("seed.txt"));
    let mut sc = Sidecar::start();
    sc.call(
        "index.build",
        json!({"roots": [root.to_str().unwrap()], "maxEntries": TEST_LIMITS.max_entries, "maxBytes": TEST_LIMITS.max_bytes}),
    );
    wait_for_watch(&mut sc);
    for d in 0..40 {
        for f in 0..25 {
            touch(&root.join(format!("d{d}/f{f}.txt")));
        }
    }
    wait_until("the whole burst", || {
        sc.paths(&root).len() == fresh_paths(&root).len()
    });
    assert_eq!(sc.paths(&root), fresh_paths(&root));
}

/// Measures live updates on a real tree: `RFE_BENCH_ROOT=$HOME cargo test --release -p rfe-indexd --test live_tests
/// -- --ignored --nocapture`.
#[test]
#[ignore]
fn real_tree_update_latency() {
    let root = std::path::PathBuf::from(std::env::var("RFE_BENCH_ROOT").expect("RFE_BENCH_ROOT"));
    let mut sc = Sidecar::start();
    let t0 = Instant::now();
    let b = sc.call(
        "index.build",
        json!({"roots": [root.to_str().unwrap()], "maxEntries": 2000000, "maxBytes": 128u64 << 20}),
    );
    println!(
        "build {:?}: {} entries, {} bytes",
        t0.elapsed(),
        b["entries"],
        b["bytes"]
    );
    let t1 = Instant::now();
    wait_until("watches", || {
        let l = sc.call("index.stats", json!({}))["live"].clone();
        if l["error"] != "" {
            println!("watch error: {l}");
        }
        l["watching"] == true || l["error"] != ""
    });
    let live = sc.call("index.stats", json!({}))["live"].clone();
    println!("watches ready after {:?}: {live}", t1.elapsed());

    let dir = root.join("rfe-live-bench");
    fs::create_dir_all(&dir).unwrap();
    let before = sc.paths(&dir).len();
    let t2 = Instant::now();
    for i in 0..3 {
        let f = dir.join(format!("probe{i}.txt"));
        fs::write(&f, "x").unwrap();
        let name = f.to_str().unwrap().to_string();
        let t = Instant::now();
        wait_until("probe", || sc.paths(&dir).contains(&name));
        println!("file {i} visible after {:?}", t.elapsed());
    }
    println!("before {before}, three probes in {:?}", t2.elapsed());
    let stats = sc.call("index.stats", json!({}));
    println!("stats {stats}");
    let status = fs::read_to_string(format!("/proc/{}/status", sc.child.id())).unwrap();
    for l in status
        .lines()
        .filter(|l| l.starts_with("VmHWM") || l.starts_with("VmRSS"))
    {
        println!("{l}");
    }
    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn recents_come_from_the_live_index_and_decline_before_it_is_ready() {
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    touch(&root.join("a.txt"));
    let r = root.to_str().unwrap();

    let mut sc = Sidecar::start();
    let before = sc.call("recents.index", json!({"roots": [r], "limit": 5}));
    assert_eq!(before["ready"], false, "no index yet: {before}");

    sc.call(
        "index.build",
        json!({"roots": [r], "maxEntries": TEST_LIMITS.max_entries, "maxBytes": TEST_LIMITS.max_bytes}),
    );
    wait_for_watch(&mut sc);
    let v = sc.call("recents.index", json!({"roots": [r], "limit": 5}));
    assert_eq!(v["ready"], true, "{v}");
    assert_eq!(v["partial"], false);
    assert_eq!(v["entries"].as_array().unwrap().len(), 1);

    // A new file reaches recents without a rebuild.
    touch(&root.join("docs/created.txt"));
    wait_until("a created file in recents", || {
        let v = sc.call("recents.index", json!({"roots": [r], "limit": 5}));
        v["ready"] == true
            && v["entries"]
                .as_array()
                .unwrap()
                .iter()
                .any(|e| e["path"].as_str().unwrap().ends_with("docs/created.txt"))
    });

    // A root the index does not cover is declined, so the caller walks.
    let other = tempfile::tempdir().unwrap();
    let v = sc.call(
        "recents.index",
        json!({"roots": [other.path().to_str().unwrap()], "limit": 5}),
    );
    assert_eq!(v["ready"], false, "{v}");
}

/// A rebuild over a live watcher must not storm: the walk's own opens used to overflow the old watcher's inotify queue,
/// which asked for another full rebuild, over and over.
#[cfg(target_os = "linux")]
#[test]
fn a_second_build_does_not_trigger_rebuilds_of_its_own() {
    let queued: usize = fs::read_to_string("/proc/sys/fs/inotify/max_queued_events")
        .ok()
        .and_then(|s| s.trim().parse().ok())
        .unwrap_or(16384);
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    // More directories than the kernel queue holds, so a watcher that sees the walk overflows.
    for i in 0..(queued * 3).min(200_000) {
        fs::create_dir_all(root.join(format!("d{}/s{}", i / 200, i % 200))).unwrap();
    }
    let mut sc = Sidecar::start();
    let body = json!({"roots": [root.to_str().unwrap()], "maxEntries": TEST_LIMITS.max_entries, "maxBytes": TEST_LIMITS.max_bytes});
    sc.call("index.build", body.clone());
    wait_for_watch(&mut sc);
    sc.call("index.build", body);
    std::thread::sleep(Duration::from_secs(3));
    let live = sc.call("index.stats", json!({}))["live"].clone();
    assert_eq!(live["rebuilds"], 0, "{live}");
}
