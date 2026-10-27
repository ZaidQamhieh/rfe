//! The host app catalog against throwaway agents: the agent's desktop entries come from a temp
//! folder (`XDG_DATA_DIRS`), never from this machine, and the launcher is a script that records
//! its arguments, so nothing is ever started. Run with RFE_AGENT_BIN set.

mod common;

use common::{agent_bin, cli, free_port, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentError};
use rfe_desktop_lib::apps::{self, APP_MESSAGES, NO_CATALOG_MESSAGE};
use rfe_desktop_lib::flows::{self, Saved};
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use serde_json::json;
use std::collections::BTreeSet;
use std::net::TcpStream;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};
use tempfile::TempDir;

const PW: &str = "pw-for-app-tests";

/// How the throwaway agent sees the machine it runs on.
struct Env {
    /// A graphical session is claimed (DISPLAY set), or not (neither DISPLAY nor WAYLAND_DISPLAY).
    display: bool,
    /// A directory holding a `gio` that records its arguments, or none on the agent's PATH.
    launcher: Option<PathBuf>,
}

struct AppAgent {
    child: Child,
    data: TempDir,
    desktop: TempDir,
    host: String,
}

impl AppAgent {
    fn start(env: &Env) -> Self {
        let data = TempDir::new().unwrap();
        let roots = TempDir::new().unwrap();
        let desktop = TempDir::new().unwrap();
        std::fs::create_dir_all(desktop.path().join("user-data/applications")).unwrap();
        std::fs::create_dir_all(desktop.path().join("system-data/applications")).unwrap();
        let host = format!("127.0.0.1:{}", free_port());
        let mut cmd = Command::new(agent_bin());
        cmd.args(["-addr", &host, "-name", "rfe-desktop-apps-test"])
            .args(["-data", data.path().to_str().unwrap()])
            .args(["-roots", roots.path().to_str().unwrap()])
            .env_clear()
            .env(
                "PATH",
                env.launcher
                    .as_deref()
                    .unwrap_or(Path::new("/nonexistent-rfe-test-path")),
            )
            .env("HOME", desktop.path())
            .env("XDG_DATA_HOME", desktop.path().join("user-data"))
            .env("XDG_DATA_DIRS", desktop.path().join("system-data"))
            .env("RFE_TEST_LAUNCH_LOG", desktop.path().join("launcher-args"))
            .stdout(Stdio::null())
            .stderr(Stdio::null());
        if env.display {
            cmd.env("DISPLAY", ":rfe-test");
        }
        let child = cmd.spawn().expect("start rfe");
        let deadline = Instant::now() + Duration::from_secs(20);
        while TcpStream::connect(&host).is_err() {
            assert!(Instant::now() < deadline, "agent did not start on {host}");
            std::thread::sleep(Duration::from_millis(100));
        }
        let a = AppAgent {
            child,
            data,
            desktop,
            host,
        };
        cli(&["adduser", "-password", PW, "-data", a.dir(), "owner"]);
        a
    }

    fn dir(&self) -> &str {
        self.data.path().to_str().unwrap()
    }

    fn entry(&self, file: &str, body: &str) {
        std::fs::write(
            self.desktop
                .path()
                .join("user-data/applications")
                .join(file),
            body,
        )
        .unwrap();
    }

    fn audit(&self) -> String {
        cli(&["audit", "-data", self.dir()])
    }

    fn launcher_log(&self) -> Option<String> {
        std::fs::read_to_string(self.desktop.path().join("launcher-args")).ok()
    }

    /// The app id the agent gives a desktop file (`app_` + sha256 of the versioned file name).
    fn id_of(&self, name: &str) -> String {
        use sha2::{Digest, Sha256};
        format!(
            "app_{}",
            hex::encode(Sha256::digest(
                format!("rfe-host-app-v1\0{name}").as_bytes()
            ))
        )
    }
}

