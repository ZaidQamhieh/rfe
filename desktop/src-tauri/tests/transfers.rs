//! Downloads and uploads against a throwaway agent: a multi-chunk file arrives intact in both
//! directions, an interrupted transfer continues instead of starting over, a cancel leaves no file
//! under a real name (and no partial file at all), and a hostile remote name cannot leave the
//! downloads folder. Set RFE_AGENT_BIN to a built agent.

mod common;

#[path = "support/ui.rs"]
mod ui;
use common::{agent_bin, cli, free_port, identity, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentClient};
use rfe_desktop_lib::transfers::{
    numbered_name, publish_no_clobber, safe_name, transfer_message, Conn, Ctx, Options, Policy,
    Resolution, State, TransferView, Transfers, PARALLEL, SKIPPED, TRANSFER_CODES,
};
use sha2::{Digest, Sha256};
use std::io::{Read, Write};
use std::net::{Shutdown, TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tempfile::TempDir;

const PW: &str = "pw-for-transfer-tests";
const MIB: usize = 1 << 20;

/// An agent with data and roots folders the test can see, and an owner login on it.
struct Rig {
    child: Child,
    data: TempDir,
    roots: TempDir,
    host: String,
    fingerprint: String,
    token: String,
    _identity: TempDir,
}

impl Rig {
    async fn new() -> Self {
        let data = TempDir::new().unwrap();
        let roots = TempDir::new().unwrap();
        let host = format!("127.0.0.1:{}", free_port());
        let child = Command::new(agent_bin())
            .args(["-addr", &host, "-name", "rfe-desktop-transfers"])
            .args(["-data", data.path().to_str().unwrap()])
            .args(["-roots", roots.path().to_str().unwrap()])
            .env("PATH", "/nonexistent-rfe-test-path")
            .env_remove("DBUS_SESSION_BUS_ADDRESS")
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .expect("start rfe");
        let deadline = Instant::now() + Duration::from_secs(20);
        while TcpStream::connect(&host).is_err() {
            assert!(Instant::now() < deadline, "agent did not start on {host}");
            std::thread::sleep(Duration::from_millis(100));
        }
        cli(&[
            "adduser",
            "-password",
            PW,
            "-data",
            data.path().to_str().unwrap(),
            "owner",
        ]);
        let fingerprint = capture_fingerprint(&host).await.unwrap();
        let (id, identity_dir) = identity();
        let ok = AgentClient::pinned(&host, &fingerprint)
            .unwrap()
            .login(&id, "owner", PW, "transfers test")
            .await
            .unwrap();
        Rig {
            child,
            data,
            roots,
            host,
            fingerprint,
            token: ok.device_token,
            _identity: identity_dir,
        }
    }

    fn conn_via(&self, host: &str) -> Conn {
        Conn {
            host: host.to_string(),
            fingerprint: self.fingerprint.clone(),
            token: self.token.clone(),
        }
    }

    fn conn(&self) -> Conn {
        self.conn_via(&self.host)
    }

    /// A path on the agent, inside its roots folder.
    fn remote(&self, name: &str) -> String {
        format!("{}/{name}", self.root())
    }

    fn root(&self) -> &str {
        self.roots.path().to_str().unwrap()
    }

    /// Throttles the agent so a transfer is still running when the test acts.
    async fn throttle(&self, bytes_per_sec: u64) {
        let raw = Raw::new(&self.host);
        let resp = raw
            .http
            .put(format!("{}/settings/bandwidth", raw.base))
            .bearer_auth(&self.token)
            .json(&serde_json::json!({
                "maxUploadBytesPerSec": bytes_per_sec,
                "maxDownloadBytesPerSec": bytes_per_sec,
            }))
            .send()
            .await
            .unwrap();
        assert!(resp.status().is_success(), "throttle: {}", resp.status());
    }

    /// Every `.tmp` file under the agent's data folder: its upload sessions' temporary files.
    fn temp_files(&self) -> Vec<PathBuf> {
        fn walk(dir: &Path, out: &mut Vec<PathBuf>) {
            for e in std::fs::read_dir(dir).into_iter().flatten().flatten() {
                let p = e.path();
                if p.is_dir() {
                    walk(&p, out);
                } else if p.extension().is_some_and(|x| x == "tmp") {
                    out.push(p);
                }
            }
        }
        let mut out = vec![];
        walk(self.data.path(), &mut out);
        out
    }
}

impl Drop for Rig {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// A TCP relay between the app and the agent that can cut a connection after some bytes. The TLS
/// session runs straight through it, so the pinned certificate still matches.
struct Proxy {
    addr: String,
    to_client: Arc<AtomicU64>,
    to_agent: Arc<AtomicU64>,
    cut_to_client: Arc<AtomicU64>,
    cut_to_agent: Arc<AtomicU64>,
}

fn pipe(
    mut from: TcpStream,
    mut to: TcpStream,
    count: Arc<AtomicU64>,
    cut: Arc<AtomicU64>,
) -> impl FnOnce() {
    move || {
        let mut buf = [0u8; 16 * 1024];
        loop {
            let n = match from.read(&mut buf) {
                Ok(0) | Err(_) => break,
                Ok(n) => n,
            };
            if to.write_all(&buf[..n]).is_err() {
                break;
            }
            let total = count.fetch_add(n as u64, Ordering::SeqCst) + n as u64;
            if total >= cut.load(Ordering::SeqCst) {
                break;
            }
        }
        let _ = from.shutdown(Shutdown::Both);
        let _ = to.shutdown(Shutdown::Both);
    }
}

impl Proxy {
    fn start(upstream: &str) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let p = Proxy {
            addr: listener.local_addr().unwrap().to_string(),
            to_client: Arc::default(),
            to_agent: Arc::default(),
            cut_to_client: Arc::new(AtomicU64::new(u64::MAX)),
            cut_to_agent: Arc::new(AtomicU64::new(u64::MAX)),
        };
        let upstream = upstream.to_string();
        let (tc, ta) = (p.to_client.clone(), p.to_agent.clone());
        let (cc, ca) = (p.cut_to_client.clone(), p.cut_to_agent.clone());
        std::thread::spawn(move || {
            for conn in listener.incoming() {
                let Ok(client) = conn else { continue };
                let Ok(agent) = TcpStream::connect(&upstream) else {
                    continue;
                };
                let (c2, a2) = (client.try_clone().unwrap(), agent.try_clone().unwrap());
                std::thread::spawn(pipe(client, a2, ta.clone(), ca.clone()));
                std::thread::spawn(pipe(agent, c2, tc.clone(), cc.clone()));
            }
        });
        p
    }

    /// Cuts every connection once `n` more bytes have gone from the agent to the app.
    fn cut_after_download_bytes(&self, n: u64) {
        self.to_client.store(0, Ordering::SeqCst);
        self.cut_to_client.store(n, Ordering::SeqCst);
    }

    /// Cuts every connection once `n` more bytes have gone from the app to the agent.
    fn cut_after_upload_bytes(&self, n: u64) {
        self.to_agent.store(0, Ordering::SeqCst);
        self.cut_to_agent.store(n, Ordering::SeqCst);
    }

    /// Lets everything through again and starts counting from zero.
    fn heal(&self) {
        self.cut_to_client.store(u64::MAX, Ordering::SeqCst);
        self.cut_to_agent.store(u64::MAX, Ordering::SeqCst);
        self.to_client.store(0, Ordering::SeqCst);
        self.to_agent.store(0, Ordering::SeqCst);
    }
}

/// Bytes that are not repeating, so a wrong offset shows up in the hash.
fn pattern(len: usize) -> Vec<u8> {
    let mut x: u64 = 0x9e37_79b9_7f4a_7c15;
    (0..len)
        .map(|_| {
            x ^= x << 13;
            x ^= x >> 7;
            x ^= x << 17;
            (x >> 24) as u8
        })
        .collect()
}

fn sha256_file(path: &Path) -> String {
    let mut f = std::fs::File::open(path).unwrap();
    let mut h = Sha256::new();
    let mut buf = vec![0u8; MIB];
    loop {
        let n = f.read(&mut buf).unwrap();
        if n == 0 {
            return hex::encode(h.finalize());
        }
        h.update(&buf[..n]);
    }
}

fn small() -> Options {
    Options {
        chunk_size: MIB,
        segment_size: MIB as u64,
        parallel: PARALLEL,
    }
}

fn names_in(dir: &Path) -> Vec<String> {
    let mut v: Vec<String> = std::fs::read_dir(dir)
        .into_iter()
        .flatten()
        .flatten()
        .map(|e| e.file_name().to_string_lossy().into_owned())
        .collect();
    v.sort();
    v
}

async fn wait_for(
    t: &Transfers,
    id: &str,
    what: &str,
    ok: impl Fn(&TransferView) -> bool,
) -> TransferView {
    let end = Instant::now() + Duration::from_secs(90);
    loop {
        let v = t.get(id).expect("the transfer is listed");
        if ok(&v) {
            return v;
        }
        assert!(Instant::now() < end, "timed out waiting for {what}: {v:?}");
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
}

async fn finished(t: &Transfers, id: &str) -> TransferView {
    wait_for(t, id, "the transfer to end", |v| {
        matches!(v.state, State::Done | State::Failed | State::Cancelled)
    })
    .await
}

fn dirs() -> (TempDir, PathBuf) {
    let base = TempDir::new().unwrap();
    let folder = base.path().join("downloads").join("RFE Desktop");
    (base, folder)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_multi_chunk_download_matches_its_sha256() {
    let rig = Rig::new().await;
    let data = pattern(10 * MIB + 12_345);
    std::fs::write(rig.remote("big.bin"), &data).unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());

    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote("big.bin"),
        )
        .await
        .unwrap();
    let v = finished(&t, &id).await;

    assert_eq!(v.state, State::Done, "{v:?}");
    assert!(v.verified, "the agent's checksum should have matched");
    assert_eq!((v.done, v.total), (data.len() as u64, data.len() as u64));
    assert_eq!(v.name, "big.bin");
    let saved = folder.join("big.bin");
    assert_eq!(v.local_path, saved.display().to_string());
    assert_eq!(sha256_file(&saved), hex::encode(Sha256::digest(&data)));
    assert_eq!(
        names_in(&folder),
        ["big.bin"],
        "no part file may stay behind"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_empty_file_downloads() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("empty.txt"), b"").unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote("empty.txt"),
        )
        .await
        .unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        std::fs::metadata(folder.join("empty.txt")).unwrap().len(),
        0
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_interrupted_download_resumes_from_what_it_has() {
    let rig = Rig::new().await;
    let data = pattern(10 * MIB);
    std::fs::write(rig.remote("big.bin"), &data).unwrap();
    let proxy = Proxy::start(&rig.host);
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());

    proxy.cut_after_download_bytes((3 * MIB + MIB / 2) as u64);
    let id = t
        .start_download(
            Ctx::fixed(rig.conn_via(&proxy.addr), folder.clone()),
            &rig.remote("big.bin"),
        )
        .await
        .unwrap();
    let failed = finished(&t, &id).await;
    assert_eq!(failed.state, State::Failed, "{failed:?}");
    assert!(!failed.error.is_empty());
    assert!(
        failed.done > 0 && failed.done < data.len() as u64,
        "{failed:?}"
    );
    // The part file is kept, hidden, and holds exactly what arrived; nothing has a real name.
    let kept = names_in(&folder);
    assert_eq!(kept.len(), 1, "{kept:?}");
    assert!(kept[0].starts_with(".rfe-") && kept[0].ends_with(".part"));
    assert_eq!(
        std::fs::metadata(folder.join(&kept[0])).unwrap().len(),
        failed.done
    );

    proxy.heal();
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        sha256_file(&folder.join("big.bin")),
        hex::encode(Sha256::digest(&data))
    );
    assert_eq!(names_in(&folder), ["big.bin"]);
    // Only the missing part came over again (plus headers and the checksum answer).
    let again = proxy.to_client.load(Ordering::SeqCst);
    let missing = data.len() as u64 - failed.done;
    assert!(
        again >= missing && again < missing + 300_000,
        "the retry moved {again} bytes for {missing} missing"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_multi_chunk_upload_matches_on_the_agent_side() {
    let rig = Rig::new().await;
    let data = pattern(10 * MIB + 777);
    let src = TempDir::new().unwrap();
    let local = src.path().join("up.bin");
    std::fs::write(&local, &data).unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());

    let id = t
        .start_upload(
            Ctx::fixed(rig.conn(), folder),
            local.to_str().unwrap(),
            rig.root(),
        )
        .await
        .unwrap();
    let v = finished(&t, &id).await;

    assert_eq!(v.state, State::Done, "{v:?}");
    assert!(
        v.verified,
        "the agent reports the whole-file hash it checked"
    );
    assert_eq!(v.remote_path, rig.remote("up.bin"));
    assert_eq!((v.done, v.total), (data.len() as u64, data.len() as u64));
    assert_eq!(
        sha256_file(&rig.roots.path().join("up.bin")),
        hex::encode(Sha256::digest(&data))
    );
    assert!(rig.temp_files().is_empty(), "{:?}", rig.temp_files());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_empty_file_uploads() {
    let rig = Rig::new().await;
    let src = TempDir::new().unwrap();
    let local = src.path().join("nothing.txt");
    std::fs::write(&local, b"").unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_upload(
            Ctx::fixed(rig.conn(), folder),
            local.to_str().unwrap(),
            rig.root(),
        )
        .await
        .unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        std::fs::metadata(rig.roots.path().join("nothing.txt"))
            .unwrap()
            .len(),
        0
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_interrupted_upload_sends_only_the_missing_chunks() {
    let rig = Rig::new().await;
    let data = pattern(10 * MIB);
    let src = TempDir::new().unwrap();
    let local = src.path().join("up.bin");
    std::fs::write(&local, &data).unwrap();
    let proxy = Proxy::start(&rig.host);
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());

    proxy.cut_after_upload_bytes((3 * MIB + MIB / 2) as u64);
    let id = t
        .start_upload(
            Ctx::fixed(rig.conn_via(&proxy.addr), folder),
            local.to_str().unwrap(),
            rig.root(),
        )
        .await
        .unwrap();
    let failed = finished(&t, &id).await;
    assert_eq!(failed.state, State::Failed, "{failed:?}");
    assert!(
        failed.done >= MIB as u64 && failed.done < data.len() as u64,
        "{failed:?}"
    );
    assert!(
        !rig.roots.path().join("up.bin").exists(),
        "nothing under the real name until the upload is complete"
    );

    proxy.heal();
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        sha256_file(&rig.roots.path().join("up.bin")),
        hex::encode(Sha256::digest(&data))
    );
    let again = proxy.to_agent.load(Ordering::SeqCst);
    let missing = data.len() as u64 - failed.done;
    assert!(
        again + 2 * MIB as u64 > missing && again < missing + 300_000,
        "the retry sent {again} bytes for {missing} missing"
    );
}

