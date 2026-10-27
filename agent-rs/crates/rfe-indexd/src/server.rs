//! The request loop: reads frames from stdin, answers on stdout, runs each request on its own thread so a
//! long index build never blocks a query.

use crate::filter::{Compiled, Filters};
use crate::index::{Index, Limits};
use crate::live::{Events, Watch, MAX_DIRTY_DIRS};
use crate::recents;
use rfe_proto::{code, error_response, read_frame, write_frame, Envelope, Hello, KIND_JSON};
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::io::{Read, Write};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex, RwLock};
use std::time::{Duration, Instant};

pub const NAME: &str = "rfe-indexd";

struct State {
    index: RwLock<Option<Arc<Index>>>,
    building: AtomicBool,
    /// Events were lost and the rebuild that repairs the index has not finished: the index may miss changes.
    resync: AtomicBool,
    cancels: Mutex<HashMap<u64, Arc<AtomicBool>>>,
    /// Held while a build starts and while a batch of changes is applied, so the two never interleave.
    mutate: Mutex<()>,
    events: Arc<Events>,
    watch: Mutex<Option<Arc<Watch>>>,
    last_build: Mutex<Option<BuildReq>>,
    applier_started: AtomicBool,
    updates: AtomicU64,
    rebuilds: AtomicU64,
    stop: AtomicBool,
}

#[derive(Deserialize, Clone)]
#[serde(rename_all = "camelCase")]
struct BuildReq {
    roots: Vec<String>,
    max_entries: usize,
    max_bytes: usize,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct QueryReq {
    #[serde(default)]
    filters: Filters,
    roots: Vec<String>,
    limit: usize,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct RecentsReq {
    roots: Vec<String>,
    limit: usize,
    #[serde(default)]
    budget_ms: u64,
}

type Out<W> = Arc<Mutex<W>>;

fn send<W: Write>(out: &Out<W>, v: &Value) {
    let bytes = serde_json::to_vec(v).unwrap_or_default();
    let mut w = out.lock().unwrap_or_else(|e| e.into_inner());
    // A write failure means the Go agent is gone; the read loop will see EOF and exit.
    let _ = write_frame(&mut *w, KIND_JSON, &bytes).and_then(|_| w.flush());
}

/// Serves requests until the input closes. Generic over the streams so tests can drive it in memory.
pub fn serve<R: Read, W: Write + Send + 'static>(mut input: R, output: W) -> std::io::Result<()> {
    let out: Out<W> = Arc::new(Mutex::new(output));
    send(
        &out,
        &serde_json::to_value(Hello::new(
            NAME,
            rfe_proto::release_version(env!("CARGO_PKG_VERSION")),
        ))
        .unwrap(),
    );
    let state = Arc::new(State {
        index: RwLock::new(None),
        building: AtomicBool::new(false),
        resync: AtomicBool::new(false),
        cancels: Mutex::new(HashMap::new()),
        mutate: Mutex::new(()),
        events: Arc::new(Events::default()),
        watch: Mutex::new(None),
        last_build: Mutex::new(None),
        applier_started: AtomicBool::new(false),
        updates: AtomicU64::new(0),
        rebuilds: AtomicU64::new(0),
        stop: AtomicBool::new(false),
    });
    let mut workers: Vec<std::thread::JoinHandle<()>> = Vec::new();
    while let Some(frame) = read_frame(&mut input)? {
        if frame.kind != KIND_JSON {
            continue;
        }
        let Ok(env) = serde_json::from_slice::<Envelope>(&frame.payload) else {
            send(
                &out,
                &error_response(0, code::BAD_REQUEST, "malformed request"),
            );
            continue;
        };
        if env.op == "cancel" {
            if let Some(flag) = state
                .cancels
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .get(&env.target)
            {
                flag.store(true, Ordering::Relaxed);
            }
            send(&out, &json!({"id": env.id, "ok": true}));
            continue;
        }
        // A request id already in flight would overwrite (and later remove) the other job's cancel flag, and an
        // unbounded number of requests would each get a thread (audit F-12).
        workers.retain(|h| !h.is_finished());
        if workers.len() >= MAX_IN_FLIGHT {
            send(
                &out,
                &error_response(env.id, code::BUSY, "too many requests in flight"),
            );
            continue;
        }
        let cancel = Arc::new(AtomicBool::new(false));
        {
            let mut cancels = state.cancels.lock().unwrap_or_else(|e| e.into_inner());
            if cancels.contains_key(&env.id) {
                drop(cancels);
                send(
                    &out,
                    &error_response(env.id, code::BAD_REQUEST, "request id already in use"),
                );
                continue;
            }
            cancels.insert(env.id, Arc::clone(&cancel));
        }
        let (state, out2, payload) = (Arc::clone(&state), Arc::clone(&out), frame.payload);
        workers.retain(|h| !h.is_finished());
        workers.push(std::thread::spawn(move || {
            let id = env.id;
            let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                handle(&state, &env.op, &payload, &cancel)
            }));
            let resp = match result {
                Ok(Ok(mut v)) => {
                    v["id"] = json!(id);
                    v["ok"] = json!(true);
                    v
                }
                Ok(Err((c, m))) => error_response(id, c, &m),
                Err(_) => error_response(id, code::INTERNAL, "request panicked"),
            };
            state
                .cancels
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .remove(&id);
            send(&out2, &resp);
        }));
    }
    for h in workers {
        let _ = h.join();
    }
    state.stop.store(true, Ordering::Relaxed);
    Ok(())
}

