//! Root handling in `Index::build` / `Index::apply` and tie handling in `recents::scan`.

mod common;

use common::{touch, touch_body, TEST_LIMITS};
use rfe_indexd::filter::{Compiled, Filters};
use rfe_indexd::index::Index;
use rfe_indexd::recents;
use std::fs;
use std::path::Path;
use std::sync::atomic::AtomicBool;
use std::time::{Duration, SystemTime};

fn s(p: &Path) -> String {
    p.to_str().unwrap().to_string()
}

fn build_roots(roots: &[String]) -> Index {
    Index::build(roots, TEST_LIMITS, &AtomicBool::new(false))
}

fn paths(idx: &Index, roots: &[String]) -> Vec<String> {
    idx.query(&Compiled::new(Filters::default()), roots, usize::MAX)
        .0
        .into_iter()
        .map(|e| e.path)
        .collect()
}

#[test]
fn nested_roots_are_indexed_once_and_stay_in_step_with_updates() {
    let t = tempfile::tempdir().unwrap();
    let data = t.path().join("data");
    let photos = data.join("photos");
    touch(&data.join("a.txt"));
    touch(&photos.join("p1.jpg"));
    let roots = vec![s(&data), s(&photos)];

    let single = build_roots(&[s(&data)]);
    let idx = build_roots(&roots);
    assert_eq!(
        paths(&idx, &[s(&data)]),
        paths(&single, &[s(&data)]),
        "a nested root must not index its subtree a second time"
    );
    assert_eq!(idx.stats.entries, single.stats.entries);

    // A change inside the nested root must show up exactly once.
    touch(&photos.join("p2.jpg"));
    let updated = idx
        .apply(&[s(&photos)], &AtomicBool::new(false))
        .expect("update applies")
        .index;
    let got = paths(&updated, &[s(&data)]);
    let wanted = s(&photos.join("p2.jpg"));
    assert_eq!(got.iter().filter(|p| **p == wanted).count(), 1, "{got:?}");
    assert_eq!(
        got,
        paths(&build_roots(&[s(&data)]), &[s(&data)]),
        "updated index equals a fresh build"
    );
}

#[test]
fn duplicate_and_reordered_nested_roots_keep_the_outermost_once() {
    let t = tempfile::tempdir().unwrap();
    let data = t.path().join("data");
    touch(&data.join("photos/p.jpg"));
    touch(&t.path().join("other/o.txt"));
    let other = t.path().join("other");
    // Inner first, then outer, then a repeat of the outer, plus a disjoint root.
    let idx = build_roots(&[s(&data.join("photos")), s(&data), s(&data), s(&other)]);
    let all = paths(&idx, &[s(&data), s(&other)]);
    let mut unique = all.clone();
    unique.sort();
    unique.dedup();
    assert_eq!(all.len(), unique.len(), "no path twice: {all:?}");
    assert!(all.contains(&s(&data.join("photos/p.jpg"))));
    assert!(all.contains(&s(&other.join("o.txt"))));
}

#[test]
fn change_at_a_root_with_a_trailing_separator_is_applied() {
    let t = tempfile::tempdir().unwrap();
    let root = t.path().join("root");
    touch(&root.join("a.txt"));
    let sep = std::path::MAIN_SEPARATOR;
    let slashed = format!("{}{sep}", s(&root));
    let idx = build_roots(&[slashed]);

    touch_body(&root.join("new.txt"), "n");
    // The watcher reports the root without a trailing separator.
    let updated = idx
        .apply(&[s(&root)], &AtomicBool::new(false))
        .expect("update applies")
        .index;
    let got = paths(&updated, &[s(&root)]);
    assert!(
        got.contains(&s(&root.join("new.txt"))),
        "root-level change was dropped: {got:?}"
    );
}

