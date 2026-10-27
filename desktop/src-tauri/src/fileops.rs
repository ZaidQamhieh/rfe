//! The rest of the agent's file operations: search, copy and move, archives, checksums,
//! permissions, share links, trash, recent files, previews and thumbnails, text editing and
//! Wake-on-LAN. Every call goes over the pinned connection with the login of the chosen host
//! (`host`, or the active session), like `files.rs`; the window never sees a token or a URL.
//!
//! Paths are the *agent's* paths and are validated with the same rules as in `files.rs` before they
//! are sent. Whether an operation is allowed is the agent's decision (its roots, jail and the
//! device's grants); a refusal is shown as the agent worded it.

use crate::agent_client::AgentError;
use crate::files::{
    session, settle, user_message, validate_name, validate_path, BatchBody, FileEntry,
};
use crate::secrets::Offloaded;
use base64::Engine as _;
use reqwest::Method;
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::path::Path;

/// Most paths one batch call may carry.
const MAX_BATCH: usize = 1000;
/// Largest text file the window opens for reading or editing.
pub const MAX_TEXT_BYTES: usize = 1 << 20;
/// Largest thumbnail the app accepts.
const MAX_THUMB_BYTES: usize = 4 << 20;
const MAX_QUERY_BYTES: usize = 256;
const ERR_UNCONFIRMED: &str = "The agent did not confirm that the change was made.";

const ERR_QUERY: &str = "a search needs 1 to 256 characters";
const ERR_BATCH: &str = "choose between 1 and 1000 items";
const ERR_ALGO: &str = "the checksum must be sha256, sha1 or md5";
const ERR_MODE: &str = "a permission mode is 3 or 4 octal digits, for example 0755";
const ERR_TYPE: &str = "unknown file category";
const ERR_NOT_TEXT: &str = "this file is not text, or is too large to open here";

fn local(msg: &str) -> AgentError {
    AgentError::Local(msg.into())
}

fn paths_ok(paths: &[String]) -> Result<(), AgentError> {
    if paths.is_empty() || paths.len() > MAX_BATCH {
        return Err(local(ERR_BATCH));
    }
    paths.iter().try_for_each(|p| validate_path(p))
}

pub(crate) fn encode_mime(path: &str, bytes: &[u8]) -> String {
    let lower = path.to_ascii_lowercase();
    let mime = if bytes.starts_with(&[0xFF, 0xD8]) {
        "image/jpeg"
    } else if bytes.starts_with(&[0x89, b'P', b'N', b'G']) {
        "image/png"
    } else if bytes.starts_with(b"GIF8") {
        "image/gif"
    } else if lower.ends_with(".svg") {
        "image/svg+xml"
    } else if lower.ends_with(".webp") || bytes.get(8..12) == Some(b"WEBP") {
        "image/webp"
    } else {
        "image/jpeg"
    };
    format!(
        "data:{mime};base64,{}",
        base64::engine::general_purpose::STANDARD.encode(bytes)
    )
}

// ---------------------------------------------------------------------------------------------
// Search and recent
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct SearchOpts {
    pub query: String,
    pub root: Option<String>,
    pub limit: Option<u32>,
    /// `folder`, `image`, `video`, `audio`, `document`, `archive`, `other`.
    pub types: Vec<String>,
    pub ext: Option<String>,
    pub min_size: Option<i64>,
    pub max_size: Option<i64>,
}

const TYPES: [&str; 7] = [
    "folder", "image", "video", "audio", "document", "archive", "other",
];

/// The agent answers a batch (copy, move, restore, discard) with 202 and one result per item, so a
/// refused item is not an HTTP error. Success means every item worked; otherwise the first refusal is
/// shown in the agent's words, with how many items it was.
fn batch_outcome(body: BatchBody) -> Result<(), AgentError> {
    let total = body.results.len();
    if total == 0 {
        return Err(local(ERR_UNCONFIRMED));
    }
    let failed: Vec<_> = body.results.iter().filter(|i| !i.ok).collect();
    let Some(first) = failed.first() else {
        return Ok(());
    };
    let (code, message) = match &first.error {
        Some(e) => (e.code.clone(), e.message.clone()),
        None => ("FAILED".to_string(), ERR_UNCONFIRMED.to_string()),
    };
    let message = if total > 1 {
        format!("{} of {total} items failed: {message}", failed.len())
    } else {
        message
    };
    Err(AgentError::Server {
        status: 200,
        code,
        message,
    })
}

