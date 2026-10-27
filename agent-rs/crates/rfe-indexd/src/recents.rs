//! Most recently modified files under a set of roots: a parallel walk keeping a bounded top-K, with the same
//! pruning and file-only rule as the Go agent's recents handler.

use crate::entry::{join_path, stat_of, wire_entry, WireEntry};
use crate::walk::{self, At, Sub, Visitor};
use std::cmp::Reverse;
use std::collections::BinaryHeap;
use std::sync::atomic::{AtomicBool, AtomicI64, Ordering};
use std::sync::Mutex;
use std::time::Instant;

struct Item {
    mtime_ns: i64,
    entry: WireEntry,
}

impl PartialEq for Item {
    fn eq(&self, o: &Self) -> bool {
        self.mtime_ns == o.mtime_ns && self.entry.path == o.entry.path
    }
}
impl Eq for Item {}
impl PartialOrd for Item {
    fn partial_cmp(&self, o: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(o))
    }
}
impl Ord for Item {
    // Older first, then by path, so ties resolve the same way on every run.
    fn cmp(&self, o: &Self) -> std::cmp::Ordering {
        self.mtime_ns
            .cmp(&o.mtime_ns)
            .then_with(|| o.entry.path.cmp(&self.entry.path))
    }
}

struct Scan<'a> {
    limit: usize,
    heap: Mutex<BinaryHeap<Reverse<Item>>>,
    /// Oldest mtime currently in a full heap; files not newer than this are skipped without locking.
    threshold: AtomicI64,
    next_id: std::sync::atomic::AtomicU32,
    cancel: &'a AtomicBool,
    deadline: Option<Instant>,
    expired: AtomicBool,
}

impl Visitor for Scan<'_> {
    fn visit(&self, at: &At, _id: u32) -> Vec<Sub> {
        let (dir, dir_path) = (at.dir, at.path);
        let mut subs = Vec::new();
        let Ok(rd) = dir.entries() else { return subs };
        for ent in rd {
            let Ok(ent) = ent else { continue };
            let os = ent.file_name();
            let Some(name) = os.to_str() else { continue };
            let Ok(ft) = ent.file_type() else { continue };
            if ft.is_dir() {
                let full = join_path(dir_path, name);
                if !walk::should_skip_dir(name, &full) {
                    subs.push(Sub {
                        name: os,
                        path: full,
                        id: self.next_id.fetch_add(1, Ordering::Relaxed),
                    });
                }
                continue;
            }
            let Ok(meta) = ent.metadata() else { continue };
            let mtime = stat_of(&meta).mtime_ns;
            if mtime <= self.threshold.load(Ordering::Relaxed) {
                continue;
            }
            let entry = wire_entry(at, &os, join_path(dir_path, name), &meta);
            let mut heap = self.heap.lock().unwrap_or_else(|e| e.into_inner());
            heap.push(Reverse(Item {
                mtime_ns: mtime,
                entry,
            }));
            if heap.len() > self.limit {
                heap.pop();
            }
            if heap.len() >= self.limit {
                if let Some(Reverse(oldest)) = heap.peek() {
                    self.threshold.store(oldest.mtime_ns, Ordering::Relaxed);
                }
            }
        }
        subs
    }

    fn stop(&self) -> bool {
        if self.cancel.load(Ordering::Relaxed) {
            return true;
        }
        if self.deadline.is_some_and(|d| Instant::now() >= d) {
            self.expired.store(true, Ordering::Relaxed);
            return true;
        }
        false
    }
}

/// Newest-first top `limit` files under `roots`. The bool is true when the deadline cut the scan short.
pub fn scan(
    roots: &[String],
    limit: usize,
    deadline: Option<Instant>,
    cancel: &AtomicBool,
) -> (Vec<WireEntry>, bool) {
    let s = Scan {
        limit: limit.max(1),
        heap: Mutex::new(BinaryHeap::new()),
        threshold: AtomicI64::new(i64::MIN),
        next_id: std::sync::atomic::AtomicU32::new(0),
        cancel,
        deadline,
        expired: AtomicBool::new(false),
    };
    for root in roots {
        walk::par_walk(&s, root, 0);
        if s.stop() {
            break;
        }
    }
    let mut items: Vec<Item> = s
        .heap
        .into_inner()
        .unwrap()
        .into_iter()
        .map(|Reverse(i)| i)
        .collect();
    items.sort_by(|a, b| b.cmp(a));
    let partial = s.expired.load(Ordering::Relaxed);
    (items.into_iter().map(|i| i.entry).collect(), partial)
}
