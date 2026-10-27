//! The compact in-memory index. Entries live in one chunk per directory: the directory path is stored once and
//! each entry keeps only its name plus a fixed-size record, so an entry costs about 55 bytes instead of the ~400
//! the Go index spends. A query walks the chunks in the same depth-first, name-sorted order Go's `fs.WalkDir`
//! produces, which keeps result order identical.

use crate::entry::{join_path, link_info, stat_of, WireEntry};
use crate::filter::{Compiled, RootScopes};
use crate::walk::{self, At, Sub, Visitor};
use std::cmp::Reverse;
use std::collections::{BinaryHeap, HashMap, HashSet};
use std::sync::atomic::{AtomicBool, AtomicU32, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Instant;

const NO_CHILD: u32 = u32::MAX;
const FLAG_DIR: u8 = 1;
const FLAG_SYMLINK: u8 = 2;
const FLAG_ASCII: u8 = 4;
/// Fixed cost per record plus a rough allowance for the chunk's own allocations.
const REC_BYTES: usize = std::mem::size_of::<Rec>();
const CHUNK_OVERHEAD: usize = 96;

#[derive(Debug, Clone, Copy)]
struct Rec {
    name_off: u32,
    name_len: u16,
    flags: u8,
    /// Chunk id of this directory's own listing, or `NO_CHILD`.
    child: u32,
    mode: u32,
    size: u64,
    mtime_ns: i64,
    ctime_ns: i64,
}

#[derive(Debug, Clone)]
struct Chunk {
    id: u32,
    dir_path: String,
    names: Vec<u8>,
    recs: Vec<Rec>,
    /// (record index, link target), sorted by record index.
    links: Vec<(u32, String)>,
}

impl Chunk {
    fn name(&self, r: &Rec) -> &str {
        // Names were validated as UTF-8 when stored.
        std::str::from_utf8(
            &self.names[r.name_off as usize..r.name_off as usize + r.name_len as usize],
        )
        .unwrap_or("")
    }

    fn name_bytes(&self, r: &Rec) -> &[u8] {
        &self.names[r.name_off as usize..r.name_off as usize + r.name_len as usize]
    }

    fn bytes(&self) -> usize {
        self.names.len() + self.recs.len() * REC_BYTES + self.dir_path.len() + CHUNK_OVERHEAD
    }

    fn link_target(&self, rec_idx: usize) -> &str {
        match self
            .links
            .binary_search_by_key(&(rec_idx as u32), |(i, _)| *i)
        {
            Ok(p) => &self.links[p].1,
            Err(_) => "",
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Stats {
    pub entries: usize,
    pub truncated: bool,
    pub build_ms: u64,
    pub bytes: usize,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Limits {
    pub max_entries: usize,
    pub max_bytes: usize,
}

/// A run of consecutive records of one chunk that are adjacent in depth-first order. Queries scan groups of
/// segments in parallel and join the results in order.
#[derive(Debug, Clone, Copy)]
struct Seg {
    pos: u32,
    start: u32,
    end: u32,
}

/// Lets a parallel scan stop work that an earlier group has made unnecessary.
#[derive(Clone, Copy)]
struct Gate<'a> {
    n: usize,
    done_at: &'a AtomicUsize,
}

impl Gate<'_> {
    fn superseded(&self) -> bool {
        self.done_at.load(Ordering::Relaxed) < self.n
    }

    fn reached(&self) {
        self.done_at.fetch_min(self.n, Ordering::Relaxed);
    }
}

#[derive(Debug)]
pub struct Index {
    /// The visible entries as runs, in depth-first order.
    segs: Vec<Seg>,
    chunks: Vec<Arc<Chunk>>,
    /// Chunk id -> position in `chunks` (`NO_CHILD` for a directory that could not be listed).
    id_to_pos: Vec<u32>,
    /// (root path, root chunk position)
    roots: Vec<(String, u32)>,
    /// Entries visible to queries, in depth-first order across `roots`; anything past this was over budget.
    visible: usize,
    /// First chunk id not yet used; updates allocate new ids from here.
    next_id: u32,
    limits: Limits,
    pub stats: Stats,
}

struct Builder<'a> {
    next_id: AtomicU32,
    chunks: Mutex<Vec<Arc<Chunk>>>,
    entries: AtomicUsize,
    bytes: AtomicUsize,
    hard_entries: usize,
    hard_bytes: usize,
    over: AtomicBool,
    cancel: &'a AtomicBool,
}

/// Lists one directory into a chunk. `reuse` maps a subdirectory name to the chunk id it already has (an update
/// keeps the subtree it knows); every other subdirectory gets a fresh id from `next_id` and is returned for the
/// caller to walk. `None` when the directory cannot be read.
fn list_dir(
    at: &At,
    id: u32,
    reuse: &dyn Fn(&[u8]) -> Option<u32>,
    next_id: &mut dyn FnMut() -> u32,
) -> Option<(Chunk, Vec<Sub>)> {
    let (dir, dir_path) = (at.dir, at.path);
    let mut chunk = Chunk {
        id,
        dir_path: dir_path.to_string(),
        names: Vec::new(),
        recs: Vec::new(),
        links: Vec::new(),
    };
    let mut subs: Vec<Sub> = Vec::new();
    let Ok(rd) = dir.entries() else {
        return None;
    };
    // Collected first so the records can be sorted by name, which is the order fs.WalkDir yields.
    struct Raw {
        name: String,
        os: std::ffi::OsString,
        meta: cap_std::fs::Metadata,
        walk_into: bool,
    }
    let mut raws: Vec<Raw> = Vec::new();
    for ent in rd {
        let Ok(ent) = ent else { continue };
        let os = ent.file_name();
        let Some(name) = os.to_str() else { continue };
        if name.len() > u16::MAX as usize {
            continue;
        }
        let Ok(ft) = ent.file_type() else { continue };
        let Ok(meta) = ent.metadata() else { continue };
        let walk_into = ft.is_dir();
        if walk_into && walk::should_skip_dir(name, &join_path(dir_path, name)) {
            continue;
        }
        raws.push(Raw {
            name: name.to_string(),
            os,
            meta,
            walk_into,
        });
    }
    raws.sort_unstable_by(|a, b| a.name.as_bytes().cmp(b.name.as_bytes()));
    for raw in raws {
        let st = stat_of(&raw.meta);
        let is_symlink = raw.meta.file_type().is_symlink();
        let mut flags = if raw.name.is_ascii() { FLAG_ASCII } else { 0 };
        let mut is_dir = raw.meta.is_dir();
        let mut link_target = String::new();
        if is_symlink {
            flags |= FLAG_SYMLINK;
            (is_dir, link_target) = link_info(at.root, dir, at.rel, &raw.os);
        }
        if is_dir {
            flags |= FLAG_DIR;
        }
        let rec_idx = chunk.recs.len();
        if !link_target.is_empty() {
            chunk.links.push((rec_idx as u32, link_target));
        }
        let mut child = NO_CHILD;
        if raw.walk_into {
            if let Some(known) = reuse(raw.name.as_bytes()) {
                child = known;
            } else {
                child = next_id();
                subs.push(Sub {
                    name: raw.os,
                    path: join_path(dir_path, &raw.name),
                    id: child,
                });
            }
        }
        let name_off = chunk.names.len() as u32;
        chunk.names.extend_from_slice(raw.name.as_bytes());
        chunk.recs.push(Rec {
            name_off,
            name_len: raw.name.len() as u16,
            flags,
            child,
            mode: st.mode,
            size: st.size,
            mtime_ns: st.mtime_ns,
            ctime_ns: st.ctime_ns,
        });
    }
    chunk.names.shrink_to_fit();
    chunk.recs.shrink_to_fit();
    Some((chunk, subs))
}

impl Visitor for Builder<'_> {
    fn visit(&self, at: &At, id: u32) -> Vec<Sub> {
        let Some((chunk, subs)) = list_dir(at, id, &|_| None, &mut || {
            self.next_id.fetch_add(1, Ordering::Relaxed)
        }) else {
            return Vec::new();
        };
        let n = chunk.recs.len();
        let b = chunk.bytes();
        self.chunks
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .push(Arc::new(chunk));
        let total_n = self.entries.fetch_add(n, Ordering::Relaxed) + n;
        let total_b = self.bytes.fetch_add(b, Ordering::Relaxed) + b;
        if total_n >= self.hard_entries || total_b >= self.hard_bytes {
            self.over.store(true, Ordering::Relaxed);
        }
        subs
    }

    fn stop(&self) -> bool {
        self.cancel.load(Ordering::Relaxed) || self.over.load(Ordering::Relaxed)
    }
}

impl Index {
    /// Walks `roots` in parallel and builds the index. `cancel` aborts the build (the partial result is dropped by
    /// the caller).
    pub fn build(roots: &[String], limits: Limits, cancel: &AtomicBool) -> Index {
        let started = Instant::now();
        let b = Builder {
            next_id: AtomicU32::new(0),
            chunks: Mutex::new(Vec::new()),
            entries: AtomicUsize::new(0),
            bytes: AtomicUsize::new(0),
            // The walk may overshoot the budget (it is parallel); the tail is cut in depth-first order afterwards.
            hard_entries: limits.max_entries.saturating_mul(3) / 2,
            hard_bytes: limits.max_bytes.saturating_mul(3) / 2,
            over: AtomicBool::new(false),
            cancel,
        };
        let mut root_ids = Vec::new();
        for root in roots {
            let id = b.next_id.fetch_add(1, Ordering::Relaxed);
            root_ids.push((root.clone(), id));
            walk::par_walk(&b, root, id);
        }
        let mut chunks = b.chunks.into_inner().unwrap();
        chunks.sort_unstable_by_key(|c| c.id);
        let max_id = b.next_id.load(Ordering::Relaxed) as usize;
        let mut id_to_pos = vec![NO_CHILD; max_id];
        for (pos, c) in chunks.iter().enumerate() {
            id_to_pos[c.id as usize] = pos as u32;
        }
        let roots: Vec<(String, u32)> = root_ids
            .into_iter()
            .map(|(p, id)| (p, id_to_pos.get(id as usize).copied().unwrap_or(NO_CHILD)))
            .collect();
        let mut idx = Index {
            segs: Vec::new(),
            chunks,
            id_to_pos,
            roots,
            visible: usize::MAX,
            next_id: max_id as u32,
            limits,
            stats: Stats::default(),
        };
        let (visible, bytes, truncated) = idx.apply_budget(limits, b.over.load(Ordering::Relaxed));
        idx.visible = visible;
        idx.segs = idx.segments(visible);
        idx.stats = Stats {
            entries: visible,
            truncated,
            build_ms: started.elapsed().as_millis() as u64,
            bytes: bytes + idx.segs.len() * std::mem::size_of::<Seg>(),
        };
        idx
    }

    /// Counts entries in depth-first order until a limit is hit.
    fn apply_budget(&self, limits: Limits, walk_overshot: bool) -> (usize, usize, bool) {
        let mut entries = 0usize;
        let mut bytes = 0usize;
        let mut truncated = walk_overshot;
        self.dfs(|chunk, i| {
            // Real per-entry cost: the record plus its name (the directory path is shared).
            let cost = REC_BYTES + chunk.recs[i].name_len as usize;
            if entries >= limits.max_entries || bytes + cost > limits.max_bytes {
                truncated = true;
                return false;
            }
            entries += 1;
            bytes += cost;
            true
        });
        // Report real memory use, not the budget estimate.
        let real: usize = self.chunks.iter().map(|c| c.bytes()).sum();
        (entries, real, truncated)
    }

    /// `dfs` that hands out the chunk's position instead of the chunk.
    fn dfs_pos<F: FnMut(u32, usize) -> bool>(&self, mut f: F) {
        self.dfs_raw(|pos, _, i| f(pos, i));
    }

    fn dfs<F: FnMut(&Chunk, usize) -> bool>(&self, mut f: F) {
        self.dfs_raw(|_, chunk, i| f(chunk, i));
    }

    /// Depth-first, name-sorted traversal over every root. The callback returns false to stop.
    fn dfs_raw<F: FnMut(u32, &Chunk, usize) -> bool>(&self, mut f: F) {
        for (_, root_pos) in &self.roots {
            if *root_pos == NO_CHILD {
                continue;
            }
            let mut stack: Vec<(u32, usize)> = vec![(*root_pos, 0)];
            while let Some((pos, i)) = stack.pop() {
                let chunk = &self.chunks[pos as usize];
                if i >= chunk.recs.len() {
                    continue;
                }
                // Resume this directory after the current entry once its subtree is done.
                stack.push((pos, i + 1));
                if !f(pos, chunk, i) {
                    return;
                }
                let child = chunk.recs[i].child;
                if child != NO_CHILD {
                    let cpos = self
                        .id_to_pos
                        .get(child as usize)
                        .copied()
                        .unwrap_or(NO_CHILD);
                    if cpos != NO_CHILD {
                        stack.push((cpos, 0));
                    }
                }
            }
        }
    }

    pub fn is_empty(&self) -> bool {
        self.stats.entries == 0
    }

    /// The first `visible` entries as depth-first runs.
    fn segments(&self, visible: usize) -> Vec<Seg> {
        let mut segs: Vec<Seg> = Vec::new();
        let mut seen = 0usize;
        self.dfs_pos(|pos, i| {
            if seen >= visible {
                return false;
            }
            seen += 1;
            match segs.last_mut() {
                Some(s) if s.pos == pos && s.end as usize == i => s.end += 1,
                _ => segs.push(Seg {
                    pos,
                    start: i as u32,
                    end: i as u32 + 1,
                }),
            }
            true
        });
        segs
    }

    /// Scans `segs` in order for matches, stopping once `limit` are found.
    fn scan(
        &self,
        segs: &[Seg],
        filters: &Compiled,
        scopes: &RootScopes,
        limit: usize,
        gate: Gate<'_>,
    ) -> Vec<WireEntry> {
        let mut out: Vec<WireEntry> = Vec::new();
        let mut buf = String::new();
        for seg in segs {
            if gate.superseded() {
                return out;
            }
            let chunk = &self.chunks[seg.pos as usize];
            for i in seg.start as usize..seg.end as usize {
                if i % 8192 == 8191 && gate.superseded() {
                    return out;
                }
                let r = &chunk.recs[i];
                let name = chunk.name(r);
                if !filters.match_name_fast(name, r.flags & FLAG_ASCII != 0, &mut buf) {
                    continue;
                }
                let is_dir = r.flags & FLAG_DIR != 0;
                if !filters.match_attrs(name, is_dir, r.size, r.mtime_ns) {
                    continue;
                }
                let path = join_path(&chunk.dir_path, name);
                if !scopes.contains(&path) {
                    continue;
                }
                out.push(WireEntry {
                    path,
                    size: r.size,
                    mtime_ns: r.mtime_ns,
                    ctime_ns: r.ctime_ns,
                    mode: r.mode,
                    is_dir,
                    is_symlink: r.flags & FLAG_SYMLINK != 0,
                    symlink_target: chunk.link_target(i).to_string(),
                });
                if out.len() >= limit {
                    gate.reached();
                    return out;
                }
            }
        }
        out
    }

    /// Newest-first top `limit` files under `roots`, answered from memory with the rule `recents::scan` uses: real
    /// directories are left out, symlinks (also to directories) are kept. None when the index cannot answer exactly:
    /// it was cut short by its budget, or a requested root is not exactly an indexed root. A sub-folder is declined
    /// because whether a symlink counts as a directory depends on the root it was resolved against, so the answer
    /// could differ from a walk of that sub-folder. Callers then walk instead.
    pub fn recents(&self, roots: &[String], limit: usize) -> Option<Vec<WireEntry>> {
        if self.stats.truncated || roots.is_empty() {
            return None;
        }
        if !roots.iter().all(|r| self.roots.iter().any(|(p, _)| p == r)) {
            return None;
        }
        let scopes = RootScopes::new(roots);
        let limit = limit.max(1);
        // Keyed like recents::scan (older first, then path descending) so the heap keeps the newest `limit`.
        type Key = (i64, Reverse<String>, u32, u32);
        let mut heap: BinaryHeap<Reverse<Key>> = BinaryHeap::new();
        let mut threshold = i64::MIN;
        for seg in &self.segs {
            let chunk = &self.chunks[seg.pos as usize];
            for i in seg.start as usize..seg.end as usize {
                let r = &chunk.recs[i];
                if r.flags & FLAG_DIR != 0 && r.flags & FLAG_SYMLINK == 0 {
                    continue;
                }
                if r.mtime_ns < threshold {
                    continue;
                }
                let path = join_path(&chunk.dir_path, chunk.name(r));
                if !scopes.contains(&path) {
                    continue;
                }
                heap.push(Reverse((r.mtime_ns, Reverse(path), seg.pos, i as u32)));
                if heap.len() > limit {
                    heap.pop();
                }
                if heap.len() >= limit {
                    if let Some(Reverse(oldest)) = heap.peek() {
                        threshold = oldest.0;
                    }
                }
            }
        }
        let mut keys: Vec<Key> = heap.into_iter().map(|Reverse(k)| k).collect();
        keys.sort_by(|a, b| b.cmp(a));
        Some(
            keys.into_iter()
                .map(|(mtime_ns, Reverse(path), pos, i)| {
                    let chunk = &self.chunks[pos as usize];
                    let r = &chunk.recs[i as usize];
                    WireEntry {
                        path,
                        size: r.size,
                        mtime_ns,
                        ctime_ns: r.ctime_ns,
                        mode: r.mode,
                        is_dir: r.flags & FLAG_DIR != 0,
                        is_symlink: r.flags & FLAG_SYMLINK != 0,
                        symlink_target: chunk.link_target(i as usize).to_string(),
                    }
                })
                .collect(),
        )
    }

    /// Matches in index order. The bool is true when `limit` was reached or the index itself was truncated.
    pub fn query(
        &self,
        filters: &Compiled,
        roots: &[String],
        limit: usize,
    ) -> (Vec<WireEntry>, bool) {
        use rayon::prelude::*;
        let scopes = RootScopes::new(roots);
        // Groups of roughly equal entry counts, scanned in parallel; their results are joined in order, so the
        // answer is the same as a sequential scan.
        let parts = rayon::current_num_threads().max(1) * 4;
        let target = (self.visible / parts).max(4096);
        let mut groups: Vec<&[Seg]> = Vec::new();
        let (mut from, mut acc) = (0usize, 0usize);
        for (n, s) in self.segs.iter().enumerate() {
            acc += (s.end - s.start) as usize;
            if acc >= target {
                groups.push(&self.segs[from..=n]);
                from = n + 1;
                acc = 0;
            }
        }
        if from < self.segs.len() {
            groups.push(&self.segs[from..]);
        }
        // The lowest group index that already has `limit` matches by itself: later groups cannot contribute.
        let done_at = AtomicUsize::new(usize::MAX);
        let parts: Vec<Vec<WireEntry>> = groups
            .par_iter()
            .enumerate()
            .map(|(n, g)| {
                let gate = Gate {
                    n,
                    done_at: &done_at,
                };
                self.scan(g, filters, &scopes, limit, gate)
            })
            .collect();
        let mut out: Vec<WireEntry> = Vec::new();
        let mut hit_limit = false;
        'join: for part in parts {
            for e in part {
                out.push(e);
                if out.len() >= limit {
                    hit_limit = true;
                    break 'join;
                }
            }
        }
        (out, self.stats.truncated || hit_limit)
    }
}