pub async fn search(
    dir: &Path,
    store: &Offloaded,
    o: &SearchOpts,
) -> Result<Vec<FileEntry>, AgentError> {
    if o.query.is_empty() || o.query.len() > MAX_QUERY_BYTES || o.query.contains('\0') {
        return Err(local(ERR_QUERY));
    }
    if let Some(r) = &o.root {
        validate_path(r)?;
    }
    if o.types.iter().any(|t| !TYPES.contains(&t.as_str())) {
        return Err(local(ERR_TYPE));
    }
    if let Some(e) = &o.ext {
        let ok = e.split(',').all(|x| {
            !x.is_empty() && x.len() <= 16 && x.chars().all(|c| c.is_ascii_alphanumeric())
        });
        if !ok {
            return Err(local(
                "extensions are letters and digits, separated by commas",
            ));
        }
    }
    let limit = o.limit.unwrap_or(100).clamp(1, 500).to_string();
    let types = o.types.join(",");
    let min = o.min_size.map(|n| n.max(0).to_string());
    let max = o.max_size.map(|n| n.max(0).to_string());
    let mut query: Vec<(&str, &str)> = vec![("q", o.query.as_str()), ("limit", limit.as_str())];
    if let Some(r) = &o.root {
        query.push(("root", r));
    }
    if !types.is_empty() {
        query.push(("types", &types));
    }
    if let Some(e) = &o.ext {
        query.push(("ext", e));
    }
    if let Some(n) = &min {
        query.push(("minSize", n));
    }
    if let Some(n) = &max {
        query.push(("maxSize", n));
    }
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(&token, Method::GET, "/search", &query, None)
        .await;
    settle(dir, store, &token, "search", r).await
}

pub async fn recent(
    dir: &Path,
    store: &Offloaded,
    root: Option<&str>,
    limit: Option<u32>,
) -> Result<Vec<FileEntry>, AgentError> {
    if let Some(r) = root {
        validate_path(r)?;
    }
    let limit = limit.unwrap_or(50).clamp(1, 500).to_string();
    let mut query: Vec<(&str, &str)> = vec![("limit", limit.as_str())];
    if let Some(r) = root {
        query.push(("root", r));
    }
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(&token, Method::GET, "/fs/recent", &query, None)
        .await;
    settle(dir, store, &token, "recent", r).await
}

// ---------------------------------------------------------------------------------------------
// Copy, move, new file, archives
// ---------------------------------------------------------------------------------------------

/// Copies or moves `sources` into `dest_dir`. `duplicate` keeps both when a name exists (the
/// agent numbers the new one); `overwrite` replaces it. Neither is the default: the agent refuses.
pub async fn transfer_op(
    dir: &Path,
    store: &Offloaded,
    mv: bool,
    sources: &[String],
    dest_dir: &str,
    duplicate: bool,
    overwrite: bool,
) -> Result<(), AgentError> {
    paths_ok(sources)?;
    validate_path(dest_dir)?;
    let (client, token) = session(dir, store).await?;
    let route = if mv { "/fs/move" } else { "/fs/copy" };
    let body = json!({"sources": sources, "destDir": dest_dir, "duplicate": duplicate, "overwrite": overwrite});
    let r = client
        .authed_json::<BatchBody>(&token, Method::POST, route, &[], Some(body))
        .await
        .and_then(batch_outcome);
    settle(dir, store, &token, if mv { "move" } else { "copy" }, r).await
}

pub async fn create_file(dir: &Path, store: &Offloaded, path: &str) -> Result<(), AgentError> {
    validate_path(path)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_empty(
            &token,
            Method::POST,
            "/fs/file",
            &[],
            Some(json!({ "path": path })),
        )
        .await;
    settle(dir, store, &token, "new file", r).await
}

pub async fn compress(
    dir: &Path,
    store: &Offloaded,
    sources: &[String],
    dest: &str,
) -> Result<(), AgentError> {
    paths_ok(sources)?;
    validate_path(dest)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_empty(
            &token,
            Method::POST,
            "/fs/compress",
            &[],
            Some(json!({"sources": sources, "dest": dest})),
        )
        .await;
    settle(dir, store, &token, "compress", r).await
}

