//! Shared by the agent-backed integration tests: starts a throwaway `rfe` on a random
//! loopback port with its own data and roots directories. Set RFE_AGENT_BIN to a built agent.
#![allow(dead_code)]

use rfe_desktop_lib::agent_client::{normalize_fingerprint, AgentError};
use rfe_desktop_lib::identity::Identity;
use rfe_desktop_lib::secrets::MemoryStore;
use serde_json::Value;
use std::net::{TcpListener, TcpStream};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};
use tempfile::TempDir;

pub fn agent_bin() -> PathBuf {
    PathBuf::from(std::env::var("RFE_AGENT_BIN").expect("set RFE_AGENT_BIN to a built rfe binary"))
}

/// A loopback port nobody is using, for an agent that is started a moment later. Asking the system
/// for port 0 and closing the socket again is not enough: under load the same number goes to the next
/// asker (another test's agent, or a client socket in the system's own range) before this agent has
/// bound it. Such an agent exits, and the test then talks to someone else's agent. So each call takes
/// the next number of a range the system never hands out by itself, and starts from a place that
/// depends on the process, so tests running side by side do not share numbers either.
pub fn free_port() -> u16 {
    use std::sync::atomic::{AtomicU32, Ordering};
    static NEXT: AtomicU32 = AtomicU32::new(0);
    const BASE: u32 = 12_000;
    const SPAN: u32 = 18_000; // below the system's range for outgoing sockets (32768 and up)
    let start = std::process::id().wrapping_mul(2_654_435_761) % SPAN;
    loop {
        let port = (BASE + (start + NEXT.fetch_add(1, Ordering::SeqCst)) % SPAN) as u16;
        if TcpListener::bind(("127.0.0.1", port)).is_ok() {
            return port;
        }
    }
}

pub fn cli(args: &[&str]) -> String {
    let out = Command::new(agent_bin())
        .args(args)
        .output()
        .expect("run rfe");
    let text = format!(
        "{}{}",
        String::from_utf8_lossy(&out.stdout),
        String::from_utf8_lossy(&out.stderr)
    );
    assert!(out.status.success(), "rfe {args:?} failed: {text}");
    text
}

pub struct Agent {
    child: Child,
    data: TempDir,
    _roots: TempDir,
    pub host: String,
}

impl Agent {
    pub fn start(port: u16) -> Self {
        let data = TempDir::new().unwrap();
        let roots = TempDir::new().unwrap();
        let host = format!("127.0.0.1:{port}");
        let child = Command::new(agent_bin())
            .args(["-addr", &host, "-name", "rfe-desktop-test"])
            .args(["-data", data.path().to_str().unwrap()])
            .args(["-roots", roots.path().to_str().unwrap()])
            // The agent shows a desktop notification for each pairing request through notify-send.
            // With no notify-send on its PATH and no session bus, a test never reaches the
            // owner's screen; tests answer requests with the CLI below.
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
        Agent {
            child,
            data,
            _roots: roots,
            host,
        }
    }

    pub fn dir(&self) -> &str {
        self.data.path().to_str().unwrap()
    }

    pub fn add_user(&self, user: &str, password: &str) {
        cli(&["adduser", "-password", password, "-data", self.dir(), user]);
    }

    /// Ground truth: the fingerprint the agent itself reports.
    pub fn status_fingerprint(&self) -> String {
        let out = cli(&["status", "-data", self.dir()]);
        let line = out
            .lines()
            .find(|l| l.starts_with("fingerprint:"))
            .expect("fingerprint line");
        normalize_fingerprint(line.trim_start_matches("fingerprint:"))
    }

    /// A fresh one-time pairing code, as `rfe pair` prints it on its first line.
    pub fn pair_code(&self) -> String {
        let out = cli(&["pair", "-data", self.dir()]);
        let first = out.lines().next().expect("pair output");
        first
            .strip_prefix("Pairing code:")
            .unwrap_or_else(|| panic!("unexpected pair output: {first}"))
            .split_whitespace()
            .next()
            .expect("a code")
            .to_string()
    }

    /// `rfe pair requests`: one line per waiting request, with its match code.
    pub fn pair_requests_cli(&self) -> String {
        cli(&["pair", "requests", "-data", self.dir()])
    }

    /// The match code the agent shows the owner for the (only) waiting request.
    pub fn waiting_match_code(&self) -> String {
        let out = self.pair_requests_cli();
        let line = out
            .lines()
            .find(|l| l.contains("match code"))
            .unwrap_or_else(|| panic!("no waiting request: {out}"));
        line.rsplit("match code").next().unwrap().trim().to_string()
    }

    /// Answers the only waiting request, as the owner would.
    pub fn answer_pair_request(&self, approve: bool) {
        let verb = if approve { "accept" } else { "reject" };
        cli(&["pair", verb, "-data", self.dir()]);
    }

