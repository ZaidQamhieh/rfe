//! This computer's own files, for the "Local files" pane: where uploads come from. Listing and the
//! few changes the pane offers (new folder, rename, delete) act on paths the user picked in the
//! window, as the user, exactly like a file manager.
//!
//! These are *local* paths, so unlike `files.rs` they are real [`Path`]s. They must be absolute and
//! hold no `..` step; a refusal comes from the operating system and is shown as it words it. Nothing
//! here reads a file's content; uploads go through `transfers.rs`, which opens the file itself.

use crate::files::{validate_name, FileEntry, Page};
use serde::Serialize;
use std::path::{Component, Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

const MAX_ENTRIES: usize = 50_000;

#[derive(Debug, Clone, Serialize)]
pub struct Place {
    pub label: String,
    pub path: String,
    /// `home`, `folder` or `drive`.
    pub kind: &'static str,
}

fn check(path: &str) -> Result<PathBuf, String> {
    let p = Path::new(path);
    let bad = path.is_empty()
        || path.len() > 4096
        || path.contains('\0')
        || !p.is_absolute()
        || p.components().any(|c| matches!(c, Component::ParentDir));
    if bad {
        return Err("a path must be absolute, with no .. step".into());
    }
    Ok(p.to_path_buf())
}

fn text(p: &Path) -> String {
    p.to_string_lossy().into_owned()
}

/// `1970-01-01T00:00:00Z` for a Unix time (no time zone database needed: the agent's listings are
/// UTC too).
fn rfc3339(secs: i64) -> String {
    let days = secs.div_euclid(86_400);
    let rem = secs.rem_euclid(86_400);
    // Days since 1970-01-01 to a civil date (Howard Hinnant's algorithm).
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    format!(
        "{y:04}-{m:02}-{d:02}T{:02}:{:02}:{:02}Z",
        rem / 3600,
        rem % 3600 / 60,
        rem % 60
    )
}

fn stamp(t: std::io::Result<SystemTime>) -> String {
    match t.ok().and_then(|t| t.duration_since(UNIX_EPOCH).ok()) {
        Some(d) => rfc3339(d.as_secs() as i64),
        None => String::new(),
    }
}

/// `drwxr-xr-x`, the same text the agent sends for its own entries.
#[cfg(unix)]
fn mode_text(meta: &std::fs::Metadata) -> String {
    use std::os::unix::fs::PermissionsExt;
    let m = meta.permissions().mode();
    let kind = if meta.is_dir() {
        'd'
    } else if meta.file_type().is_symlink() {
        'L'
    } else {
        '-'
    };
    let bit = |mask: u32, c: char| if m & mask != 0 { c } else { '-' };
    format!(
        "{kind}{}{}{}{}{}{}{}{}{}",
        bit(0o400, 'r'),
        bit(0o200, 'w'),
        bit(0o100, 'x'),
        bit(0o040, 'r'),
        bit(0o020, 'w'),
        bit(0o010, 'x'),
        bit(0o004, 'r'),
        bit(0o002, 'w'),
        bit(0o001, 'x'),
    )
}

#[cfg(not(unix))]
fn mode_text(meta: &std::fs::Metadata) -> String {
    if meta.permissions().readonly() {
        "-r--r--r--".into()
    } else {
        "-rw-rw-rw-".into()
    }
}

fn entry(path: &Path, meta: &std::fs::Metadata, is_dir: bool, link: bool) -> FileEntry {
    FileEntry {
        name: path
            .file_name()
            .map(|n| n.to_string_lossy().into_owned())
            .unwrap_or_else(|| text(path)),
        path: text(path),
        is_dir,
        size: if is_dir { 0 } else { meta.len() as i64 },
        mime_type: String::new(),
        mode: mode_text(meta),
        modified: stamp(meta.modified()),
        created: stamp(meta.created()),
        is_symlink: link,
        symlink_target: String::new(),
        child_count: None,
    }
}

fn crumbs(path: &Path) -> Vec<crate::files::Crumb> {
    let mut out = Vec::new();
    let mut acc = PathBuf::new();
    for c in path.components() {
        acc.push(c);
        let label = match c {
            Component::RootDir => "/".to_string(),
            Component::Prefix(p) => p.as_os_str().to_string_lossy().into_owned(),
            other => other.as_os_str().to_string_lossy().into_owned(),
        };
        out.push(crate::files::Crumb {
            label,
            path: text(&acc),
        });
    }
    out
}

/// The folders a person starts from: home, the usual folders that exist, and the drives.
pub fn places(app: &tauri::AppHandle) -> Vec<Place> {
    use tauri::Manager;
    let r = app.path();
    let mut out: Vec<Place> = Vec::new();
    let mut add = |label: &str, p: Option<PathBuf>, kind: &'static str| {
        if let Some(p) = p.filter(|p| p.is_dir()) {
            let path = text(&p);
            if !out.iter().any(|x| x.path == path) {
                out.push(Place {
                    label: label.into(),
                    path,
                    kind,
                });
            }
        }
    };
    add("Home", r.home_dir().ok(), "home");
    add("Desktop", r.desktop_dir().ok(), "folder");
    add("Documents", r.document_dir().ok(), "folder");
    add("Downloads", r.download_dir().ok(), "folder");
    add("Pictures", r.picture_dir().ok(), "folder");
    add("Music", r.audio_dir().ok(), "folder");
    add("Videos", r.video_dir().ok(), "folder");
    #[cfg(unix)]
    add("Computer", Some(PathBuf::from("/")), "drive");
    #[cfg(windows)]
    for letter in b'A'..=b'Z' {
        add(
            &format!("{}:", letter as char),
            Some(PathBuf::from(format!("{}:\\", letter as char))),
            "drive",
        );
    }
    out
}