pub async fn extract(
    dir: &Path,
    store: &Offloaded,
    archive: &str,
    dest_dir: &str,
) -> Result<FileEntry, AgentError> {
    validate_path(archive)?;
    validate_path(dest_dir)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(
            &token,
            Method::POST,
            "/fs/extract",
            &[],
            Some(json!({"archive": archive, "destDir": dest_dir})),
        )
        .await;
    settle(dir, store, &token, "extract", r).await
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct ArchiveEntry {
    pub path: String,
    pub size: i64,
    pub modified: String,
    pub is_dir: bool,
}

#[derive(Deserialize)]
struct ArchiveList {
    #[serde(default)]
    entries: Vec<ArchiveEntry>,
}

pub async fn archive_list(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    limit: Option<u32>,
) -> Result<Vec<ArchiveEntry>, AgentError> {
    validate_path(path)?;
    let limit = limit.unwrap_or(500).clamp(1, 5000).to_string();
    let (client, token) = session(dir, store).await?;
    let r: Result<ArchiveList, AgentError> = client
        .authed_json(
            &token,
            Method::GET,
            "/fs/archive",
            &[("path", path), ("limit", limit.as_str())],
            None,
        )
        .await;
    settle(dir, store, &token, "archive", r.map(|l| l.entries)).await
}

// ---------------------------------------------------------------------------------------------
// Checksums and permissions
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Checksum {
    pub path: String,
    pub algorithm: String,
    pub checksum: String,
}

fn algo_ok(algo: &str) -> Result<(), AgentError> {
    if matches!(algo, "sha256" | "sha1" | "md5") {
        Ok(())
    } else {
        Err(local(ERR_ALGO))
    }
}

pub async fn checksum(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    algo: &str,
) -> Result<Checksum, AgentError> {
    validate_path(path)?;
    algo_ok(algo)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(
            &token,
            Method::GET,
            "/fs/checksum",
            &[("path", path), ("algo", algo)],
            None,
        )
        .await;
    settle(dir, store, &token, "checksum", r).await
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct HashRow {
    pub path: String,
    pub hash: String,
    pub error: String,
}

#[derive(Deserialize)]
struct HashRows {
    #[serde(default)]
    checksums: Vec<HashRow>,
}

pub async fn checksums(
    dir: &Path,
    store: &Offloaded,
    paths: &[String],
    algo: &str,
) -> Result<Vec<HashRow>, AgentError> {
    paths_ok(paths)?;
    algo_ok(algo)?;
    let (client, token) = session(dir, store).await?;
    let r: Result<HashRows, AgentError> = client
        .authed_json(
            &token,
            Method::POST,
            "/fs/checksums",
            &[],
            Some(json!({"paths": paths, "algo": algo})),
        )
        .await;
    settle(dir, store, &token, "checksums", r.map(|h| h.checksums)).await
}

pub async fn chmod(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    mode: &str,
) -> Result<FileEntry, AgentError> {
    validate_path(path)?;
    let ok = (3..=4).contains(&mode.len()) && mode.chars().all(|c| ('0'..='7').contains(&c));
    if !ok {
        return Err(local(ERR_MODE));
    }
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(
            &token,
            Method::POST,
            "/fs/chmod",
            &[],
            Some(json!({"path": path, "mode": mode})),
        )
        .await;
    settle(dir, store, &token, "chmod", r).await
}

// ---------------------------------------------------------------------------------------------
// Share links
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct ShareLink {
    /// Shown once, when the link is made; the agent keeps only its hash.
    pub token: String,
    pub token_hash: String,
    pub expires_at: i64,
    pub url: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct ShareSummary {
    pub token_hash: String,
    pub path: String,
    pub expires_at: i64,
}

pub async fn mint_share(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    expires_in_seconds: u32,
) -> Result<ShareLink, AgentError> {
    validate_path(path)?;
    if !(60..=24 * 3600).contains(&expires_in_seconds) {
        return Err(local("a share link lasts from 1 minute to 24 hours"));
    }
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(
            &token,
            Method::POST,
            "/share/mint",
            &[],
            Some(json!({"path": path, "expiresInSeconds": expires_in_seconds})),
        )
        .await;
    settle(dir, store, &token, "share", r).await
}

pub async fn share_list(dir: &Path, store: &Offloaded) -> Result<Vec<ShareSummary>, AgentError> {
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json(&token, Method::GET, "/share", &[], None)
        .await;
    settle(dir, store, &token, "share list", r).await
}

pub async fn share_revoke(
    dir: &Path,
    store: &Offloaded,
    token_hash: &str,
) -> Result<(), AgentError> {
    let ok = token_hash.len() == 64 && token_hash.chars().all(|c| c.is_ascii_hexdigit());
    if !ok {
        return Err(local("that is not a share link id"));
    }
    let (client, token) = session(dir, store).await?;
    let route = format!("/share/{token_hash}");
    let r = client
        .authed_empty(&token, Method::DELETE, &route, &[], None)
        .await;
    settle(dir, store, &token, "share revoke", r).await
}

// ---------------------------------------------------------------------------------------------
// Trash
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct TrashEntry {
    pub id: String,
    pub name: String,
    pub original_path: String,
    pub deleted_at: String,
    pub size: i64,
    pub is_dir: bool,
}

#[derive(Deserialize)]
struct TrashList {
    #[serde(default)]
    items: Vec<TrashEntry>,
}

fn ids_ok(ids: &[String]) -> Result<(), AgentError> {
    let ok = !ids.is_empty()
        && ids.len() <= MAX_BATCH
        && ids
            .iter()
            .all(|i| !i.is_empty() && i.len() <= 128 && !i.chars().any(|c| c.is_control()));
    if ok {
        Ok(())
    } else {
        Err(local(ERR_BATCH))
    }
}

pub async fn trash_list(dir: &Path, store: &Offloaded) -> Result<Vec<TrashEntry>, AgentError> {
    let (client, token) = session(dir, store).await?;
    let r: Result<TrashList, AgentError> = client
        .authed_json(&token, Method::GET, "/trash", &[], None)
        .await;
    settle(dir, store, &token, "trash list", r.map(|l| l.items)).await
}

pub async fn trash_restore(
    dir: &Path,
    store: &Offloaded,
    ids: &[String],
) -> Result<(), AgentError> {
    ids_ok(ids)?;
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_json::<BatchBody>(
            &token,
            Method::POST,
            "/trash/restore",
            &[],
            Some(json!({ "ids": ids })),
        )
        .await
        .and_then(batch_outcome);
    settle(dir, store, &token, "trash restore", r).await
}

/// Deletes the given trash items for good, or all of them when `ids` is empty.
pub async fn trash_empty(dir: &Path, store: &Offloaded, ids: &[String]) -> Result<(), AgentError> {
    if !ids.is_empty() {
        ids_ok(ids)?;
    }
    let (client, token) = session(dir, store).await?;
    // No body at all means "empty everything": the agent refuses `{"ids": []}` on purpose, so that a
    // request with nothing selected can never wipe the trash.
    let body = if ids.is_empty() {
        None
    } else {
        Some(json!({ "ids": ids }))
    };
    let r = client
        .authed_empty(&token, Method::DELETE, "/trash", &[], body)
        .await;
    settle(dir, store, &token, "trash empty", r).await
}

// ---------------------------------------------------------------------------------------------
// Previews, thumbnails and text editing
// ---------------------------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TextFile {
    pub path: String,
    pub text: String,
    /// The agent's modified time when it was read; sent back when saving so a change made in the
    /// meantime is noticed instead of overwritten.
    pub modified: String,
    pub size: usize,
}

/// Reads a small text file for the preview and the editor.
pub async fn read_text(dir: &Path, store: &Offloaded, path: &str) -> Result<TextFile, AgentError> {
    validate_path(path)?;
    let (client, token) = session(dir, store).await?;
    let r = async {
        let meta: FileEntry = client
            .authed_json(&token, Method::GET, "/fs/meta", &[("path", path)], None)
            .await?;
        if meta.is_dir || meta.size < 0 || meta.size as usize > MAX_TEXT_BYTES {
            return Err(local(ERR_NOT_TEXT));
        }
        let bytes = client
            .authed_bytes(&token, "/content", &[("path", path)], MAX_TEXT_BYTES)
            .await?;
        let text = String::from_utf8(bytes).map_err(|_| local(ERR_NOT_TEXT))?;
        if text.contains('\0') {
            return Err(local(ERR_NOT_TEXT));
        }
        Ok(TextFile {
            path: path.to_string(),
            size: text.len(),
            text,
            modified: meta.modified,
        })
    }
    .await;
    settle(dir, store, &token, "read text", r).await
}

/// Saves a text file. `base_modified` is the value from [`read_text`]: the agent refuses when the
/// file changed since.
pub async fn write_text(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    text: &str,
    base_modified: &str,
) -> Result<FileEntry, AgentError> {
    validate_path(path)?;
    if text.len() > MAX_TEXT_BYTES || text.contains('\0') {
        return Err(local(ERR_NOT_TEXT));
    }
    let (client, token) = session(dir, store).await?;
    let mut query = vec![("path", path)];
    if !base_modified.is_empty() {
        query.push(("baseModified", base_modified));
    }
    let r = client
        .authed_put(&token, "/content", &query, text.as_bytes().to_vec())
        .await;
    settle(dir, store, &token, "write text", r).await
}

/// A thumbnail as a `data:` URL the window can show without any other network access.
pub async fn thumb(
    dir: &Path,
    store: &Offloaded,
    path: &str,
    size: Option<u32>,
) -> Result<String, AgentError> {
    validate_path(path)?;
    let size = size.unwrap_or(256).clamp(32, 1024).to_string();
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_bytes(
            &token,
            "/thumb",
            &[("path", path), ("size", size.as_str())],
            MAX_THUMB_BYTES,
        )
        .await
        .map(|b| encode_mime(path, &b));
    settle(dir, store, &token, "thumbnail", r).await
}

/// Sends a Wake-on-LAN packet from the agent to a sleeping computer on its network.
pub async fn wake(dir: &Path, store: &Offloaded, mac: &str) -> Result<(), AgentError> {
    let parts: Vec<&str> = mac.split([':', '-']).collect();
    let ok = parts.len() == 6
        && parts
            .iter()
            .all(|p| p.len() == 2 && p.chars().all(|c| c.is_ascii_hexdigit()));
    if !ok {
        return Err(local("a MAC address looks like aa:bb:cc:dd:ee:ff"));
    }
    let (client, token) = session(dir, store).await?;
    let r = client
        .authed_empty(
            &token,
            Method::POST,
            "/wol",
            &[],
            Some(json!({ "mac": mac })),
        )
        .await;
    settle(dir, store, &token, "wake", r).await
}

// ---------------------------------------------------------------------------------------------
// Commands (registered in lib.rs)
// ---------------------------------------------------------------------------------------------

fn keystore(host: Option<String>) -> Offloaded {
    Offloaded::os_keystore().scoped(host)
}

fn data_dir(app: &tauri::AppHandle) -> Result<std::path::PathBuf, String> {
    use tauri::Manager;
    app.path().app_data_dir().map_err(|e| e.to_string())
}

macro_rules! run {
    ($app:expr, $host:expr, |$d:ident, $s:ident| $body:expr) => {{
        let $d = data_dir(&$app)?;
        let $s = keystore($host);
        $body.await.map_err(|e| user_message(&e))
    }};
}

#[tauri::command]
pub async fn files_search(
    app: tauri::AppHandle,
    host: Option<String>,
    options: SearchOpts,
) -> Result<Vec<FileEntry>, String> {
    run!(app, host, |d, s| search(&d, &s, &options))
}

#[tauri::command]
pub async fn files_recent(
    app: tauri::AppHandle,
    host: Option<String>,
    root: Option<String>,
    limit: Option<u32>,
) -> Result<Vec<FileEntry>, String> {
    run!(app, host, |d, s| recent(&d, &s, root.as_deref(), limit))
}

#[tauri::command]
pub async fn files_copy(
    app: tauri::AppHandle,
    host: Option<String>,
    sources: Vec<String>,
    dest_dir: String,
    duplicate: bool,
    overwrite: bool,
) -> Result<(), String> {
    run!(app, host, |d, s| transfer_op(
        &d, &s, false, &sources, &dest_dir, duplicate, overwrite
    ))
}

#[tauri::command]
pub async fn files_move(
    app: tauri::AppHandle,
    host: Option<String>,
    sources: Vec<String>,
    dest_dir: String,
    duplicate: bool,
    overwrite: bool,
) -> Result<(), String> {
    run!(app, host, |d, s| transfer_op(
        &d, &s, true, &sources, &dest_dir, duplicate, overwrite
    ))
}

#[tauri::command]
pub async fn files_create_file(
    app: tauri::AppHandle,
    host: Option<String>,
    parent: String,
    name: String,
) -> Result<(), String> {
    validate_path(&parent).map_err(|e| user_message(&e))?;
    validate_name(&name).map_err(|e| user_message(&e))?;
    let path = format!("{}/{}", parent.trim_end_matches(['/', '\\']), name);
    run!(app, host, |d, s| create_file(&d, &s, &path))
}

#[tauri::command]
pub async fn files_compress(
    app: tauri::AppHandle,
    host: Option<String>,
    sources: Vec<String>,
    dest: String,
) -> Result<(), String> {
    run!(app, host, |d, s| compress(&d, &s, &sources, &dest))
}

#[tauri::command]
pub async fn files_extract(
    app: tauri::AppHandle,
    host: Option<String>,
    archive: String,
    dest_dir: String,
) -> Result<FileEntry, String> {
    run!(app, host, |d, s| extract(&d, &s, &archive, &dest_dir))
}

#[tauri::command]
pub async fn files_archive_list(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    limit: Option<u32>,
) -> Result<Vec<ArchiveEntry>, String> {
    run!(app, host, |d, s| archive_list(&d, &s, &path, limit))
}

#[tauri::command]
pub async fn files_checksum(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    algo: String,
) -> Result<Checksum, String> {
    run!(app, host, |d, s| checksum(&d, &s, &path, &algo))
}

#[tauri::command]
pub async fn files_checksums(
    app: tauri::AppHandle,
    host: Option<String>,
    paths: Vec<String>,
    algo: String,
) -> Result<Vec<HashRow>, String> {
    run!(app, host, |d, s| checksums(&d, &s, &paths, &algo))
}

#[tauri::command]
pub async fn files_chmod(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    mode: String,
) -> Result<FileEntry, String> {
    run!(app, host, |d, s| chmod(&d, &s, &path, &mode))
}

#[tauri::command]
pub async fn share_mint(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    expires_in_seconds: u32,
) -> Result<ShareLink, String> {
    run!(app, host, |d, s| mint_share(
        &d,
        &s,
        &path,
        expires_in_seconds
    ))
}

#[tauri::command]
pub async fn share_links(
    app: tauri::AppHandle,
    host: Option<String>,
) -> Result<Vec<ShareSummary>, String> {
    run!(app, host, |d, s| share_list(&d, &s))
}

#[tauri::command]
pub async fn share_revoke_link(
    app: tauri::AppHandle,
    host: Option<String>,
    token_hash: String,
) -> Result<(), String> {
    run!(app, host, |d, s| share_revoke(&d, &s, &token_hash))
}

#[tauri::command]
pub async fn trash_items(
    app: tauri::AppHandle,
    host: Option<String>,
) -> Result<Vec<TrashEntry>, String> {
    run!(app, host, |d, s| trash_list(&d, &s))
}

#[tauri::command]
pub async fn trash_restore_items(
    app: tauri::AppHandle,
    host: Option<String>,
    ids: Vec<String>,
) -> Result<(), String> {
    run!(app, host, |d, s| trash_restore(&d, &s, &ids))
}

#[tauri::command]
pub async fn trash_empty_items(
    app: tauri::AppHandle,
    host: Option<String>,
    ids: Vec<String>,
) -> Result<(), String> {
    run!(app, host, |d, s| trash_empty(&d, &s, &ids))
}

#[tauri::command]
pub async fn files_read_text(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
) -> Result<TextFile, String> {
    run!(app, host, |d, s| read_text(&d, &s, &path))
}

#[tauri::command]
pub async fn files_write_text(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    text: String,
    base_modified: String,
) -> Result<FileEntry, String> {
    run!(app, host, |d, s| write_text(
        &d,
        &s,
        &path,
        &text,
        &base_modified
    ))
}

#[tauri::command]
pub async fn files_thumb(
    app: tauri::AppHandle,
    host: Option<String>,
    path: String,
    size: Option<u32>,
) -> Result<String, String> {
    run!(app, host, |d, s| thumb(&d, &s, &path, size))
}

#[tauri::command]
pub async fn wake_computer(
    app: tauri::AppHandle,
    host: Option<String>,
    mac: String,
) -> Result<(), String> {
    run!(app, host, |d, s| wake(&d, &s, &mac))
}
