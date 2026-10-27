//! Keeping the index current: file system events mark directories dirty, a debounced applier re-lists them and
//! swaps in the updated index. Events are only a hint; a periodic full rebuild by the agent remains the
//! authority, so a missed event costs staleness, never a wrong answer for long.

use notify::{EventKind, RecommendedWatcher, RecursiveMode, Watcher};
use std::collections::HashSet;
use std::path::Path;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Condvar, Mutex};
use std::time::{Duration, Instant};

/// More dirty directories than this in one batch is cheaper to handle with a full rebuild.
pub const MAX_DIRTY_DIRS: usize = 4096;
/// A batch is applied once events have been quiet this long, or this long after the first one.
pub const QUIET: Duration = Duration::from_millis(250);
pub const MAX_WAIT: Duration = Duration::from_secs(2);

#[derive(Default)]
struct Pending {
    paths: HashSet<String>,
    first: Option<Instant>,
    last: Option<Instant>,
    /// The watcher lost events (queue overflow, error): only a rebuild is safe.
    overflow: bool,
}

/// Directories reported dirty since the last batch.
#[derive(Default)]
pub struct Events {
    pending: Mutex<Pending>,
    cv: Condvar,
}

/// One batch taken from [`Events`].
pub struct Batch {
    pub dirs: Vec<String>,
    pub overflow: bool,
}

impl Events {
    pub fn mark(&self, dir: String) {
        let mut p = self.pending.lock().unwrap_or_else(|e| e.into_inner());
        let now = Instant::now();
        p.first.get_or_insert(now);
        p.last = Some(now);
        p.paths.insert(dir);
        self.cv.notify_all();
    }

    pub fn mark_overflow(&self) {
        let mut p = self.pending.lock().unwrap_or_else(|e| e.into_inner());
        let now = Instant::now();
        p.first.get_or_insert(now);
        p.last = Some(now);
        p.overflow = true;
        self.cv.notify_all();
    }

    /// True while a lost-events marker waits for the applier: the published index may be missing changes.
    pub fn overflow_pending(&self) -> bool {
        self.pending
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .overflow
    }

    /// Forgets everything pending (a full rebuild is about to read the disk afresh).
    pub fn clear(&self) {
        *self.pending.lock().unwrap_or_else(|e| e.into_inner()) = Pending::default();
    }

    /// Puts a batch back (the applier could not run it yet).
    pub fn restore(&self, batch: Batch) {
        let mut p = self.pending.lock().unwrap_or_else(|e| e.into_inner());
        let now = Instant::now();
        p.first.get_or_insert(now);
        p.last = Some(now);
        p.paths.extend(batch.dirs);
        p.overflow |= batch.overflow;
    }

    /// Blocks until a batch is ready (events have gone quiet) or `stop` is set; `None` on stop.
    pub fn next_batch(&self, stop: &AtomicBool) -> Option<Batch> {
        let mut p = self.pending.lock().unwrap_or_else(|e| e.into_inner());
        loop {
            if stop.load(Ordering::Relaxed) {
                return None;
            }
            match (p.first, p.last) {
                (Some(first), Some(last)) => {
                    let now = Instant::now();
                    if now.duration_since(last) >= QUIET || now.duration_since(first) >= MAX_WAIT {
                        let taken = std::mem::take(&mut *p);
                        return Some(Batch {
                            dirs: taken.paths.into_iter().collect(),
                            overflow: taken.overflow,
                        });
                    }
                    let wait = QUIET.saturating_sub(now.duration_since(last));
                    p = self
                        .cv
                        .wait_timeout(p, wait.max(Duration::from_millis(10)))
                        .unwrap()
                        .0;
                }
                _ => {
                    p = self
                        .cv
                        .wait_timeout(p, Duration::from_millis(500))
                        .unwrap()
                        .0;
                }
            }
        }
    }
}

/// What the watcher is doing, for `index.stats`.
#[derive(Default)]
pub struct Health {
    pub watches: AtomicUsize,
    /// Every directory has a watch (registration finished without error).
    pub complete: AtomicBool,
    pub error: Mutex<String>,
}

/// A running set of watches feeding [`Events`].
pub struct Watch {
    watcher: Mutex<RecommendedWatcher>,
    pub health: Arc<Health>,
}

fn per_directory() -> bool {
    // inotify has no recursive mode of its own: notify would add a watch for every directory, including the
    // pruned ones (.git, node_modules). FSEvents and ReadDirectoryChangesW watch a whole tree cheaply.
    cfg!(target_os = "linux")
}

impl Watch {
    pub fn new(events: Arc<Events>) -> notify::Result<Watch> {
        let handler = move |res: notify::Result<notify::Event>| match res {
            Ok(ev) => {
                if matches!(ev.kind, EventKind::Access(_)) {
                    return;
                }
                if ev.need_rescan() {
                    events.mark_overflow();
                    return;
                }
                for p in &ev.paths {
                    if let Some(parent) = p.parent().and_then(Path::to_str) {
                        events.mark(parent.to_string());
                    }
                }
            }
            Err(_) => events.mark_overflow(),
        };
        Ok(Watch {
            watcher: Mutex::new(notify::recommended_watcher(handler)?),
            health: Arc::new(Health::default()),
        })
    }

