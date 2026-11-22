//! Generate a one-time pairing code for a phone (`POST /pairing/generate`). Only an admin session
//! (one signed in with the account) may do it; any other session gets a plain "not allowed"
//! state instead of an error. The code goes to the window once, to be shown; this app never
//! writes it to `state.json`, the keystore or the log, and keeps no copy after answering.

use crate::agent_client::{AgentClient, AgentError};
use crate::applog;
use crate::flows;
use crate::health::{STATUS_FORBIDDEN, STATUS_UNAUTHORIZED};
use crate::secrets::Offloaded;
use serde::Serialize;
use std::path::Path;
use std::process::{Command, Output, Stdio};
use std::time::{Duration, Instant};

/// How long the agent program on this PC may take to answer one admin command before the window stops
/// waiting for it and stops the program, so a hung program cannot freeze the dialog.
const CLI_TIMEOUT: Duration = Duration::from_secs(10);

/// How often a running agent program is checked for having finished while the window waits for it.
const CLI_POLL: Duration = Duration::from_millis(25);

/// How long a code made here stays valid. Shorter than the agent's default hour, because the
/// code sits on a screen; "Generate a new code" makes another.
pub const TTL_SECONDS: u64 = 600;

/// What the window gets: a code and its lifetime, or the fact that this login may not make one.
#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct CodeView {
    /// `"ok"` or `"forbidden"`.
    pub status: &'static str,
    /// Empty unless `status` is `"ok"`.
    pub code: String,
    pub expires_in_seconds: u64,
    /// The text the phone's QR scanner reads (JSON); carries the live code, so it is redacted in
    /// `Debug` and never logged. Empty unless `status` is `"ok"`.
    pub qr: String,
}

// Not derived: a `{:?}` in a log line or a test failure must not print a live pairing code.
impl std::fmt::Debug for CodeView {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("CodeView")
            .field("status", &self.status)
            .field(
                "code",
                &if self.code.is_empty() {
                    ""
                } else {
                    "<redacted>"
                },
            )
            .field("expires_in_seconds", &self.expires_in_seconds)
            .field("qr", &if self.qr.is_empty() { "" } else { "<redacted>" })
            .finish()
    }
}

/// Mints a code with the saved login. A refusal because the session is not an admin one is a
/// result (`forbidden`), not an error, so the window can explain it; every other failure is an
/// error whose text is shown as it is.
pub async fn generate(dir: &Path, store: &Offloaded) -> Result<CodeView, AgentError> {
    generate_for(dir, store, TTL_SECONDS).await
}

/// The lifetimes the settings offer, in seconds: two, five and ten minutes.
pub const LIFETIMES: [u64; 3] = [120, 300, 600];

/// Like `generate`, with a lifetime from [`LIFETIMES`]; anything else is the default.
pub async fn generate_for(
    dir: &Path,
    store: &Offloaded,
    ttl_seconds: u64,
) -> Result<CodeView, AgentError> {
    let ttl = if LIFETIMES.contains(&ttl_seconds) {
        ttl_seconds
    } else {
        TTL_SECONDS
    };
    let r = generate_inner(dir, store, ttl).await;
    match &r {
        Ok(v) if v.status == "ok" => applog::info("pairing code generation: ok"),
        Ok(_) => {
            applog::info("pairing code generation: refused, this login is not an admin session")
        }
        Err(e) => applog::error(&format!("pairing code generation failed: {e}")),
    }
    r
}

