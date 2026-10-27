use rfe_indexd::filter::{Compiled, Filters};
use rfe_indexd::index::{Index, Limits};
use rfe_indexd::recents;
use std::fs;
use std::path::Path;
use std::sync::atomic::AtomicBool;

fn big() -> Limits {
    Limits {
        max_entries: 1_000_000,
        max_bytes: 1 << 30,
    }
}

fn build(root: &Path) -> Index {
    Index::build(
        &[root.to_str().unwrap().to_string()],
        big(),
        &AtomicBool::new(false),
    )
}

fn all(idx: &Index, root: &Path) -> Vec<String> {
    let r = root.to_str().unwrap().to_string();
    let (entries, _) = idx.query(
        &Compiled::new(Filters::default()),
        std::slice::from_ref(&r),
        100_000,
    );
    entries
        .into_iter()
        .map(|e| {
            e.path
                .strip_prefix(&r)
                .unwrap()
                .trim_start_matches('/')
                .to_string()
        })
        .collect()
}

fn touch(p: &Path) {
    fs::create_dir_all(p.parent().unwrap()).unwrap();
    fs::write(p, b"x").unwrap();
}

#[test]
fn order_is_depth_first_by_name_like_go_walkdir() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    // "a-b" sorts after "a" but a plain byte sort of whole paths would put "a-b" before "a/c".
    touch(&r.join("a/c"));
    touch(&r.join("a-b"));
    touch(&r.join("a/b/z"));
    touch(&r.join("B"));
    touch(&r.join("zeta"));
    let idx = build(r);
    assert_eq!(
        all(&idx, r),
        vec!["B", "a", "a/b", "a/b/z", "a/c", "a-b", "zeta"]
    );
}

#[test]
fn hidden_and_dependency_directories_are_pruned_but_hidden_files_are_kept() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    touch(&r.join(".cache/x"));
    touch(&r.join("node_modules/pkg/index.js"));
    touch(&r.join(".hidden-file"));
    touch(&r.join("src/main.rs"));
    let idx = build(r);
    assert_eq!(all(&idx, r), vec![".hidden-file", "src", "src/main.rs"]);
}

#[cfg(unix)]
#[test]
fn symlinks_are_listed_but_never_followed_out_of_the_root() {
    use std::os::unix::fs::symlink;
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    let outside = t.path().join("outside");
    touch(&root.join("inside/file.txt"));
    touch(&outside.join("secret.txt"));
    symlink(&outside, root.join("escape")).unwrap();
    symlink("inside", root.join("alias")).unwrap();
    symlink("nowhere", root.join("broken")).unwrap();
    let idx = build(&root);
    let names = all(&idx, &root);
    assert!(
        names.contains(&"escape".to_string()),
        "the link itself is an entry"
    );
    assert!(
        !names.iter().any(|n| n.contains("secret.txt")),
        "nothing behind an escaping link is listed: {names:?}"
    );
    assert!(
        !names.iter().any(|n| n.starts_with("alias/")),
        "links are not traversed: {names:?}"
    );

    let r = root.to_str().unwrap().to_string();
    let (entries, _) = idx.query(&Compiled::new(Filters::default()), &[r], 1000);
    let by = |n: &str| {
        entries
            .iter()
            .find(|e| e.path.ends_with(n))
            .unwrap()
            .clone()
    };
    assert!(
        by("/alias").is_symlink && by("/alias").is_dir,
        "a link to an in-root directory reports as a directory"
    );
    assert!(
        by("/escape").is_symlink && !by("/escape").is_dir,
        "a link out of the root is not a directory"
    );
    assert_eq!(by("/broken").symlink_target, "nowhere");
    assert!(!by("/inside").is_symlink && by("/inside").is_dir);
}