pub fn list(path: &str) -> Result<Page, String> {
    let dir = check(path)?;
    let rd = std::fs::read_dir(&dir).map_err(|e| format!("cannot open {}: {e}", dir.display()))?;
    let mut entries = Vec::new();
    for item in rd.take(MAX_ENTRIES) {
        let Ok(item) = item else { continue };
        let p = item.path();
        let Ok(lmeta) = std::fs::symlink_metadata(&p) else {
            continue;
        };
        let link = lmeta.file_type().is_symlink();
        // A link shows what it leads to; a broken one shows as itself.
        let (meta, is_dir) = match std::fs::metadata(&p) {
            Ok(m) => {
                let d = m.is_dir();
                (m, d)
            }
            Err(_) => (lmeta, false),
        };
        entries.push(entry(&p, &meta, is_dir, link));
    }
    Ok(Page {
        path: text(&dir),
        crumbs: crumbs(&dir),
        entries,
        next_cursor: None,
    })
}

pub fn create_folder(parent: &str, name: &str) -> Result<FileEntry, String> {
    let parent = check(parent)?;
    validate_name(name).map_err(|e| e.to_string())?;
    let p = parent.join(name);
    std::fs::create_dir(&p).map_err(|e| format!("cannot create {}: {e}", p.display()))?;
    let meta = std::fs::metadata(&p).map_err(|e| e.to_string())?;
    Ok(entry(&p, &meta, true, false))
}

/// Renames without replacing something created at `to` after the check above (audit F-102). A plain `rename`
/// replaces silently on Unix; a hard link refuses an existing name, so a file is linked in and the old name
/// dropped. Folders cannot be linked, and a file system without links (FAT) falls back to the plain rename.
fn rename_no_replace(from: &Path, to: &Path) -> std::io::Result<()> {
    // Linux only: link(2) does not follow a symlink there, so a renamed link stays a link. Elsewhere (macOS and
    // the BSDs may follow it) the plain rename below is used.
    #[cfg(target_os = "linux")]
    {
        let is_dir = std::fs::symlink_metadata(from)
            .map(|m| m.is_dir())
            .unwrap_or(false);
        if !is_dir && from != to {
            match std::fs::hard_link(from, to) {
                Ok(()) => return std::fs::remove_file(from),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => return Err(e),
                Err(_) => {}
            }
        }
    }
    std::fs::rename(from, to)
}

