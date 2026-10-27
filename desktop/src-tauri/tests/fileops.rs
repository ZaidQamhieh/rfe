//! The file tools against a throwaway agent whose only root is a folder this test fills: copy and
//! move (and the agent refusing to overwrite), compress and extract, checksums, permission modes,
//! share links, the trash and restore, the text editor's stale-write guard, search, and what each
//! refuses before it asks the agent anything.
//!
//! The agent's trash is pointed into a temporary folder (`XDG_DATA_HOME`), so nothing here can reach
//! the real desktop trash.

mod common;

use common::{agent_bin, cli, free_port, Raw};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentError};
use rfe_desktop_lib::fileops::{self, SearchOpts};
use rfe_desktop_lib::files;
use rfe_desktop_lib::flows;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded};
use std::net::TcpStream;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};
use tempfile::TempDir;

struct FileAgent {
    child: Child,
    data: TempDir,
    /// The one folder the agent allows. Empty string in `roots` means "no folder limit".
    root: TempDir,
    /// Holds the agent's trash for the life of the agent.
    _xdg: TempDir,
    host: String,
}

impl FileAgent {
    fn start() -> Self {
        Self::start_with(false, true)
    }

    fn start_with(read_only: bool, limit_to_root: bool) -> Self {
        let (data, root, xdg) = (
            TempDir::new().unwrap(),
            TempDir::new().unwrap(),
            TempDir::new().unwrap(),
        );
        let host = format!("127.0.0.1:{}", free_port());
        let mut cmd = Command::new(agent_bin());
        cmd.args(["-addr", &host, "-name", "rfe-fileops-test"])
            .args(["-data", data.path().to_str().unwrap()])
            .args([
                "-roots",
                if limit_to_root {
                    root.path().to_str().unwrap()
                } else {
                    ""
                },
            ]);
        if read_only {
            cmd.arg("-read-only");
        }
        let child = cmd
            // No notify-send, no session bus, and a trash that lives in a temporary folder.
            .env("PATH", "/nonexistent-rfe-test-path")
            .env_remove("DBUS_SESSION_BUS_ADDRESS")
            .env("XDG_DATA_HOME", xdg.path())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .expect("start rfe");
        let deadline = Instant::now() + Duration::from_secs(20);
        while TcpStream::connect(&host).is_err() {
            assert!(Instant::now() < deadline, "agent did not start on {host}");
            std::thread::sleep(Duration::from_millis(100));
        }
        FileAgent {
            child,
            data,
            root,
            _xdg: xdg,
            host,
        }
    }

    fn root(&self) -> String {
        self.root.path().to_str().unwrap().to_string()
    }

    fn at(&self, rel: &str) -> String {
        format!("{}/{rel}", self.root())
    }

    fn data_dir(&self) -> &str {
        self.data.path().to_str().unwrap()
    }

    /// Signs in as the owner (an account: every file permission). Returns the app's state folder.
    async fn owner(&self, store: &Offloaded) -> TempDir {
        self.owner_saved(store).await.0
    }

    /// Like `owner`, and the login the agent gave: its token is an admin's.
    async fn owner_saved(&self, store: &Offloaded) -> (TempDir, flows::Saved) {
        cli(&[
            "adduser",
            "-password",
            "pw-for-fileops-test",
            "-data",
            self.data_dir(),
            "owner",
        ]);
        let fp = capture_fingerprint(&self.host).await.unwrap();
        let dir = TempDir::new().unwrap();
        let saved = flows::login(
            dir.path(),
            &self.host,
            &fp,
            "owner",
            "pw-for-fileops-test",
            "Fileops test",
            store,
        )
        .await
        .unwrap();
        (dir, saved)
    }
}

impl Drop for FileAgent {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn memory() -> Offloaded {
    Offloaded::new(MemoryStore::default())
}

fn write(path: impl AsRef<Path>, text: &str) {
    let path = path.as_ref();
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, text).unwrap();
}

fn refusal(e: &AgentError) -> (u16, &str, &str) {
    match e {
        AgentError::Server {
            status,
            code,
            message,
        } => (*status, code, message),
        other => panic!("not a refusal from the agent: {other:?}"),
    }
}

fn local_message(e: &AgentError) -> String {
    match e {
        AgentError::Local(m) => m.clone(),
        other => panic!("not a local refusal: {other:?}"),
    }
}