/// Rewrites `path` with `data` and gives it a modification time well after the old one, as an edit
/// a minute later would (the agent's validator has one-second resolution).
fn edit_later(path: &Path, data: &[u8]) {
    let before = std::fs::metadata(path).unwrap().modified().unwrap();
    std::fs::write(path, data).unwrap();
    std::fs::File::options()
        .write(true)
        .open(path)
        .unwrap()
        .set_modified(before + Duration::from_secs(60))
        .unwrap();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_download_never_continues_old_bytes_into_a_file_that_changed() {
    let rig = Rig::new().await;
    let old = pattern(10 * MIB);
    let new: Vec<u8> = old.iter().map(|b| b ^ 0x5a).collect();
    std::fs::write(rig.remote("big.bin"), &old).unwrap();
    let proxy = Proxy::start(&rig.host);
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());

    proxy.cut_after_download_bytes((3 * MIB + MIB / 2) as u64);
    let id = t
        .start_download(
            Ctx::fixed(rig.conn_via(&proxy.addr), folder.clone()),
            &rig.remote("big.bin"),
        )
        .await
        .unwrap();
    let failed = finished(&t, &id).await;
    assert_eq!(failed.state, State::Failed, "{failed:?}");
    assert!(
        failed.done > 0 && failed.done < old.len() as u64,
        "{failed:?}"
    );

    // Same length, other bytes: only the version of the file tells the two apart (a mixed file would
    // still be the right size).
    edit_later(Path::new(&rig.remote("big.bin")), &new);
    proxy.heal();
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert!(v.verified, "{v:?}");
    assert_eq!(
        sha256_file(&folder.join("big.bin")),
        hex::encode(Sha256::digest(&new)),
        "the file is the new one, whole"
    );
    // It started over: the retry moved the whole file, not just the missing part.
    assert!(
        proxy.to_client.load(Ordering::SeqCst) >= new.len() as u64,
        "the retry moved {} bytes",
        proxy.to_client.load(Ordering::SeqCst)
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_upload_session_for_a_file_that_changed_is_closed_on_the_agent() {
    let rig = Rig::new().await;
    let old = pattern(10 * MIB);
    let new: Vec<u8> = old.iter().map(|b| b ^ 0x5a).collect();
    let src = TempDir::new().unwrap();
    let local = src.path().join("up.bin");
    std::fs::write(&local, &old).unwrap();
    let proxy = Proxy::start(&rig.host);
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());

    proxy.cut_after_upload_bytes((3 * MIB + MIB / 2) as u64);
    let id = t
        .start_upload(
            Ctx::fixed(rig.conn_via(&proxy.addr), folder),
            local.to_str().unwrap(),
            rig.root(),
        )
        .await
        .unwrap();
    let failed = finished(&t, &id).await;
    assert_eq!(failed.state, State::Failed, "{failed:?}");
    assert_eq!(
        rig.temp_files().len(),
        1,
        "the half-sent upload is on the agent"
    );

    edit_later(&local, &new);
    proxy.heal();
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        sha256_file(&rig.roots.path().join("up.bin")),
        hex::encode(Sha256::digest(&new))
    );
    assert!(
        rig.temp_files().is_empty(),
        "the old session's temporary file was left behind: {:?}",
        rig.temp_files()
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn cancelling_a_download_leaves_no_file_at_all() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("slow.bin"), pattern(8 * MIB)).unwrap();
    rig.throttle(MIB as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote("slow.bin"),
        )
        .await
        .unwrap();
    wait_for(&t, &id, "bytes to arrive", |v| v.done > 0).await;
    assert_eq!(
        names_in(&folder).len(),
        1,
        "the part file exists while running"
    );

    t.cancel(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Cancelled, "{v:?}");
    assert!(v.error.is_empty());
    assert!(names_in(&folder).is_empty(), "{:?}", names_in(&folder));
    tokio::time::sleep(Duration::from_millis(500)).await;
    assert!(names_in(&folder).is_empty(), "something kept writing");

    // A cancelled transfer can be started again and then completes.
    rig.throttle(0).await;
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(names_in(&folder), ["slow.bin"]);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn cancelling_an_upload_leaves_nothing_on_the_agent() {
    let rig = Rig::new().await;
    let src = TempDir::new().unwrap();
    let local = src.path().join("slow.bin");
    std::fs::write(&local, pattern(8 * MIB)).unwrap();
    rig.throttle(MIB as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_upload(
            Ctx::fixed(rig.conn(), folder),
            local.to_str().unwrap(),
            rig.root(),
        )
        .await
        .unwrap();
    wait_for(&t, &id, "a chunk to be stored", |v| v.done > 0).await;
    assert!(
        !rig.temp_files().is_empty(),
        "the agent holds a temporary file"
    );

    t.cancel(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Cancelled, "{v:?}");
    assert!(!rig.roots.path().join("slow.bin").exists());
    assert!(rig.temp_files().is_empty(), "{:?}", rig.temp_files());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn pausing_a_download_keeps_what_arrived_and_resuming_finishes_it() {
    let rig = Rig::new().await;
    let data = pattern(8 * MIB);
    std::fs::write(rig.remote("slow.bin"), &data).unwrap();
    rig.throttle(MIB as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote("slow.bin"),
        )
        .await
        .unwrap();
    wait_for(&t, &id, "bytes to arrive", |v| v.done > 0).await;

    t.pause(&id).unwrap();
    let paused = wait_for(&t, &id, "the pause", |v| v.state == State::Paused).await;
    assert!(paused.error.is_empty(), "{paused:?}");
    assert!(
        paused.done > 0 && paused.done < data.len() as u64,
        "{paused:?}"
    );
    // The hidden part file stays and holds what arrived; nothing has the real name yet.
    let kept = names_in(&folder);
    assert_eq!(kept.len(), 1, "{kept:?}");
    assert!(kept[0].starts_with(".rfe-") && kept[0].ends_with(".part"));
    // A paused transfer stays paused: nothing keeps writing.
    let size = std::fs::metadata(folder.join(&kept[0])).unwrap().len();
    tokio::time::sleep(Duration::from_millis(600)).await;
    assert_eq!(
        std::fs::metadata(folder.join(&kept[0])).unwrap().len(),
        size
    );
    assert_eq!(t.get(&id).unwrap().state, State::Paused);
    // Pausing it again is not an error.
    t.pause(&id).unwrap();

    rig.throttle(0).await;
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        sha256_file(&folder.join("slow.bin")),
        hex::encode(Sha256::digest(&data))
    );
    assert_eq!(names_in(&folder), ["slow.bin"]);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn cancelling_a_paused_download_discards_what_was_kept() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("slow.bin"), pattern(8 * MIB)).unwrap();
    rig.throttle(MIB as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote("slow.bin"),
        )
        .await
        .unwrap();
    wait_for(&t, &id, "bytes to arrive", |v| v.done > 0).await;
    t.pause(&id).unwrap();
    wait_for(&t, &id, "the pause", |v| v.state == State::Paused).await;
    assert_eq!(names_in(&folder).len(), 1, "the part file is kept");

    t.cancel(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Cancelled, "{v:?}");
    assert!(names_in(&folder).is_empty(), "{:?}", names_in(&folder));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_cancel_right_after_a_pause_is_a_cancel_and_keeps_nothing() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("slow.bin"), pattern(8 * MIB)).unwrap();
    rig.throttle(MIB as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote("slow.bin"),
        )
        .await
        .unwrap();
    wait_for(&t, &id, "bytes to arrive", |v| v.done > 0).await;
    // Both are asked for before the runner looks at either.
    t.pause(&id).unwrap();
    t.cancel(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Cancelled, "{v:?}");
    assert!(names_in(&folder).is_empty(), "{:?}", names_in(&folder));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn pausing_an_upload_keeps_its_session_and_resuming_sends_only_what_is_missing() {
    let rig = Rig::new().await;
    let src = TempDir::new().unwrap();
    let local = src.path().join("slow.bin");
    let data = pattern(8 * MIB);
    std::fs::write(&local, &data).unwrap();
    rig.throttle(MIB as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_upload(
            Ctx::fixed(rig.conn(), folder),
            local.to_str().unwrap(),
            rig.root(),
        )
        .await
        .unwrap();
    wait_for(&t, &id, "a chunk to be stored", |v| v.done > 0).await;

    t.pause(&id).unwrap();
    let paused = wait_for(&t, &id, "the pause", |v| v.state == State::Paused).await;
    assert!(
        paused.done > 0 && paused.done < data.len() as u64,
        "{paused:?}"
    );
    assert!(
        !rig.temp_files().is_empty(),
        "the agent keeps the temporary file of the paused upload"
    );
    assert!(!rig.roots.path().join("slow.bin").exists());

    rig.throttle(0).await;
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        sha256_file(&rig.roots.path().join("slow.bin")),
        hex::encode(Sha256::digest(&data))
    );
    assert!(rig.temp_files().is_empty(), "{:?}", rig.temp_files());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_finished_transfer_cannot_be_paused() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("small.bin"), pattern(1000)).unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_download(Ctx::fixed(rig.conn(), folder), &rig.remote("small.bin"))
        .await
        .unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(
        t.pause(&id).unwrap_err(),
        "that transfer cannot be paused now"
    );
    assert!(t.pause("no-such-id").is_err());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn only_two_transfers_run_at_once_and_the_rest_wait() {
    let rig = Rig::new().await;
    for i in 0..4 {
        std::fs::write(rig.remote(&format!("f{i}.bin")), pattern(MIB)).unwrap();
    }
    rig.throttle((MIB / 2) as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let mut ids = vec![];
    for i in 0..4 {
        ids.push(
            t.start_download(
                Ctx::fixed(rig.conn(), folder.clone()),
                &rig.remote(&format!("f{i}.bin")),
            )
            .await
            .unwrap(),
        );
    }
    let mut most = 0;
    let end = Instant::now() + Duration::from_secs(90);
    loop {
        let all = t.list();
        let running = all.iter().filter(|v| v.state == State::Running).count();
        most = most.max(running);
        assert!(running <= PARALLEL, "{running} running at once");
        if all.iter().all(|v| v.state == State::Done) {
            break;
        }
        assert!(Instant::now() < end, "timed out: {all:?}");
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    assert_eq!(most, PARALLEL, "two should have run together");
    assert_eq!(names_in(&folder).len(), 4);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_queued_transfer_can_be_cancelled_before_it_starts() {
    let rig = Rig::new().await;
    for i in 0..3 {
        std::fs::write(rig.remote(&format!("q{i}.bin")), pattern(4 * MIB)).unwrap();
    }
    rig.throttle(MIB as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let mut ids = vec![];
    for i in 0..3 {
        ids.push(
            t.start_download(
                Ctx::fixed(rig.conn(), folder.clone()),
                &rig.remote(&format!("q{i}.bin")),
            )
            .await
            .unwrap(),
        );
    }
    wait_for(&t, &ids[2], "to wait for a slot", |v| {
        v.state == State::Queued
    })
    .await;
    t.cancel(&ids[2]).unwrap();
    let v = finished(&t, &ids[2]).await;
    assert_eq!(v.state, State::Cancelled);
    assert_eq!(v.done, 0);
    t.cancel(&ids[0]).unwrap();
    t.cancel(&ids[1]).unwrap();
    finished(&t, &ids[0]).await;
    finished(&t, &ids[1]).await;
    assert!(names_in(&folder).is_empty());
    assert_eq!(t.clear_finished(), 3);
    assert!(t.list().is_empty());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn hostile_remote_names_stay_inside_the_downloads_folder() {
    let rig = Rig::new().await;
    // Names a Linux agent really can hold: backslashes, a leading dot, a newline, a bidi override.
    let hostile = [
        "..\\..\\evil.txt",
        ".hidden",
        "line\nbreak.txt",
        "a\u{202e}txt.exe",
        "...",
    ];
    for (i, name) in hostile.iter().enumerate() {
        std::fs::write(rig.roots.path().join(name), format!("body {i}")).unwrap();
    }
    let base = TempDir::new().unwrap();
    let folder = base.path().join("RFE Desktop");
    let t = Transfers::with_options(small());
    for name in hostile {
        let id = t
            .start_download(Ctx::fixed(rig.conn(), folder.clone()), &rig.remote(name))
            .await
            .unwrap();
        let v = finished(&t, &id).await;
        assert_eq!(v.state, State::Done, "{name:?}: {v:?}");
        let saved = PathBuf::from(&v.local_path);
        assert_eq!(saved.parent().unwrap(), folder, "{name:?} escaped");
        assert!(!v.name.starts_with('.') && !v.name.contains(['/', '\\', '\n', '\u{202e}']));
    }
    assert_eq!(
        names_in(base.path()),
        ["RFE Desktop"],
        "something was written outside"
    );
    let saved = names_in(&folder);
    assert_eq!(saved.len(), hostile.len(), "{saved:?}");
    assert!(saved.iter().all(|n| !n.starts_with('.')), "{saved:?}");

    // The same name twice never overwrites: the second copy gets a number.
    let again = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote(".hidden"),
        )
        .await
        .unwrap();
    let v = finished(&t, &again).await;
    assert_eq!(v.state, State::Done);
    assert_eq!(v.name, "hidden (1)");
    assert_eq!(
        std::fs::read_to_string(folder.join("hidden")).unwrap(),
        "body 1"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_path_that_climbs_out_of_the_agents_folders_is_refused_and_writes_nothing() {
    let rig = Rig::new().await;
    let base = TempDir::new().unwrap();
    let folder = base.path().join("RFE Desktop");
    let t = Transfers::with_options(small());
    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &format!("{}/../../../../../../etc/passwd", rig.root()),
        )
        .await
        .unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Failed, "{v:?}");
    assert_eq!(v.error, transfer_message("FORBIDDEN").unwrap());
    assert!(names_in(&folder).is_empty(), "{:?}", names_in(&folder));

    let id = t
        .start_download(
            Ctx::fixed(rig.conn(), folder.clone()),
            &rig.remote("missing.txt"),
        )
        .await
        .unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.error, transfer_message("PATH_NOT_FOUND").unwrap());
    assert!(names_in(&folder).is_empty());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_upload_with_no_preference_never_replaces_a_file_on_the_agent() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("taken.txt"), "original").unwrap();
    let src = TempDir::new().unwrap();
    let local = src.path().join("taken.txt");
    std::fs::write(&local, "new").unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let id = t
        .start_upload(
            Ctx::fixed(rig.conn(), folder),
            local.to_str().unwrap(),
            rig.root(),
        )
        .await
        .unwrap();
    let v = finished(&t, &id).await;
    // Nothing chose to replace it: the old file stays and the new one gets the next number.
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(v.name, "taken (1).txt");
    assert_eq!(
        std::fs::read_to_string(rig.remote("taken.txt")).unwrap(),
        "original"
    );
    assert_eq!(
        std::fs::read_to_string(rig.remote("taken (1).txt")).unwrap(),
        "new"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn only_a_regular_file_the_user_typed_can_be_uploaded() {
    let rig = Rig::new().await;
    let src = TempDir::new().unwrap();
    let (_base, folder) = dirs();
    let ctx = || Ctx::fixed(rig.conn(), folder.clone());
    let t = Transfers::with_options(small());
    let real = src.path().join("real.txt");
    std::fs::write(&real, "x").unwrap();
    std::os::unix::fs::symlink(&real, src.path().join("link.txt")).unwrap();
    std::os::unix::fs::symlink("/etc/passwd", src.path().join("passwd-link")).unwrap();

    let start = |p: &str| {
        let t = t.clone();
        let ctx = ctx();
        let p = p.to_string();
        let root = rig.root().to_string();
        async move { t.start_upload(ctx, &p, &root).await }
    };
    let e = start("").await.unwrap_err();
    assert!(e.contains("enter the full path"), "{e}");
    let e = start("relative/file.txt").await.unwrap_err();
    assert!(e.contains("starting with /"), "{e}");
    let e = start(src.path().to_str().unwrap()).await.unwrap_err();
    assert!(e.contains("not a regular file"), "{e}");
    let e = start(src.path().join("link.txt").to_str().unwrap())
        .await
        .unwrap_err();
    assert!(e.contains("symbolic link"), "{e}");
    let e = start(src.path().join("passwd-link").to_str().unwrap())
        .await
        .unwrap_err();
    assert!(e.contains("symbolic link"), "{e}");
    let e = start(src.path().join("nope.txt").to_str().unwrap())
        .await
        .unwrap_err();
    assert!(e.contains("cannot read that file"), "{e}");
    let e = start("/dev/null").await.unwrap_err();
    assert!(e.contains("not a regular file"), "{e}");
    assert!(t.list().is_empty(), "a refused path must not be queued");

    // The real path works.
    assert!(start(real.to_str().unwrap()).await.is_ok());
    let e = t
        .start_upload(ctx(), real.to_str().unwrap(), "  ")
        .await
        .unwrap_err();
    assert!(e.contains("enter the folder"), "{e}");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_download_needs_a_signed_in_session_and_a_path() {
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let signed_out = Ctx {
        creds: Arc::new(|| Box::pin(async { Err("not signed in".to_string()) })),
        download_dir: folder.clone(),
    };
    let e = t.start_download(signed_out, "/x/y.txt").await.unwrap_err();
    assert_eq!(e, "not signed in");
    let conn = Conn {
        host: "127.0.0.1:1".into(),
        fingerprint: "ab".repeat(32),
        token: "secret-token".into(),
    };
    assert!(!format!("{conn:?}").contains("secret-token"));
    let e = t
        .start_download(Ctx::fixed(conn.clone(), folder.clone()), "   ")
        .await
        .unwrap_err();
    assert!(e.contains("enter the path of the file"), "{e}");
    let e = t
        .start_download(Ctx::fixed(conn.clone(), folder), "/a\0b")
        .await
        .unwrap_err();
    assert!(e.contains("not usable"), "{e}");
    assert!(t.cancel("nope").unwrap_err().contains("no such transfer"));
    assert!(t.retry("nope").unwrap_err().contains("no such transfer"));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_unreachable_agent_fails_the_transfer_with_a_reason_and_it_can_be_retried() {
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    let conn = Conn {
        host: format!("127.0.0.1:{}", free_port()),
        fingerprint: "ab".repeat(32),
        token: "t".into(),
    };
    let id = t
        .start_download(Ctx::fixed(conn, folder.clone()), "/x/y.txt")
        .await
        .unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Failed);
    assert!(
        v.error.contains("cannot reach the agent securely"),
        "{}",
        v.error
    );
    assert!(t.retry(&id).is_ok());
    assert_eq!(finished(&t, &id).await.state, State::Failed);
    // Done and running transfers cannot be retried; a failed one can be given up on.
    t.cancel(&id).unwrap();
    assert_eq!(t.get(&id).unwrap().state, State::Cancelled);
    assert!(names_in(&folder).is_empty());
}

// ---- names, without an agent ----

#[test]
fn safe_name_keeps_only_a_plain_last_component() {
    let cases = [
        ("/srv/docs/report.pdf", "report.pdf"),
        ("C:\\Users\\me\\report.pdf", "report.pdf"),
        ("/a/b/", "b"),
        ("../../etc/passwd", "passwd"),
        ("..", "download"),
        (".", "download"),
        ("/", "download"),
        ("", "download"),
        (".bashrc", "bashrc"),
        ("...hidden.txt", "hidden.txt"),
        ("..\\..\\evil.txt", "evil.txt"),
        ("a/..\\b", "b"),
        ("name.txt.", "name.txt"),
        ("  spaced name .txt  ", "spaced name .txt"),
        ("ctrl\u{0}\u{7}\r\nname", "ctrlname"),
        ("rtl\u{202e}gpj.exe", "rtlgpj.exe"),
        ("zero\u{200b}width\u{feff}", "zerowidth"),
        ("caf\u{e9}.txt", "caf\u{e9}.txt"),
    ];
    for (input, want) in cases {
        let got = safe_name(input);
        assert_eq!(got, want, "{input:?}");
        assert!(!got.contains(['/', '\\', '\0']) && !got.starts_with('.') && !got.is_empty());
    }
}

#[test]
fn safe_name_is_creatable_on_windows_too() {
    let cases = [
        ("a<b>c:d\"e|f?g*h.txt", "a_b_c_d_e_f_g_h.txt"),
        ("/srv/CON", "_CON"),
        ("/srv/nul.txt", "_nul.txt"),
        ("/srv/Com1.tar.gz", "_Com1.tar.gz"),
        ("/srv/LPT9", "_LPT9"),
        ("/srv/console.txt", "console.txt"),
        ("/srv/com10", "com10"),
    ];
    for (input, want) in cases {
        assert_eq!(safe_name(input), want, "{input:?}");
    }
}

#[test]
fn safe_name_cuts_long_names_on_a_character_boundary_and_keeps_the_extension() {
    let long = format!("{}.tar", "\u{e9}".repeat(300));
    let got = safe_name(&long);
    assert!(got.len() <= 200, "{}", got.len());
    assert!(got.ends_with(".tar") && got.starts_with('\u{e9}'));
    let no_ext = "x".repeat(500);
    assert_eq!(safe_name(&no_ext).len(), 200);
    // An "extension" that is really most of the name must not leave nothing of the stem.
    let odd = format!("a.{}", "b".repeat(300));
    assert!(!safe_name(&odd).is_empty() && safe_name(&odd).len() <= 200);
}

#[test]
fn numbered_names_put_the_counter_before_the_extension() {
    assert_eq!(numbered_name("a.txt", 0), "a.txt");
    assert_eq!(numbered_name("a.txt", 2), "a (2).txt");
    assert_eq!(numbered_name("archive.tar.gz", 1), "archive.tar (1).gz");
    assert_eq!(numbered_name("noext", 3), "noext (3)");
    assert_eq!(numbered_name("dotted", 1), "dotted (1)");
}

#[test]
fn publishing_never_replaces_an_existing_file() {
    let dir = TempDir::new().unwrap();
    std::fs::write(dir.path().join("f.txt"), "mine").unwrap();
    std::fs::write(dir.path().join("f (1).txt"), "mine too").unwrap();
    std::os::unix::fs::symlink("/nonexistent/target", dir.path().join("f (2).txt")).unwrap();
    let part = dir.path().join(".rfe-x.part");
    std::fs::write(&part, "new").unwrap();
    let at = publish_no_clobber(&part, dir.path(), "f.txt").unwrap();
    assert_eq!(at, dir.path().join("f (3).txt"));
    assert_eq!(std::fs::read_to_string(&at).unwrap(), "new");
    assert_eq!(
        std::fs::read_to_string(dir.path().join("f.txt")).unwrap(),
        "mine"
    );
    assert_eq!(
        std::fs::read_to_string(dir.path().join("f (1).txt")).unwrap(),
        "mine too"
    );
    assert!(!part.exists(), "the part file is removed once published");
}

// ---- every message is explained ----

#[test]
fn every_transfer_message_is_in_the_user_guide() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let guide = std::fs::read_to_string(root.join("../docs/user-guide.md")).unwrap();
    for code in TRANSFER_CODES {
        let text = transfer_message(code).expect("a listed code has a message");
        assert!(
            guide.contains(&format!("| {text} | `{code}` |")),
            "user-guide.md has no row for {code} with the text {text:?}"
        );
    }
    let code = std::fs::read_to_string(root.join("src/transfers.rs")).unwrap();
    let ui = ui::js();
    let flat = |s: &str| {
        s.split_whitespace()
            .collect::<Vec<_>>()
            .join(" ")
            .replace("\\ ", "")
            .replace("\" \"", "")
    };
    let (code, ui) = (flat(&code), flat(&ui));
    for stem in [
        "enter the path of the file on the computer",
        "enter the full path of the file on this computer",
        "enter the folder on the computer to upload into",
        "that path on the computer is not usable",
        "cannot use the downloads folder",
        "give the full path of the file, starting with /",
        "that path is a symbolic link; type the real path of the file",
        "that is not a regular file",
        "cannot read that file",
        "the file changed while it was being opened; try again",
        "no such transfer",
        "that transfer cannot be retried now",
        "The downloaded copy does not match the file on the computer",
        "the downloaded file could not be put in place",
        "the agent ended the download early",
        "cannot find your Downloads folder",
        "cannot write",
        "a background task failed",
    ] {
        assert!(code.contains(stem), "{stem:?} is no longer in transfers.rs");
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
    }
    for stem in [
        "Checking the file",
        "Waiting for a free slot",
        "Checksum verified",
    ] {
        assert!(
            ui.contains(stem),
            "{stem:?} is no longer in the window scripts"
        );
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
    }
}

/// The saved login is switched to another agent after a transfer was queued: the transfer must not
/// run against that other agent (same path, other computer); once the login is back it goes on.
#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_transfer_never_runs_against_another_agent_after_a_switch() {
    use std::sync::atomic::AtomicBool;
    let rig = Rig::new().await;
    std::fs::write(rig.remote("f.txt"), b"hello").unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());

    let away = Arc::new(AtomicBool::new(false));
    let calls = Arc::new(AtomicU64::new(0));
    let (real, other) = (rig.conn(), rig.conn_via("127.0.0.1:1"));
    let ctx = Ctx {
        creds: {
            let (away, calls) = (away.clone(), calls.clone());
            Arc::new(move || {
                // The first read is the queueing itself; the switch happens right after it.
                let first = calls.fetch_add(1, Ordering::SeqCst) == 0;
                let c = if first || !away.load(Ordering::SeqCst) {
                    real.clone()
                } else {
                    other.clone()
                };
                Box::pin(async move { Ok(c) })
            })
        },
        download_dir: folder.clone(),
    };
    away.store(true, Ordering::SeqCst);
    let id = t.start_download(ctx, &rig.remote("f.txt")).await.unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Failed, "{v:?}");
    assert!(v.error.contains("belongs to"), "{v:?}");
    assert!(
        !folder.join("f.txt").exists(),
        "nothing may be saved from the wrong agent"
    );

    away.store(false, Ordering::SeqCst);
    t.retry(&id).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(std::fs::read(folder.join("f.txt")).unwrap(), b"hello");
}

// ---- the settings: a taken name, the speed limit, how many run at once, checksums ----

fn ask() -> Policy {
    Policy::Ask
}

fn pol(r: Resolution) -> Policy {
    Policy::Do(r)
}

async fn download_of(t: &Transfers, rig: &Rig, folder: &Path, name: &str) -> String {
    t.start_download(
        Ctx::fixed(rig.conn(), folder.to_path_buf()),
        &rig.remote(name),
    )
    .await
    .unwrap()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_taken_download_name_follows_the_saved_choice() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("a.txt"), b"new").unwrap();
    let (_base, folder) = dirs();
    std::fs::create_dir_all(&folder).unwrap();
    let t = Transfers::with_options(small());

    // Keep both is what the core does with no preference at all.
    std::fs::write(folder.join("a.txt"), b"old").unwrap();
    let id = download_of(&t, &rig, &folder, "a.txt").await;
    assert_eq!(finished(&t, &id).await.state, State::Done);
    assert_eq!(names_in(&folder), ["a (1).txt", "a.txt"]);
    assert_eq!(std::fs::read(folder.join("a.txt")).unwrap(), b"old");

    t.set_prefs(2, 0, pol(Resolution::Replace), true);
    let id = download_of(&t, &rig, &folder, "a.txt").await;
    assert_eq!(finished(&t, &id).await.state, State::Done);
    assert_eq!(std::fs::read(folder.join("a.txt")).unwrap(), b"new");
    assert_eq!(
        names_in(&folder),
        ["a (1).txt", "a.txt"],
        "no part file stays"
    );

    std::fs::write(folder.join("a.txt"), b"mine").unwrap();
    t.set_prefs(2, 0, pol(Resolution::Skip), true);
    let id = download_of(&t, &rig, &folder, "a.txt").await;
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Cancelled, "{v:?}");
    assert_eq!(v.error, SKIPPED);
    assert_eq!(std::fs::read(folder.join("a.txt")).unwrap(), b"mine");
    assert_eq!(names_in(&folder), ["a (1).txt", "a.txt"]);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_taken_download_name_waits_for_an_answer_and_holds_no_place() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("a.txt"), b"new").unwrap();
    std::fs::write(rig.remote("b.txt"), b"bee").unwrap();
    let (_base, folder) = dirs();
    std::fs::create_dir_all(&folder).unwrap();
    std::fs::write(folder.join("a.txt"), b"old").unwrap();
    let t = Transfers::with_options(small());
    // One place only: a question that kept it would hold up b.txt for good.
    t.set_prefs(1, 0, ask(), true);

    let a = download_of(&t, &rig, &folder, "a.txt").await;
    let v = wait_for(&t, &a, "the question", |v| v.state == State::Conflict).await;
    let c = v.conflict.expect("what holds the name");
    assert_eq!((c.is_dir, c.size), (false, 3));
    assert!(c.modified_ms.is_some());

    let b = download_of(&t, &rig, &folder, "b.txt").await;
    assert_eq!(finished(&t, &b).await.state, State::Done);
    assert_eq!(t.get(&a).unwrap().state, State::Conflict, "still waiting");

    assert!(
        t.resolve(&b, Resolution::Skip).is_err(),
        "nothing to answer"
    );
    t.resolve(&a, Resolution::Replace).unwrap();
    let v = finished(&t, &a).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert!(v.conflict.is_none());
    assert_eq!(std::fs::read(folder.join("a.txt")).unwrap(), b"new");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn an_unanswered_question_can_be_cancelled_and_skip_leaves_the_old_file() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("a.txt"), b"new").unwrap();
    let (_base, folder) = dirs();
    std::fs::create_dir_all(&folder).unwrap();
    std::fs::write(folder.join("a.txt"), b"old").unwrap();
    let t = Transfers::with_options(small());
    t.set_prefs(2, 0, ask(), true);

    let a = download_of(&t, &rig, &folder, "a.txt").await;
    wait_for(&t, &a, "the question", |v| v.state == State::Conflict).await;
    t.cancel(&a).unwrap();
    assert_eq!(finished(&t, &a).await.state, State::Cancelled);

    let b = download_of(&t, &rig, &folder, "a.txt").await;
    wait_for(&t, &b, "the question", |v| v.state == State::Conflict).await;
    t.resolve(&b, Resolution::Skip).unwrap();
    let v = finished(&t, &b).await;
    assert_eq!((v.state, v.error.as_str()), (State::Cancelled, SKIPPED));
    assert_eq!(std::fs::read(folder.join("a.txt")).unwrap(), b"old");
    assert_eq!(names_in(&folder), ["a.txt"]);
}