pub fn rename(path: &str, new_name: &str) -> Result<FileEntry, String> {
    let from = check(path)?;
    validate_name(new_name).map_err(|e| e.to_string())?;
    let to = from
        .parent()
        .ok_or_else(|| "this item cannot be renamed".to_string())?
        .join(new_name);
    if to != from && std::fs::symlink_metadata(&to).is_ok() {
        return Err(format!("“{new_name}” already exists"));
    }
    rename_no_replace(&from, &to).map_err(|e| {
        if e.kind() == std::io::ErrorKind::AlreadyExists {
            format!("“{new_name}” already exists")
        } else {
            format!("cannot rename {}: {e}", from.display())
        }
    })?;
    // The rename already happened, so a link that leads nowhere is described as itself
    // instead of turning a successful rename into an error.
    let (meta, is_dir, link) = match std::fs::metadata(&to) {
        Ok(m) => {
            let d = m.is_dir();
            let l = std::fs::symlink_metadata(&to)
                .map(|x| x.file_type().is_symlink())
                .unwrap_or(false);
            (m, d, l)
        }
        Err(_) => (
            std::fs::symlink_metadata(&to).map_err(|e| e.to_string())?,
            false,
            true,
        ),
    };
    Ok(entry(&to, &meta, is_dir, link))
}

/// Deletes for good. The window asks first, naming what will go.
pub fn delete(path: &str) -> Result<(), String> {
    let p = check(path)?;
    if p.parent().is_none() {
        return Err("a drive or the root cannot be deleted".into());
    }
    let meta =
        std::fs::symlink_metadata(&p).map_err(|e| format!("cannot find {}: {e}", p.display()))?;
    let r = if meta.is_dir() {
        std::fs::remove_dir_all(&p)
    } else {
        std::fs::remove_file(&p)
    };
    r.map_err(|e| format!("cannot delete {}: {e}", p.display()))
}

/// A small text file on this computer, for the preview.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LocalText {
    pub path: String,
    pub text: String,
    pub modified: String,
    pub size: usize,
}

/// The most text a preview reads, the same limit as for a file on a server.
pub const MAX_TEXT_BYTES: u64 = 1 << 20;

/// Reads a text file of up to `MAX_TEXT_BYTES`; anything else says why it cannot be shown.
pub fn read_text(path: &str) -> Result<LocalText, String> {
    use std::io::Read;
    let p = check(path)?;
    let meta = std::fs::metadata(&p).map_err(|e| format!("Could not read {path}: {e}"))?;
    if !meta.is_file() {
        return Err("This is not a file.".into());
    }
    if meta.len() > MAX_TEXT_BYTES {
        return Err("This file is too large to show as text (over 1 MB).".into());
    }
    let mut bytes = Vec::new();
    std::fs::File::open(&p)
        .and_then(|f| f.take(MAX_TEXT_BYTES + 1).read_to_end(&mut bytes))
        .map_err(|e| format!("Could not read {path}: {e}"))?;
    let text = String::from_utf8(bytes).map_err(|_| "This file is not text.".to_string())?;
    if text.contains('\0') {
        return Err("This file is not text.".into());
    }
    Ok(LocalText {
        path: path.to_string(),
        size: text.len(),
        text,
        modified: stamp(meta.modified()),
    })
}

/// The largest picture a preview reads from this computer.
pub const MAX_IMAGE_BYTES: u64 = 8 << 20;