/// The most requests the sidecar works on at once; more are refused with BUSY.
const MAX_IN_FLIGHT: usize = 64;

type HandlerResult = Result<Value, (&'static str, String)>;

fn parse<T: for<'de> Deserialize<'de>>(payload: &[u8]) -> Result<T, (&'static str, String)> {
    serde_json::from_slice(payload)
        .map_err(|e| (code::BAD_REQUEST, format!("bad request body: {e}")))
}

fn handle(state: &Arc<State>, op: &str, payload: &[u8], cancel: &AtomicBool) -> HandlerResult {
    match op {
        "ping" => Ok(json!({})),
        "index.build" => {
            let req: BuildReq = parse(payload)?;
            build(state, &req, cancel)
        }
        "index.stats" => match state
            .index
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .as_ref()
        {
            Some(i) => {
                let s = i.stats;
                Ok(json!({
                    "ready": true, "entries": s.entries, "truncated": s.truncated, "buildMs": s.build_ms, "bytes": s.bytes,
                    "live": live_stats(state),
                }))
            }
            None => Ok(json!({"ready": false})),
        },
        "index.query" => {
            let req: QueryReq = parse(payload)?;
            let Some(idx) = state
                .index
                .read()
                .unwrap_or_else(|e| e.into_inner())
                .clone()
            else {
                return Ok(json!({"ready": false, "entries": [], "truncated": false}));
            };
            let (entries, truncated) =
                idx.query(&Compiled::new(req.filters), &req.roots, req.limit);
            Ok(json!({"ready": true, "entries": entries, "truncated": truncated}))
        }
        // Recents from the live in-memory index, or {"ready": false} when it cannot answer exactly (no index yet,
        // the file watcher is not healthy so it may be stale, it was cut by its budget, or a root is not
        // exactly an indexed root). The caller then asks recents.scan, which walks.
        "recents.index" => {
            let req: RecentsReq = parse(payload)?;
            let Some(idx) = state
                .index
                .read()
                .unwrap_or_else(|e| e.into_inner())
                .clone()
            else {
                return Ok(json!({"ready": false}));
            };
            if !live_ok(state) {
                return Ok(json!({"ready": false}));
            }
            match idx.recents(&req.roots, req.limit) {
                Some(entries) => Ok(json!({"ready": true, "entries": entries, "partial": false})),
                None => Ok(json!({"ready": false})),
            }
        }
        "recents.scan" => {
            let req: RecentsReq = parse(payload)?;
            let deadline =
                (req.budget_ms > 0).then(|| Instant::now() + Duration::from_millis(req.budget_ms));
            let (entries, partial) = recents::scan(&req.roots, req.limit, deadline, cancel);
            if cancel.load(Ordering::Relaxed) {
                return Err((code::CANCELED, "scan canceled".into()));
            }
            Ok(json!({"entries": entries, "partial": partial}))
        }
        other => Err((code::BAD_REQUEST, format!("unknown op {other:?}"))),
    }
}

/// True while the file watcher covers every indexed directory and has not failed, and no lost events or rebuild
/// are outstanding, so the index can be trusted to be current.
fn live_ok(state: &State) -> bool {
    if state.building.load(Ordering::Acquire)
        || state.resync.load(Ordering::Acquire)
        || state.events.overflow_pending()
    {
        return false;
    }
    match state
        .watch
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .as_ref()
    {
        Some(w) => {
            w.health.complete.load(Ordering::Relaxed)
                && w.health
                    .error
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .is_empty()
        }
        None => false,
    }
}

fn live_stats(state: &State) -> Value {
    let (watches, complete, error) = match state
        .watch
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .as_ref()
    {
        Some(w) => (
            w.health.watches.load(Ordering::Relaxed),
            w.health.complete.load(Ordering::Relaxed),
            w.health
                .error
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .clone(),
        ),
        None => (0, false, "no watcher".to_string()),
    };
    json!({
        "watching": complete && error.is_empty(),
        "watches": watches,
        "updates": state.updates.load(Ordering::Relaxed),
        "rebuilds": state.rebuilds.load(Ordering::Relaxed),
        "error": error,
    })
}

/// Clears `building` if a build panics, so the sidecar does not answer BUSY to every later build. `release` clears it
/// on the normal paths and disarms the guard, so it can never clear the flag of a build that started afterwards.
struct BuildingGuard<'a>(&'a AtomicBool);