async fn generate_inner(
    dir: &Path,
    store: &Offloaded,
    ttl_seconds: u64,
) -> Result<CodeView, AgentError> {
    let saved = {
        let dir = dir.to_path_buf();
        store
            .run(move |s| flows::load_saved(&dir, s))
            .await
            .map_err(AgentError::Local)?
    };
    if saved.token.is_empty() || saved.host.is_empty() {
        return Err(AgentError::Local("not signed in".into()));
    }
    // The pin list is the record of what is trusted: a forgotten pin ends the session here.
    let pinned = flows::list_pins(dir)
        .map_err(AgentError::Local)?
        .into_iter()
        .find(|p| p.host == flows::pin_key(&saved.host))
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                saved.host
            ))
        })?;
    let made = AgentClient::pinned(&saved.host, &pinned)?
        .generate_pairing_code(&saved.token, ttl_seconds)
        .await;
    match made {
        Ok(g) if g.pairing_code.is_empty() => Err(AgentError::Local(
            "unexpected response: the agent sent no pairing code".into(),
        )),
        Ok(g) => {
            applog::register_secret(&g.pairing_code);
            let qr = if g.qr_payload.is_null() {
                String::new()
            } else {
                g.qr_payload.to_string()
            };
            applog::register_secret(&qr);
            Ok(CodeView {
                status: "ok",
                code: g.pairing_code,
                expires_in_seconds: g.expires_in_seconds,
                qr,
            })
        }
        Err(AgentError::Server {
            status: STATUS_FORBIDDEN,
            ..
        }) => Ok(CodeView {
            status: "forbidden",
            code: String::new(),
            expires_in_seconds: 0,
            qr: String::new(),
        }),
        Err(e) => {
            if let AgentError::Server {
                status: STATUS_UNAUTHORIZED,
                ..
            } = &e
            {
                // The agent refuses this token: drop the dead login so the window goes back to
                // sign-in, as for the device list.
                let dir = dir.to_path_buf();
                let token = saved.token.clone();
                store
                    .run(move |st| flows::drop_refused_token(&dir, st, &token))
                    .await
                    .map_err(AgentError::Local)?;
            }
            Err(e)
        }
    }
}

/// A pairing code from the agent on this PC, for when no computer has been added yet. It runs the
/// agent's own `pair -json` (the admin CLI opens the agent's data directly, so no login is
/// needed), the same code `rfe pair` prints. Same shape as [`generate_for`], so the window shows it
/// the same way.
pub async fn generate_local(ttl_seconds: u64) -> Result<CodeView, AgentError> {
    let out = tokio::task::spawn_blocking(move || {
        let bin = local_agent_bin().ok_or_else(|| {
            AgentError::Local(
                "this PC has no RFE agent installed, so it cannot make a pairing code".into(),
            )
        })?;
        let args = ["pair", "-json", "-ttl", &format!("{ttl_seconds}s")].map(String::from);
        run_cli(&bin, &args, CLI_TIMEOUT)
    })
    .await
    .map_err(|e| AgentError::Local(e.to_string()))??;
    if !out.status.success() {
        let why = String::from_utf8_lossy(&out.stderr);
        let why = why.trim();
        return Err(AgentError::Local(if why.is_empty() {
            "the RFE agent on this PC could not make a pairing code".into()
        } else {
            why.to_string()
        }));
    }
    let g: crate::agent_client::GeneratedCode =
        serde_json::from_slice(&out.stdout).map_err(|_| {
            AgentError::Local(
                "the RFE agent on this PC is too old to make a pairing code; update it".into(),
            )
        })?;
    if g.pairing_code.is_empty() {
        return Err(AgentError::Local("the agent sent no pairing code".into()));
    }
    applog::register_secret(&g.pairing_code);
    let qr = g.qr_payload.to_string();
    applog::register_secret(&qr);
    Ok(CodeView {
        status: "ok",
        code: g.pairing_code,
        expires_in_seconds: g.expires_in_seconds,
        qr,
    })
}