/// A picture on this computer as a `data:` URL for the preview.
pub fn read_image(path: &str) -> Result<String, String> {
    use std::io::Read;
    let p = check(path)?;
    let meta = std::fs::metadata(&p).map_err(|e| format!("Could not read {path}: {e}"))?;
    if !meta.is_file() {
        return Err("This is not a file.".into());
    }
    if meta.len() > MAX_IMAGE_BYTES {
        return Err("This picture is too large to preview (over 8 MB).".into());
    }
    let mut bytes = Vec::new();
    std::fs::File::open(&p)
        .and_then(|f| f.take(MAX_IMAGE_BYTES + 1).read_to_end(&mut bytes))
        .map_err(|e| format!("Could not read {path}: {e}"))?;
    Ok(crate::fileops::encode_mime(path, &bytes))
}

/// Endings that start a program when opened. The window opens documents and folders with the
/// system's default app; it does not run programs.
const PROGRAMS: &[&str] = &[
    "exe", "bat", "cmd", "com", "msi", "scr", "ps1", "vbs", "js", "jar", "sh", "run", "appimage",
    "desktop", "lnk", "app", "command", "pif", "reg", "dll",
];

/// Extensions of plain data. A file system without permissions (FAT, exFAT, some network shares) shows
/// every file as executable, so the executable bit alone must not stop a photo or a document from opening.
const DATA: &[&str] = &[
    "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "heic", "tif", "tiff", "mp3", "flac", "wav",
    "ogg", "m4a", "aac", "mp4", "mkv", "mov", "avi", "webm", "m4v", "pdf", "txt", "md", "csv",
    "doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "rtf", "zip", "7z", "rar",
    "tar", "gz",
];

fn starts_program(ext: &str, exec_bit: bool) -> bool {
    PROGRAMS.contains(&ext) || (exec_bit && !DATA.contains(&ext))
}

/// Whether opening a file called `name` could start a program. Windows ignores trailing dots and spaces in a
/// name (`evil.exe.` is `evil.exe`) and reads `name:stream` as an alternate data stream, so on Windows those are
/// stripped or refused before the ending is looked at (audit F-101).
fn name_starts_program(name: &str, exec_bit: bool, windows: bool) -> bool {
    let mut n = name;
    if windows {
        if n.contains(':') {
            return true;
        }
        n = n.trim_end_matches(['.', ' ']);
    }
    let ext = std::path::Path::new(n)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    starts_program(&ext, exec_bit)
}

/// Opens a file or folder on this computer with the system's default app.
pub fn open(path: &str) -> Result<(), String> {
    let p = check(path)?;
    let meta = std::fs::metadata(&p).map_err(|e| format!("Could not open {path}: {e}"))?;
    if meta.is_file() {
        // A link is judged by its own name and by the file it leads to: the name alone let `photo.jpg` pointing at
        // a program through, and the target's mode alone let a program hide behind a data name (audit F-45).
        let mut names = vec![p.file_name().map(|n| n.to_string_lossy().into_owned())];
        if let Ok(real) = std::fs::canonicalize(&p) {
            names.push(real.file_name().map(|n| n.to_string_lossy().into_owned()));
        }
        let exec = is_executable(&meta);
        if names
            .into_iter()
            .flatten()
            .any(|n| name_starts_program(&n, exec, cfg!(windows)))
        {
            return Err("This file would start a program, so it is not opened from here.".into());
        }
    }
    #[cfg(target_os = "windows")]
    let mut cmd = {
        let mut c = std::process::Command::new("explorer");
        c.arg(&p);
        c
    };
    #[cfg(target_os = "macos")]
    let mut cmd = {
        let mut c = std::process::Command::new("open");
        c.arg(&p);
        c
    };
    #[cfg(all(unix, not(target_os = "macos")))]
    let mut cmd = {
        let mut c = std::process::Command::new("xdg-open");
        c.arg(&p);
        c
    };
    cmd.stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|e| format!("Could not open {path}: {e}"))
}