impl Drop for AppAgent {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// A directory with a `gio` that writes its arguments to `$RFE_TEST_LAUNCH_LOG` and starts nothing.
fn fake_launcher() -> TempDir {
    use std::os::unix::fs::PermissionsExt;
    let bin = TempDir::new().unwrap();
    let gio = bin.path().join("gio");
    std::fs::write(
        &gio,
        "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RFE_TEST_LAUNCH_LOG\"\n",
    )
    .unwrap();
    std::fs::set_permissions(&gio, std::fs::Permissions::from_mode(0o700)).unwrap();
    bin
}

struct Session {
    dir: TempDir,
    store: Offloaded,
    saved: Saved,
}

/// Signs in with the account, as the app does. An account sign-in carries no app rights.
async fn sign_in(a: &AppAgent) -> Session {
    let fp = capture_fingerprint(&a.host).await.unwrap();
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let saved = flows::login(dir.path(), &a.host, &fp, "owner", PW, "Desktop", &store)
        .await
        .unwrap();
    Session { dir, store, saved }
}

/// What the owner does with the agent's device settings: set this device's app rights.
async fn grant(a: &AppAgent, s: &Session, view: bool, launch: bool) {
    let raw = Raw::new(&a.host);
    let resp = raw
        .http
        .patch(format!("{}/devices/{}", raw.base, s.saved.device_id))
        .bearer_auth(&s.saved.token)
        .json(&json!({"viewApps": view, "launchApps": launch}))
        .send()
        .await
        .unwrap();
    assert!(resp.status().is_success(), "grant: {}", resp.status());
}

fn server_error(e: &AgentError) -> (u16, &str) {
    match e {
        AgentError::Server { status, code, .. } => (*status, code),
        other => panic!("not a server error: {other:?}"),
    }
}

const HARMLESS: &str =
    "[Desktop Entry]\nType=Application\nName=Harmless\nExec=true\nCategories=Utility;\n";
const NO_COMMAND: &str = "[Desktop Entry]\nType=Application\nName=Display Only\n";

#[tokio::test]
async fn the_catalog_lists_the_hosts_apps_and_grants_decide_what_the_computer_may_do() {
    let a = AppAgent::start(&Env {
        display: false,
        launcher: None,
    });
    a.entry("harmless.desktop", HARMLESS);
    a.entry("display-only.desktop", NO_COMMAND);
    let s = sign_in(&a).await;
    let harmless = a.id_of("harmless.desktop");
    let mut refusals = vec![];

    // No rights: an account sign-in does not include them, for the list or for a launch.
    let e = apps::list(s.dir.path(), &s.store).await.unwrap_err();
    assert_eq!(server_error(&e), (403, "APP_VIEW_FORBIDDEN"), "{e:?}");
    refusals.push(apps::describe(&e));
    let e = apps::launch(s.dir.path(), &s.store, &harmless)
        .await
        .unwrap_err();
    assert_eq!(server_error(&e), (403, "APP_LAUNCH_FORBIDDEN"), "{e:?}");
    refusals.push(apps::describe(&e));

    // May look, may not launch: the list says so and a launch is still refused.
    grant(&a, &s, true, false).await;
    let catalog = apps::list(s.dir.path(), &s.store).await.unwrap();
    assert_eq!(catalog.platform, "linux");
    assert!(!catalog.launch_allowed);
    let names: Vec<_> = catalog.apps.iter().map(|x| x.name.as_str()).collect();
    assert_eq!(names, ["Display Only", "Harmless"], "{catalog:?}");
    let h = catalog.apps.iter().find(|x| x.name == "Harmless").unwrap();
    assert_eq!((h.id.as_str(), h.launchable), (harmless.as_str(), true));
    if common::modern("app categories") {
        assert_eq!(h.category, "Utilities");
    }
    assert!(
        !catalog.apps[0].launchable,
        "an entry with no command is listed but not launchable"
    );
    let e = apps::launch(s.dir.path(), &s.store, &harmless)
        .await
        .unwrap_err();
    assert_eq!(server_error(&e), (403, "APP_LAUNCH_FORBIDDEN"));

    // The list carries no path or command.
    let json = serde_json::to_string(&catalog).unwrap();
    assert!(
        !json.contains("user-data") && !json.contains("Exec") && !json.contains("true\""),
        "{json}"
    );

    // May launch. This agent claims no graphical session, so a real entry reaches that refusal.
    grant(&a, &s, true, true).await;
    let catalog = apps::list(s.dir.path(), &s.store).await.unwrap();
    assert!(catalog.launch_allowed);
    let e = apps::launch(s.dir.path(), &s.store, &harmless)
        .await
        .unwrap_err();
    assert_eq!(server_error(&e), (503, "NO_INTERACTIVE_SESSION"), "{e:?}");
    refusals.push(apps::describe(&e));

    // An entry the host cannot start, and one it does not have.
    let e = apps::launch(s.dir.path(), &s.store, &a.id_of("display-only.desktop"))
        .await
        .unwrap_err();
    assert_eq!(server_error(&e), (409, "APP_NOT_LAUNCHABLE"), "{e:?}");
    refusals.push(apps::describe(&e));
    let e = apps::launch(s.dir.path(), &s.store, &a.id_of("never-installed.desktop"))
        .await
        .unwrap_err();
    assert_eq!(server_error(&e), (404, "APP_NOT_FOUND"), "{e:?}");
    refusals.push(apps::describe(&e));

    // Each of those reads differently, and none of them is the agent's raw text.
    let distinct: BTreeSet<_> = refusals.iter().collect();
    assert_eq!(distinct.len(), refusals.len(), "{refusals:#?}");
    for text in &refusals {
        assert!(
            !text.contains("APP_") && !text.contains("NO_INTERACTIVE"),
            "{text}"
        );
    }
    assert!(
        a.launcher_log().is_none(),
        "nothing may have reached a launcher"
    );
}

#[tokio::test]
async fn a_launch_with_a_session_hands_only_the_registered_entry_to_the_launcher() {
    let bin = fake_launcher();
    let a = AppAgent::start(&Env {
        display: true,
        launcher: Some(bin.path().to_path_buf()),
    });
    a.entry("harmless.desktop", HARMLESS);
    let s = sign_in(&a).await;
    grant(&a, &s, true, true).await;

    let id = a.id_of("harmless.desktop");
    apps::launch(s.dir.path(), &s.store, &id).await.unwrap();
    let log = a.launcher_log().expect("the launcher was called");
    let lines: Vec<_> = log.lines().collect();
    assert_eq!(lines.len(), 2, "{log}");
    assert_eq!(lines[0], "launch");
    assert!(
        lines[1].ends_with("/user-data/applications/harmless.desktop"),
        "{log}"
    );
    assert!(
        a.audit().contains("outcome=started"),
        "the agent audits the launch"
    );
}

#[tokio::test]
async fn a_host_without_its_launcher_is_told_apart_from_one_without_a_session() {
    let a = AppAgent::start(&Env {
        display: true,
        launcher: None,
    });
    a.entry("harmless.desktop", HARMLESS);
    let s = sign_in(&a).await;
    grant(&a, &s, true, true).await;
    let e = apps::launch(s.dir.path(), &s.store, &a.id_of("harmless.desktop"))
        .await
        .unwrap_err();
    assert_eq!(server_error(&e), (503, "APP_LAUNCH_UNAVAILABLE"), "{e:?}");
    assert!(apps::describe(&e).contains("gio"), "{}", apps::describe(&e));
    assert_ne!(
        apps::describe(&e),
        message_for("NO_INTERACTIVE_SESSION"),
        "503 for two different reasons reads the same"
    );
}

#[tokio::test]
async fn a_host_with_no_apps_gives_an_empty_catalog_not_an_error() {
    let a = AppAgent::start(&Env {
        display: false,
        launcher: None,
    });
    let s = sign_in(&a).await;
    grant(&a, &s, true, false).await;
    let catalog = apps::list(s.dir.path(), &s.store).await.unwrap();
    assert!(catalog.apps.is_empty());
    assert!(!catalog.launch_allowed);
}

#[tokio::test]
async fn an_id_that_is_not_a_catalog_id_never_reaches_the_network() {
    // No agent and no login at all: if the id were not checked first, this would say "not signed
    // in" (or try to connect). It must refuse the id itself.
    let dir = TempDir::new().unwrap();
    let store = Offloaded::new(MemoryStore::default());
    let good = format!("app_{}", "ab12".repeat(16));
    for bad in [
        "",
        "app_",
        "../../v1/devices",
        &format!("{good}/../../devices"),
        &format!("{good}?x=1"),
        &format!("{good}#x"),
        &format!("{good}\n"),
        &good[..67],
        &format!("{good}0"),
        &good.to_uppercase(),
        &good.replace("app_", "App_"),
        &format!("app_{}", "g".repeat(64)),
        &format!("app_{}", "é".repeat(32)),
        "app_%2e%2e%2f",
    ] {
        let e = apps::launch(dir.path(), &store, bad).await.unwrap_err();
        assert!(
            e.to_string().starts_with("unexpected app id"),
            "{bad:?} was not refused as an id: {e}"
        );
        assert!(apps::validate_app_id(bad).is_err(), "{bad:?}");
    }
    assert!(apps::validate_app_id(&good).is_ok());
    // A well-formed id with no login is refused for the login, which proves the control above.
    let e = apps::launch(dir.path(), &store, &good).await.unwrap_err();
    assert_eq!(e.to_string(), "not signed in");
}

#[tokio::test]
async fn a_revoked_login_ends_the_session_and_says_so() {
    let a = AppAgent::start(&Env {
        display: false,
        launcher: None,
    });
    let s = sign_in(&a).await;
    cli(&["revoke", "-data", a.dir(), &s.saved.device_id]);
    let e = apps::list(s.dir.path(), &s.store).await.unwrap_err();
    assert_eq!(server_error(&e), (401, "UNAUTHORIZED"), "{e:?}");
    assert!(
        apps::describe(&e).contains("Sign in again"),
        "{}",
        apps::describe(&e)
    );
    let d = s.dir.path().to_path_buf();
    let after = s
        .store
        .run(move |st| flows::load_saved(&d, st))
        .await
        .unwrap();
    assert!(after.token.is_empty(), "the dead token was not dropped");
    assert!(!after.host.is_empty(), "the pin and the agent stay");
}

fn message_for(code: &str) -> &'static str {
    APP_MESSAGES
        .iter()
        .find(|(c, _)| *c == code)
        .unwrap_or_else(|| panic!("{code} has no message"))
        .1
}