#[tokio::test]
async fn copy_and_move_keep_or_remove_the_source_and_never_overwrite_by_default() {
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    write(a.at("src/a.txt"), "alpha");
    write(a.at("src/b.txt"), "beta");
    std::fs::create_dir(a.at("dst")).unwrap();
    write(a.at("dst/a.txt"), "already here");

    // A copy leaves the source, and a name that exists is refused (nothing is replaced silently).
    let one = [a.at("src/b.txt")];
    fileops::transfer_op(app.path(), &store, false, &one, &a.at("dst"), false, false)
        .await
        .unwrap();
    assert_eq!(std::fs::read_to_string(a.at("dst/b.txt")).unwrap(), "beta");
    assert!(a.root.path().join("src/b.txt").exists());
    let clash = [a.at("src/a.txt")];
    let err = fileops::transfer_op(
        app.path(),
        &store,
        false,
        &clash,
        &a.at("dst"),
        false,
        false,
    )
    .await
    .unwrap_err();
    assert!(matches!(err, AgentError::Server { .. }), "{err:?}");
    assert_eq!(
        std::fs::read_to_string(a.at("dst/a.txt")).unwrap(),
        "already here"
    );

    // Duplicate keeps both; overwrite replaces.
    fileops::transfer_op(app.path(), &store, false, &clash, &a.at("dst"), true, false)
        .await
        .unwrap();
    assert_eq!(std::fs::read_dir(a.at("dst")).unwrap().count(), 3);
    fileops::transfer_op(app.path(), &store, false, &clash, &a.at("dst"), false, true)
        .await
        .unwrap();
    assert_eq!(std::fs::read_to_string(a.at("dst/a.txt")).unwrap(), "alpha");

    // A move takes the source away.
    fileops::transfer_op(
        app.path(),
        &store,
        true,
        &one,
        &a.at("dst/../moved"),
        false,
        false,
    )
    .await
    .ok();
    std::fs::create_dir(a.at("moved")).unwrap();
    fileops::transfer_op(app.path(), &store, true, &one, &a.at("moved"), false, false)
        .await
        .unwrap();
    assert!(!a.root.path().join("src/b.txt").exists());
    assert!(a.root.path().join("moved/b.txt").exists());
}

#[tokio::test]
async fn compress_and_extract_round_trip_and_the_archive_can_be_listed() {
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    write(a.at("pack/one.txt"), "one");
    write(a.at("pack/sub/two.txt"), "two");

    let zip = a.at("pack.zip");
    fileops::compress(app.path(), &store, &[a.at("pack")], &zip)
        .await
        .unwrap();
    assert!(a.root.path().join("pack.zip").is_file());
    let list = fileops::archive_list(app.path(), &store, &zip, None)
        .await
        .unwrap();
    let paths: Vec<&str> = list.iter().map(|e| e.path.as_str()).collect();
    assert!(
        paths.iter().any(|p| p.ends_with("one.txt"))
            && paths.iter().any(|p| p.ends_with("two.txt")),
        "{paths:?}"
    );

    std::fs::create_dir(a.at("out")).unwrap();
    fileops::extract(app.path(), &store, &zip, &a.at("out"))
        .await
        .unwrap();
    let mut found = Vec::new();
    for e in walk(&a.root.path().join("out")) {
        found.push(e.file_name().unwrap().to_string_lossy().to_string());
    }
    found.sort();
    assert_eq!(found, ["one.txt", "two.txt"], "{found:?}");
}

fn walk(dir: &Path) -> Vec<PathBuf> {
    let mut out = Vec::new();
    for e in std::fs::read_dir(dir).unwrap() {
        let p = e.unwrap().path();
        if p.is_dir() {
            out.extend(walk(&p));
        } else {
            out.push(p);
        }
    }
    out
}

#[tokio::test]
async fn a_checksum_matches_the_file_and_an_unknown_algorithm_is_refused_locally() {
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    write(a.at("hash.txt"), "hello");

    let sum = fileops::checksum(app.path(), &store, &a.at("hash.txt"), "sha256")
        .await
        .unwrap();
    assert_eq!(
        sum.checksum,
        "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
    );
    assert_eq!(sum.algorithm, "sha256");
    let md5 = fileops::checksum(app.path(), &store, &a.at("hash.txt"), "md5")
        .await
        .unwrap();
    assert_eq!(md5.checksum, "5d41402abc4b2a76b9719d911017c592");

    let err = fileops::checksum(app.path(), &store, &a.at("hash.txt"), "crc32")
        .await
        .unwrap_err();
    assert_eq!(
        local_message(&err),
        "the checksum must be sha256, sha1 or md5"
    );
}