#[cfg(unix)]
fn is_executable(meta: &std::fs::Metadata) -> bool {
    use std::os::unix::fs::PermissionsExt;
    meta.permissions().mode() & 0o111 != 0
}

#[cfg(not(unix))]
fn is_executable(_: &std::fs::Metadata) -> bool {
    false
}

/// Writes a new text file into a folder, under a name that is not taken yet ("a.txt", then
/// "a (1).txt", ...). Returns the path it made. Nothing is ever overwritten.
pub fn save_text(dir: &str, name: &str, body: &str) -> Result<String, String> {
    let d = check(dir)?;
    validate_name(name).map_err(|e| e.to_string())?;
    if !d.is_dir() {
        return Err("That folder does not exist on this computer.".into());
    }
    let (stem, ext) = match name.rfind('.') {
        Some(i) if i > 0 => (&name[..i], &name[i..]),
        _ => (name, ""),
    };
    for n in 0..1000u32 {
        let candidate = if n == 0 {
            name.to_string()
        } else {
            format!("{stem} ({n}){ext}")
        };
        let p = d.join(&candidate);
        match std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&p)
        {
            Ok(mut f) => {
                use std::io::Write;
                f.write_all(body.as_bytes())
                    .map_err(|e| format!("Could not write {}: {e}", p.display()))?;
                return Ok(text(&p));
            }
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(e) => return Err(format!("Could not create {}: {e}", p.display())),
        }
    }
    Err("Too many files with that name in the folder.".into())
}

/// Every file under `path` (or `path` itself when it is a file), with its path relative to the
/// parent of `path`, for uploading a folder. Links are not followed, so a loop cannot run forever.
pub fn walk(path: &str) -> Result<Vec<(PathBuf, String)>, String> {
    walk_all(path).map(|(files, _)| files)
}

/// Like [`walk`], and also every folder under `path` (the folder itself included) as a path relative
/// to the parent of `path`, so an empty folder can be made on the other side too.
/// The files of a tree (path, relative path) and its folders (relative paths).
pub type Tree = (Vec<(PathBuf, String)>, Vec<String>);

pub fn walk_all(path: &str) -> Result<Tree, String> {
    let root = check(path)?;
    let base = root
        .parent()
        .ok_or_else(|| "pick a folder or a file, not the root".to_string())?
        .to_path_buf();
    let relative = |p: &Path| -> Result<String, String> {
        Ok(p.strip_prefix(&base)
            .map_err(|e| e.to_string())?
            .components()
            .map(|c| c.as_os_str().to_string_lossy().into_owned())
            .collect::<Vec<_>>()
            .join("/"))
    };
    let mut files = Vec::new();
    let mut dirs = Vec::new();
    let mut stack = vec![root.clone()];
    while let Some(p) = stack.pop() {
        // The folder or file the user picked is followed even when it is a link (the
        // window lists it as what it leads to); links found inside it are not, so a
        // loop cannot run forever.
        let meta = if p == root {
            std::fs::metadata(&p)
        } else {
            std::fs::symlink_metadata(&p)
        }
        .map_err(|e| format!("cannot read {}: {e}", p.display()))?;
        if meta.is_dir() {
            dirs.push(relative(&p)?);
            let rd = match std::fs::read_dir(&p) {
                Ok(rd) => rd,
                // An unreadable folder fails the upload rather than being left out: a partial copy reported as
                // done would lose files without the user knowing.
                Err(e) => return Err(format!("cannot open {}: {e}", p.display())),
            };
            for item in rd.flatten() {
                stack.push(item.path());
            }
        } else if meta.is_file() {
            let rel = relative(&p)?;
            files.push((p, rel));
        }
        if files.len() + dirs.len() > MAX_ENTRIES {
            return Err("that folder holds too many files to upload at once".into());
        }
    }
    files.sort_by(|a, b| a.1.cmp(&b.1));
    // Shallowest first, so a parent is made before what is inside it.
    dirs.sort_by(|a, b| (a.matches('/').count(), a).cmp(&(b.matches('/').count(), b)));
    Ok((files, dirs))
}

