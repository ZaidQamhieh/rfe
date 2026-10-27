//! The file browser's Rust core: the agent's roots, directory listings and the few safe changes the
//! agent offers (new folder, rename, delete to trash). Every call goes over the pinned connection
//! with the saved login, like `flows::list_devices`; the window never sees a token or a URL.
//!
//! Paths in this file are the *agent's* paths, text that names a place on another computer. They are
//! never turned into a local `Path`, never joined with one and never opened here. Each one is checked
//! ([`validate_path`], [`validate_name`]) before it goes anywhere, then handed to the HTTP library as a
//! query value or a JSON field, which percent-encodes it. Whether a path is allowed is the agent's
//! decision (its roots, jail and the device's permissions); a refusal is shown as the agent worded it.

use crate::agent_client::{AgentClient, AgentError};
use crate::applog;
use crate::flows;
use crate::secrets::Offloaded;
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::path::Path;

/// Longest path this app will send. Linux allows 4096 bytes; the agent refuses what its system refuses.
const MAX_PATH_BYTES: usize = 4096;
/// Longest single name (every common file system stops at 255 bytes).
const MAX_NAME_BYTES: usize = 255;
/// The agent caps a page at 1000 entries whatever is asked for.
const MAX_PAGE: u32 = 1000;
const DEFAULT_PAGE: u32 = 500;

pub const ERR_PATH_ABSOLUTE: &str = "a path must be absolute (start with / or a drive letter)";
pub const ERR_PATH_BAD: &str =
    "a path cannot be empty, hold a NUL character or a .. step, or be longer than 4096 bytes";
pub const ERR_NAME_BAD: &str = "a name must be 1 to 255 bytes, with no / or \\, no control characters, no space at either end, and not . or ..";
pub const ERR_DELETE_UNCONFIRMED: &str = "the agent did not say whether the delete worked";
/// Starts every refusal the agent words itself, so the window can show it as the agent's own.
pub const AGENT_SAYS: &str = "The agent says:";

// ---------------------------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------------------------

/// An agent path split into its root and steps. `/srv/a b` is root `/` and steps `srv`, `a b`;
/// `C:\Users\x` is root `C:\` and steps `Users`, `x`; `\\host\share\x` is root `\\host\share\`.
struct Parsed {
    root: String,
    sep: char,
    steps: Vec<String>,
}

fn split_steps(rest: &str, seps: &[char]) -> Vec<String> {
    rest.split(|c| seps.contains(&c))
        .filter(|s| !s.is_empty())
        .map(str::to_string)
        .collect()
}

fn parse(path: &str) -> Result<Parsed, AgentError> {
    let bad = || AgentError::Local(ERR_PATH_BAD.into());
    if path.is_empty() || path.len() > MAX_PATH_BYTES || path.contains('\0') {
        return Err(bad());
    }
    let b = path.as_bytes();
    let parsed = if let Some(rest) = path.strip_prefix('/') {
        Parsed {
            root: "/".into(),
            sep: '/',
            steps: split_steps(rest, &['/']),
        }
    } else if b.len() >= 3
        && b[0].is_ascii_alphabetic()
        && b[1] == b':'
        && matches!(b[2], b'\\' | b'/')
    {
        Parsed {
            root: path[..3].into(),
            sep: b[2] as char,
            steps: split_steps(&path[3..], &['\\', '/']),
        }
    } else if let Some(rest) = path.strip_prefix("\\\\") {
        let mut parts = split_steps(rest, &['\\']);
        if parts.len() < 2 {
            return Err(AgentError::Local(ERR_PATH_ABSOLUTE.into()));
        }
        let steps = parts.split_off(2);
        Parsed {
            root: format!("\\\\{}\\{}\\", parts[0], parts[1]),
            sep: '\\',
            steps,
        }
    } else {
        return Err(AgentError::Local(ERR_PATH_ABSOLUTE.into()));
    };
    if parsed.steps.iter().any(|s| s == "..") {
        return Err(bad());
    }
    Ok(parsed)
}

/// Checks a path before it is sent: absolute, no NUL, no `..` step, not absurdly long. A `.` step
/// or doubled separators are harmless (the agent cleans them). Control characters other than NUL
/// are allowed, because file names may hold them and a listing must stay browsable.
pub fn validate_path(path: &str) -> Result<(), AgentError> {
    parse(path).map(|_| ())
}

