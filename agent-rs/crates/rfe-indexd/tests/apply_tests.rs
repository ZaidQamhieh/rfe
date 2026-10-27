//! Folding changes into an index must give exactly what a fresh build of the same tree gives.

use rfe_indexd::filter::{Compiled, Filters};
use rfe_indexd::index::{Index, Limits};
use std::fs;
use std::path::Path;
use std::sync::atomic::AtomicBool;

fn limits() -> Limits {
    Limits {
        max_entries: 1_000_000,
        max_bytes: 1 << 30,
    }
}

fn build(root: &Path) -> Index {
    Index::build(
        &[root.to_str().unwrap().to_string()],
        limits(),
        &AtomicBool::new(false),
    )
}

fn everything(idx: &Index, root: &Path) -> Vec<rfe_indexd::entry::WireEntry> {
    idx.query(
        &Compiled::new(Filters::default()),
        &[root.to_str().unwrap().to_string()],
        usize::MAX,
    )
    .0
}

fn touch(p: &Path, body: &str) {
    fs::create_dir_all(p.parent().unwrap()).unwrap();
    fs::write(p, body).unwrap();
}

fn s(p: &Path) -> String {
    p.to_str().unwrap().to_string()
}

fn apply(idx: &Index, dirty: &[&Path]) -> Index {
    let dirty: Vec<String> = dirty.iter().map(|p| s(p)).collect();
    idx.apply(&dirty, &AtomicBool::new(false))
        .expect("update applies")
        .index
}

fn assert_same(updated: &Index, root: &Path, what: &str) {
    let fresh = build(root);
    let (a, b) = (everything(updated, root), everything(&fresh, root));
    let names =
        |v: &[rfe_indexd::entry::WireEntry]| v.iter().map(|e| e.path.clone()).collect::<Vec<_>>();
    assert_eq!(names(&a), names(&b), "{what}: entries differ");
    assert_eq!(a, b, "{what}: metadata differs");
    assert_eq!(updated.stats.entries, fresh.stats.entries, "{what}: count");
}

fn fixture() -> (tempfile::TempDir, std::path::PathBuf) {
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    touch(&root.join("a.txt"), "a");
    touch(&root.join("docs/readme.md"), "hello");
    touch(&root.join("docs/deep/er/file.bin"), "xx");
    touch(&root.join("src/main.rs"), "fn main(){}");
    touch(&root.join(".hidden/x"), "x");
    (t, root)
}

#[test]
fn file_created_modified_and_deleted() {
    let (_t, root) = fixture();
    let mut idx = build(&root);

    touch(&root.join("docs/new.txt"), "new");
    idx = apply(&idx, &[&root.join("docs")]);
    assert_same(&idx, &root, "create");

    fs::write(root.join("docs/new.txt"), "a much longer body than before").unwrap();
    idx = apply(&idx, &[&root.join("docs")]);
    assert_same(&idx, &root, "modify");

    fs::remove_file(root.join("docs/new.txt")).unwrap();
    idx = apply(&idx, &[&root.join("docs")]);
    assert_same(&idx, &root, "delete");
}

#[test]
fn directories_created_removed_renamed_and_moved() {
    let (_t, root) = fixture();
    let mut idx = build(&root);

    touch(&root.join("new/sub/a.txt"), "1");
    touch(&root.join("new/b.txt"), "2");
    idx = apply(&idx, &[&root]);
    assert_same(&idx, &root, "new subtree");

    // Events inside a subtree the same batch walks fresh are harmless.
    idx = apply(&idx, &[&root.join("new"), &root.join("new/sub"), &root]);
    assert_same(&idx, &root, "redundant dirty dirs");

    fs::rename(root.join("docs"), root.join("manuals")).unwrap();
    idx = apply(&idx, &[&root]);
    assert_same(&idx, &root, "dir renamed");
    assert!(everything(&idx, &root)
        .iter()
        .any(|e| e.path.ends_with("manuals/deep/er/file.bin")));

    fs::rename(root.join("manuals/deep"), root.join("src/deep")).unwrap();
    idx = apply(&idx, &[&root.join("manuals"), &root.join("src")]);
    assert_same(&idx, &root, "dir moved");

    fs::remove_dir_all(root.join("src")).unwrap();
    idx = apply(&idx, &[&root]);
    assert_same(&idx, &root, "tree removed");
    assert!(!everything(&idx, &root)
        .iter()
        .any(|e| e.path.contains("/src")));

    // A dirty directory that is already gone resolves through its parent.
    touch(&root.join("tmp/x/y.txt"), "y");
    idx = apply(&idx, &[&root]);
    fs::remove_dir_all(root.join("tmp")).unwrap();
    idx = apply(&idx, &[&root.join("tmp/x")]);
    assert_same(&idx, &root, "dirty child of a removed dir");
}

#[test]
fn symlinks_and_permissions() {
    use std::os::unix::fs::{symlink, PermissionsExt};
    let (_t, root) = fixture();
    let mut idx = build(&root);

    symlink("docs", root.join("alias")).unwrap();
    symlink("/etc/hostname", root.join("abs")).unwrap();
    idx = apply(&idx, &[&root]);
    assert_same(&idx, &root, "symlinks");

    fs::set_permissions(root.join("a.txt"), fs::Permissions::from_mode(0o600)).unwrap();
    idx = apply(&idx, &[&root]);
    assert_same(&idx, &root, "chmod");
    let a = everything(&idx, &root)
        .into_iter()
        .find(|e| e.path.ends_with("/a.txt"))
        .unwrap();
    assert_eq!(a.mode & 0o777, 0o600);
}

#[test]
fn hidden_and_unknown_directories_are_ignored() {
    let (_t, root) = fixture();
    let idx = build(&root);
    touch(&root.join(".hidden/more"), "m");
    touch(&root.join("node_modules/p/i.js"), "j");
    let idx = apply(
        &idx,
        &[
            &root.join(".hidden"),
            &root.join("node_modules/p"),
            Path::new("/not/under/any/root"),
        ],
    );
    assert_same(&idx, &root, "pruned dirs");
}

#[test]
fn a_truncated_or_overgrown_index_asks_for_a_rebuild() {
    let (_t, root) = fixture();
    let tiny = Index::build(
        &[s(&root)],
        Limits {
            max_entries: 3,
            max_bytes: 1 << 20,
        },
        &AtomicBool::new(false),
    );
    assert!(tiny.stats.truncated);
    assert!(tiny.apply(&[s(&root)], &AtomicBool::new(false)).is_none());

    let full = build(&root);
    let cap = Index::build(
        &[s(&root)],
        Limits {
            max_entries: full.stats.entries + 1,
            max_bytes: 1 << 20,
        },
        &AtomicBool::new(false),
    );
    touch(&root.join("one.txt"), "1");
    touch(&root.join("two.txt"), "2");
    assert!(
        cap.apply(&[s(&root)], &AtomicBool::new(false)).is_none(),
        "growing past the budget needs a rebuild"
    );
}

#[test]
fn unchanged_chunks_are_shared_not_copied() {
    let (_t, root) = fixture();
    let idx = build(&root);
    let before = idx.stats.bytes;
    touch(&root.join("src/extra.rs"), "x");
    let idx2 = apply(&idx, &[&root.join("src")]);
    assert!(idx2.stats.bytes >= before);
    assert_same(&idx2, &root, "one dir changed");
}