#[tokio::test]
async fn chmod_changes_the_mode_and_a_bad_mode_never_reaches_the_agent() {
    use std::os::unix::fs::PermissionsExt;
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    write(a.at("m.txt"), "m");

    fileops::chmod(app.path(), &store, &a.at("m.txt"), "600")
        .await
        .unwrap();
    let mode = std::fs::metadata(a.at("m.txt"))
        .unwrap()
        .permissions()
        .mode()
        & 0o777;
    assert_eq!(mode, 0o600);
    fileops::chmod(app.path(), &store, &a.at("m.txt"), "0644")
        .await
        .unwrap();
    let mode = std::fs::metadata(a.at("m.txt"))
        .unwrap()
        .permissions()
        .mode()
        & 0o777;
    assert_eq!(mode, 0o644);

    for bad in ["", "6", "99", "12345", "rwx", "0o755"] {
        let err = fileops::chmod(app.path(), &store, &a.at("m.txt"), bad)
            .await
            .unwrap_err();
        assert_eq!(
            local_message(&err),
            "a permission mode is 3 or 4 octal digits, for example 0755",
            "{bad:?}"
        );
    }
}

#[tokio::test]
async fn share_links_are_off_by_default_and_the_agents_refusal_is_shown_as_it_worded_it() {
    // The owner switches sharing on in the agent's own settings; it starts off, and no command line
    // option turns it on, so this test cannot mint one. It checks what the window can: the lifetime is
    // checked before anything is sent, the agent's refusal comes through in its words, the list is
    // empty, and a link id that is not a hash never reaches the agent.
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    write(a.at("shared.txt"), "share me");

    for secs in [0, 59, 24 * 3600 + 1] {
        let err = fileops::mint_share(app.path(), &store, &a.at("shared.txt"), secs)
            .await
            .unwrap_err();
        assert_eq!(
            local_message(&err),
            "a share link lasts from 1 minute to 24 hours",
            "{secs}"
        );
    }
    let err = fileops::mint_share(app.path(), &store, &a.at("shared.txt"), 3600)
        .await
        .unwrap_err();
    let (status, _, message) = refusal(&err);
    assert_eq!(
        (status, message),
        (403, "share links are disabled on this agent")
    );
    assert!(fileops::share_list(app.path(), &store)
        .await
        .unwrap()
        .is_empty());

    let bad = fileops::share_revoke(app.path(), &store, "not-a-hash")
        .await
        .unwrap_err();
    assert_eq!(local_message(&bad), "that is not a share link id");
}

#[tokio::test]
async fn a_share_link_is_made_listed_fetched_once_and_revoked_when_the_owner_allows_sharing() {
    let a = FileAgent::start();
    let store = memory();
    let (app, saved) = a.owner_saved(&store).await;
    write(a.at("shared.txt"), "share me");
    // Only the agent's owner can switch sharing on (the app has no control for it).
    let raw = Raw::new(&a.host);
    let on = raw
        .http
        .patch(format!("{}/settings", raw.base))
        .bearer_auth(&saved.token)
        .json(&serde_json::json!({"allowSharing": true}))
        .send()
        .await
        .unwrap();
    assert!(on.status().is_success(), "allow sharing: {}", on.status());

    let link = fileops::mint_share(app.path(), &store, &a.at("shared.txt"), 3600)
        .await
        .unwrap();
    assert!(!link.token.is_empty() && !link.token_hash.is_empty() && !link.url.is_empty());
    let listed = fileops::share_list(app.path(), &store).await.unwrap();
    assert_eq!(listed.len(), 1);
    assert_eq!(listed[0].token_hash, link.token_hash);
    assert_eq!(listed[0].path, a.at("shared.txt"));

    // The link works without a login, and only once.
    let get = || {
        raw.http
            .get(format!("{}/share/{}", raw.base, link.token))
            .send()
    };
    let first = get().await.unwrap();
    assert!(first.status().is_success(), "fetch: {}", first.status());
    assert_eq!(first.text().await.unwrap(), "share me");
    assert!(
        !get().await.unwrap().status().is_success(),
        "a link is single-use"
    );

    // A fresh link can be turned off before anyone uses it.
    let second = fileops::mint_share(app.path(), &store, &a.at("shared.txt"), 3600)
        .await
        .unwrap();
    fileops::share_revoke(app.path(), &store, &second.token_hash)
        .await
        .unwrap();
    let gone = raw
        .http
        .get(format!("{}/share/{}", raw.base, second.token))
        .send()
        .await
        .unwrap();
    assert!(
        !gone.status().is_success(),
        "a revoked link no longer works"
    );
    assert!(fileops::share_list(app.path(), &store)
        .await
        .unwrap()
        .iter()
        .all(|s| s.token_hash != second.token_hash));
}