/// The result of folding filesystem changes into an index.
pub struct Applied {
    pub index: Index,
    /// Directories that did not exist in the old snapshot (they need a watch).
    pub new_dirs: Vec<String>,
}

impl Index {
    /// Every directory the index lists, as absolute paths.
    pub fn dirs(&self) -> impl Iterator<Item = &str> {
        self.chunks.iter().map(|c| c.dir_path.as_str())
    }

    /// The chunk position of directory `path` and the index of the root that holds it, found by following names
    /// down from the root chunk (records are sorted by name, so each step is a binary search).
    fn locate(&self, path: &str, replaced: &HashMap<u32, Arc<Chunk>>) -> Option<(usize, u32)> {
        let sep = std::path::MAIN_SEPARATOR;
        for (ri, (root, root_pos)) in self.roots.iter().enumerate() {
            let prefix = format!("{}{sep}", root.strip_suffix(sep).unwrap_or(root));
            let rel = if path == root {
                ""
            } else if let Some(r) = path.strip_prefix(prefix.as_str()) {
                r
            } else {
                continue;
            };
            let mut pos = *root_pos;
            if pos == NO_CHILD {
                return None;
            }
            if !rel.is_empty() {
                for comp in rel.split(sep) {
                    let chunk = replaced.get(&pos).unwrap_or(&self.chunks[pos as usize]);
                    let at = chunk
                        .recs
                        .binary_search_by(|r| chunk.name_bytes(r).cmp(comp.as_bytes()))
                        .ok()?;
                    let child = chunk.recs[at].child;
                    if child == NO_CHILD {
                        return None;
                    }
                    pos = *self.id_to_pos.get(child as usize)?;
                    if pos == NO_CHILD {
                        return None;
                    }
                }
            }
            return Some((ri, pos));
        }
        None
    }