const SEARCH_VISITS: usize = 200_000;

/// The kind a search type names, by the file's extension.
fn is_type(kind: &str, name: &str, is_dir: bool) -> bool {
    let ext = name.rsplit_once('.').map(|(_, e)| e.to_ascii_lowercase());
    let ext = ext.as_deref().unwrap_or("");
    match kind {
        "folder" => is_dir,
        "image" => {
            !is_dir
                && matches!(
                    ext,
                    "png"
                        | "jpg"
                        | "jpeg"
                        | "gif"
                        | "webp"
                        | "bmp"
                        | "svg"
                        | "heic"
                        | "tif"
                        | "tiff"
                )
        }
        "video" => !is_dir && matches!(ext, "mp4" | "mov" | "mkv" | "avi" | "webm" | "m4v" | "wmv"),
        "audio" => {
            !is_dir && matches!(ext, "mp3" | "flac" | "wav" | "ogg" | "m4a" | "opus" | "aac")
        }
        "archive" => {
            !is_dir
                && matches!(
                    ext,
                    "zip" | "tar" | "gz" | "tgz" | "bz2" | "xz" | "zst" | "7z" | "rar"
                )
        }
        _ => false,
    }
}

/// Entries under `root` whose name contains `query` (any case), breadth first. Links to folders are
/// not followed, and the walk stops after a fixed number of entries, so it always ends.
pub fn search(
    root: &str,
    query: &str,
    limit: usize,
    types: &[String],
    min_size: u64,
) -> Result<Vec<FileEntry>, String> {
    let root = check(root)?;
    let needle = query.trim().to_lowercase();
    if needle.is_empty() {
        return Err("type something to search for".into());
    }
    let limit = limit.clamp(1, 1000);
    let mut out = Vec::new();
    let mut queue = std::collections::VecDeque::from([root]);
    let mut visited = 0usize;
    while let Some(dir) = queue.pop_front() {
        let Ok(rd) = std::fs::read_dir(&dir) else {
            continue;
        };
        for item in rd.flatten() {
            visited += 1;
            if visited > SEARCH_VISITS {
                return Ok(out);
            }
            let p = item.path();
            let Ok(lmeta) = std::fs::symlink_metadata(&p) else {
                continue;
            };
            let link = lmeta.file_type().is_symlink();
            let name = item.file_name().to_string_lossy().into_owned();
            let (meta, is_dir) = match std::fs::metadata(&p) {
                Ok(m) => {
                    let d = m.is_dir();
                    (m, d)
                }
                Err(_) => (lmeta, false),
            };
            if is_dir && !link {
                queue.push_back(p.clone());
            }
            if !name.to_lowercase().contains(&needle) {
                continue;
            }
            if !types.is_empty() && !types.iter().any(|t| is_type(t, &name, is_dir)) {
                continue;
            }
            if min_size > 0 && (is_dir || meta.len() < min_size) {
                continue;
            }
            out.push(entry(&p, &meta, is_dir, link));
            if out.len() >= limit {
                return Ok(out);
            }
        }
    }
    Ok(out)
}

/// Searches this computer from `root` (the home folder when the window names none).
#[tauri::command]
pub async fn local_search(
    app: tauri::AppHandle,
    query: String,
    limit: Option<usize>,
    types: Option<Vec<String>>,
    min_size: Option<u64>,
    root: Option<String>,
) -> Result<Vec<FileEntry>, String> {
    use tauri::Manager;
    let root = match root.filter(|r| !r.is_empty() && r != "/") {
        Some(r) => r,
        None => text(&app.path().home_dir().map_err(|e| e.to_string())?),
    };
    tokio::task::spawn_blocking(move || {
        search(
            &root,
            &query,
            limit.unwrap_or(200),
            &types.unwrap_or_default(),
            min_size.unwrap_or(0),
        )
    })
    .await
    .map_err(|e| e.to_string())?
}

