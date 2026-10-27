//! The file browser against a throwaway agent whose only root is a folder this test fills: nested
//! folders, a symlink that leaves the root, unicode and awkward names, a folder big enough to need
//! pages, and the changes (new folder, rename, trash) with and without permission.
//!
//! The agent's trash is pointed into a temporary folder (`XDG_DATA_HOME`), so nothing here can reach
//! the real desktop trash.

mod common;

#[path = "support/ui.rs"]
mod ui;
use common::{agent_bin, cli, free_port};
use rfe_desktop_lib::agent_client::{capture_fingerprint, AgentError};
use rfe_desktop_lib::files::{self, FileEntry};
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
    /// A real folder next to the root: what a symlink in the root points at.
    outside: TempDir,
    xdg: TempDir,
    host: String,
}

impl FileAgent {
    fn start() -> Self {
        Self::start_with(false, true)
    }

    fn start_with(read_only: bool, limit_to_root: bool) -> Self {
        let (data, root, outside, xdg) = (
            TempDir::new().unwrap(),
            TempDir::new().unwrap(),
            TempDir::new().unwrap(),
            TempDir::new().unwrap(),
        );
        let host = format!("127.0.0.1:{}", free_port());
        let mut cmd = Command::new(agent_bin());
        cmd.args(["-addr", &host, "-name", "rfe-files-test"])
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
            outside,
            xdg,
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

    fn trash_files(&self) -> PathBuf {
        self.xdg.path().join("Trash").join("files")
    }

    /// Signs in as the owner (an account: every file permission). Returns the app's state folder.
    async fn owner(&self, store: &Offloaded) -> TempDir {
        cli(&[
            "adduser",
            "-password",
            "pw-for-files-test",
            "-data",
            self.data_dir(),
            "owner",
        ]);
        let fp = capture_fingerprint(&self.host).await.unwrap();
        let dir = TempDir::new().unwrap();
        flows::login(
            dir.path(),
            &self.host,
            &fp,
            "owner",
            "pw-for-files-test",
            "Files test",
            store,
        )
        .await
        .unwrap();
        dir
    }

    /// Pairs with a one-time code: an ordinary device, which starts with browsing only.
    async fn ordinary(&self, store: &Offloaded) -> TempDir {
        let out = cli(&["pair", "-data", self.data_dir()]);
        let code = out
            .lines()
            .next()
            .and_then(|l| l.strip_prefix("Pairing code:"))
            .and_then(|l| l.split_whitespace().next())
            .expect("a pairing code")
            .to_string();
        let fp = capture_fingerprint(&self.host).await.unwrap();
        let dir = TempDir::new().unwrap();
        flows::pair(
            dir.path(),
            &self.host,
            &fp,
            &code,
            "Files test (code)",
            store,
        )
        .await
        .unwrap();
        dir
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

fn names(entries: &[FileEntry]) -> Vec<&str> {
    entries.iter().map(|e| e.name.as_str()).collect()
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

#[tokio::test]
async fn lists_nested_folders_unicode_names_and_shows_a_symlink_as_a_symlink() {
    let a = FileAgent::start();
    let (store, root) = (memory(), a.root());
    let app = a.owner(&store).await;

    write(a.at("docs/deep/er/leaf.txt"), "leaf");
    write(a.at("docs/readme.md"), "# hi");
    write(a.at("ünï/日本語 file.txt"), "u");
    write(a.at("a+b c.txt"), "plus");
    write(a.at("100%.txt"), "pct");
    write(a.at("semi;colon#hash?q=1&r=2.txt"), "odd");
    write(a.at("emoji \u{1F600}.txt"), "emoji");
    std::fs::create_dir(a.at("empty")).unwrap();
    write(a.outside.path().join("secret.txt"), "outside secret");
    std::os::unix::fs::symlink(a.outside.path(), a.at("escape")).unwrap();
    std::os::unix::fs::symlink(a.at("docs"), a.at("inside")).unwrap();

    let r = files::roots(app.path(), &store).await.unwrap();
    assert_eq!(r.source, "roots");
    assert_eq!(r.locations.len(), 1, "{r:?}");
    assert_eq!(r.locations[0].path, root);
    assert!(!r.read_only && !r.access_denied);
    if common::modern("the agent reports permissions") {
        let caps = r.caps.expect("an up-to-date agent reports permissions");
        assert!(caps.browse && caps.modify && caps.delete, "{caps:?}");
    }

    let top = files::list(app.path(), &store, &root, None, None)
        .await
        .unwrap();
    assert_eq!(top.path, root);
    assert!(top.next_cursor.is_none());
    let got = names(&top.entries);
    for want in [
        "docs",
        "ünï",
        "a+b c.txt",
        "100%.txt",
        "semi;colon#hash?q=1&r=2.txt",
        "emoji \u{1F600}.txt",
        "empty",
        "escape",
        "inside",
    ] {
        assert!(got.contains(&want), "{want:?} missing from {got:?}");
    }
    // Sorted by name, as the agent sends it.
    let mut sorted = got.clone();
    sorted.sort_unstable();
    assert_eq!(got, sorted);

    let docs = top.entries.iter().find(|e| e.name == "docs").unwrap();
    assert!(docs.is_dir && !docs.is_symlink, "{docs:?}");
    if common::modern("folder child counts") {
        assert_eq!(docs.child_count, Some(2), "{docs:?}");
    }
    let esc = top.entries.iter().find(|e| e.name == "escape").unwrap();
    assert!(esc.is_symlink, "{esc:?}");
    assert_eq!(esc.symlink_target, a.outside.path().to_str().unwrap());
    let inside = top.entries.iter().find(|e| e.name == "inside").unwrap();
    // The agent does not follow a symlink to learn whether it is a folder, so the listing says
    // "symlink" and leaves `isDir` false; `meta` resolves it, and says folder.
    assert!(inside.is_symlink && !inside.is_dir, "{inside:?}");
    let resolved = files::meta(app.path(), &store, &a.at("inside"))
        .await
        .unwrap();
    assert!(resolved.is_dir && !resolved.is_symlink, "{resolved:?}");
    assert_eq!(resolved.path, a.at("docs"));

    // Nested folders and the trail to them.
    let er = files::list(app.path(), &store, &a.at("docs/deep/er"), None, None)
        .await
        .unwrap();
    assert_eq!(names(&er.entries), ["leaf.txt"]);
    assert_eq!(er.entries[0].size, 4);
    assert_eq!(er.entries[0].path, a.at("docs/deep/er/leaf.txt"));
    let labels: Vec<&str> = er.crumbs.iter().map(|c| c.label.as_str()).collect();
    assert_eq!(&labels[labels.len() - 3..], ["docs", "deep", "er"]);
    assert_eq!(er.crumbs.last().unwrap().path, er.path);

    // Unicode and awkward names survive the trip into the URL and back.
    let u = files::list(app.path(), &store, &a.at("ünï"), None, None)
        .await
        .unwrap();
    assert_eq!(names(&u.entries), ["日本語 file.txt"]);
    let m = files::meta(app.path(), &store, &a.at("semi;colon#hash?q=1&r=2.txt"))
        .await
        .unwrap();
    assert_eq!(m.size, 3);
    let m = files::meta(app.path(), &store, &a.at("a+b c.txt"))
        .await
        .unwrap();
    assert_eq!(m.size, 4);

    // A symlink to a folder inside the root is followed; the one that leaves the root is refused,
    // by the agent, in its own words, and nothing from outside is shown.
    let via = files::list(app.path(), &store, &a.at("inside"), None, None)
        .await
        .unwrap();
    assert!(names(&via.entries).contains(&"readme.md"), "{via:?}");

    for e in [
        files::list(app.path(), &store, &a.at("escape"), None, None)
            .await
            .unwrap_err(),
        files::list(app.path(), &store, &a.at("escape/secret.txt"), None, None)
            .await
            .unwrap_err(),
        files::meta(app.path(), &store, &a.at("escape"))
            .await
            .unwrap_err(),
        files::meta(app.path(), &store, &a.at("escape/secret.txt"))
            .await
            .unwrap_err(),
    ] {
        let (status, code, message) = refusal(&e);
        assert_eq!((status, code), (403, "FORBIDDEN"), "{e:?}");
        assert!(message.contains("outside allowed root"), "{message}");
        let shown = files::user_message(&e);
        assert!(shown.starts_with(files::AGENT_SAYS), "{shown}");
        assert!(shown.contains("(FORBIDDEN)"), "{shown}");
        assert!(!shown.contains("outside secret"), "{shown}");
    }

    // Outside the root altogether, by an absolute path.
    let e = files::list(
        app.path(),
        &store,
        a.outside.path().to_str().unwrap(),
        None,
        None,
    )
    .await
    .unwrap_err();
    assert_eq!(refusal(&e).1, "FORBIDDEN", "{e:?}");

    // `..` and relative paths never leave the app.
    for bad in [
        format!(
            "{root}/../{}",
            a.outside.path().file_name().unwrap().to_str().unwrap()
        ),
        format!("{root}/docs/../../etc"),
        "docs".to_string(),
        "".to_string(),
        format!("{root}/x\0y"),
    ] {
        let e = files::list(app.path(), &store, &bad, None, None)
            .await
            .unwrap_err();
        assert!(matches!(e, AgentError::Local(_)), "{bad:?}: {e:?}");
    }

    // A path that does not exist is the agent's "not found".
    let e = files::list(app.path(), &store, &a.at("nope"), None, None)
        .await
        .unwrap_err();
    assert_eq!(refusal(&e).0, 404, "{e:?}");
    // A file is not a folder: whatever the agent says, it is an error and not a listing.
    assert!(
        files::list(app.path(), &store, &a.at("a+b c.txt"), None, None)
            .await
            .is_err()
    );
}

#[tokio::test]
async fn a_large_folder_comes_in_pages_that_join_up_exactly() {
    let a = FileAgent::start();
    let (store, root) = (memory(), a.root());
    let app = a.owner(&store).await;

    let big = a.at("big");
    std::fs::create_dir(&big).unwrap();
    let mut want = Vec::new();
    for i in 0..1234 {
        let name = format!("f{i:05}.txt");
        std::fs::write(format!("{big}/{name}"), "").unwrap();
        want.push(name);
    }
    want.sort();

    // Default page, then follow the cursor to the end.
    let mut got = Vec::new();
    let mut cursor: Option<String> = None;
    let mut pages = 0;
    loop {
        let p = files::list(app.path(), &store, &big, cursor.as_deref(), Some(500))
            .await
            .unwrap();
        pages += 1;
        assert!(p.entries.len() <= 500);
        got.extend(p.entries.iter().map(|e| e.name.clone()));
        match p.next_cursor {
            Some(c) => cursor = Some(c),
            None => break,
        }
    }
    assert_eq!(pages, 3, "1234 entries at 500 a page");
    assert_eq!(got, want, "every entry once, in order");

    // The folder that holds it counts at most 1000.
    let top = files::list(app.path(), &store, &root, None, None)
        .await
        .unwrap();
    let b = top.entries.iter().find(|e| e.name == "big").unwrap();
    if common::modern("folder child counts") {
        assert_eq!(b.child_count, Some(1000));
    }

    // Asking for more than the agent allows is clamped, not refused; zero is a page of one.
    let p = files::list(app.path(), &store, &big, None, Some(50_000))
        .await
        .unwrap();
    assert_eq!(p.entries.len(), 1000);
    assert!(p.next_cursor.is_some());
    let p = files::list(app.path(), &store, &big, None, Some(0))
        .await
        .unwrap();
    assert_eq!(p.entries.len(), 1);
}

#[tokio::test]
async fn a_cursor_on_an_awkward_name_resumes_in_the_right_place() {
    let a = FileAgent::start();
    let (store, root) = (memory(), a.root());
    let app = a.owner(&store).await;
    let dir = a.at("tricky");
    std::fs::create_dir(&dir).unwrap();
    let all = [
        "a&b=c+d",
        "b c%20d",
        "c#frag?q",
        "d;e",
        "ü",
        "日本語",
        "\u{1F600} smile",
    ];
    for n in all {
        std::fs::write(format!("{dir}/{n}"), "").unwrap();
    }
    let mut want: Vec<&str> = all.to_vec();
    want.sort_unstable();
    let mut got: Vec<String> = Vec::new();
    let mut cursor: Option<String> = None;
    loop {
        let p = files::list(app.path(), &store, &dir, cursor.as_deref(), Some(2))
            .await
            .unwrap();
        got.extend(p.entries.iter().map(|e| e.name.clone()));
        match p.next_cursor {
            Some(c) => cursor = Some(c),
            None => break,
        }
    }
    assert_eq!(got, want);
    let _ = root;
}

#[tokio::test]
async fn new_folder_rename_and_trash_work_for_an_owner_and_never_leave_the_root() {
    let a = FileAgent::start();
    let (store, root) = (memory(), a.root());
    let app = a.owner(&store).await;
    write(a.outside.path().join("secret.txt"), "outside");
    std::os::unix::fs::symlink(a.outside.path(), a.at("escape")).unwrap();
    write(a.at("keep.txt"), "keep me");

    // New folder, with a unicode name, then the same name again.
    let made = files::create_folder(app.path(), &store, &root, "neu ünï")
        .await
        .unwrap();
    assert!(made.is_dir);
    assert_eq!(made.path, a.at("neu ünï"));
    assert!(Path::new(&a.at("neu ünï")).is_dir());
    let e = files::create_folder(app.path(), &store, &root, "neu ünï")
        .await
        .unwrap_err();
    assert_eq!(refusal(&e).1, "CONFLICT", "{e:?}");
    assert!(files::user_message(&e).contains("already exists"), "{e:?}");

    // Rename it, and onto a name that exists.
    let moved = files::rename(app.path(), &store, &a.at("neu ünï"), "umbenannt")
        .await
        .unwrap();
    assert_eq!(moved.path, a.at("umbenannt"));
    assert!(!Path::new(&a.at("neu ünï")).exists() && Path::new(&a.at("umbenannt")).is_dir());
    let e = files::rename(app.path(), &store, &a.at("umbenannt"), "keep.txt")
        .await
        .unwrap_err();
    if common::modern("renaming onto an existing name answers CONFLICT") {
        assert_eq!(refusal(&e).1, "CONFLICT", "{e:?}");
        assert_eq!(
            std::fs::read_to_string(a.at("keep.txt")).unwrap(),
            "keep me"
        );
    }

    // Names that could climb out or hide a path are refused before the agent is asked.
    for bad in ["../x", "a/b", "a\\b", "", ".", "..", " x", "x\n"] {
        let e = files::create_folder(app.path(), &store, &root, bad)
            .await
            .unwrap_err();
        assert!(
            matches!(&e, AgentError::Local(m) if m == files::ERR_NAME_BAD),
            "{bad:?}: {e:?}"
        );
        let e = files::rename(app.path(), &store, &a.at("keep.txt"), bad)
            .await
            .unwrap_err();
        assert!(
            matches!(&e, AgentError::Local(m) if m == files::ERR_NAME_BAD),
            "{bad:?}: {e:?}"
        );
    }
    assert!(!a.outside.path().join("x").exists());

    // The agent's jail, not this app, refuses a folder outside the root or through the symlink.
    let outside = a.outside.path().to_str().unwrap();
    for parent in [outside.to_string(), a.at("escape")] {
        let e = files::create_folder(app.path(), &store, &parent, "pwned")
            .await
            .unwrap_err();
        assert_eq!(refusal(&e).1, "FORBIDDEN", "{parent}: {e:?}");
    }
    assert!(!a.outside.path().join("pwned").exists());
    let e = files::rename(app.path(), &store, &a.at("escape/secret.txt"), "taken")
        .await
        .unwrap_err();
    assert_eq!(refusal(&e).1, "FORBIDDEN", "{e:?}");
    let e = files::trash(app.path(), &store, &a.at("escape/secret.txt"))
        .await
        .unwrap_err();
    assert_eq!(refusal(&e).1, "FORBIDDEN", "{e:?}");
    assert!(a.outside.path().join("secret.txt").exists());

    // Delete goes to the trash, where it can be restored; it is not removed for good.
    files::trash(app.path(), &store, &a.at("keep.txt"))
        .await
        .unwrap();
    assert!(!Path::new(&a.at("keep.txt")).exists());
    assert_eq!(
        std::fs::read_to_string(a.trash_files().join("keep.txt")).unwrap(),
        "keep me",
        "the file is in the agent's trash"
    );
    let top = files::list(app.path(), &store, &root, None, None)
        .await
        .unwrap();
    assert!(!names(&top.entries).contains(&"keep.txt"));
    // A path that is already gone is the agent's per-item error.
    let e = files::trash(app.path(), &store, &a.at("keep.txt"))
        .await
        .unwrap_err();
    assert!(matches!(e, AgentError::Server { .. }), "{e:?}");
}

#[tokio::test]
async fn a_browse_only_device_can_look_and_is_refused_with_the_agents_words() {
    let a = FileAgent::start();
    let (store, root) = (memory(), a.root());
    let app = a.ordinary(&store).await;
    write(a.at("seen.txt"), "x");

    let r = files::roots(app.path(), &store).await.unwrap();
    if common::modern("the agent reports permissions") {
        let caps = r.caps.expect("permissions are reported");
        assert!(caps.browse && !caps.modify && !caps.delete, "{caps:?}");
    }
    let top = files::list(app.path(), &store, &root, None, None)
        .await
        .unwrap();
    assert_eq!(names(&top.entries), ["seen.txt"]);

    for e in [
        files::create_folder(app.path(), &store, &root, "nope")
            .await
            .unwrap_err(),
        files::rename(app.path(), &store, &a.at("seen.txt"), "renamed")
            .await
            .map(|_| ())
            .unwrap_err(),
        files::trash(app.path(), &store, &a.at("seen.txt"))
            .await
            .unwrap_err(),
    ] {
        let (status, code, _) = refusal(&e);
        assert_eq!((status, code), (403, "CAPABILITY_DENIED"), "{e:?}");
        let shown = files::user_message(&e);
        assert!(
            shown.starts_with("The agent says: device lacks "),
            "{shown}"
        );
        assert!(shown.ends_with("(CAPABILITY_DENIED)"), "{shown}");
    }
    assert!(Path::new(&a.at("seen.txt")).exists());
    assert!(!Path::new(&a.at("nope")).exists());
    assert!(!Path::new(&a.at("renamed")).exists());
}

#[tokio::test]
async fn a_read_only_agent_refuses_changes_and_says_so_up_front() {
    let a = FileAgent::start_with(true, true);
    let (store, root) = (memory(), a.root());
    let app = a.owner(&store).await;
    write(a.at("f.txt"), "x");

    let r = files::roots(app.path(), &store).await.unwrap();
    assert!(r.read_only, "{r:?}");
    assert!(files::list(app.path(), &store, &root, None, None)
        .await
        .is_ok());
    let e = files::create_folder(app.path(), &store, &root, "nope")
        .await
        .unwrap_err();
    assert_eq!(refusal(&e).1, "READ_ONLY", "{e:?}");
    assert!(files::user_message(&e).contains("read-only"), "{e:?}");
    assert!(files::trash(app.path(), &store, &a.at("f.txt"))
        .await
        .is_err());
    assert!(Path::new(&a.at("f.txt")).exists());
}

#[tokio::test]
async fn an_agent_with_no_folder_limit_offers_its_drives() {
    let a = FileAgent::start_with(false, false);
    let store = memory();
    let app = a.owner(&store).await;
    let r = files::roots(app.path(), &store).await.unwrap();
    assert_eq!(r.source, "drives");
    assert!(
        r.locations
            .iter()
            .any(|l| l.path == "/" && l.is_os && l.total_bytes > 0),
        "{r:?}"
    );
    // Control: the same call with a limit gives the folder, not the drives.
    let b = FileAgent::start();
    let sb = memory();
    let ab = b.owner(&sb).await;
    assert_eq!(files::roots(ab.path(), &sb).await.unwrap().source, "roots");
}

#[tokio::test]
async fn a_revoked_login_returns_to_sign_in_and_nothing_is_listed_without_one() {
    let a = FileAgent::start();
    let (store, root) = (memory(), a.root());

    // Not signed in at all.
    let e = files::roots(TempDir::new().unwrap().path(), &store)
        .await
        .unwrap_err();
    assert!(
        matches!(&e, AgentError::Local(m) if m == "not signed in"),
        "{e:?}"
    );

    let app = a.owner(&store).await;
    let saved = flows::load_saved(app.path(), &store).unwrap();
    assert!(files::list(app.path(), &store, &root, None, None)
        .await
        .is_ok());
    a.child_revoke(&saved.device_id);
    let e = files::list(app.path(), &store, &root, None, None)
        .await
        .unwrap_err();
    assert_eq!(refusal(&e).0, 401, "{e:?}");
    assert_eq!(
        files::user_message(&e),
        "The agent no longer accepts this login. Sign in again."
    );
    assert!(
        flows::load_saved(app.path(), &store)
            .unwrap()
            .token
            .is_empty(),
        "the dead token is dropped so the window returns to sign-in"
    );
}

impl FileAgent {
    fn child_revoke(&self, device_id: &str) {
        cli(&["revoke", "-data", self.data_dir(), device_id]);
    }
}

// --- the guide explains every message this feature can show -------------------------------------

/// Stems of what the file browser itself says, from `src/files.rs` and `ui/*.js`.
const MESSAGES: &[&str] = &[
    "a path must be absolute (start with / or a drive letter)",
    "a path cannot be empty, hold a NUL character or a .. step, or be longer than 4096 bytes",
    "a name must be 1 to 255 bytes",
    "the agent did not say whether the delete worked",
    "The agent says:",
    "No folder is open to this login",
    "This folder is empty.",
    "The agent lists more items than are shown",
    "New folder name",
    "Moved to Trash:",
    "unexpected route",
    "The agent is read-only",
    "This computer may look but not change",
];

fn read(rel: &str) -> String {
    let p = Path::new(env!("CARGO_MANIFEST_DIR")).join(rel);
    std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("{}: {e}", p.display()))
}

#[test]
fn every_file_browser_message_is_in_the_guide_and_still_in_the_app() {
    let guide = read("../docs/user-guide.md");
    let source = [
        read("src/files.rs"),
        read("src/agent_client.rs"),
        ui::js(),
        read("../ui/index.html"),
    ]
    .join("\n");
    // Source literals wrap long strings; compare with the wrapping removed.
    let flat = source
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .replace("\\ ", "");
    for stem in MESSAGES {
        assert!(
            guide.contains(stem),
            "user-guide.md does not explain {stem:?}"
        );
        assert!(
            flat.contains(stem) || source.contains(stem),
            "{stem:?} is listed but no longer in the app"
        );
    }
}

#[tokio::test]
async fn a_permanent_delete_skips_the_trash() {
    let a = FileAgent::start();
    let (store, root) = (memory(), a.root());
    let app = a.owner(&store).await;
    write(a.at("gone.txt"), "bye");
    write(a.at("kept.txt"), "stay");

    files::delete(app.path(), &store, &a.at("gone.txt"), true)
        .await
        .unwrap();
    assert!(!Path::new(&a.at("gone.txt")).exists());
    assert!(
        !a.trash_files().join("gone.txt").exists(),
        "a permanent delete leaves nothing in the agent's trash"
    );

    files::delete(app.path(), &store, &a.at("kept.txt"), false)
        .await
        .unwrap();
    assert!(
        a.trash_files().join("kept.txt").exists(),
        "the default is the trash"
    );
    let top = files::list(app.path(), &store, &root, None, None)
        .await
        .unwrap();
    assert!(!names(&top.entries).contains(&"gone.txt"));
}