impl BuildingGuard<'_> {
    fn release(self) {
        self.0.store(false, Ordering::Release);
        std::mem::forget(self);
    }
}

impl Drop for BuildingGuard<'_> {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

/// A full walk. Pending change events are dropped when it starts (the walk reads the disk afresh) and events that
/// arrive during it are applied to its result afterwards.
fn build(state: &Arc<State>, req: &BuildReq, cancel: &AtomicBool) -> HandlerResult {
    {
        let _g = state.mutate.lock().unwrap_or_else(|e| e.into_inner());
        if state.building.swap(true, Ordering::AcqRel) {
            return Err((code::BUSY, "an index build is already running".into()));
        }
        state.events.clear();
    }
    let guard = BuildingGuard(&state.building);
    let idx = Index::build(
        &req.roots,
        Limits {
            max_entries: req.max_entries,
            max_bytes: req.max_bytes,
        },
        cancel,
    );
    if cancel.load(Ordering::Relaxed) {
        guard.release();
        return Err((code::CANCELED, "build canceled".into()));
    }
    let s = idx.stats;
    let idx = Arc::new(idx);
    // Watches go in right after the walk; a change made in that window waits for the next reconcile.
    if let Ok(w) = Watch::new(Arc::clone(&state.events)) {
        let w = Arc::new(w);
        *state.watch.lock().unwrap_or_else(|e| e.into_inner()) = Some(Arc::clone(&w));
        let (roots, for_watch) = (req.roots.clone(), Arc::clone(&idx));
        std::thread::spawn(move || w.register(&roots, for_watch.dirs()));
    }
    *state.last_build.lock().unwrap_or_else(|e| e.into_inner()) = Some(req.clone());
    *state.index.write().unwrap_or_else(|e| e.into_inner()) = Some(idx);
    state.resync.store(false, Ordering::Release);
    guard.release();
    if !state.applier_started.swap(true, Ordering::AcqRel) {
        let st = Arc::clone(state);
        std::thread::spawn(move || applier(st));
    }
    Ok(
        json!({"entries": s.entries, "truncated": s.truncated, "buildMs": s.build_ms, "bytes": s.bytes}),
    )
}

/// Applies batches of changed directories to the live index until the server stops.
fn applier(state: Arc<State>) {
    while let Some(batch) = state.events.next_batch(&state.stop) {
        let guard = state.mutate.lock().unwrap_or_else(|e| e.into_inner());
        if state.building.load(Ordering::Acquire) {
            state.events.restore(batch);
            drop(guard);
            std::thread::sleep(Duration::from_millis(300));
            continue;
        }
        let Some(current) = state
            .index
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
        else {
            continue;
        };
        let applied = if batch.overflow || batch.dirs.len() > MAX_DIRTY_DIRS {
            None
        } else {
            current.apply(&batch.dirs, &AtomicBool::new(false))
        };
        match applied {
            Some(a) => {
                if let Some(w) = state
                    .watch
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone()
                {
                    w.add(a.new_dirs.iter().map(String::as_str));
                }
                *state.index.write().unwrap_or_else(|e| e.into_inner()) = Some(Arc::new(a.index));
                state.updates.fetch_add(1, Ordering::Relaxed);
            }
            None => {
                state.resync.store(true, Ordering::Release);
                drop(guard);
                let req = state
                    .last_build
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .clone();
                if let Some(req) = req {
                    state.rebuilds.fetch_add(1, Ordering::Relaxed);
                    let _ = build(&state, &req, &AtomicBool::new(false));
                }
            }
        }
    }
}