#[tauri::command]
pub async fn local_places(app: tauri::AppHandle) -> Result<Vec<Place>, String> {
    Ok(places(&app))
}

#[tauri::command]
pub async fn local_list(path: String) -> Result<Page, String> {
    tokio::task::spawn_blocking(move || list(&path))
        .await
        .map_err(|e| e.to_string())?
}

#[tauri::command]
pub async fn local_create_folder(parent: String, name: String) -> Result<FileEntry, String> {
    create_folder(&parent, &name)
}

#[tauri::command]
pub async fn local_rename(path: String, new_name: String) -> Result<FileEntry, String> {
    rename(&path, &new_name)
}

#[tauri::command]
pub async fn local_delete(path: String) -> Result<(), String> {
    tokio::task::spawn_blocking(move || delete(&path))
        .await
        .map_err(|e| e.to_string())?
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dates_are_rfc3339_utc() {
        assert_eq!(rfc3339(0), "1970-01-01T00:00:00Z");
        assert_eq!(rfc3339(1_791_900_000), "2026-10-13T14:00:00Z");
        assert_eq!(rfc3339(951_782_400), "2000-02-29T00:00:00Z");
    }

    #[test]
    fn the_executable_bit_alone_does_not_block_a_document() {
        assert!(starts_program("sh", false));
        assert!(starts_program("", true));
        assert!(starts_program("bin", true));
        assert!(!starts_program("jpg", true));
        assert!(!starts_program("pdf", true));
        assert!(!starts_program("txt", false));
    }

    #[test]
    fn names_that_windows_reads_as_programs_are_refused() {
        assert!(name_starts_program("evil.exe.", false, true));
        assert!(name_starts_program("evil.exe ", false, true));
        assert!(name_starts_program("evil.exe::$DATA", false, true));
        assert!(
            !name_starts_program("evil.exe.", false, false),
            "a Linux name is taken as it is"
        );
        assert!(name_starts_program("run.sh", false, false));
        assert!(!name_starts_program("photo.jpg", true, false));
    }

    #[cfg(unix)]
    #[test]
    fn a_rename_never_replaces_an_existing_file() {
        let t = tempfile::tempdir().unwrap();
        let (a, b) = (t.path().join("a.txt"), t.path().join("b.txt"));
        std::fs::write(&a, b"a").unwrap();
        std::fs::write(&b, b"b").unwrap();
        let err = rename_no_replace(&a, &b).unwrap_err();
        assert_eq!(err.kind(), std::io::ErrorKind::AlreadyExists);
        assert_eq!(std::fs::read(&b).unwrap(), b"b");
        let c = t.path().join("c.txt");
        rename_no_replace(&a, &c).unwrap();
        assert!(!a.exists());
        assert_eq!(std::fs::read(&c).unwrap(), b"a");
    }

    #[cfg(unix)]
    #[test]
    fn a_link_to_a_program_is_not_opened() {
        let t = tempfile::tempdir().unwrap();
        let target = t.path().join("tool.sh");
        std::fs::write(&target, b"#!/bin/sh\n").unwrap();
        let link = t.path().join("photo.jpg");
        std::os::unix::fs::symlink(&target, &link).unwrap();
        let err = open(link.to_str().unwrap()).unwrap_err();
        assert!(err.contains("start a program"), "{err}");
    }

    #[test]
    fn thumbnails_name_gif_and_svg_images() {
        let mime = |p: &str, b: &[u8]| crate::fileops::encode_mime(p, b);
        assert!(mime("a.gif", b"GIF89a..").starts_with("data:image/gif;"));
        assert!(mime("a.svg", b"<svg/>").starts_with("data:image/svg+xml;"));
        assert!(mime("a.png", &[0x89, b'P', b'N', b'G']).starts_with("data:image/png;"));
    }

    #[test]
    fn a_path_must_be_absolute_and_free_of_parent_steps() {
        assert!(check("relative/x").is_err());
        assert!(check("/a/../b").is_err());
        assert!(check("").is_err());
        assert!(check("/a/b").is_ok());
    }

    #[test]
    fn folders_files_and_renames_work_on_a_real_directory() {
        let t = tempfile::tempdir().unwrap();
        let root = t.path().to_str().unwrap().to_string();
        let f = create_folder(&root, "docs").unwrap();
        assert!(f.is_dir && f.name == "docs");
        assert!(
            create_folder(&root, "docs").is_err(),
            "an existing name is refused"
        );
        std::fs::write(t.path().join("docs/a.txt"), b"hello").unwrap();
        let page = list(&root).unwrap();
        assert_eq!(page.entries.len(), 1);
        let r = rename(&f.path, "papers").unwrap();
        assert_eq!(r.name, "papers");
        let walked = walk(&r.path).unwrap();
        assert_eq!(walked.len(), 1);
        assert_eq!(walked[0].1, "papers/a.txt");
        delete(&r.path).unwrap();
        assert!(list(&root).unwrap().entries.is_empty());
    }

    #[cfg(unix)]
    #[test]
    fn walking_follows_a_linked_root_and_skips_an_unreadable_subfolder() {
        use std::os::unix::fs::{symlink, PermissionsExt};
        let t = tempfile::tempdir().unwrap();
        let real = t.path().join("real");
        std::fs::create_dir_all(real.join("open")).unwrap();
        std::fs::create_dir_all(real.join("locked")).unwrap();
        std::fs::write(real.join("open/a.txt"), b"a").unwrap();
        std::fs::write(real.join("locked/b.txt"), b"b").unwrap();
        symlink(&real, t.path().join("link")).unwrap();
        let linked = walk_all(t.path().join("link").to_str().unwrap()).unwrap();
        assert!(!linked.0.is_empty(), "a linked root yields its files");
        std::fs::set_permissions(real.join("locked"), std::fs::Permissions::from_mode(0o000))
            .unwrap();
        let unreadable = std::fs::read_dir(real.join("locked")).is_err();
        let walked = walk_all(real.to_str().unwrap());
        std::fs::set_permissions(real.join("locked"), std::fs::Permissions::from_mode(0o755))
            .unwrap();
        if unreadable {
            let e =
                walked.expect_err("an unreadable subfolder must fail the upload, not be dropped");
            assert!(e.contains("locked"), "{e}");
        }
    }

    #[cfg(unix)]
    #[test]
    fn renaming_a_dangling_link_succeeds() {
        let t = tempfile::tempdir().unwrap();
        let l = t.path().join("dead");
        std::os::unix::fs::symlink("/nonexistent-target", &l).unwrap();
        let r = rename(l.to_str().unwrap(), "dead2").unwrap();
        assert_eq!(r.name, "dead2");
    }
}

#[tauri::command]
pub async fn local_read_text(path: String) -> Result<LocalText, String> {
    tokio::task::spawn_blocking(move || read_text(&path))
        .await
        .map_err(|e| e.to_string())?
}

#[tauri::command]
pub async fn local_read_image(path: String) -> Result<String, String> {
    tokio::task::spawn_blocking(move || read_image(&path))
        .await
        .map_err(|e| e.to_string())?
}

#[tauri::command]
pub async fn local_open(path: String) -> Result<(), String> {
    tokio::task::spawn_blocking(move || open(&path))
        .await
        .map_err(|e| e.to_string())?
}

#[tauri::command]
pub async fn local_save_text(dir: String, name: String, body: String) -> Result<String, String> {
    tokio::task::spawn_blocking(move || save_text(&dir, &name, &body))
        .await
        .map_err(|e| e.to_string())?
}