#[test]
fn every_app_code_has_its_own_plain_message_and_other_errors_keep_their_wording() {
    let mut seen = BTreeSet::new();
    for (code, text) in APP_MESSAGES {
        assert!(text.len() > 30, "{code}: too terse: {text}");
        assert!(!text.contains(code), "{code}: shows the code, not words");
        assert!(seen.insert(*text), "{code} shares its message");
        let e = AgentError::Server {
            status: 400,
            code: (*code).into(),
            message: "raw agent text".into(),
        };
        assert_eq!(apps::describe(&e), *text);
    }
    // Not an app code: the rest of the app's wording applies.
    let e = AgentError::Server {
        status: 401,
        code: "UNAUTHORIZED".into(),
        message: "x".into(),
    };
    assert_eq!(apps::describe(&e), e.to_string());
    let e = AgentError::Network("connection refused".into());
    assert_eq!(apps::describe(&e), e.to_string());
    // An agent from before the catalog answers a bare 404.
    let old = AgentError::Server {
        status: 404,
        code: "HTTP_404".into(),
        message: "the agent returned an unexpected error".into(),
    };
    assert_eq!(apps::describe(&old), NO_CATALOG_MESSAGE);
    assert!(!seen.contains(NO_CATALOG_MESSAGE));
}

/// The guide explains every message, with the exact wording and its code.
#[test]
fn the_user_guide_explains_every_app_message() {
    let path = Path::new(env!("CARGO_MANIFEST_DIR")).join("../docs/user-guide.md");
    let guide = std::fs::read_to_string(path).unwrap();
    for (code, text) in APP_MESSAGES {
        assert!(
            guide.contains(&format!("| {text} | `{code}` |")),
            "user-guide.md has no row for {code} with the text {text:?}"
        );
    }
    for stem in [
        NO_CATALOG_MESSAGE,
        "unexpected app id",
        "unknown launch status",
        "Asked the PC to open",
        "Launching not allowed",
        "Cannot be launched",
    ] {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
    }
}