/// Runs one of the agent's admin commands on this PC (no sign-in: the admin CLI opens the agent's
/// data directly) and returns what it printed. Only the commands the Devices page needs are allowed,
/// and ids and permission lists are checked so nothing can be smuggled in as a flag.
pub async fn local_admin(args: Vec<String>) -> Result<String, AgentError> {
    let word = |s: &str| {
        !s.is_empty()
            && s.len() <= 120
            && !s.starts_with('-')
            && s.chars()
                .all(|c| c.is_ascii_alphanumeric() || "-,".contains(c))
    };
    let on_off = |s: &str| matches!(s, "on" | "off");
    // A folder limit is a path (or empty to clear it); it must not look like a flag.
    let folder =
        |s: &str| s.len() <= 4096 && !s.starts_with('-') && !s.chars().any(char::is_control);
    let ok = match args
        .iter()
        .map(String::as_str)
        .collect::<Vec<_>>()
        .as_slice()
    {
        ["devices", "-json"] => true,
        ["revoke", id] | ["remove", id] => word(id),
        ["allow", id, list] => word(id) && word(list),
        ["apps", id, view, launch] => word(id) && on_off(view) && on_off(launch),
        ["readonly", id, v] => word(id) && on_off(v),
        ["jail", id, path] => word(id) && folder(path),
        ["pair", "requests", "-json"] => true,
        ["pair", "accept", id] | ["pair", "reject", id] => word(id),
        ["audit", "-json", "-n", n] => n.len() <= 3 && n.chars().all(|c| c.is_ascii_digit()),
        _ => false,
    };
    if !ok {
        return Err(AgentError::Local("that command is not allowed".into()));
    }
    let out = tokio::task::spawn_blocking(move || {
        let bin = local_agent_bin()
            .ok_or_else(|| AgentError::Local("this PC has no RFE agent installed".into()))?;
        run_cli(&bin, &args, CLI_TIMEOUT)
    })
    .await
    .map_err(|e| AgentError::Local(e.to_string()))??;
    if !out.status.success() {
        let why = String::from_utf8_lossy(&out.stderr);
        let why = why.trim();
        return Err(AgentError::Local(if why.is_empty() {
            "the RFE agent could not do that".into()
        } else {
            why.to_string()
        }));
    }
    Ok(String::from_utf8_lossy(&out.stdout).into_owned())
}

/// Runs the agent program `bin` with `args` and returns what it printed, like `Command::output`, but
/// gives up after `timeout`: the program is then stopped through the process id recorded when it was
/// started (never by name), reaped, and an error says it did not answer. Its output is read on two
/// helper threads while it runs, so a program that prints more than a pipe holds cannot block itself.
fn run_cli(bin: &Path, args: &[String], timeout: Duration) -> Result<Output, AgentError> {
    let run_err =
        |e: std::io::Error| AgentError::Local(format!("could not run the RFE agent: {e}"));
    let mut child = Command::new(bin)
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(run_err)?;
    /* A read error ends the capture early; what was read so far is kept, and the exit status still decides success. */
    let drain = |pipe: Option<Box<dyn std::io::Read + Send>>| {
        std::thread::spawn(move || {
            let mut buf = Vec::new();
            if let Some(mut p) = pipe {
                let _ = p.read_to_end(&mut buf);
            }
            buf
        })
    };
    let out = drain(child.stdout.take().map(|p| Box::new(p) as _));
    let err = drain(child.stderr.take().map(|p| Box::new(p) as _));
    let deadline = Instant::now() + timeout;
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => std::thread::sleep(CLI_POLL),
            outcome => {
                /* Timed out, or the wait itself failed: stop the program by its recorded pid and reap it. The kill
                can fail only when the program just ended on its own, which is the outcome wanted anyway. */
                let _ = child.kill();
                let _ = child.wait();
                return Err(match outcome {
                    Err(e) => run_err(e),
                    _ => AgentError::Local(format!(
                        "the RFE agent did not answer within {} seconds and was stopped; try again",
                        timeout.as_secs_f64()
                    )),
                });
            }
        }
    };
    let join = |h: std::thread::JoinHandle<Vec<u8>>| h.join().unwrap_or_default();
    Ok(Output {
        status,
        stdout: join(out),
        stderr: join(err),
    })
}