async fn upload_of(t: &Transfers, rig: &Rig, local: &Path) -> String {
    t.start_upload(
        Ctx::fixed(rig.conn(), PathBuf::from("/unused")),
        local.to_str().unwrap(),
        rig.root(),
    )
    .await
    .unwrap()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_taken_upload_name_follows_the_saved_choice() {
    let rig = Rig::new().await;
    let src = TempDir::new().unwrap();
    let local = src.path().join("up.txt");
    std::fs::write(&local, b"fresh").unwrap();
    std::fs::write(rig.remote("up.txt"), b"old").unwrap();
    let t = Transfers::with_options(small());

    t.set_prefs(2, 0, pol(Resolution::Both), true);
    let id = upload_of(&t, &rig, &local).await;
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert_eq!(v.name, "up (1).txt");
    assert_eq!(std::fs::read(rig.remote("up.txt")).unwrap(), b"old");
    assert_eq!(std::fs::read(rig.remote("up (1).txt")).unwrap(), b"fresh");

    // The second copy goes to the next free number.
    let id = upload_of(&t, &rig, &local).await;
    let v = finished(&t, &id).await;
    assert_eq!((v.state, v.name.as_str()), (State::Done, "up (2).txt"));

    t.set_prefs(2, 0, pol(Resolution::Replace), true);
    let id = upload_of(&t, &rig, &local).await;
    assert_eq!(finished(&t, &id).await.state, State::Done);
    assert_eq!(std::fs::read(rig.remote("up.txt")).unwrap(), b"fresh");

    std::fs::write(rig.remote("up.txt"), b"theirs").unwrap();
    t.set_prefs(2, 0, pol(Resolution::Skip), true);
    let id = upload_of(&t, &rig, &local).await;
    let v = finished(&t, &id).await;
    assert_eq!((v.state, v.error.as_str()), (State::Cancelled, SKIPPED));
    assert_eq!(std::fs::read(rig.remote("up.txt")).unwrap(), b"theirs");
    assert!(rig.temp_files().is_empty(), "{:?}", rig.temp_files());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn a_taken_upload_name_waits_for_an_answer() {
    let rig = Rig::new().await;
    let src = TempDir::new().unwrap();
    let local = src.path().join("up.txt");
    std::fs::write(&local, b"fresh").unwrap();
    std::fs::write(rig.remote("up.txt"), b"older").unwrap();
    let t = Transfers::with_options(small());
    t.set_prefs(2, 0, ask(), true);

    let id = upload_of(&t, &rig, &local).await;
    let v = wait_for(&t, &id, "the question", |v| v.state == State::Conflict).await;
    let c = v.conflict.expect("what holds the name");
    assert_eq!((c.is_dir, c.size), (false, 5));
    assert!(
        !c.modified_text.is_empty(),
        "the agent says when it changed"
    );

    t.resolve(&id, Resolution::Both).unwrap();
    let v = finished(&t, &id).await;
    assert_eq!(
        (v.state, v.name.as_str()),
        (State::Done, "up (1).txt"),
        "{v:?}"
    );
    assert_eq!(std::fs::read(rig.remote("up.txt")).unwrap(), b"older");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn the_speed_limit_holds_for_downloads_and_uploads_and_can_be_lifted() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("slow.bin"), pattern(3 * MIB)).unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    t.set_prefs(2, 1_000_000, pol(Resolution::Both), true);

    let began = Instant::now();
    let id = download_of(&t, &rig, &folder, "slow.bin").await;
    assert_eq!(finished(&t, &id).await.state, State::Done);
    let took = began.elapsed();
    // 3 MiB is 3.1 million bytes: at a million a second it cannot take less than about 2.5 s.
    assert!(
        took >= Duration::from_millis(2400),
        "downloaded in {took:?}"
    );

    let src = TempDir::new().unwrap();
    let local = src.path().join("slowup.bin");
    std::fs::write(&local, pattern(3 * MIB)).unwrap();
    let began = Instant::now();
    let id = upload_of(&t, &rig, &local).await;
    assert_eq!(finished(&t, &id).await.state, State::Done);
    let took = began.elapsed();
    assert!(took >= Duration::from_millis(2400), "uploaded in {took:?}");

    // Lifted, the next one is quick again.
    t.set_prefs(2, 0, pol(Resolution::Both), true);
    let began = Instant::now();
    let id = download_of(&t, &rig, &folder, "slow.bin").await;
    assert_eq!(finished(&t, &id).await.state, State::Done);
    assert!(began.elapsed() < Duration::from_millis(2400));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn the_number_of_parallel_transfers_follows_the_setting_even_while_running() {
    let rig = Rig::new().await;
    for i in 0..6 {
        std::fs::write(rig.remote(&format!("f{i}.bin")), pattern(MIB)).unwrap();
    }
    rig.throttle((MIB / 2) as u64).await;
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    t.set_prefs(4, 0, pol(Resolution::Both), true);
    let mut ids = vec![];
    for i in 0..6 {
        ids.push(download_of(&t, &rig, &folder, &format!("f{i}.bin")).await);
    }
    let count = |t: &Transfers| {
        t.list()
            .iter()
            .filter(|v| v.state == State::Running)
            .count()
    };
    let end = Instant::now() + Duration::from_secs(30);
    while count(&t) < 4 {
        assert!(
            Instant::now() < end,
            "four should run together: {:?}",
            t.list()
        );
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    // Cut to one while four run: no new one starts until fewer than one run.
    t.set_prefs(1, 0, pol(Resolution::Both), true);
    let mut peak_after = 0;
    let end = Instant::now() + Duration::from_secs(120);
    let mut settled = false;
    loop {
        let all = t.list();
        let running = all.iter().filter(|v| v.state == State::Running).count();
        if running <= 1 {
            settled = true;
        }
        if settled {
            peak_after = peak_after.max(running);
        }
        if all.iter().all(|v| v.state == State::Done) {
            break;
        }
        assert!(Instant::now() < end, "timed out: {all:?}");
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    assert!(
        settled && peak_after <= 1,
        "peak {peak_after} after the cut"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn checksum_checking_can_be_turned_off_for_downloads() {
    let rig = Rig::new().await;
    std::fs::write(rig.remote("c.bin"), pattern(MIB)).unwrap();
    let (_base, folder) = dirs();
    let t = Transfers::with_options(small());
    t.set_prefs(2, 0, pol(Resolution::Both), false);
    let id = download_of(&t, &rig, &folder, "c.bin").await;
    let v = finished(&t, &id).await;
    assert_eq!(v.state, State::Done, "{v:?}");
    assert!(!v.verified, "nothing was compared");
    assert_eq!(
        sha256_file(&folder.join("c.bin")),
        hex::encode(Sha256::digest(pattern(MIB)))
    );
}