/// Checks a name the user typed for a new folder or a rename.
pub fn validate_name(name: &str) -> Result<(), AgentError> {
    let ok = !name.is_empty()
        && name.len() <= MAX_NAME_BYTES
        && name != "."
        && name != ".."
        && name.trim() == name
        && !name
            .chars()
            .any(|c| c.is_control() || matches!(c, '/' | '\\'));
    if ok {
        Ok(())
    } else {
        Err(AgentError::Local(ERR_NAME_BAD.into()))
    }
}

/// One step of the breadcrumb trail: what to show and the path it leads to.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct Crumb {
    pub label: String,
    pub path: String,
}

/// The trail from the root of `path` down to it, the root first (`/`, `C:\`).
pub fn crumbs(path: &str) -> Result<Vec<Crumb>, AgentError> {
    let p = parse(path)?;
    let mut out = vec![Crumb {
        label: p.root.clone(),
        path: p.root.clone(),
    }];
    let mut at = p.root.clone();
    for (i, step) in p.steps.iter().enumerate() {
        if i > 0 {
            at.push(p.sep);
        }
        at.push_str(step);
        out.push(Crumb {
            label: step.clone(),
            path: at.clone(),
        });
    }
    Ok(out)
}

/// The folder that holds `path`, or `None` for a root.
pub fn parent_of(path: &str) -> Result<Option<String>, AgentError> {
    let mut trail = crumbs(path)?;
    if trail.len() < 2 {
        return Ok(None);
    }
    trail.pop();
    Ok(trail.pop().map(|c| c.path))
}

/// `parent` plus one new name. The only place a new path is made, and only from text the user
/// typed (checked by [`validate_name`]) and a path the agent sent.
pub fn join(parent: &str, name: &str) -> Result<String, AgentError> {
    validate_name(name)?;
    let p = parse(parent)?;
    let here = crumbs(parent)?.pop().map(|c| c.path).unwrap_or_default();
    Ok(if p.steps.is_empty() {
        format!("{here}{name}")
    } else {
        format!("{here}{}{name}", p.sep)
    })
}

// ---------------------------------------------------------------------------------------------
// What the agent sends, and what the window gets
// ---------------------------------------------------------------------------------------------

/// What this login may do with files. `None` in [`Roots`] means the agent did not say (an older
/// agent); the window then offers the actions and shows the agent's refusal if there is one.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Caps {
    pub browse: bool,
    pub download: bool,
    pub upload: bool,
    pub modify: bool,
    pub delete: bool,
    pub share: bool,
}

#[derive(Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
struct Settings {
    roots: Vec<String>,
    read_only: bool,
    access_denied: bool,
    file_capabilities: Option<Caps>,
}

#[derive(Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
struct DriveBody {
    path: String,
    label: String,
    total_bytes: i64,
    free_bytes: i64,
    #[serde(rename = "isOS")]
    is_os: bool,
}