/// Asserts `idx.query` over `query_roots` returns exactly `limit` distinct paths, all taken from `all`.
fn assert_limit_unique(idx: &Index, query_roots: &[String], all: &[String], limit: usize) {
    let (got, truncated) = idx.query(&Compiled::new(Filters::default()), query_roots, limit);
    let got: Vec<String> = got.into_iter().map(|e| e.path).collect();
    let mut unique = got.clone();
    unique.sort();
    unique.dedup();
    assert_eq!(
        unique.len(),
        got.len(),
        "limit {limit}: duplicate hit in {got:?}"
    );
    assert_eq!(
        got.len(),
        limit,
        "limit {limit}: short result {got:?} of {all:?}"
    );
    assert!(
        got.iter().all(|p| all.contains(p)),
        "{got:?} not within {all:?}"
    );
    assert!(
        truncated,
        "limit {limit} was reached, so the answer must say it may be cut"
    );
}

#[test]
fn overlapping_roots_never_use_up_the_limit_on_repeats() {
    let t = tempfile::tempdir().unwrap();
    let data = t.path().join("data");
    let photos = data.join("photos");
    let other = t.path().join("other");
    for i in 0..4 {
        touch(&data.join(format!("a{i}.txt")));
        touch(&photos.join(format!("p{i}.jpg")));
        touch(&other.join(format!("o{i}.txt")));
    }
    let sep = std::path::MAIN_SEPARATOR;
    let (d, p, o) = (s(&data), s(&photos), s(&other));
    // Overlap, repeats and trailing-separator variants, both when building and when querying.
    let idx = build_roots(&[
        p.clone(),
        format!("{d}{sep}"),
        d.clone(),
        o.clone(),
        format!("{o}{sep}"),
    ]);
    let all = paths(&idx, &[d.clone(), o.clone()]);
    let mut unique = all.clone();
    unique.sort();
    unique.dedup();
    assert_eq!(all.len(), unique.len(), "index holds a path twice: {all:?}");
    let single = build_roots(&[d.clone(), o.clone()]);
    assert_eq!(all, paths(&single, &[d.clone(), o.clone()]));

    let variants = [
        vec![d.clone(), o.clone()],
        vec![d.clone(), p.clone(), o.clone(), p.clone()],
        vec![
            format!("{d}{sep}"),
            d.clone(),
            format!("{p}{sep}"),
            format!("{o}{sep}"),
            o.clone(),
        ],
    ];
    for roots in &variants {
        let scoped = paths(&idx, roots);
        assert_eq!(scoped, all, "query roots {roots:?}");
        for limit in [1, 5, all.len() - 1, all.len()] {
            assert_limit_unique(&idx, roots, &scoped, limit);
        }
    }
}

#[cfg(unix)]
#[test]
fn symlinked_root_beside_its_target_returns_limit_unique_hits() {
    let t = tempfile::tempdir().unwrap();
    let data = t.path().join("data");
    for i in 0..4 {
        touch(&data.join(format!("a{i}.txt")));
    }
    let link = t.path().join("link");
    std::os::unix::fs::symlink(&data, &link).unwrap();
    let roots = vec![s(&data), s(&link)];
    let idx = build_roots(&roots);
    // The two roots are different paths (a link is not a path prefix of its target), so both trees are listed.
    let all = paths(&idx, &roots);
    for limit in [1, all.len() - 1, all.len()] {
        assert_limit_unique(&idx, &roots, &all, limit);
    }
}

fn set_mtime(p: &Path, at: SystemTime) {
    fs::File::options()
        .write(true)
        .open(p)
        .unwrap()
        .set_modified(at)
        .unwrap();
}

#[test]
fn recents_walk_resolves_equal_mtime_ties_by_path_whatever_the_visit_order() {
    let t = tempfile::tempdir().unwrap();
    let (a, b) = (t.path().join("a"), t.path().join("b"));
    touch(&a.join("f"));
    touch(&b.join("f"));
    let at = SystemTime::UNIX_EPOCH + Duration::from_secs(1_700_000_000);
    set_mtime(&a.join("f"), at);
    set_mtime(&b.join("f"), at);

    // Roots are walked in the order given, so each order visits the tied files differently.
    for roots in [[s(&a), s(&b)], [s(&b), s(&a)]] {
        let (got, _) = recents::scan(&roots, 1, None, &AtomicBool::new(false));
        let names: Vec<_> = got.iter().map(|e| e.path.clone()).collect();
        assert_eq!(names, vec![s(&a.join("f"))], "roots {roots:?}");
    }
}