    /// Whether this agent has approve-on-PC with the commit-then-reveal nonce (`/pair/request/{id}/reveal`):
    /// a newer agent answers with a JSON error body, an older one with a bare-text 404.
    pub async fn supports_pair_request(&self) -> bool {
        let raw = Raw::new(&self.host);
        let resp = raw
            .http
            .post(format!("{}/pair/request/probe/reveal", raw.base))
            .json(&serde_json::json!({}))
            .send()
            .await
            .unwrap();
        resp.headers()
            .get("content-type")
            .and_then(|v| v.to_str().ok())
            .is_some_and(|v| v.starts_with("application/json"))
    }

    /// For tests of approve-on-PC: `true` when the agent can do it. An older agent fails the test
    /// unless RFE_ALLOW_OLD_AGENT is set, which the previous-release CI job sets on purpose, so a
    /// missing feature can never pass silently in the main job.
    pub async fn require_pair_request(&self) -> bool {
        if self.supports_pair_request().await {
            return true;
        }
        assert!(
            std::env::var_os("RFE_ALLOW_OLD_AGENT").is_some(),
            "this agent has no /pair/request; set RFE_ALLOW_OLD_AGENT=1 to test an older release"
        );
        eprintln!("skipped: agent has no /pair/request");
        false
    }

    /// Blocks a device on the agent, as the owner would at the PC.
    pub fn revoke_cli(&self, device_id: &str) {
        cli(&["revoke", "-data", self.dir(), device_id]);
    }

    /// Deletes a device row on the agent.
    pub fn remove_cli(&self, device_id: &str) {
        cli(&["remove", "-data", self.dir(), device_id]);
    }

    pub fn devices_cli(&self) -> String {
        cli(&["devices", "-data", self.dir()])
    }

    pub fn audit_cli(&self) -> String {
        cli(&["audit", "-data", self.dir()])
    }

    pub fn stop(mut self) -> TempDir {
        let _ = self.child.kill();
        let _ = self.child.wait();
        // keep the data dir alive for the caller by moving it out
        std::mem::replace(&mut self.data, TempDir::new().unwrap())
    }
}

impl Drop for Agent {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

pub struct Raw {
    pub http: reqwest::Client,
    pub base: String,
}

impl Raw {
    /// Plain HTTPS to the agent, trusting any certificate: the tests build requests the app never
    /// would (a replayed nonce, a wrong signature) to make the agent refuse them.
    pub fn new(host: &str) -> Self {
        Self {
            http: reqwest::Client::builder()
                .danger_accept_invalid_certs(true)
                .build()
                .unwrap(),
            base: format!("https://{host}/v1"),
        }
    }

    pub async fn nonce(&self) -> String {
        let v: Value = self
            .http
            .post(format!("{}/auth/challenge", self.base))
            .send()
            .await
            .unwrap()
            .json()
            .await
            .unwrap();
        v["nonce"].as_str().unwrap().to_string()
    }

    /// The agent's refusal as the app would see it.
    pub async fn refused(&self, path: &str, body: Value) -> AgentError {
        let resp = self
            .http
            .post(format!("{}{path}", self.base))
            .json(&body)
            .send()
            .await
            .unwrap();
        let status = resp.status().as_u16();
        assert!(status >= 400, "{path} unexpectedly succeeded: {status}");
        let v: Value = resp.json().await.unwrap();
        AgentError::Server {
            status,
            code: v["code"].as_str().unwrap_or_default().to_string(),
            message: v["message"].as_str().unwrap_or_default().to_string(),
        }
    }

    /// Like `refused`, as a paired device presenting its bearer token.
    pub async fn refused_as(&self, token: &str, path: &str, body: Value) -> AgentError {
        let resp = self
            .http
            .post(format!("{}{path}", self.base))
            .bearer_auth(token)
            .json(&body)
            .send()
            .await
            .unwrap();
        let status = resp.status().as_u16();
        assert!(status >= 400, "{path} unexpectedly succeeded: {status}");
        let v: Value = resp.json().await.unwrap();
        AgentError::Server {
            status,
            code: v["code"].as_str().unwrap_or_default().to_string(),
            message: v["message"].as_str().unwrap_or_default().to_string(),
        }
    }

    pub async fn ok(&self, path: &str, body: Value) {
        let resp = self
            .http
            .post(format!("{}{path}", self.base))
            .json(&body)
            .send()
            .await
            .unwrap();
        assert!(resp.status().is_success(), "{path}: {}", resp.status());
    }
}

pub fn identity() -> (Identity, TempDir) {
    let dir = TempDir::new().unwrap();
    let id = Identity::load_or_create(dir.path(), &MemoryStore::default()).unwrap();
    (id, dir)
}

/// `true` when the agent under test is expected to have the feature a test is about to check.
/// The previous-release CI job sets RFE_ALLOW_OLD_AGENT on purpose and a feature that release
/// lacks is then skipped with a note; without the flag the check always runs, so a missing
/// feature can never pass silently in the main job.
pub fn modern(feature: &str) -> bool {
    if std::env::var_os("RFE_ALLOW_OLD_AGENT").is_some() {
        eprintln!("skipped (older agent): {feature}");
        return false;
    }
    true
}
