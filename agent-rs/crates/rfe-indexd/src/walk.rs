//! A parallel directory walk built on rooted directory handles.
//!
//! Every directory is opened relative to its parent's handle and never through a symlink that leaves the
//! root, so a link swapped in during the walk cannot make it list files outside the roots it was given.

use cap_std::ambient_authority;
use cap_std::fs::Dir;
use std::ffi::OsString;
use std::sync::Arc;

/// A subdirectory to descend into.
pub struct Sub {
    pub name: OsString,
    pub path: String,
    pub id: u32,
}

/// Where a visit happens: the directory's handle and absolute path, plus the walk root's handle and the
/// directory's path relative to it (`""` for the root), which symlink resolution needs.
pub struct At<'a> {
    pub root: &'a Dir,
    pub dir: &'a Dir,
    pub path: &'a str,
    pub rel: &'a str,
}

/// What a walk does with each directory. `visit` lists one directory and returns the subdirectories to recurse
/// into; it runs on rayon worker threads, so implementations must be `Sync`.
pub trait Visitor: Sync {
    fn visit(&self, at: &At, id: u32) -> Vec<Sub>;
    /// Checked before each directory is opened; true stops the walk early (cancel, budget, deadline).
    fn stop(&self) -> bool;
}

/// Walks `root` (an absolute path) in parallel. A root that cannot be opened is skipped silently.
pub fn par_walk<V: Visitor>(v: &V, root: &str, root_id: u32) {
    let Ok(dir) = Dir::open_ambient_dir(root, ambient_authority()) else {
        return;
    };
    let dir = Arc::new(dir);
    rayon::scope(|s| {
        let at = Node {
            root: Arc::clone(&dir),
            dir,
            path: root.to_string(),
            rel: String::new(),
            id: root_id,
        };
        descend(s, v, at)
    });
}

/// Walks a subdirectory that is already open inside the walk root `root` (`rel` is its path from that root), so
/// the walk stays inside the same sandbox as the walk it extends.
pub fn par_walk_sub<V: Visitor>(
    v: &V,
    root: Arc<Dir>,
    dir: Dir,
    path: String,
    rel: String,
    id: u32,
) {
    rayon::scope(|s| {
        let node = Node {
            root,
            dir: Arc::new(dir),
            path,
            rel,
            id,
        };
        descend(s, v, node)
    });
}

struct Node {
    root: Arc<Dir>,
    dir: Arc<Dir>,
    path: String,
    rel: String,
    id: u32,
}

fn descend<'s, V: Visitor>(s: &rayon::Scope<'s>, v: &'s V, node: Node) {
    if v.stop() {
        return;
    }
    let subs = v.visit(
        &At {
            root: &node.root,
            dir: &node.dir,
            path: &node.path,
            rel: &node.rel,
        },
        node.id,
    );
    for sub in subs {
        let parent = Arc::clone(&node.dir);
        let root = Arc::clone(&node.root);
        let rel = rel_join(&node.rel, &sub.name.to_string_lossy());
        s.spawn(move |s| {
            if v.stop() {
                return;
            }
            // open_dir never follows a link out of the sandbox; failures (permissions, a vanished dir) skip it.
            if let Ok(child) = parent.open_dir(&sub.name) {
                drop(parent);
                let node = Node {
                    root,
                    dir: Arc::new(child),
                    path: sub.path,
                    rel,
                    id: sub.id,
                };
                descend(s, v, node);
            }
        });
    }
}

/// Joins a root-relative directory path and a child name with `/` (cap-std accepts it on every platform).
pub fn rel_join(rel: &str, name: &str) -> String {
    if rel.is_empty() {
        name.to_string()
    } else {
        format!("{rel}/{name}")
    }
}

/// Directories a walk prunes (never the root itself): Linux pseudo-filesystems, hidden directories and
/// dependency caches. Same rule as the Go agent's `shouldSkipVirtualDir`.
pub fn should_skip_dir(name: &str, full_path: &str) -> bool {
    #[cfg(target_os = "linux")]
    if matches!(full_path, "/proc" | "/sys" | "/dev" | "/sysroot") {
        return true;
    }
    let _ = full_path;
    (name.len() > 1 && name.starts_with('.')) || name == "node_modules"
}