    /// Adds `id` and every chunk below it to `out`.
    fn collect_subtree(
        &self,
        id: u32,
        replaced: &HashMap<u32, Arc<Chunk>>,
        out: &mut HashSet<u32>,
    ) {
        let mut stack = vec![id];
        while let Some(id) = stack.pop() {
            let Some(&pos) = self.id_to_pos.get(id as usize) else {
                continue; // a chunk created by this update
            };
            if pos == NO_CHILD || !out.insert(id) {
                continue;
            }
            let chunk = replaced.get(&pos).unwrap_or(&self.chunks[pos as usize]);
            stack.extend(
                chunk
                    .recs
                    .iter()
                    .map(|r| r.child)
                    .filter(|c| *c != NO_CHILD),
            );
        }
    }

    /// Re-lists the directories in `dirty` (absolute paths) and returns the index that results; chunks nothing
    /// touched are shared with `self`. `None` means the change cannot be folded in (the index was cut by its
    /// budget, a root went away, or the result outgrew the budget) and the caller should rebuild from scratch.
    pub fn apply(&self, dirty: &[String], cancel: &AtomicBool) -> Option<Applied> {
        use crate::walk::{rel_join, Sub};
        let started = Instant::now();
        if self.stats.truncated {
            return None;
        }
        let sep = std::path::MAIN_SEPARATOR;
        let mut work: Vec<String> = dirty.to_vec();
        work.sort_by_key(|p| (p.matches(sep).count(), p.clone()));
        work.dedup();
        let mut replaced: HashMap<u32, Arc<Chunk>> = HashMap::new();
        let mut removed: HashSet<u32> = HashSet::new();
        let mut added: Vec<Arc<Chunk>> = Vec::new();
        let mut next_id = self.next_id;
        let mut fresh: Vec<String> = Vec::new();
        let mut root_dirs: HashMap<usize, Arc<cap_std::fs::Dir>> = HashMap::new();

        let mut w = 0;
        while w < work.len() {
            if cancel.load(Ordering::Relaxed) {
                return None;
            }
            let path = work[w].clone();
            w += 1;
            // A directory inside a subtree this batch just walked is already current.
            if fresh
                .iter()
                .any(|f| path == *f || path.starts_with(&format!("{f}{sep}")))
            {
                continue;
            }
            let Some((ri, pos)) = self.locate(&path, &replaced) else {
                continue;
            };
            let root_dir = match root_dirs.get(&ri) {
                Some(d) => Arc::clone(d),
                None => {
                    let d = cap_std::fs::Dir::open_ambient_dir(
                        &self.roots[ri].0,
                        cap_std::ambient_authority(),
                    )
                    .ok()?;
                    let d = Arc::new(d);
                    root_dirs.insert(ri, Arc::clone(&d));
                    d
                }
            };
            let root_path = &self.roots[ri].0;
            let rel = path
                .strip_prefix(root_path.strip_suffix(sep).unwrap_or(root_path))
                .unwrap_or("")
                .trim_start_matches(sep)
                .replace(sep, "/");
            let dir = if rel.is_empty() {
                root_dir.try_clone().ok()
            } else {
                root_dir.open_dir(&rel).ok()
            };
            let Some(dir) = dir else {
                // Gone or unreadable: the directory above it is what changed.
                if rel.is_empty() {
                    return None;
                }
                if let Some(parent) = std::path::Path::new(&path)
                    .parent()
                    .and_then(|p| p.to_str())
                {
                    work.push(parent.to_string());
                }
                continue;
            };
            let old = replaced
                .get(&pos)
                .cloned()
                .unwrap_or_else(|| Arc::clone(&self.chunks[pos as usize]));
            let old_children: HashMap<Vec<u8>, u32> = old
                .recs
                .iter()
                .filter(|r| r.child != NO_CHILD)
                .map(|r| (old.name_bytes(r).to_vec(), r.child))
                .collect();
            let at = At {
                root: &root_dir,
                dir: &dir,
                path: &path,
                rel: &rel,
            };
            let listed = list_dir(
                &at,
                old.id,
                &|name| old_children.get(name).copied(),
                &mut || {
                    next_id += 1;
                    next_id - 1
                },
            );
            let Some((chunk, subs)) = listed else {
                self.collect_subtree(old.id, &replaced, &mut removed);
                continue;
            };
            let kept: HashSet<u32> = chunk
                .recs
                .iter()
                .map(|r| r.child)
                .filter(|c| *c != NO_CHILD)
                .collect();
            for cid in old_children.values().filter(|c| !kept.contains(c)) {
                self.collect_subtree(*cid, &replaced, &mut removed);
            }
            replaced.insert(pos, Arc::new(chunk));
            // A directory's own size and times are stored in its parent's listing; keep that record current.
            if !rel.is_empty() {
                if let Ok(meta) = root_dir.symlink_metadata(&rel) {
                    let parent_path = std::path::Path::new(&path)
                        .parent()
                        .and_then(|p| p.to_str())
                        .unwrap_or("");
                    if let (Some((_, ppos)), Some(name)) = (
                        self.locate(parent_path, &replaced),
                        std::path::Path::new(&path)
                            .file_name()
                            .and_then(|n| n.to_str()),
                    ) {
                        let mut parent = Chunk::clone(
                            replaced.get(&ppos).unwrap_or(&self.chunks[ppos as usize]),
                        );
                        if let Ok(at) = parent
                            .recs
                            .binary_search_by(|r| parent.name_bytes(r).cmp(name.as_bytes()))
                        {
                            let st = stat_of(&meta);
                            let r = &mut parent.recs[at];
                            (r.size, r.mtime_ns, r.ctime_ns, r.mode) =
                                (st.size, st.mtime_ns, st.ctime_ns, st.mode);
                            replaced.insert(ppos, Arc::new(parent));
                        }
                    }
                }
            }
            for Sub {
                name,
                path: sub_path,
                id,
            } in subs
            {
                let Ok(child) = dir.open_dir(&name) else {
                    continue;
                };
                let b = Builder {
                    next_id: AtomicU32::new(next_id),
                    chunks: Mutex::new(Vec::new()),
                    entries: AtomicUsize::new(0),
                    bytes: AtomicUsize::new(0),
                    hard_entries: self.limits.max_entries.saturating_mul(3) / 2,
                    hard_bytes: self.limits.max_bytes.saturating_mul(3) / 2,
                    over: AtomicBool::new(false),
                    cancel,
                };
                let sub_rel = rel_join(&rel, &name.to_string_lossy());
                walk::par_walk_sub(
                    &b,
                    Arc::clone(&root_dir),
                    child,
                    sub_path.clone(),
                    sub_rel,
                    id,
                );
                if b.over.load(Ordering::Relaxed) || cancel.load(Ordering::Relaxed) {
                    return None;
                }
                next_id = b.next_id.load(Ordering::Relaxed);
                added.extend(b.chunks.into_inner().unwrap());
                fresh.push(sub_path);
            }
        }

        let mut chunks: Vec<Arc<Chunk>> = Vec::with_capacity(self.chunks.len() + added.len());
        for (pos, c) in self.chunks.iter().enumerate() {
            if removed.contains(&c.id) {
                continue;
            }
            chunks.push(
                replaced
                    .remove(&(pos as u32))
                    .unwrap_or_else(|| Arc::clone(c)),
            );
        }
        let new_dirs: Vec<String> = added.iter().map(|c| c.dir_path.clone()).collect();
        chunks.extend(added);
        let mut id_to_pos = vec![NO_CHILD; next_id as usize];
        for (pos, c) in chunks.iter().enumerate() {
            id_to_pos[c.id as usize] = pos as u32;
        }
        let roots: Vec<(String, u32)> = self
            .roots
            .iter()
            .map(|(p, old_pos)| {
                let id = self.chunks[*old_pos as usize].id;
                (p.clone(), id_to_pos[id as usize])
            })
            .collect();
        let entries: usize = chunks.iter().map(|c| c.recs.len()).sum();
        let bytes: usize = chunks.iter().map(|c| c.bytes()).sum();
        if entries > self.limits.max_entries || bytes > self.limits.max_bytes {
            return None;
        }
        let mut index = Index {
            segs: Vec::new(),
            chunks,
            id_to_pos,
            roots,
            visible: entries,
            next_id,
            limits: self.limits,
            stats: Stats::default(),
        };
        index.segs = index.segments(entries);
        index.stats = Stats {
            entries,
            truncated: false,
            build_ms: started.elapsed().as_millis() as u64,
            bytes: bytes + index.segs.len() * std::mem::size_of::<Seg>(),
        };
        Some(Applied { index, new_dirs })
    }
}