/// The agent program on this PC: `$RFE_AGENT_BIN`, else `rfe` (or the older `rfe-agent`) on the
/// PATH, beside the desktop app, in `~/.local/bin` or in `/usr/bin`.
fn local_agent_bin() -> Option<std::path::PathBuf> {
    if let Some(p) = std::env::var_os("RFE_AGENT_BIN").map(std::path::PathBuf::from) {
        if p.is_file() {
            return Some(p);
        }
    }
    let mut dirs: Vec<std::path::PathBuf> = std::env::var_os("PATH")
        .map(|p| std::env::split_paths(&p).collect())
        .unwrap_or_default();
    /* The installed app keeps the agent beside itself (~/.local/opt/rfe-desktop/rfe), which is on no PATH. */
    if let Some(d) = std::env::current_exe()
        .ok()
        .and_then(|e| e.parent().map(|p| p.to_path_buf()))
    {
        dirs.push(d);
    }
    if let Some(h) = std::env::var_os("HOME") {
        dirs.push(std::path::PathBuf::from(h).join(".local/bin"));
    }
    dirs.push("/usr/bin".into());
    ["rfe", "rfe-agent"]
        .iter()
        .flat_map(|n| dirs.iter().map(move |d| d.join(n)))
        .find(|p| p.is_file())
}

/// `run_cli` against `/bin/sh` stand-ins. The hung-program case, with the 10 s limit and the pid check,
/// is in tests/pairing_cli_timeout.rs.
#[cfg(all(test, unix))]
mod pairing_cli_tests {
    use super::run_cli;
    use std::path::Path;
    use std::time::Duration;

    fn sh(script: &str) -> Vec<String> {
        vec!["-c".into(), script.into()]
    }

    #[test]
    fn pairing_cli_returns_stdout_stderr_and_the_exit_status() {
        let out = run_cli(
            Path::new("/bin/sh"),
            &sh("printf out; printf err >&2; exit 3"),
            Duration::from_secs(10),
        )
        .unwrap();
        assert_eq!(out.status.code(), Some(3));
        assert_eq!(out.stdout, b"out");
        assert_eq!(out.stderr, b"err");
    }

    #[test]
    fn pairing_cli_output_larger_than_a_pipe_does_not_block_the_program() {
        let out = run_cli(
            Path::new("/bin/sh"),
            &sh("head -c 400000 /dev/zero; head -c 400000 /dev/zero >&2"),
            Duration::from_secs(10),
        )
        .unwrap();
        assert!(out.status.success());
        assert_eq!((out.stdout.len(), out.stderr.len()), (400_000, 400_000));
    }

    #[test]
    fn pairing_cli_that_cannot_start_says_so() {
        let e = run_cli(Path::new("/nonexistent/rfe"), &[], Duration::from_secs(1)).unwrap_err();
        assert!(
            format!("{e:?}").contains("could not run the RFE agent"),
            "{e:?}"
        );
    }
}

#[cfg(test)]
mod local_admin_tests {
    use super::local_admin;

    fn refused(args: &[&str]) -> bool {
        let args: Vec<String> = args.iter().map(|s| s.to_string()).collect();
        let r = tokio::runtime::Builder::new_current_thread()
            .build()
            .unwrap()
            .block_on(local_admin(args));
        matches!(r, Err(e) if e.to_string().contains("not allowed"))
    }

    #[test]
    fn refuses_anything_that_is_not_a_listed_command() {
        assert!(refused(&["jail", "abc", "--data"]));
        assert!(refused(&["jail", "abc", "/a\nb"]));
        assert!(refused(&["readonly", "abc", "maybe"]));
        assert!(refused(&["apps", "abc", "on"]));
        assert!(refused(&["pair", "accept", "a b"]));
        assert!(refused(&["audit", "-json", "-n", "99999"]));
        assert!(refused(&["setup"]));
        assert!(refused(&["devices", "-json", "-data", "/tmp"]));
    }

    #[test]
    fn refuses_an_id_or_list_that_starts_with_a_dash() {
        assert!(refused(&["revoke", "--help"]));
        assert!(refused(&["remove", "-x"]));
        assert!(refused(&["allow", "-x", "browse"]));
        assert!(refused(&["allow", "abc", "-browse"]));
        assert!(refused(&["apps", "--help", "on", "off"]));
        assert!(refused(&["readonly", "-x", "on"]));
        assert!(refused(&["jail", "-x", "/srv"]));
        assert!(refused(&["pair", "accept", "-x"]));
        assert!(refused(&["pair", "reject", "--help"]));
    }
}