/// A place to start browsing: a folder the agent was confined to, or a drive.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Location {
    pub path: String,
    pub label: String,
    pub total_bytes: i64,
    pub free_bytes: i64,
    pub is_os: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Roots {
    pub locations: Vec<Location>,
    /// `roots` when the agent confines this login to folders, `drives` when it does not.
    pub source: &'static str,
    pub read_only: bool,
    /// The agent says this login's folder is outside everything the agent allows.
    pub access_denied: bool,
    pub caps: Option<Caps>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct FileEntry {
    pub name: String,
    pub path: String,
    pub is_dir: bool,
    pub size: i64,
    pub mime_type: String,
    pub mode: String,
    pub modified: String,
    pub created: String,
    pub is_symlink: bool,
    pub symlink_target: String,
    pub child_count: Option<u32>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase", default)]
#[derive(Default)]
struct Listing {
    path: String,
    entries: Vec<FileEntry>,
    next_cursor: Option<String>,
}

/// One page of a folder, in the order the agent sends it (by name).
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Page {
    pub path: String,
    pub crumbs: Vec<Crumb>,
    pub entries: Vec<FileEntry>,
    /// Pass back as `cursor` to get the next page; `None` when this was the last.
    pub next_cursor: Option<String>,
}

#[derive(Deserialize)]
pub(crate) struct BatchBody {
    #[serde(default)]
    pub(crate) results: Vec<BatchItem>,
}

#[derive(Deserialize)]
pub(crate) struct BatchItem {
    #[serde(default)]
    pub(crate) ok: bool,
    pub(crate) error: Option<ItemError>,
}

#[derive(Deserialize)]
pub(crate) struct ItemError {
    #[serde(default)]
    pub(crate) code: String,
    #[serde(default)]
    pub(crate) message: String,
}

// ---------------------------------------------------------------------------------------------
// Talking to the agent
// ---------------------------------------------------------------------------------------------

/// The pinned client and the saved login, or the reason there is none. Same rules as the device
/// list: a forgotten pin ends the session here.
pub(crate) async fn session(
    dir: &Path,
    store: &Offloaded,
) -> Result<(AgentClient, String), AgentError> {
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
    let key = flows::pin_key(&saved.host);
    let pinned = flows::list_pins(dir)
        .map_err(AgentError::Local)?
        .into_iter()
        .find(|p| p.host == key)
        .map(|p| p.fingerprint)
        .ok_or_else(|| {
            AgentError::Local(format!(
                "{} is no longer a trusted agent; connect and compare its fingerprint again",
                saved.host
            ))
        })?;
    Ok((AgentClient::pinned(&saved.host, &pinned)?, saved.token))
}

/// Logs the outcome (never a path or a name: only what was done and how it ended) and, when the
/// agent no longer accepts the login, drops the dead token so the window returns to sign-in.
pub(crate) async fn settle<T>(
    dir: &Path,
    store: &Offloaded,
    token: &str,
    op: &str,
    r: Result<T, AgentError>,
) -> Result<T, AgentError> {
    match &r {
        Ok(_) => applog::debug(&format!("files: {op}: ok")),
        Err(AgentError::Server { status, code, .. }) => {
            applog::error(&format!("files: {op} refused ({status} {code})"))
        }
        Err(AgentError::Network(_)) => applog::error(&format!("files: {op} failed (network)")),
        Err(AgentError::Local(_)) => applog::error(&format!("files: {op} failed (local)")),
    }
    if let Err(AgentError::Server { status: 401, .. }) = &r {
        let dir = dir.to_path_buf();
        let token = token.to_string();
        store
            .run(move |st| flows::drop_refused_token(&dir, st, &token))
            .await
            .map_err(AgentError::Local)?;
    }
    r
}

/// The text the window shows for an error. A refusal the agent worded itself (outside the allowed
/// folders, read-only, permission, a name that exists) is shown as the agent sent it, with its code;
/// everything else keeps the app's usual wording.
pub fn user_message(e: &AgentError) -> String {
    match e {
        AgentError::Server { code, message, .. }
            if !matches!(code.as_str(), "UNAUTHORIZED" | "INTERNAL")
                && !code.starts_with("HTTP_") =>
        {
            format!("{AGENT_SAYS} {message} ({code})")
        }
        other => other.to_string(),
    }
}

/// Where this login may start browsing, and what it may do. With folder roots set on the agent
/// those folders are the locations; with none, the agent's drives are.
pub async fn roots(dir: &Path, store: &Offloaded) -> Result<Roots, AgentError> {
    let (client, token) = session(dir, store).await?;
    let r = async {
        let s: Settings = client
            .authed_json(&token, reqwest::Method::GET, "/settings", &[], None)
            .await?;
        let browse_denied = s.file_capabilities.as_ref().is_some_and(|c| !c.browse);
        let (source, locations) = if s.access_denied || browse_denied {
            ("roots", Vec::new())
        } else if !s.roots.is_empty() {
            let list = s
                .roots
                .iter()
                .map(|p| Location {
                    label: crumbs(p)
                        .ok()
                        .and_then(|mut t| t.pop())
                        .map(|c| c.label)
                        .unwrap_or_else(|| p.clone()),
                    path: p.clone(),
                    total_bytes: 0,
                    free_bytes: 0,
                    is_os: false,
                })
                .collect();
            ("roots", list)
        } else {
            let drives: Vec<DriveBody> = client
                .authed_json(&token, reqwest::Method::GET, "/system/drives", &[], None)
                .await?;
            let list = drives
                .into_iter()
                .map(|d| Location {
                    label: if d.label.is_empty() {
                        d.path.clone()
                    } else {
                        d.label
                    },
                    path: d.path,
                    total_bytes: d.total_bytes,
                    free_bytes: d.free_bytes,
                    is_os: d.is_os,
                })
                .collect();
            ("drives", list)
        };
        Ok(Roots {
            locations,
            source,
            read_only: s.read_only,
            access_denied: s.access_denied,
            caps: s.file_capabilities,
        })
    }
    .await;
    settle(dir, store, &token, "roots", r).await
}

/// One page of the folder at `path`. `cursor` is the previous page's `next_cursor`; `limit` is
/// clamped to what the agent allows (1 to 1000, 500 when not given).
pub async fn list(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    cursor: Option<&str>,
    limit: Option<u32>,
) -> Result<Page, AgentError> {
    validate_path(path)?;
    if let Some(c) = cursor {
        if c.is_empty() || c.len() > MAX_PATH_BYTES || c.contains('\0') {
            return Err(AgentError::Local(ERR_PATH_BAD.into()));
        }
    }
    let limit = limit.unwrap_or(DEFAULT_PAGE).clamp(1, MAX_PAGE).to_string();
    let (client, token) = session(dir, store).await?;
    let mut query = vec![("path", path), ("limit", limit.as_str())];
    if let Some(c) = cursor {
        query.push(("cursor", c));
    }
    let r = async {
        let l: Listing = client
            .authed_json(&token, reqwest::Method::GET, "/fs", &query, None)
            .await?;
        // The agent answers with the real folder it listed; the trail follows that.
        let shown = if validate_path(&l.path).is_ok() {
            l.path.as_str()
        } else {
            path
        };
        Ok(Page {
            crumbs: crumbs(shown)?,
            path: shown.to_string(),
            entries: l.entries,
            next_cursor: l.next_cursor.filter(|c| !c.is_empty()),
        })
    }
    .await;
    settle(dir, store, &token, "list", r).await
}

/// The agent's own record of one entry. Used for a symlink that is not known to be a folder, so the
/// agent's decision about it (allowed, or outside the allowed folders) is shown as it words it.
pub async fn meta(dir: &Path, store: &Offloaded, path: &str) -> Result<FileEntry, AgentError> {
    validate_path(path)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(
            &token,
            reqwest::Method::GET,
            "/fs/meta",
            &[("path", path)],
            None,
        )
        .await;
    settle(dir, store, &token, "meta", r).await
}

/// Makes a folder called `name` inside `parent`. The agent refuses it for a login without the
/// modify permission, in read-only mode, outside its folders, or when the name exists.
pub async fn create_folder(
    dir: &Path,
    store: &Offloaded,
    parent: &str,
    name: &str,
) -> Result<FileEntry, AgentError> {
    let target = join(parent, name)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(
            &token,
            reqwest::Method::POST,
            "/fs/folder",
            &[],
            Some(json!({ "path": target })),
        )
        .await;
    settle(dir, store, &token, "new folder", r).await
}

/// Renames the entry at `path` to `new_name` inside the same folder. It cannot move an entry: the
/// new path is the old folder plus a name that holds no separator.
pub async fn rename(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    new_name: &str,
) -> Result<FileEntry, AgentError> {
    validate_path(path)?;
    let parent = parent_of(path)?.ok_or_else(|| AgentError::Local(ERR_PATH_BAD.into()))?;
    let target = join(&parent, new_name)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(
            &token,
            reqwest::Method::PATCH,
            "/fs/rename",
            &[],
            Some(json!({ "src": path, "dst": target })),
        )
        .await;
    settle(dir, store, &token, "rename", r).await
}

/// Moves the entry at `path` to the agent's trash, where it can be restored. There is no way to
/// delete permanently from here: the request never carries `permanent`.
pub async fn trash(dir: &Path, store: &Offloaded, path: &str) -> Result<(), AgentError> {
    delete(dir, store, path, false).await
}

/// Deletes `path` on the computer: into its Trash, or for good when `permanent` is set.
pub async fn delete(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    permanent: bool,
) -> Result<(), AgentError> {
    validate_path(path)?;
    let (client, token) = session(dir, store).await?;
    let query: Vec<(&str, &str)> = if permanent {
        vec![("path", path), ("permanent", "true")]
    } else {
        vec![("path", path)]
    };
    let r = async {
        let body: BatchBody = client
            .authed_json(&token, reqwest::Method::DELETE, "/fs", &query, None)
            .await?;
        match body.results.first() {
            Some(BatchItem { ok: true, .. }) => Ok(()),
            Some(BatchItem { error: Some(e), .. }) => Err(AgentError::Server {
                status: 200,
                code: e.code.clone(),
                message: e.message.clone(),
            }),
            Some(_) | None => Err(AgentError::Local(ERR_DELETE_UNCONFIRMED.into())),
        }
    }
    .await;
    settle(
        dir,
        store,
        &token,
        if permanent {
            "delete permanently"
        } else {
            "delete to trash"
        },
        r,
    )
    .await
}

// ---------------------------------------------------------------------------------------------
// Commands (registered in lib.rs)
// ---------------------------------------------------------------------------------------------

fn keystore() -> Offloaded {
    Offloaded::os_keystore()
}

fn data_dir(app: &tauri::AppHandle) -> Result<std::path::PathBuf, String> {
    use tauri::Manager;
    app.path().app_data_dir().map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn files_roots(app: tauri::AppHandle, host: Option<String>) -> Result<Roots, String> {
    roots(&data_dir(&app)?, &keystore().scoped(host))
        .await
        .map_err(|e| user_message(&e))
}

#[tauri::command]
pub async fn files_list(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    cursor: Option<String>,
    limit: Option<u32>,
) -> Result<Page, String> {
    list(
        &data_dir(&app)?,
        &keystore().scoped(host),
        &path,
        cursor.as_deref(),
        limit,
    )
    .await
    .map_err(|e| user_message(&e))
}

#[tauri::command]
pub async fn files_meta(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
) -> Result<FileEntry, String> {
    meta(&data_dir(&app)?, &keystore().scoped(host), &path)
        .await
        .map_err(|e| user_message(&e))
}

#[tauri::command]
pub async fn files_create_folder(
    app: tauri::AppHandle,
    host: Option<String>,
    parent: String,
    name: String,
) -> Result<FileEntry, String> {
    create_folder(&data_dir(&app)?, &keystore().scoped(host), &parent, &name)
        .await
        .map_err(|e| user_message(&e))
}

#[tauri::command]
pub async fn files_rename(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    new_name: String,
) -> Result<FileEntry, String> {
    rename(&data_dir(&app)?, &keystore().scoped(host), &path, &new_name)
        .await
        .map_err(|e| user_message(&e))
}

#[tauri::command]
pub async fn files_trash(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    permanent: Option<bool>,
) -> Result<(), String> {
    delete(
        &data_dir(&app)?,
        &keystore().scoped(host),
        &path,
        permanent.unwrap_or(false),
    )
    .await
    .map_err(|e| user_message(&e))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn is_local(r: Result<(), AgentError>, text: &str) -> bool {
        matches!(r, Err(AgentError::Local(m)) if m == text)
    }

    #[test]
    fn paths_must_be_absolute_and_clean() {
        for ok in [
            "/",
            "/srv",
            "/srv/a b/ünï/日本語",
            "/a/./b",
            "//a",
            "C:\\",
            "C:/Users/x",
            "D:\\a\\b c",
            "\\\\host\\share\\dir",
            "/with\nnewline",
            "/semi;colon?and#hash%20and+plus",
        ] {
            assert!(validate_path(ok).is_ok(), "{ok:?}");
        }
        assert!(is_local(validate_path(""), ERR_PATH_BAD));
        assert!(is_local(validate_path("/a\0b"), ERR_PATH_BAD));
        assert!(is_local(validate_path("/a/../b"), ERR_PATH_BAD));
        assert!(is_local(validate_path("/.."), ERR_PATH_BAD));
        assert!(is_local(validate_path("C:\\a\\..\\b"), ERR_PATH_BAD));
        assert!(is_local(validate_path("C:/a/../b"), ERR_PATH_BAD));
        assert!(is_local(
            validate_path(&format!("/{}", "a".repeat(5000))),
            ERR_PATH_BAD
        ));
        for rel in [
            "a/b",
            "./a",
            "~/a",
            "C:",
            "C:a",
            "\\\\host",
            "https://x/y",
            "file:///etc",
        ] {
            assert!(is_local(validate_path(rel), ERR_PATH_ABSOLUTE), "{rel:?}");
        }
        // A step that merely contains dots is a name, not a parent reference.
        assert!(validate_path("/a/..b/c..").is_ok());
    }

    #[test]
    fn names_are_one_plain_step() {
        for ok in [
            "docs",
            "a b",
            "ünï",
            "日本語",
            ".hidden",
            "a..b",
            "x.tar.gz",
            &"é".repeat(127),
        ] {
            assert!(validate_name(ok).is_ok(), "{ok:?}");
        }
        for bad in [
            "",
            ".",
            "..",
            "a/b",
            "a\\b",
            " a",
            "a ",
            "a\nb",
            "a\tb",
            "a\0b",
            "\u{7f}",
            &"a".repeat(256),
            &"é".repeat(128),
        ] {
            assert!(is_local(validate_name(bad), ERR_NAME_BAD), "{bad:?}");
        }
    }

    #[test]
    fn trail_and_parent_for_each_path_style() {
        let labels = |p: &str| {
            crumbs(p)
                .unwrap()
                .into_iter()
                .map(|c| (c.label, c.path))
                .collect::<Vec<_>>()
        };
        assert_eq!(labels("/"), [("/".into(), "/".into())]);
        assert_eq!(
            labels("/srv//a b/"),
            [
                ("/".into(), "/".into()),
                ("srv".into(), "/srv".into()),
                ("a b".into(), "/srv/a b".into())
            ]
        );
        assert_eq!(
            labels("C:\\Users\\x"),
            [
                ("C:\\".into(), "C:\\".into()),
                ("Users".into(), "C:\\Users".into()),
                ("x".into(), "C:\\Users\\x".into())
            ]
        );
        assert_eq!(
            labels("\\\\nas\\share\\a"),
            [
                ("\\\\nas\\share\\".into(), "\\\\nas\\share\\".into()),
                ("a".into(), "\\\\nas\\share\\a".into())
            ]
        );
        assert_eq!(parent_of("/").unwrap(), None);
        assert_eq!(parent_of("/srv").unwrap().as_deref(), Some("/"));
        assert_eq!(parent_of("/srv/a").unwrap().as_deref(), Some("/srv"));
        assert_eq!(parent_of("C:\\a").unwrap().as_deref(), Some("C:\\"));
        assert_eq!(parent_of("C:\\a\\b").unwrap().as_deref(), Some("C:\\a"));
    }

    #[test]
    fn a_new_path_is_the_parent_plus_one_name() {
        assert_eq!(join("/", "x").unwrap(), "/x");
        assert_eq!(join("/srv/", "x y").unwrap(), "/srv/x y");
        assert_eq!(join("/srv/a", "日本語").unwrap(), "/srv/a/日本語");
        assert_eq!(join("C:\\", "x").unwrap(), "C:\\x");
        assert_eq!(join("C:\\a", "x").unwrap(), "C:\\a\\x");
        assert!(is_local(join("/srv", "../x").map(|_| ()), ERR_NAME_BAD));
        assert!(is_local(join("/srv", "a/b").map(|_| ()), ERR_NAME_BAD));
        assert!(is_local(join("rel", "x").map(|_| ()), ERR_PATH_ABSOLUTE));
        assert!(is_local(join("/a/../b", "x").map(|_| ()), ERR_PATH_BAD));
    }

    #[test]
    fn the_agents_own_refusals_are_shown_as_it_worded_them() {
        let server = |status, code: &str, message: &str| AgentError::Server {
            status,
            code: code.into(),
            message: message.into(),
        };
        assert_eq!(
            user_message(&server(
                403,
                "CAPABILITY_DENIED",
                "device lacks modify permission"
            )),
            "The agent says: device lacks modify permission (CAPABILITY_DENIED)"
        );
        assert_eq!(
            user_message(&server(
                403,
                "FORBIDDEN",
                "path is outside allowed root: /x"
            )),
            "The agent says: path is outside allowed root: /x (FORBIDDEN)"
        );
        // Sign-in and internal errors keep the app's wording.
        assert_eq!(
            user_message(&server(401, "UNAUTHORIZED", "x")),
            "The agent no longer accepts this login. Sign in again."
        );
        assert!(user_message(&server(500, "INTERNAL", "x")).contains("internal error"));
        assert_eq!(
            user_message(&AgentError::Local("not signed in".into())),
            "not signed in"
        );
    }
}