    /// Watches every directory in `dirs` (or, where the platform does it cheaply, each root recursively).
    /// Stops at the first failure and records why (typically the inotify watch limit).
    pub fn register<'a>(&self, roots: &[String], dirs: impl Iterator<Item = &'a str>) {
        let mut w = self.watcher.lock().unwrap_or_else(|e| e.into_inner());
        let result: notify::Result<()> = if per_directory() {
            let mut r = Ok(());
            for d in dirs {
                match w.watch(Path::new(d), RecursiveMode::NonRecursive) {
                    Ok(()) => {
                        self.health.watches.fetch_add(1, Ordering::Relaxed);
                    }
                    Err(e) => {
                        r = Err(e);
                        break;
                    }
                }
            }
            r
        } else {
            roots.iter().try_for_each(|root| {
                w.watch(Path::new(root), RecursiveMode::Recursive)?;
                self.health.watches.fetch_add(1, Ordering::Relaxed);
                Ok(())
            })
        };
        match result {
            Ok(()) => self.health.complete.store(true, Ordering::Relaxed),
            Err(e) => *self.health.error.lock().unwrap_or_else(|e| e.into_inner()) = e.to_string(),
        }
    }

    /// Watches directories that appeared after the first registration.
    pub fn add<'a>(&self, dirs: impl Iterator<Item = &'a str>) {
        if !per_directory() {
            return;
        }
        let mut w = self.watcher.lock().unwrap_or_else(|e| e.into_inner());
        for d in dirs {
            match w.watch(Path::new(d), RecursiveMode::NonRecursive) {
                Ok(()) => {
                    self.health.watches.fetch_add(1, Ordering::Relaxed);
                }
                Err(e) => {
                    self.health.complete.store(false, Ordering::Relaxed);
                    *self.health.error.lock().unwrap_or_else(|e| e.into_inner()) = e.to_string();
                    return;
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn events_are_deduplicated_and_released_after_a_quiet_period() {
        let ev = Events::default();
        let stop = AtomicBool::new(false);
        for _ in 0..100 {
            ev.mark("/r/a".into());
        }
        ev.mark("/r/b".into());
        let start = Instant::now();
        let mut b = ev.next_batch(&stop).unwrap();
        assert!(start.elapsed() >= QUIET, "waits for quiet");
        b.dirs.sort();
        assert_eq!(b.dirs, vec!["/r/a", "/r/b"]);
        assert!(!b.overflow);
    }

    #[test]
    fn a_constant_trickle_still_flushes_after_the_max_wait() {
        let ev = Arc::new(Events::default());
        let stop = Arc::new(AtomicBool::new(false));
        let (e2, s2) = (Arc::clone(&ev), Arc::clone(&stop));
        let feeder = std::thread::spawn(move || {
            while !s2.load(Ordering::Relaxed) {
                e2.mark("/r/busy".into());
                std::thread::sleep(Duration::from_millis(50));
            }
        });
        let start = Instant::now();
        let b = ev.next_batch(&stop).unwrap();
        stop.store(true, Ordering::Relaxed);
        feeder.join().unwrap();
        assert!(start.elapsed() < MAX_WAIT + Duration::from_millis(500));
        assert_eq!(b.dirs, vec!["/r/busy"]);
    }

    #[test]
    fn clear_drops_pending_and_restore_puts_a_batch_back() {
        let ev = Events::default();
        let stop = AtomicBool::new(false);
        ev.mark("/r/a".into());
        ev.clear();
        ev.mark_overflow();
        let b = ev.next_batch(&stop).unwrap();
        assert!(b.dirs.is_empty() && b.overflow);
        ev.restore(Batch {
            dirs: vec!["/r/x".into()],
            overflow: true,
        });
        let b = ev.next_batch(&stop).unwrap();
        assert_eq!(b.dirs, vec!["/r/x"]);
        assert!(b.overflow);
    }

    #[test]
    fn a_lost_events_marker_is_visible_until_its_batch_is_taken() {
        let ev = Events::default();
        let stop = AtomicBool::new(false);
        assert!(!ev.overflow_pending());
        ev.mark("/r/a".into());
        assert!(!ev.overflow_pending());
        ev.mark_overflow();
        assert!(ev.overflow_pending());
        ev.next_batch(&stop).unwrap();
        assert!(!ev.overflow_pending());
    }

    #[test]
    fn a_failed_watch_is_reported_and_not_complete() {
        let w = Watch::new(Arc::new(Events::default())).unwrap();
        if per_directory() {
            w.register(&[], ["/definitely/not/a/directory"].into_iter());
            assert!(!w.health.complete.load(Ordering::Relaxed));
            assert!(!w
                .health
                .error
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .is_empty());
        }
    }

    #[test]
    fn stop_ends_the_wait() {
        let ev = Events::default();
        let stop = AtomicBool::new(true);
        assert!(ev.next_batch(&stop).is_none());
    }
}