#[tokio::test]
async fn a_trashed_file_is_listed_and_restored_to_where_it_was_or_discarded_for_good() {
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    write(a.at("keep.txt"), "keep");
    write(a.at("drop.txt"), "drop");

    files::trash(app.path(), &store, &a.at("keep.txt"))
        .await
        .unwrap();
    files::trash(app.path(), &store, &a.at("drop.txt"))
        .await
        .unwrap();
    assert!(!a.root.path().join("keep.txt").exists());
    let items = fileops::trash_list(app.path(), &store).await.unwrap();
    let keep = items
        .iter()
        .find(|i| i.name == "keep.txt")
        .expect("keep.txt in the trash");
    let drop = items
        .iter()
        .find(|i| i.name == "drop.txt")
        .expect("drop.txt in the trash");
    assert_eq!(keep.original_path, a.at("keep.txt"));

    fileops::trash_restore(app.path(), &store, std::slice::from_ref(&keep.id))
        .await
        .unwrap();
    assert_eq!(std::fs::read_to_string(a.at("keep.txt")).unwrap(), "keep");

    fileops::trash_empty(app.path(), &store, std::slice::from_ref(&drop.id))
        .await
        .unwrap();
    assert!(!a.root.path().join("drop.txt").exists());
    let items = fileops::trash_list(app.path(), &store).await.unwrap();
    assert!(
        items
            .iter()
            .all(|i| i.name != "drop.txt" && i.name != "keep.txt"),
        "{items:?}"
    );
}

#[tokio::test]
async fn the_text_editor_refuses_to_save_over_a_file_that_changed_meanwhile() {
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    write(a.at("note.txt"), "first");

    let opened = fileops::read_text(app.path(), &store, &a.at("note.txt"))
        .await
        .unwrap();
    assert_eq!(opened.text, "first");
    // Someone else changes it after it was opened.
    std::thread::sleep(Duration::from_millis(1100));
    write(a.at("note.txt"), "second, by someone else");
    let err = fileops::write_text(
        app.path(),
        &store,
        &a.at("note.txt"),
        "mine",
        &opened.modified,
    )
    .await
    .unwrap_err();
    assert!(matches!(err, AgentError::Server { .. }), "{err:?}");
    assert_eq!(
        std::fs::read_to_string(a.at("note.txt")).unwrap(),
        "second, by someone else"
    );

    // Saved from what was just read, it goes through.
    let fresh = fileops::read_text(app.path(), &store, &a.at("note.txt"))
        .await
        .unwrap();
    fileops::write_text(
        app.path(),
        &store,
        &a.at("note.txt"),
        "mine",
        &fresh.modified,
    )
    .await
    .unwrap();
    assert_eq!(std::fs::read_to_string(a.at("note.txt")).unwrap(), "mine");

    // A file with a NUL byte is not text.
    let err = fileops::write_text(app.path(), &store, &a.at("note.txt"), "a\0b", "")
        .await
        .unwrap_err();
    assert_eq!(
        local_message(&err),
        "this file is not text, or is too large to open here"
    );
}

#[tokio::test]
async fn bad_input_is_refused_before_the_agent_is_asked() {
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    let root = a.root();
    let none = |q: &str| SearchOpts {
        query: q.into(),
        root: Some(root.clone()),
        limit: None,
        types: vec![],
        ext: None,
        min_size: None,
        max_size: None,
    };

    for q in ["".to_string(), "x".repeat(300)] {
        let err = fileops::search(app.path(), &store, &none(&q))
            .await
            .unwrap_err();
        assert_eq!(local_message(&err), "a search needs 1 to 256 characters");
    }
    let mut o = none("a");
    o.types = vec!["spreadsheet".into()];
    assert_eq!(
        local_message(&fileops::search(app.path(), &store, &o).await.unwrap_err()),
        "unknown file category"
    );

    let err = fileops::transfer_op(app.path(), &store, false, &[], &root, false, false)
        .await
        .unwrap_err();
    assert_eq!(local_message(&err), "choose between 1 and 1000 items");
    let many: Vec<String> = (0..1001).map(|i| format!("{root}/f{i}")).collect();
    let err = fileops::compress(app.path(), &store, &many, &format!("{root}/x.zip"))
        .await
        .unwrap_err();
    assert_eq!(local_message(&err), "choose between 1 and 1000 items");

    let err = fileops::chmod(app.path(), &store, "relative/path", "644")
        .await
        .unwrap_err();
    assert!(local_message(&err).contains("absolute"), "{err:?}");
    let err = fileops::wake(app.path(), &store, "not-a-mac")
        .await
        .unwrap_err();
    assert_eq!(
        local_message(&err),
        "a MAC address looks like aa:bb:cc:dd:ee:ff"
    );
}

#[tokio::test]
async fn a_file_outside_the_agents_folder_is_refused_by_the_agent() {
    let a = FileAgent::start();
    let store = memory();
    let app = a.owner(&store).await;
    let outside = TempDir::new().unwrap();
    write(outside.path().join("secret.txt"), "secret");
    let target = outside.path().join("secret.txt");
    let err = fileops::checksum(app.path(), &store, target.to_str().unwrap(), "sha256")
        .await
        .unwrap_err();
    let (status, _, _) = refusal(&err);
    assert!(status == 403 || status == 404, "{err:?}");
}