#[test]
fn filters_roots_and_limit() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    touch(&r.join("docs/Report.PDF"));
    touch(&r.join("docs/notes.txt"));
    touch(&r.join("pics/a.jpg"));
    touch(&r.join("pics2/b.jpg"));
    let idx = build(r);
    let rs = r.to_str().unwrap().to_string();

    let q = |f: Filters, roots: &[String], limit| idx.query(&Compiled::new(f), roots, limit);
    let (e, _) = q(
        Filters {
            needle: "report".into(),
            ..Default::default()
        },
        std::slice::from_ref(&rs),
        10,
    );
    assert_eq!(e.len(), 1);
    assert!(e[0].path.ends_with("docs/Report.PDF"));

    let (e, _) = q(
        Filters {
            exts: Some(vec!["jpg".into()]),
            ..Default::default()
        },
        std::slice::from_ref(&rs),
        10,
    );
    assert_eq!(e.len(), 2);

    // A narrower root must not leak a sibling that only shares a name prefix ("pics" vs "pics2").
    let pics = format!("{rs}/pics");
    let (e, _) = q(
        Filters {
            exts: Some(vec!["jpg".into()]),
            ..Default::default()
        },
        &[pics],
        10,
    );
    assert_eq!(e.len(), 1);
    assert!(e[0].path.ends_with("pics/a.jpg"));

    let (e, truncated) = q(Filters::default(), &[rs], 2);
    assert_eq!(e.len(), 2);
    assert!(truncated, "hitting the limit reports truncation");
}

#[test]
fn budget_truncates_flags_it_and_stays_bounded() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    for i in 0..50 {
        touch(&r.join(format!("d{i:02}/f")));
    }
    let idx = Index::build(
        &[r.to_str().unwrap().to_string()],
        Limits {
            max_entries: 10,
            max_bytes: 1 << 30,
        },
        &AtomicBool::new(false),
    );
    assert!(idx.stats.truncated);
    assert_eq!(idx.stats.entries, 10);
    let names = all(&idx, r);
    // The walk is parallel, so which entries survive an over-budget tree is not guaranteed to match Go's
    // depth-first cut; what is guaranteed is the flag, the bound, and DFS order among the entries kept.
    let mut sorted = names.clone();
    sorted.sort();
    assert_eq!(names, sorted, "kept entries stay in name order");
}

#[test]
fn entries_cost_far_less_than_the_go_index() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    for d in 0..20 {
        for f in 0..200 {
            touch(&r.join(format!("project-{d}/src/module-{f:03}.rs")));
        }
    }
    let idx = build(r);
    let per_entry = idx.stats.bytes / idx.stats.entries;
    // The Go index spends about 394 bytes per entry (measured); this one must stay well under half of that.
    assert!(per_entry < 120, "{per_entry} bytes per entry");
}

#[test]
fn unreadable_or_missing_roots_are_skipped() {
    let idx = Index::build(
        &["/definitely/not/here".to_string()],
        big(),
        &AtomicBool::new(false),
    );
    assert_eq!(idx.stats.entries, 0);
    assert!(idx.is_empty());
}

#[test]
fn recents_returns_newest_files_only() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    let mk = |name: &str, age_secs: u64| {
        let p = r.join(name);
        touch(&p);
        let when = std::time::SystemTime::now() - std::time::Duration::from_secs(age_secs);
        fs::File::options()
            .write(true)
            .open(&p)
            .unwrap()
            .set_modified(when)
            .unwrap();
    };
    mk("old.txt", 5000);
    mk("sub/mid.txt", 3000);
    mk("new.txt", 10);
    mk(".hidden/skipped.txt", 1);
    fs::create_dir_all(r.join("emptydir")).unwrap();
    let (e, partial) = recents::scan(
        &[r.to_str().unwrap().to_string()],
        2,
        None,
        &AtomicBool::new(false),
    );
    assert!(!partial);
    let names: Vec<_> = e
        .iter()
        .map(|x| x.path.rsplit('/').next().unwrap().to_string())
        .collect();
    assert_eq!(
        names,
        vec!["new.txt", "mid.txt"],
        "newest first, directories and hidden dirs excluded"
    );
}

#[test]
fn recents_deadline_marks_a_partial_result() {
    let t = tempfile::tempdir().unwrap();
    touch(&t.path().join("a/b/c.txt"));
    let past = std::time::Instant::now() - std::time::Duration::from_secs(1);
    let (_, partial) = recents::scan(
        &[t.path().to_str().unwrap().to_string()],
        5,
        Some(past),
        &AtomicBool::new(false),
    );
    assert!(partial);
}

#[test]
fn link_targets_are_raw_and_dir_links_resolve_inside_the_root_not_the_subdir() {
    use std::os::unix::fs::symlink;
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    touch(&root.join("docs/readme.md"));
    touch(&root.join("a/x.txt"));
    // The link lives in `a` but points at a sibling directory: it stays inside the root, so it is a directory
    // link even though `a`'s own handle could not reach it.
    symlink("../docs", root.join("a/up")).unwrap();
    // An absolute target is reported as written.
    let abs = root.join("docs/readme.md");
    symlink(&abs, root.join("abs")).unwrap();

    let idx = build(&root);
    let r = root.to_str().unwrap().to_string();
    let (entries, _) = idx.query(&Compiled::new(Filters::default()), &[r], 1000);
    let by = |n: &str| entries.iter().find(|e| e.path.ends_with(n)).unwrap();
    assert!(by("/a/up").is_dir, "dir link resolved against the root");
    assert_eq!(by("/a/up").symlink_target, "../docs");
    assert_eq!(by("/abs").symlink_target, abs.to_str().unwrap());
    assert!(!by("/abs").is_dir);
}

fn age(p: &Path, age_secs: u64) {
    let when = std::time::SystemTime::now() - std::time::Duration::from_secs(age_secs);
    fs::File::options()
        .write(true)
        .open(p)
        .unwrap()
        .set_modified(when)
        .unwrap();
}

#[test]
fn index_recents_match_the_walk() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    for (name, secs) in [
        ("old.txt", 5000u64),
        ("sub/mid.txt", 3000),
        ("sub/deep/newer.txt", 500),
        ("new.txt", 10),
        (".hidden/skipped.txt", 1),
        ("node_modules/pkg/skipped.js", 2),
    ] {
        let p = r.join(name);
        touch(&p);
        age(&p, secs);
    }
    fs::create_dir_all(r.join("emptydir")).unwrap();
    #[cfg(unix)]
    {
        // Symlinks are kept (also to directories), as in the walk. One only: two made in the same instant tie on
        // mtime, and the walk breaks ties by visit order.
        std::os::unix::fs::symlink(r.join("sub"), r.join("link-to-dir")).unwrap();
    }
    let root = r.to_str().unwrap().to_string();
    let idx = build(r);
    for limit in [1usize, 2, 3, 100] {
        let (walk, partial) = recents::scan(
            std::slice::from_ref(&root),
            limit,
            None,
            &AtomicBool::new(false),
        );
        assert!(!partial);
        let from_index = idx
            .recents(std::slice::from_ref(&root), limit)
            .expect("a complete index answers recents");
        let a: Vec<_> = walk
            .iter()
            .map(|e| (&e.path, e.mtime_ns, e.is_dir))
            .collect();
        let b: Vec<_> = from_index
            .iter()
            .map(|e| (&e.path, e.mtime_ns, e.is_dir))
            .collect();
        assert_eq!(a, b, "limit {limit}: index and walk must agree");
    }
}

#[test]
fn index_recents_declines_what_it_cannot_answer_exactly() {
    let t = tempfile::tempdir().unwrap();
    let r = t.path();
    for i in 0..50 {
        touch(&r.join(format!("d{i}/f.txt")));
    }
    let root = r.to_str().unwrap().to_string();

    // Cut short by its budget: the index no longer holds every file.
    let small = Index::build(
        std::slice::from_ref(&root),
        Limits {
            max_entries: 10,
            max_bytes: 1 << 30,
        },
        &AtomicBool::new(false),
    );
    assert!(small.stats.truncated);
    assert!(small.recents(std::slice::from_ref(&root), 5).is_none());

    // A root the index does not cover.
    let idx = build(r);
    let other = tempfile::tempdir().unwrap();
    let other_root = other.path().to_str().unwrap().to_string();
    assert!(idx.recents(&[other_root], 5).is_none());

    // A folder under an indexed root is declined: symlinks resolve against the root, so a sub-folder walk could
    // answer differently.
    let sub = r.join("d3").to_str().unwrap().to_string();
    assert!(idx.recents(std::slice::from_ref(&sub), 5).is_none());
    assert_eq!(
        idx.recents(std::slice::from_ref(&root), 5).unwrap().len(),
        5
    );
}
