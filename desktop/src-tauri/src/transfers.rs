//! File downloads and uploads against the agent's transfer API, driven from the Rust core.
//!
//! Downloads read `GET /content` in `Range` segments into a hidden part file in the downloads folder
//! and move it to its final name only when it is whole and (when the agent can say) verified, so a
//! cancelled or failed download never leaves a file under a real name. A part file is kept after a
//! failure, and a retry continues from its size. Uploads use the resumable session API: the whole
//! file's SHA-256 is sent when the session opens, every chunk carries its own SHA-256, and the agent
//! verifies the whole file and renames it into place when the upload is completed. A retry asks the
//! agent which chunks it already has and sends only the rest.
//!
//! The window never sees a token or a URL: it starts a transfer by remote or local path and polls
//! `transfer_list`. At most [`PARALLEL`] transfers run at once, the rest wait. The list lives in
//! memory; after a restart the window starts with an empty list (a part file left behind by a crash
//! stays in the downloads folder under a name starting `.rfe-`).

use crate::agent_client::{
    network_error, read_json, response_error, AgentClient, AgentError, CLIENT_VERSION,
    MAX_JSON_BODY,
};
use crate::applog;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::HashSet;
use std::fs::File;
use std::future::Future;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::pin::Pin;
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicU8, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::{Duration, Instant, SystemTime};
use tokio::sync::{watch, Notify, OwnedSemaphorePermit, Semaphore};

/// How many transfers move data at the same time unless the person chose another number.
pub const PARALLEL: usize = 2;
/// The most the person can choose.
pub const MAX_PARALLEL: usize = 8;

const DEFAULT_CHUNK: usize = 8 << 20;
/// A part file can only be continued when the version it came from is known.
fn resume_is_unsafe(offset: u64, version: &Option<String>) -> bool {
    offset > 0 && version.is_none()
}

/// The most files one folder download may queue.
const MAX_FOLDER_FILES: usize = 50_000;
// The most an agent may ask for per chunk: a larger answer would make this side allocate it whole.
const MAX_SESSION_CHUNK: usize = 32 << 20;
const DEFAULT_SEGMENT: u64 = 4 << 20;
/// Upper bounds for one request, so a stalled connection ends; a healthy one never gets near them.
const SEGMENT_TIMEOUT: Duration = Duration::from_secs(300);
const CHUNK_TIMEOUT: Duration = Duration::from_secs(600);
/// The agent hashes the whole file before it answers.
const HASH_TIMEOUT: Duration = Duration::from_secs(3600);
const MAX_REMOTE_PATH: usize = 4096;
/// The folder under the user's download folder that holds every download.
pub const FOLDER_NAME: &str = "RFE Desktop";

#[derive(Clone, Copy, Debug)]
pub struct Options {
    /// Upload chunk size in bytes (the agent accepts up to 32 MiB).
    pub chunk_size: usize,
    /// Bytes asked for per download request.
    pub segment_size: u64,
    pub parallel: usize,
}

impl Default for Options {
    fn default() -> Self {
        Self {
            chunk_size: DEFAULT_CHUNK,
            segment_size: DEFAULT_SEGMENT,
            parallel: PARALLEL,
        }
    }
}

/// Where and as whom to talk to the agent. Read again at every start and every retry, so a login that
/// changed in between is used.
#[derive(Clone)]
pub struct Conn {
    pub host: String,
    pub fingerprint: String,
    pub token: String,
}

// Not derived: a `{:?}` in a log line or a test failure must not print the login token.
impl std::fmt::Debug for Conn {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Conn")
            .field("host", &self.host)
            .field("fingerprint", &self.fingerprint)
            .field("token", &"<redacted>")
            .finish()
    }
}

type BoxFuture<T> = Pin<Box<dyn Future<Output = T> + Send>>;
pub type Creds = Arc<dyn Fn() -> BoxFuture<Result<Conn, String>> + Send + Sync>;

/// What one transfer needs from the app: the connection and the downloads folder.
#[derive(Clone)]
pub struct Ctx {
    pub creds: Creds,
    pub download_dir: PathBuf,
}

impl Ctx {
    pub fn fixed(conn: Conn, download_dir: PathBuf) -> Self {
        Self {
            creds: Arc::new(move || {
                let c = conn.clone();
                Box::pin(async move { Ok(c) })
            }),
            download_dir,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Direction {
    Download,
    Upload,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum State {
    Queued,
    Running,
    /// Stopped by the user; the part file or upload session is kept so a resume continues from it.
    Paused,
    /// The name is taken at the destination and the person has to say what to do. The transfer holds
    /// no slot while it waits.
    Conflict,
    Done,
    Failed,
    Cancelled,
}

/// What a name that is already taken at the destination looks like, for the question to the person.
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ConflictView {
    pub is_dir: bool,
    pub size: u64,
    /// Milliseconds since 1970 when this computer knows it (a name taken in the downloads folder).
    pub modified_ms: Option<u64>,
    /// The agent's own text for the time (a name taken on the computer), empty when unknown.
    pub modified_text: String,
}

/// What the person (or the saved preference) decided for a name that is taken.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Resolution {
    /// Put the new file in place of the old one.
    Replace,
    /// Keep both: the new file gets `name (1)` and so on.
    Both,
    /// Leave the old one and do not transfer.
    Skip,
}

impl Resolution {
    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "replace" => Some(Self::Replace),
            "keep" => Some(Self::Both),
            "skip" => Some(Self::Skip),
            _ => None,
        }
    }
}

/// What to do when a name is taken and nobody has said yet.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Policy {
    Ask,
    Do(Resolution),
}

impl Policy {
    pub fn parse(s: &str) -> Option<Self> {
        if s == "ask" {
            return Some(Self::Ask);
        }
        Resolution::parse(s).map(Self::Do)
    }

    fn code(self) -> u8 {
        match self {
            Self::Ask => 0,
            Self::Do(Resolution::Replace) => 1,
            Self::Do(Resolution::Both) => 2,
            Self::Do(Resolution::Skip) => 3,
        }
    }

    fn from_code(c: u8) -> Self {
        match c {
            0 => Self::Ask,
            1 => Self::Do(Resolution::Replace),
            3 => Self::Do(Resolution::Skip),
            _ => Self::Do(Resolution::Both),
        }
    }
}

/// The text a transfer ends with when the person (or the preference) chose to leave the old file.
/// The window shows such a transfer as skipped, not as failed.
pub const SKIPPED: &str = "Skipped: a file with this name already exists";

/// What the window shows for one transfer.
#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TransferView {
    pub id: String,
    pub direction: Direction,
    pub state: State,
    /// The file's name: the sanitised local name for a download (its final name once done), the
    /// name on the computer for an upload.
    pub name: String,
    pub remote_path: String,
    /// Where a download was saved, or the file an upload reads. Empty until a download is done.
    pub local_path: String,
    pub done: u64,
    /// 0 while the size is not known yet.
    pub total: u64,
    pub error: String,
    /// The agent confirmed the content is intact (a matching SHA-256).
    pub verified: bool,
    /// The saved host (`host:port`) the transfer belongs to.
    pub host: String,
    /// Set while the state is `conflict`: what already has the name.
    pub conflict: Option<ConflictView>,
}

enum Kind {
    Download { part: PathBuf, name: String },
    Upload { src: PathBuf },
}

/// The same file, as far as a retry can tell.
#[derive(Clone, PartialEq, Debug)]
struct Ident {
    size: u64,
    mtime: Option<SystemTime>,
    id: (u64, u64),
}

#[derive(Clone)]
struct Session {
    id: String,
    chunk_size: usize,
    total_chunks: usize,
}

/// What a transfer keeps between attempts.
#[derive(Default)]
struct Resume {
    session: Option<Session>,
    ident: Option<Ident>,
    /// A download's validator of the remote file (its ETag or Last-Modified) as of the first byte
    /// kept in the part file; a resume only goes on if the agent still has that version.
    remote_version: Option<String>,
}

struct Item {
    view: Mutex<TransferView>,
    cancel: Mutex<watch::Sender<bool>>,
    ctx: Ctx,
    /// The agent this transfer was queued for. The saved login can be switched to another agent
    /// meanwhile; the paths, sessions and part files here mean nothing to that one.
    host: String,
    fingerprint: String,
    kind: Kind,
    resume: Mutex<Resume>,
    /// Set with the stop signal when the user pauses, so the stop keeps what was transferred.
    pausing: AtomicBool,
    /// A cancel was asked for: whatever else is pending, the attempt ends as a cancel.
    cancelling: AtomicBool,
    /// Held while a cancel or the end of an attempt decides between Paused and Cancelled, so the two
    /// can never both win.
    decide: Mutex<()>,
    /// What the person chose for a taken name; it holds for every later attempt of this transfer.
    resolution: Mutex<Option<Resolution>>,
    /// Wakes a transfer that waits in the `conflict` state.
    decided: Notify,
    /// The place in the line of running transfers. Given up while a question is open.
    slot: Mutex<Option<Slot>>,
}

fn lock<T>(m: &Mutex<T>) -> MutexGuard<'_, T> {
    m.lock().unwrap_or_else(|p| p.into_inner())
}

impl Item {
    fn update(&self, f: impl FnOnce(&mut TransferView)) {
        f(&mut lock(&self.view));
    }

    fn snapshot(&self) -> TransferView {
        lock(&self.view).clone()
    }

    fn set_progress(&self, done: u64, total: u64) {
        self.update(|v| {
            v.done = done;
            v.total = total;
        });
    }
}

/// A running place. When the person lowered the number of parallel transfers while all places were
/// in use, the next place given back is dropped instead of returned.
struct Slot {
    permit: Option<OwnedSemaphorePermit>,
    inner: Arc<Inner>,
}

impl Drop for Slot {
    fn drop(&mut self) {
        let Some(permit) = self.permit.take() else {
            return;
        };
        // Pay one unit of debt if any is owed. The debt is kept under a lock that the settings change
        // also holds while it takes places away, so a place given back can never slip past a cut.
        let mut owed = lock(&self.inner.debt);
        if *owed > 0 {
            *owed -= 1;
            permit.forget();
        } else {
            // Returned while the lock is still held, for the same reason.
            drop(permit);
        }
    }
}

/// What the person chose in the settings, read while a transfer runs.
struct Prefs {
    /// 0 is no limit; bytes per second.
    limit_bps: AtomicU64,
    policy: AtomicU8,
    verify: AtomicBool,
}

/// One speed limit for all transfers together: each piece of data reserves its time on the line.
struct Limiter {
    next_free: Mutex<Instant>,
}

impl Limiter {
    /// Waits until `bytes` may go by at `bps`. A line that was idle does not save up more than a
    /// quarter of a second.
    async fn take(&self, bps: u64, bytes: usize) {
        if bps == 0 || bytes == 0 {
            return;
        }
        let wait_until = {
            let mut next = lock(&self.next_free);
            let now = Instant::now();
            let start = (*next).max(now.checked_sub(Duration::from_millis(250)).unwrap_or(now));
            *next = start + Duration::from_secs_f64(bytes as f64 / bps as f64);
            start
        };
        let now = Instant::now();
        if wait_until > now {
            tokio::time::sleep(wait_until - now).await;
        }
    }
}

struct Inner {
    opts: Options,
    slots: Arc<Semaphore>,
    /// How many places exist (running transfers plus free places), after the last change.
    places: Mutex<usize>,
    debt: Mutex<usize>,
    prefs: Prefs,
    limiter: Limiter,
    items: Mutex<Vec<Arc<Item>>>,
}

#[derive(Clone)]
pub struct Transfers {
    inner: Arc<Inner>,
}

impl Default for Transfers {
    fn default() -> Self {
        Self::with_options(Options::default())
    }
}

impl Transfers {
    pub fn with_options(opts: Options) -> Self {
        let parallel = opts.parallel.clamp(1, MAX_PARALLEL);
        Self {
            inner: Arc::new(Inner {
                slots: Arc::new(Semaphore::new(parallel)),
                places: Mutex::new(parallel),
                debt: Mutex::new(0),
                prefs: Prefs {
                    limit_bps: AtomicU64::new(0),
                    // Without a window's preference a taken name keeps both files, as it always did.
                    policy: AtomicU8::new(Policy::Do(Resolution::Both).code()),
                    verify: AtomicBool::new(true),
                },
                limiter: Limiter {
                    next_free: Mutex::new(Instant::now()),
                },
                opts,
                items: Mutex::new(Vec::new()),
            }),
        }
    }

    /// Applies the settings. They count for transfers already running too: the speed limit at the
    /// next piece of data, the number of parallel transfers as places come free.
    pub fn set_prefs(
        &self,
        parallel: usize,
        limit_bytes_per_sec: u64,
        policy: Policy,
        verify: bool,
    ) {
        let inner = &self.inner;
        inner
            .prefs
            .limit_bps
            .store(limit_bytes_per_sec, Ordering::SeqCst);
        inner.prefs.policy.store(policy.code(), Ordering::SeqCst);
        inner.prefs.verify.store(verify, Ordering::SeqCst);
        let want = parallel.clamp(1, MAX_PARALLEL);
        let mut places = lock(&inner.places);
        if want > *places {
            // First cancel what is still owed from an earlier cut, then add the rest.
            let mut add = want - *places;
            {
                let mut owed = lock(&inner.debt);
                let pay = (*owed).min(add);
                *owed -= pay;
                add -= pay;
            }
            if add > 0 {
                inner.slots.add_permits(add);
            }
        } else if want < *places {
            let cut = *places - want;
            let mut owed = lock(&inner.debt);
            let taken = inner.slots.forget_permits(cut);
            *owed += cut - taken;
        }
        *places = want;
    }

    /// How many transfers move data now.
    pub fn running_count(&self) -> usize {
        lock(&self.inner.items)
            .iter()
            .filter(|i| lock(&i.view).state == State::Running)
            .count()
    }

    /// Pauses every running or waiting transfer, keeping what each has. Returns how many.
    pub fn pause_all(&self) -> usize {
        let ids: Vec<String> = self
            .list()
            .into_iter()
            .filter(|v| matches!(v.state, State::Running | State::Queued))
            .map(|v| v.id)
            .collect();
        ids.iter().filter(|id| self.pause(id).is_ok()).count()
    }

    /// The person's answer to a transfer in the `conflict` state.
    pub fn resolve(&self, id: &str, how: Resolution) -> Result<(), String> {
        let item = self.find(id)?;
        if item.snapshot().state != State::Conflict {
            return Err("that transfer is not waiting for an answer".into());
        }
        *lock(&item.resolution) = Some(how);
        item.decided.notify_one();
        Ok(())
    }

    /// Queues a download of `remote_path` into `ctx.download_dir`. Needs a tokio runtime.
    pub async fn start_download(&self, ctx: Ctx, remote_path: &str) -> Result<String, String> {
        validate_remote(remote_path, "enter the path of the file on the computer")?;
        let conn = (ctx.creds)().await?;
        let (host, fingerprint) = (conn.host, conn.fingerprint);
        std::fs::create_dir_all(&ctx.download_dir).map_err(|e| {
            format!(
                "cannot use the downloads folder {}: {e}",
                ctx.download_dir.display()
            )
        })?;
        let id = new_id()?;
        let name = safe_name(remote_path);
        let part = ctx.download_dir.join(format!(".rfe-{id}.part"));
        let view = TransferView {
            id: id.clone(),
            direction: Direction::Download,
            state: State::Queued,
            name: name.clone(),
            remote_path: remote_path.to_string(),
            local_path: String::new(),
            done: 0,
            total: 0,
            error: String::new(),
            verified: false,
            host: host.clone(),
            conflict: None,
        };
        self.add(
            ctx,
            (host, fingerprint),
            Kind::Download { part, name },
            view,
        );
        Ok(id)
    }

    /// Queues an upload of the regular file at `local_path` into the folder `remote_dir` on the
    /// computer. The file keeps its name and an existing file of that name is never replaced.
    pub async fn start_upload(
        &self,
        ctx: Ctx,
        local_path: impl AsRef<Path>,
        remote_dir: &str,
    ) -> Result<String, String> {
        let local_path = local_path.as_ref();
        // A Path, not a string: a name that is not valid UTF-8 must not be mangled on its way in (audit F-44).
        if local_path.to_string_lossy().trim().is_empty() {
            return Err("enter the full path of the file on this computer".into());
        }
        validate_remote(
            remote_dir,
            "enter the folder on the computer to upload into",
        )?;
        let src = local_path.to_path_buf();
        let (_file, ident) = open_source(&src)?;
        let name = remote_file_name(&src)?;
        let conn = (ctx.creds)().await?;
        let (host, fingerprint) = (conn.host, conn.fingerprint);
        let id = new_id()?;
        let remote_path = join_remote(remote_dir, &name);
        let view = TransferView {
            id: id.clone(),
            direction: Direction::Upload,
            state: State::Queued,
            name,
            remote_path: remote_path.clone(),
            local_path: local_path.to_string_lossy().into_owned(),
            done: 0,
            total: ident.size,
            error: String::new(),
            verified: false,
            host: host.clone(),
            conflict: None,
        };
        self.add(ctx, (host, fingerprint), Kind::Upload { src }, view);
        Ok(id)
    }

    fn add(&self, ctx: Ctx, (host, fingerprint): (String, String), kind: Kind, view: TransferView) {
        let (tx, rx) = watch::channel(false);
        let item = Arc::new(Item {
            view: Mutex::new(view),
            cancel: Mutex::new(tx),
            ctx,
            host,
            fingerprint,
            kind,
            resume: Mutex::new(Resume::default()),
            pausing: AtomicBool::new(false),
            cancelling: AtomicBool::new(false),
            decide: Mutex::new(()),
            resolution: Mutex::new(None),
            decided: Notify::new(),
            slot: Mutex::new(None),
        });
        lock(&self.inner.items).push(item.clone());
        tokio::spawn(run(self.inner.clone(), item, rx));
    }

    fn find(&self, id: &str) -> Result<Arc<Item>, String> {
        lock(&self.inner.items)
            .iter()
            .find(|i| lock(&i.view).id == id)
            .cloned()
            .ok_or_else(|| "no such transfer".to_string())
    }

    /// Every transfer, newest first.
    pub fn list(&self) -> Vec<TransferView> {
        lock(&self.inner.items)
            .iter()
            .rev()
            .map(|i| i.snapshot())
            .collect()
    }

    pub fn get(&self, id: &str) -> Option<TransferView> {
        self.find(id).ok().map(|i| i.snapshot())
    }

    /// Stops a waiting or running transfer, or gives up on a failed one. A download's part file
    /// and an upload's session on the computer are removed; a finished transfer is left alone.
    pub fn cancel(&self, id: &str) -> Result<(), String> {
        let item = self.find(id)?;
        let _deciding = lock(&item.decide);
        let state = item.snapshot().state;
        match state {
            State::Queued | State::Running | State::Conflict => {
                // A pause that was asked for but not yet seen must not turn this cancel into a pause.
                item.cancelling.store(true, Ordering::SeqCst);
                item.pausing.store(false, Ordering::SeqCst);
                lock(&item.cancel).send_replace(true);
            }
            State::Failed | State::Paused => {
                item.update(|v| {
                    v.state = State::Cancelled;
                    v.error.clear();
                });
                discard_local(&item);
                if let Some(session) = lock(&item.resume).session.take() {
                    let item = item.clone();
                    tokio::spawn(async move {
                        if let Ok(conn) = conn_for(&item).await {
                            delete_session(&conn, &session.id).await;
                        }
                    });
                }
            }
            State::Done | State::Cancelled => {}
        }
        Ok(())
    }

    /// Stops a waiting or running transfer without throwing away what it has: a download keeps its
    /// part file, an upload keeps its session. `retry` continues it.
    pub fn pause(&self, id: &str) -> Result<(), String> {
        let item = self.find(id)?;
        let state = item.snapshot().state;
        match state {
            State::Queued | State::Running => {
                item.pausing.store(true, Ordering::SeqCst);
                lock(&item.cancel).send_replace(true);
                Ok(())
            }
            State::Paused => Ok(()),
            _ => Err("that transfer cannot be paused now".into()),
        }
    }

    /// Starts a failed, paused or cancelled transfer again. A download continues from what it already has;
    /// an upload sends only the chunks the agent does not have.
    pub fn retry(&self, id: &str) -> Result<(), String> {
        let item = self.find(id)?;
        // Held for the whole restart so a cancel racing it is either seen or runs after it, never wiped (audit F-62).
        let deciding = lock(&item.decide);
        {
            let mut v = lock(&item.view);
            if !matches!(v.state, State::Failed | State::Paused | State::Cancelled) {
                return Err("that transfer cannot be retried now".into());
            }
            v.state = State::Queued;
            v.error.clear();
        }
        item.pausing.store(false, Ordering::SeqCst);
        item.cancelling.store(false, Ordering::SeqCst);
        let (tx, rx) = watch::channel(false);
        *lock(&item.cancel) = tx;
        drop(deciding);
        tokio::spawn(run(self.inner.clone(), item, rx));
        Ok(())
    }

    /// Removes finished and cancelled transfers from the list; failed ones stay until the user
    /// retries or cancels them. Returns how many were removed.
    pub fn clear_finished(&self) -> usize {
        let mut items = lock(&self.inner.items);
        let before = items.len();
        items.retain(|i| !matches!(lock(&i.view).state, State::Done | State::Cancelled));
        before - items.len()
    }
}

async fn wait_cancel(rx: &mut watch::Receiver<bool>) {
    if rx.wait_for(|stop| *stop).await.is_err() {
        // The sender is gone without a cancel: never end the transfer for that.
        std::future::pending::<()>().await;
    }
}

/// Waits for a running place, unless the transfer is stopped first.
async fn take_place(inner: &Arc<Inner>, item: &Item, cancel: &mut watch::Receiver<bool>) -> bool {
    let permit = tokio::select! {
        s = inner.slots.clone().acquire_owned() => s.ok(),
        _ = wait_cancel(cancel) => None,
    };
    match permit {
        Some(permit) => {
            *lock(&item.slot) = Some(Slot {
                permit: Some(permit),
                inner: inner.clone(),
            });
            true
        }
        None => false,
    }
}

/// One attempt at a transfer, from waiting for a slot to its end state.
async fn run(inner: Arc<Inner>, item: Arc<Item>, mut cancel: watch::Receiver<bool>) {
    if !take_place(&inner, &item, &mut cancel).await {
        stop(&item).await;
        return;
    }
    item.update(|v| {
        v.state = State::Running;
        v.error.clear();
        v.conflict = None;
    });
    let direction = item.snapshot().direction;
    let outcome = tokio::select! {
        r = work(&inner, &item) => Some(r),
        _ = wait_cancel(&mut cancel) => None,
    };
    drop(lock(&item.slot).take());
    match outcome {
        Some(Ok(())) => {
            item.update(|v| v.state = State::Done);
            applog::info(&format!("{direction:?} finished"));
        }
        Some(Err(msg)) if msg == SKIPPED => {
            discard_local(&item);
            let session = lock(&item.resume).session.take();
            if let Some(session) = session {
                if let Ok(conn) = conn_for(&item).await {
                    delete_session(&conn, &session.id).await;
                }
            }
            item.update(|v| {
                v.state = State::Cancelled;
                v.error = SKIPPED.to_string();
                v.conflict = None;
            });
            applog::info("transfer skipped");
        }
        Some(Err(msg)) => {
            // The message can carry the request URL (with the remote path) or a local path; the log feeds the
            // diagnostics report, so it only records that the transfer failed. The window shows the message.
            applog::error(&format!("{direction:?} failed"));
            item.update(|v| {
                v.state = State::Failed;
                v.error = msg;
            });
        }
        None => stop(&item).await,
    }
}

/// Settles a taken name: by the answer already given for this transfer, else by the saved choice,
/// else by asking the person. While the question is open the transfer gives up its running place,
/// so it does not hold up the others.
async fn settle_conflict(
    inner: &Arc<Inner>,
    item: &Item,
    existing: ConflictView,
) -> Result<Resolution, String> {
    if let Some(how) = *lock(&item.resolution) {
        return Ok(how);
    }
    let how = match Policy::from_code(inner.prefs.policy.load(Ordering::SeqCst)) {
        Policy::Do(how) => how,
        Policy::Ask => {
            drop(lock(&item.slot).take());
            item.update(|v| {
                v.state = State::Conflict;
                v.conflict = Some(existing);
                v.error.clear();
            });
            let how = loop {
                item.decided.notified().await;
                if let Some(how) = *lock(&item.resolution) {
                    break how;
                }
            };
            item.update(|v| {
                v.state = State::Queued;
                v.conflict = None;
            });
            let permit = inner
                .slots
                .clone()
                .acquire_owned()
                .await
                .map_err(|_| "the transfer list was closed".to_string())?;
            *lock(&item.slot) = Some(Slot {
                permit: Some(permit),
                inner: inner.clone(),
            });
            item.update(|v| v.state = State::Running);
            how
        }
    };
    *lock(&item.resolution) = Some(how);
    Ok(how)
}

/// The end of an attempt that was told to stop: a pause keeps the partial work, a cancel removes it.
async fn stop(item: &Item) {
    // A cancel wins over a pause, however the two were ordered against the end of the attempt. The
    // choice and the Paused state are published under the lock `cancel` takes, so a cancel either
    // comes first (and is seen here) or comes after (and finds the transfer Paused and discards it).
    let paused = {
        let _deciding = lock(&item.decide);
        let paused =
            item.pausing.swap(false, Ordering::SeqCst) && !item.cancelling.load(Ordering::SeqCst);
        if paused {
            item.update(|v| {
                v.state = State::Paused;
                v.error.clear();
            });
        }
        paused
    };
    if paused {
        applog::info("transfer paused");
    } else {
        cleanup_cancelled(item).await;
    }
}

async fn work(inner: &Arc<Inner>, item: &Item) -> Result<(), String> {
    match &item.kind {
        Kind::Download { .. } => download(inner, item).await,
        Kind::Upload { .. } => upload(inner, item).await,
    }
}

/// The saved login, if it is still the agent this transfer was queued for. After a switch to
/// another agent a transfer waits (fails, retryable) instead of acting on the wrong computer.
async fn conn_for(item: &Item) -> Result<Conn, String> {
    let conn = (item.ctx.creds)().await?;
    if conn.host != item.host || conn.fingerprint != item.fingerprint {
        return Err(format!(
            "This transfer belongs to {}. Switch back to that agent to continue it.",
            item.host
        ));
    }
    Ok(conn)
}

fn discard_local(item: &Item) {
    if let Kind::Download { part, .. } = &item.kind {
        let _ = std::fs::remove_file(part);
    }
}

async fn cleanup_cancelled(item: &Item) {
    discard_local(item);
    let session = lock(&item.resume).session.take();
    if let Some(session) = session {
        if let Ok(conn) = conn_for(item).await {
            delete_session(&conn, &session.id).await;
        }
    }
    item.update(|v| {
        v.state = State::Cancelled;
        v.error.clear();
    });
    applog::info("transfer cancelled");
}

/// Best effort: the agent forgets an upload session that nobody touches for a week anyway.
async fn delete_session(conn: &Conn, id: &str) {
    let Ok(client) = AgentClient::pinned(&conn.host, &conn.fingerprint) else {
        return;
    };
    let (http, base) = client.transport();
    // A chunk the agent is still reading holds the session for a moment; wait it out.
    for _ in 0..8 {
        let sent = http
            .delete(format!("{base}/transfers/{id}"))
            .bearer_auth(&conn.token)
            .header("X-RFE-Client-Version", CLIENT_VERSION)
            .timeout(Duration::from_secs(10))
            .send()
            .await;
        match sent {
            Ok(r) if r.status().as_u16() == 409 => {
                tokio::time::sleep(Duration::from_millis(250)).await
            }
            _ => return,
        }
    }
}

// ---- messages ----

/// Agent error codes this module words for transfers. A code listed here is shown with this text
/// instead of the generic one (a refusal to upload is not about managing devices).
pub const TRANSFER_CODES: &[&str] = &[
    "PATH_NOT_FOUND",
    "FORBIDDEN",
    "READ_ONLY",
    "CAPABILITY_DENIED",
    "CONFLICT",
    "HASH_MISMATCH",
    "CHUNK_HASH_MISMATCH",
    "PAYLOAD_TOO_LARGE",
    "RESOURCE_LIMIT",
    "TRANSFER_ACTIVE",
    "TRANSFER_NOT_OPEN",
];

pub fn transfer_message(code: &str) -> Option<&'static str> {
    Some(match code {
        "PATH_NOT_FOUND" => {
            "The computer has no file at that path. It may have been moved or deleted."
        }
        "FORBIDDEN" => {
            "This login may not use that path on the computer. It is outside the folders it can reach."
        }
        "READ_ONLY" => "The agent or this device is read-only, so it cannot accept uploads.",
        "CAPABILITY_DENIED" => {
            "This device has not been allowed to do that. Allow it on the computer, in the device's access settings."
        }
        "CONFLICT" => "A file with this name already exists on the computer.",
        "HASH_MISMATCH" => {
            "The file changed or was damaged while it was uploading. The upload was discarded; try again."
        }
        "CHUNK_HASH_MISMATCH" => {
            "A piece of the file was damaged in transit three times in a row. Try again."
        }
        "PAYLOAD_TOO_LARGE" => "The file is too large for the computer to accept.",
        "RESOURCE_LIMIT" => {
            "The computer has too many uploads open. Wait for one to finish, then retry."
        }
        "TRANSFER_ACTIVE" => {
            "The computer is still finishing the previous attempt. Retry in a moment."
        }
        "TRANSFER_NOT_OPEN" => {
            "The computer closed this upload. Retry starts it again from the beginning."
        }
        _ => return None,
    })
}

fn describe(e: &AgentError) -> String {
    if let AgentError::Server { code, .. } = e {
        if let Some(text) = transfer_message(code) {
            return text.to_string();
        }
    }
    e.to_string()
}

fn net(e: reqwest::Error) -> String {
    describe(&network_error(e))
}

async fn refused(resp: reqwest::Response) -> String {
    describe(&response_error(resp).await)
}

// ---- names and paths ----

/// The remote path as the agent will receive it, checked only for what could break the request.
fn validate_remote(path: &str, empty: &str) -> Result<(), String> {
    if path.trim().is_empty() {
        return Err(empty.to_string());
    }
    if path.contains('\0') || path.len() > MAX_REMOTE_PATH {
        return Err("that path on the computer is not usable".into());
    }
    Ok(())
}

fn is_invisible(c: char) -> bool {
    matches!(c,
        '\u{200b}'..='\u{200f}' | '\u{202a}'..='\u{202e}' | '\u{2060}'..='\u{2064}'
        | '\u{2066}'..='\u{2069}' | '\u{feff}')
}

/// A file name that is safe to create in the downloads folder, from whatever the remote path says:
/// only the last component (split on `/` and `\`), no control or direction-changing characters, no
/// leading dots (no hidden files, no `..`) and no trailing dots or spaces, at most 200 bytes with the
/// extension kept. Characters Windows forbids (`<>:"|?*`) become `_` and a Windows device name gets a
/// leading `_`. Never empty.
pub fn safe_name(remote_path: &str) -> String {
    let last = remote_path
        .rsplit(['/', '\\'])
        .find(|s| !s.is_empty())
        .unwrap_or("");
    let cleaned: String = last
        .chars()
        .filter(|c| !c.is_control() && !is_invisible(*c))
        .map(|c| if "<>:\"|?*".contains(c) { '_' } else { c })
        .collect();
    let trimmed = cleaned.trim_matches(|c: char| c == '.' || c.is_whitespace());
    if trimmed.is_empty() {
        return "download".to_string();
    }
    let reserved = is_windows_device(trimmed);
    let trimmed = &*if reserved {
        format!("_{trimmed}")
    } else {
        trimmed.to_string()
    };
    if trimmed.len() <= 200 {
        return trimmed.to_string();
    }
    let (stem, ext) = split_ext(trimmed);
    let keep = 200usize.saturating_sub(ext.len()).max(1);
    let mut end = keep.min(stem.len());
    while !stem.is_char_boundary(end) {
        end -= 1;
    }
    let short = stem[..end].trim_end_matches(|c: char| c == '.' || c.is_whitespace());
    format!("{}{ext}", if short.is_empty() { "download" } else { short })
}

/// A name Windows treats as a device (`CON`, `NUL.txt`, `COM1`...), whatever the extension or case.
fn is_windows_device(name: &str) -> bool {
    let stem = name.split('.').next().unwrap_or("").trim_end();
    let up = stem.to_ascii_uppercase();
    matches!(up.as_str(), "CON" | "PRN" | "AUX" | "NUL")
        || (up.len() == 4
            && (up.starts_with("COM") || up.starts_with("LPT"))
            && matches!(up.as_bytes()[3], b'1'..=b'9'))
}

fn split_ext(name: &str) -> (&str, &str) {
    match name.rfind('.') {
        Some(i) if i > 0 && name.len() - i <= 16 => (&name[..i], &name[i..]),
        _ => (name, ""),
    }
}

/// The `n`th candidate for `name`: itself, then `name (1)`, `name (2)` with the extension kept last.
pub fn numbered_name(name: &str, n: u32) -> String {
    if n == 0 {
        return name.to_string();
    }
    let (stem, ext) = split_ext(name);
    format!("{stem} ({n}){ext}")
}

/// Puts the finished part file at `dir/name`, or at `dir/name (1)` and so on if that is taken. The
/// file under its final name is created by a hard link (or an exclusive create when the file system
/// has no links), both of which fail instead of replacing what is there, so a file that appears
/// while the download runs is never overwritten. The part file is removed afterwards.
pub fn publish_no_clobber(part: &Path, dir: &Path, name: &str) -> std::io::Result<PathBuf> {
    for n in 0..10_000u32 {
        let target = dir.join(numbered_name(name, n));
        match std::fs::hard_link(part, &target) {
            Ok(()) => {
                let _ = std::fs::remove_file(part);
                return Ok(target);
            }
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(_) => match copy_exclusive(part, &target) {
                Ok(()) => {
                    let _ = std::fs::remove_file(part);
                    return Ok(target);
                }
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => continue,
                Err(e) => return Err(e),
            },
        }
    }
    Err(std::io::Error::new(
        std::io::ErrorKind::AlreadyExists,
        "every numbered name is taken",
    ))
}

/// Puts the finished part file at `dir/name` in place of what is there. A folder is never replaced.
/// The rename is one step, so the old file is never missing for a moment.
pub fn replace_file(part: &Path, dir: &Path, name: &str) -> std::io::Result<PathBuf> {
    let target = dir.join(name);
    if let Ok(m) = std::fs::symlink_metadata(&target) {
        if m.is_dir() {
            return Err(std::io::Error::other("a folder has that name"));
        }
    }
    std::fs::rename(part, &target)?;
    Ok(target)
}

fn copy_exclusive(from: &Path, to: &Path) -> std::io::Result<()> {
    let mut src = File::open(from)?;
    let mut dst = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(to)?;
    let copied = std::io::copy(&mut src, &mut dst).and_then(|_| dst.sync_all());
    if let Err(e) = copied {
        drop(dst);
        let _ = std::fs::remove_file(to);
        return Err(e);
    }
    Ok(())
}

/// The name an uploaded file gets on the computer: its own name without separators or control
/// characters. (Dot files are fine there; only the download side hides nothing from the user.)
fn remote_file_name(src: &Path) -> Result<String, String> {
    let raw = src
        .file_name()
        .map(|n| n.to_string_lossy().into_owned())
        .unwrap_or_default();
    let cleaned: String = raw
        .chars()
        .filter(|c| !c.is_control() && !is_invisible(*c))
        .map(|c| if c == '/' || c == '\\' { '_' } else { c })
        .collect();
    let cleaned = cleaned.trim().to_string();
    if cleaned.is_empty() || cleaned == "." || cleaned == ".." {
        return Err("that is not a regular file".into());
    }
    Ok(cleaned)
}

/// `dir` and `name` joined with the separator `dir` itself uses (a Windows agent's `C:\Users\me`
/// gets a backslash), without doubling a trailing one.
pub fn join_remote(dir: &str, name: &str) -> String {
    let sep = if dir.contains('\\') && !dir.contains('/') {
        '\\'
    } else {
        '/'
    };
    let trimmed = dir.trim_end_matches(['/', '\\']);
    format!("{trimmed}{sep}{name}")
}

// ---- local files ----

fn new_id() -> Result<String, String> {
    let mut b = [0u8; 8];
    getrandom::getrandom(&mut b).map_err(|e| format!("random: {e}"))?;
    Ok(format!("t{}", hex::encode(b)))
}

fn ident_of(m: &std::fs::Metadata) -> Ident {
    #[cfg(unix)]
    let id = {
        use std::os::unix::fs::MetadataExt;
        (m.dev(), m.ino())
    };
    // Windows has no stable inode: the creation and last-write stamps (100 ns ticks) stand in, so a
    // file replaced between the check and the open does not compare equal.
    #[cfg(windows)]
    let id = {
        use std::os::windows::fs::MetadataExt;
        (m.creation_time(), m.last_write_time())
    };
    #[cfg(not(any(unix, windows)))]
    let id = (u64::MAX, u64::MAX); // unknown platform: never claim two files are the same file
    Ident {
        size: m.len(),
        mtime: m.modified().ok(),
        id,
    }
}

/// Opens the file the user typed, if it is a regular file. The path must be absolute and its last
/// component must not be a symbolic link: a link is refused, never followed, so what is read is
/// what the user typed. The opened file is compared with what was checked, so a swap in between is
/// caught.
fn open_source(path: &Path) -> Result<(File, Ident), String> {
    if !path.is_absolute() {
        return Err("give the full path of the file, starting with /".into());
    }
    let read_error = |e: std::io::Error| format!("cannot read that file: {e}");
    let checked = std::fs::symlink_metadata(path).map_err(read_error)?;
    if checked.file_type().is_symlink() {
        return Err("that path is a symbolic link; type the real path of the file".into());
    }
    if !checked.is_file() {
        return Err("that is not a regular file".into());
    }
    let file = File::open(path).map_err(read_error)?;
    let opened = file.metadata().map_err(read_error)?;
    if !opened.is_file() || ident_of(&opened).id != ident_of(&checked).id {
        return Err("the file changed while it was being opened; try again".into());
    }
    Ok((file, ident_of(&opened)))
}

fn read_exact_at(file: &File, buf: &mut [u8], offset: u64) -> std::io::Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::FileExt;
        file.read_exact_at(buf, offset)
    }
    #[cfg(windows)]
    {
        use std::os::windows::fs::FileExt;
        let mut done = 0;
        while done < buf.len() {
            let n = file.seek_read(&mut buf[done..], offset + done as u64)?;
            if n == 0 {
                return Err(std::io::ErrorKind::UnexpectedEof.into());
            }
            done += n;
        }
        Ok(())
    }
}

fn hash_reader(mut r: impl Read) -> std::io::Result<String> {
    let mut h = Sha256::new();
    let mut buf = vec![0u8; 1 << 20];
    loop {
        let n = r.read(&mut buf)?;
        if n == 0 {
            return Ok(hex::encode(h.finalize()));
        }
        h.update(&buf[..n]);
    }
}

fn hash_file_at(file: &File, size: u64) -> std::io::Result<String> {
    let mut h = Sha256::new();
    let mut buf = vec![0u8; 1 << 20];
    let mut at = 0u64;
    while at < size {
        let n = ((size - at) as usize).min(buf.len());
        read_exact_at(file, &mut buf[..n], at)?;
        h.update(&buf[..n]);
        at += n as u64;
    }
    Ok(hex::encode(h.finalize()))
}

async fn blocking<T: Send + 'static>(
    f: impl FnOnce() -> Result<T, String> + Send + 'static,
) -> Result<T, String> {
    tokio::task::spawn_blocking(f)
        .await
        .map_err(|e| format!("a background task failed: {e}"))?
}

fn client_for(conn: &Conn) -> Result<AgentClient, String> {
    AgentClient::pinned(&conn.host, &conn.fingerprint).map_err(|e| describe(&e))
}

// ---- download ----

/// `(first, last, total)` from `Content-Range: bytes first-last/total`.
fn parse_content_range(h: &reqwest::header::HeaderMap) -> Option<(u64, u64, u64)> {
    let v = h
        .get("content-range")?
        .to_str()
        .ok()?
        .strip_prefix("bytes ")?;
    let (range, total) = v.split_once('/')?;
    let (a, b) = range.split_once('-')?;
    let (a, b, total) = (a.parse().ok()?, b.parse().ok()?, total.parse().ok()?);
    (a <= b && b < total).then_some((a, b, total))
}

/// What identifies this version of the remote file: a strong ETag, else its Last-Modified. The
/// agent sends the latter for every file; `If-Range` with it makes a changed file come back whole
/// instead of as a continuation of the old bytes.
fn validator(h: &reqwest::header::HeaderMap) -> Option<String> {
    let get = |name| h.get(name).and_then(|v| v.to_str().ok()).map(str::trim);
    match get(reqwest::header::ETAG) {
        Some(tag) if !tag.is_empty() && !tag.starts_with("W/") => Some(tag.to_string()),
        _ => get(reqwest::header::LAST_MODIFIED)
            .filter(|v| !v.is_empty())
            .map(str::to_string),
    }
}

/// The total from an unsatisfiable range's `Content-Range: bytes */total`.
fn parse_unsatisfied(h: &reqwest::header::HeaderMap) -> Option<u64> {
    h.get("content-range")?
        .to_str()
        .ok()?
        .strip_prefix("bytes */")?
        .parse()
        .ok()
}

fn open_part(part: &Path, truncate: bool) -> Result<File, String> {
    let mut o = std::fs::OpenOptions::new();
    o.write(true).create(true);
    if truncate {
        o.truncate(true);
    } else {
        o.append(true);
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        o.mode(0o600);
    }
    o.open(part)
        .map_err(|e| format!("cannot write {}: {e}", part.display()))
}

fn part_len(part: &Path) -> Result<u64, String> {
    match std::fs::metadata(part) {
        Ok(m) => Ok(m.len()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(0),
        Err(e) => Err(format!("cannot read {}: {e}", part.display())),
    }
}

/// Writes a response body to the part file as it arrives, so a dropped connection keeps what came.
#[allow(clippy::too_many_arguments)]
async fn append_body(
    mut resp: reqwest::Response,
    part: &Path,
    truncate: bool,
    expect: u64,
    from: u64,
    total: u64,
    item: &Item,
    inner: &Inner,
) -> Result<(), String> {
    let mut file = open_part(part, truncate)?;
    let mut written = 0u64;
    while let Some(chunk) = resp.chunk().await.map_err(net)? {
        // The speed limit is kept by reading no faster: the connection itself slows the agent down.
        let limit = inner.prefs.limit_bps.load(Ordering::Relaxed);
        inner.limiter.take(limit, chunk.len()).await;
        // Check before writing: bytes past the asked-for range must never reach the part file.
        if written + chunk.len() as u64 > expect {
            return Err("unexpected response: the agent sent more than was asked for".into());
        }
        file.write_all(&chunk)
            .map_err(|e| format!("cannot write {}: {e}", part.display()))?;
        written += chunk.len() as u64;
        item.set_progress(from + written, total);
    }
    file.flush()
        .map_err(|e| format!("cannot write {}: {e}", part.display()))?;
    if written != expect {
        return Err("the agent ended the download early".into());
    }
    Ok(())
}

/// The agent's SHA-256 of `path`. `Ok(None)` only when the agent does not offer the endpoint (an older
/// agent: 404, 405 or 501); any other failure is an error, because a download the person asked to verify must
/// not be published as if it had been (audit F-61).
async fn remote_checksum(
    http: &reqwest::Client,
    base: &str,
    conn: &Conn,
    path: &str,
) -> Result<Option<String>, String> {
    #[derive(Deserialize)]
    struct Sum {
        checksum: String,
    }
    let resp = http
        .get(format!("{base}/fs/checksum"))
        .query(&[("path", path), ("algo", "sha256")])
        .bearer_auth(&conn.token)
        .header("X-RFE-Client-Version", CLIENT_VERSION)
        .timeout(HASH_TIMEOUT)
        .send()
        .await
        .map_err(|e| format!("the computer did not answer the check: {e}"))?;
    let status = resp.status();
    if matches!(status.as_u16(), 404 | 405 | 501) {
        // A 404 can also mean the file itself is gone (PATH_NOT_FOUND); that is a failure, not an agent
        // that predates the check.
        let body = resp.text().await.unwrap_or_default();
        if body.contains("PATH_NOT_FOUND") {
            return Err("the file is no longer on the computer, so it could not be checked".into());
        }
        return Ok(None);
    }
    if !status.is_success() {
        return Err(format!("the computer refused the check ({status})"));
    }
    read_json::<Sum>(resp, 1 << 20)
        .await
        .map(|s| Some(s.checksum))
        .map_err(|_| "the computer's answer to the check was not understood".to_string())
}

async fn download(inner: &Arc<Inner>, item: &Item) -> Result<(), String> {
    let opts = &inner.opts;
    let Kind::Download { part, name } = &item.kind else {
        return Err("internal: not a download".into());
    };
    // A name that is taken in the downloads folder is settled before any data moves.
    let taken = item.ctx.download_dir.join(name);
    let how = match std::fs::symlink_metadata(&taken) {
        Ok(m) => {
            let modified_ms = m
                .modified()
                .ok()
                .and_then(|t| t.duration_since(SystemTime::UNIX_EPOCH).ok())
                .map(|d| d.as_millis() as u64);
            let existing = ConflictView {
                is_dir: m.is_dir(),
                size: m.len(),
                modified_ms,
                modified_text: String::new(),
            };
            match settle_conflict(inner, item, existing).await? {
                Resolution::Skip => return Err(SKIPPED.into()),
                how => Some(how),
            }
        }
        Err(_) => *lock(&item.resolution),
    };
    let verify = inner.prefs.verify.load(Ordering::SeqCst);
    let conn = conn_for(item).await?;
    let client = client_for(&conn)?;
    let (http, base) = client.transport();
    let remote = item.snapshot().remote_path;
    let segment = opts.segment_size.max(1);

    let mut total: Option<u64> = None;
    let mut restarted = false;
    let mut version = lock(&item.resume).remote_version.clone();
    // Bytes left by an earlier attempt with no validator to prove they belong to the file as it is now cannot
    // be continued safely: start over rather than append a changed file onto them (audit F-142). Checked once,
    // before the first request, so a server that never sends a validator is not restarted after every segment.
    if resume_is_unsafe(part_len(part)?, &version) {
        std::fs::File::create(part)
            .map_err(|e| format!("cannot restart {}: {e}", part.display()))?;
    }
    loop {
        let offset = part_len(part)?;
        if total.is_some_and(|t| offset >= t) {
            break;
        }
        let mut req = http
            .get(format!("{base}/content"))
            .query(&[("path", remote.as_str())])
            .bearer_auth(&conn.token)
            .header("X-RFE-Client-Version", CLIENT_VERSION)
            .header("Range", format!("bytes={offset}-{}", offset + segment - 1))
            // A gzip body would not line up with byte offsets; a ranged request is never gzipped,
            // and this keeps the first one honest too.
            .header("Accept-Encoding", "identity")
            .timeout(SEGMENT_TIMEOUT);
        // Continuing is only right if the file is the one the first bytes came from; if not, the
        // agent answers with the whole new file (200) and the part is started over below.
        if let (true, Some(v)) = (offset > 0, &version) {
            req = req.header("If-Range", v);
        }
        let resp = req.send().await.map_err(net)?;
        // A response that starts at byte 0 says which version the part file will hold.
        let first_bytes = offset == 0 || resp.status().as_u16() == 200;
        if first_bytes {
            version = validator(resp.headers());
            lock(&item.resume).remote_version = version.clone();
        }
        match resp.status().as_u16() {
            206 => {
                let (first, last, size) = parse_content_range(resp.headers())
                    .ok_or("unexpected response: the agent sent a range this app cannot read")?;
                if first != offset {
                    return Err("unexpected response: the agent sent a different range".into());
                }
                total = Some(size);
                item.set_progress(offset, size);
                append_body(
                    resp,
                    part,
                    false,
                    last - first + 1,
                    offset,
                    size,
                    item,
                    inner,
                )
                .await?;
            }
            200 => {
                // The agent ignored the range: it sent the whole file (an empty one, always).
                let size = resp
                    .content_length()
                    .ok_or("unexpected response: no length")?;
                total = Some(size);
                item.set_progress(0, size);
                append_body(resp, part, true, size, 0, size, item, inner).await?;
                if size == 0 {
                    break;
                }
            }
            416 => match parse_unsatisfied(resp.headers()) {
                Some(size) if size == offset => {
                    total = Some(size);
                }
                // The part is longer than the file now: it belongs to an older version.
                Some(size) if offset > size && !restarted => {
                    restarted = true;
                    drop(open_part(part, true)?);
                }
                _ => return Err("unexpected response: the agent refused the range".into()),
            },
            _ => return Err(refused(resp).await),
        }
    }

    // Whole. Compare with what the agent has before the file gets a real name.
    let size = total.unwrap_or(0);
    item.set_progress(size, size);
    let verified = if verify {
        let local = {
            let part = part.clone();
            blocking(move || {
                File::open(&part)
                    .and_then(hash_reader)
                    .map_err(|e| format!("cannot read {}: {e}", part.display()))
            })
            .await?
        };
        match remote_checksum(http, base, &conn, &remote).await {
            Ok(Some(sum)) if sum.eq_ignore_ascii_case(&local) => true,
            Ok(Some(_)) => {
                let _ = std::fs::remove_file(part);
                return Err("The downloaded copy does not match the file on the computer. It was discarded; start it again.".into());
            }
            Ok(None) => false,
            Err(why) => {
                return Err(format!(
                    "The download could not be checked against the computer: {why}. Retry it, or turn off verification."
                ));
            }
        }
    } else {
        false
    };
    let (part, dir, name) = (part.clone(), item.ctx.download_dir.clone(), name.clone());
    let final_path = blocking(move || {
        let placed = match how {
            Some(Resolution::Replace) => replace_file(&part, &dir, &name),
            _ => publish_no_clobber(&part, &dir, &name),
        };
        placed.map_err(|e| format!("the downloaded file could not be put in place: {e}"))
    })
    .await?;
    item.update(|v| {
        v.name = final_path
            .file_name()
            .map(|n| n.to_string_lossy().into_owned())
            .unwrap_or_default();
        v.local_path = final_path.display().to_string();
        v.verified = verified;
    });
    Ok(())
}

// ---- upload ----

#[derive(Deserialize, Default)]
#[serde(rename_all = "camelCase", default)]
struct SessionBody {
    id: String,
    chunk_size: usize,
    total_chunks: usize,
    received_chunks: Vec<usize>,
    status: String,
}

/// How many chunks the agent opens for `size` bytes: `ceil(size / chunk)`, and one for an empty file.
fn expected_chunks(size: u64, chunk: u64) -> u64 {
    size.div_ceil(chunk.max(1)).max(1)
}

fn bad_session(b: &SessionBody, size: u64) -> bool {
    b.id.is_empty()
        || b.id
            .contains(|c: char| !(c.is_ascii_alphanumeric() || c == '-' || c == '_'))
        || b.chunk_size == 0
        || b.chunk_size > MAX_SESSION_CHUNK
        || b.total_chunks as u64 != expected_chunks(size, b.chunk_size as u64)
}

fn authed(rb: reqwest::RequestBuilder, conn: &Conn) -> reqwest::RequestBuilder {
    rb.bearer_auth(&conn.token)
        .header("X-RFE-Client-Version", CLIENT_VERSION)
}

/// `name (1)`-style numbering for a path on the computer, in the style of the path's own separators.
fn numbered_remote(path: &str, n: u32) -> String {
    match path.rfind(['/', '\\']) {
        Some(i) => format!("{}{}", &path[..=i], numbered_name(&path[i + 1..], n)),
        None => numbered_name(path, n),
    }
}

/// What the agent says about the entry already at `path`; blanks where it does not say.
async fn remote_existing(
    http: &reqwest::Client,
    base: &str,
    conn: &Conn,
    path: &str,
) -> ConflictView {
    #[derive(Deserialize, Default)]
    #[serde(rename_all = "camelCase", default)]
    struct Meta {
        size: i64,
        modified: String,
        is_dir: bool,
    }
    let blank = ConflictView {
        is_dir: false,
        size: 0,
        modified_ms: None,
        modified_text: String::new(),
    };
    let Ok(resp) = authed(http.get(format!("{base}/fs/meta")), conn)
        .query(&[("path", path)])
        .timeout(Duration::from_secs(30))
        .send()
        .await
    else {
        return blank;
    };
    if !resp.status().is_success() {
        return blank;
    }
    match read_json::<Meta>(resp, MAX_JSON_BODY).await {
        Ok(m) => ConflictView {
            is_dir: m.is_dir,
            size: m.size.max(0) as u64,
            modified_ms: None,
            modified_text: m.modified,
        },
        Err(_) => blank,
    }
}

async fn upload(inner: &Arc<Inner>, item: &Item) -> Result<(), String> {
    let opts = &inner.opts;
    let Kind::Upload { src, .. } = &item.kind else {
        return Err("internal: not an upload".into());
    };
    let mut remote_path = item.snapshot().remote_path;
    let mut conn = conn_for(item).await?;
    let client = client_for(&conn)?;
    let (http, base) = client.transport();

    let (file, ident) = open_source(src)?;
    let file = Arc::new(file);
    let size = ident.size;
    item.set_progress(0, size);

    // Is the session from an earlier attempt still good, and for the same file? One for a file
    // that has changed since is of no use and would sit on the agent (with its temporary file), so
    // it is closed there before a new one is opened.
    let (saved, stale) = {
        let mut r = lock(&item.resume);
        match (r.session.clone(), &r.ident) {
            (Some(s), Some(i)) if *i == ident => (Some(s), None),
            (Some(s), _) => {
                r.session = None;
                (None, Some(s))
            }
            _ => (None, None),
        }
    };
    if let Some(s) = stale {
        delete_session(&conn, &s.id).await;
    }
    let mut have: HashSet<usize> = HashSet::new();
    let mut session = None;
    if let Some(s) = saved {
        let resp = authed(http.get(format!("{base}/transfers/{}", s.id)), &conn)
            .timeout(Duration::from_secs(30))
            .send()
            .await
            .map_err(net)?;
        match resp.status().as_u16() {
            200 => {
                let body: SessionBody = read_json(resp, MAX_JSON_BODY)
                    .await
                    .map_err(|e| e.to_string())?;
                match body.status.as_str() {
                    "open" => {
                        if body.received_chunks.iter().any(|&n| n >= s.total_chunks) {
                            // The saved session is not trustworthy; forget it so a retry starts a fresh
                            // one instead of failing the same way every time (audit F-88).
                            lock(&item.resume).session = None;
                            return Err(
                                "unexpected response: the agent listed a chunk this upload does not have"
                                    .into(),
                            );
                        }
                        have = body.received_chunks.into_iter().collect();
                        session = Some(s);
                    }
                    "completed" => {
                        lock(&item.resume).session = None;
                        item.set_progress(size, size);
                        item.update(|v| v.verified = true);
                        return Ok(());
                    }
                    _ => {}
                }
            }
            404 => {}
            _ => return Err(refused(resp).await),
        }
    }

    let session = match session {
        Some(s) => s,
        None => {
            lock(&item.resume).session = None;
            item.set_progress(0, size);
            let sha256 = {
                let file = file.clone();
                blocking(move || {
                    hash_file_at(&file, size).map_err(|e| format!("cannot read that file: {e}"))
                })
                .await?
            };
            // With a small limit the chunks are small too, so the data moves evenly and not in
            // bursts of seconds. The chunk size is fixed when the session opens.
            let limit = inner.prefs.limit_bps.load(Ordering::SeqCst);
            let chunk_size = if limit == 0 {
                opts.chunk_size
            } else {
                ((limit / 4) as usize).clamp(256 << 10, opts.chunk_size)
            };
            let mut how = *lock(&item.resolution);
            let mut copy = 0u32;
            let body = loop {
                let resp = authed(http.post(format!("{base}/transfers")), &conn)
                    .json(&serde_json::json!({
                        "path": remote_path,
                        "size": size,
                        "sha256": sha256,
                        "chunkSize": chunk_size,
                        "overwrite": how == Some(Resolution::Replace),
                    }))
                    .timeout(Duration::from_secs(60))
                    .send()
                    .await
                    .map_err(net)?;
                if resp.status().as_u16() == 201 {
                    break read_json::<SessionBody>(resp, MAX_JSON_BODY)
                        .await
                        .map_err(|e| e.to_string())?;
                }
                let e = response_error(resp).await;
                if !matches!(&e, AgentError::Server { code, .. } if code == "CONFLICT") {
                    return Err(describe(&e));
                }
                // The name is taken on the computer.
                let existing = remote_existing(http, base, &conn, &remote_path).await;
                match settle_conflict(inner, item, existing).await? {
                    Resolution::Skip => return Err(SKIPPED.into()),
                    Resolution::Replace => how = Some(Resolution::Replace),
                    Resolution::Both => {
                        how = Some(Resolution::Both);
                        copy += 1;
                        if copy > 1000 {
                            return Err("every numbered name on the computer is taken".into());
                        }
                        let base_path = item.snapshot().remote_path;
                        remote_path = numbered_remote(&base_path, copy);
                        let shown = remote_path.clone();
                        item.update(|v| {
                            v.name = shown
                                .rsplit(['/', '\\'])
                                .next()
                                .unwrap_or_default()
                                .to_string();
                        });
                    }
                }
                // The answer may have taken a while: use the login as it is now.
                conn = conn_for(item).await?;
            };
            if bad_session(&body, size) {
                return Err(
                    "unexpected response: the agent sent a session this app cannot use".into(),
                );
            }
            let s = Session {
                id: body.id,
                chunk_size: body.chunk_size,
                total_chunks: body.total_chunks,
            };
            let mut r = lock(&item.resume);
            r.session = Some(s.clone());
            r.ident = Some(ident.clone());
            s
        }
    };

    let chunk_len = |n: usize| -> u64 {
        let start = n as u64 * session.chunk_size as u64;
        size.saturating_sub(start).min(session.chunk_size as u64)
    };
    let mut sent: u64 = have.iter().map(|&n| chunk_len(n)).sum();
    item.set_progress(sent, size);

    for n in 0..session.total_chunks {
        if have.contains(&n) {
            continue;
        }
        let len = chunk_len(n) as usize;
        let (data, sum) = {
            let file = file.clone();
            let at = n as u64 * session.chunk_size as u64;
            blocking(move || {
                let mut buf = vec![0u8; len];
                read_exact_at(&file, &mut buf, at)
                    .map_err(|e| format!("cannot read that file: {e}"))?;
                let sum = hex::encode(Sha256::digest(&buf));
                Ok((buf, sum))
            })
            .await?
        };
        let mut damaged = 0;
        loop {
            // The speed limit: this piece reserves its time on the shared line first.
            let limit = inner.prefs.limit_bps.load(Ordering::Relaxed);
            inner.limiter.take(limit, len).await;
            let resp = authed(
                http.put(format!("{base}/transfers/{}/chunks/{n}", session.id)),
                &conn,
            )
            .header("X-Chunk-Sha256", &sum)
            .header("Content-Type", "application/octet-stream")
            .body(data.clone())
            .timeout(CHUNK_TIMEOUT)
            .send()
            .await
            .map_err(net)?;
            match resp.status().as_u16() {
                204 => break,
                409 if damaged < 2 => {
                    let e = response_error(resp).await;
                    if matches!(&e, AgentError::Server { code, .. } if code == "CHUNK_HASH_MISMATCH")
                    {
                        damaged += 1;
                        continue;
                    }
                    return Err(describe(&e));
                }
                _ => return Err(refused(resp).await),
            }
        }
        sent += len as u64;
        item.set_progress(sent, size);
    }

    // The file may have been edited while it was going: say so now, not after the agent hashes the whole thing.
    match file.metadata() {
        Ok(m) if ident_of(&m) == ident => {}
        _ => return Err("That file changed while it was being sent. Send it again.".into()),
    }
    // Every chunk is there: the agent hashes the whole file and renames it into place.
    item.set_progress(size, size);
    let resp = authed(
        http.post(format!("{base}/transfers/{}/complete", session.id)),
        &conn,
    )
    .timeout(HASH_TIMEOUT)
    .send()
    .await
    .map_err(net)?;
    if !resp.status().is_success() {
        let e = response_error(resp).await;
        if matches!(&e, AgentError::Server { code, .. } if code == "HASH_MISMATCH" || code == "TRANSFER_NOT_OPEN")
        {
            // The agent closed this session; the next attempt must open a new one.
            lock(&item.resume).session = None;
        }
        if matches!(&e, AgentError::Server { code, .. } if code == "CONFLICT") {
            // Something took the name while the file was on its way. Retire the session, so the
            // next attempt opens a new one and settles the name by the conflict choice again.
            lock(&item.resume).session = None;
            delete_session(&conn, &session.id).await;
        }
        return Err(describe(&e));
    }
    #[derive(Deserialize, Default)]
    #[serde(default)]
    struct Done {
        verified: bool,
    }
    // Read through the same size cap as every other agent answer: a pinned agent that never ends a 200 body
    // must not grow memory without limit (audit F-150).
    let done: Done = read_json(resp, MAX_JSON_BODY).await.unwrap_or_default();
    lock(&item.resume).session = None;
    item.update(|v| v.verified = done.verified);
    Ok(())
}

// ---- the app's side: credentials and the downloads folder ----

/// The file that remembers the folder the person chose for downloads, in the app's data folder.
const FOLDER_FILE: &str = "download-folder.txt";

/// The default folder: the user's download folder (the XDG one on Linux) plus `RFE Desktop`.
fn default_download_folder(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    use tauri::Manager;
    let paths = app.path();
    let base = paths
        .download_dir()
        .or_else(|_| paths.home_dir().map(|h| h.join("Downloads")))
        .map_err(|_| "cannot find your Downloads folder".to_string())?;
    Ok(base.join(FOLDER_NAME))
}

/// The folder every download goes to: the one the person chose in the system's folder dialog (the
/// core keeps it, the window cannot name a path), or the default one. No command takes a
/// destination from the window.
pub fn download_folder(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    use tauri::Manager;
    if let Ok(dir) = app.path().app_data_dir() {
        if let Ok(text) = std::fs::read_to_string(dir.join(FOLDER_FILE)) {
            let chosen = PathBuf::from(text.trim());
            if chosen.is_absolute() && chosen.is_dir() {
                return Ok(chosen);
            }
        }
    }
    default_download_folder(app)
}

/// Where this download goes: the usual folder, or (when the window asks to ask) one the person
/// picks now in the system's folder dialog.
async fn download_target(app: &tauri::AppHandle, ask_where: bool) -> Result<PathBuf, String> {
    if !ask_where {
        return download_folder(app);
    }
    let start = download_folder(app).ok().map(|p| p.display().to_string());
    match crate::native::pick_folder(app.clone(), Some("Save to…".into()), start).await? {
        Some(p) if std::path::Path::new(&p).is_absolute() => Ok(PathBuf::from(p)),
        _ => Err("No folder was chosen, so nothing was downloaded.".into()),
    }
}

/// Lets the person choose the folder downloads go to, and remembers it. Returns the new folder, or
/// nothing when they cancelled.
#[tauri::command]
pub async fn choose_download_folder(app: tauri::AppHandle) -> Result<Option<String>, String> {
    use tauri::Manager;
    let start = download_folder(&app).ok().map(|p| p.display().to_string());
    let Some(p) =
        crate::native::pick_folder(app.clone(), Some("Folder for downloads".into()), start).await?
    else {
        return Ok(None);
    };
    let chosen = PathBuf::from(&p);
    if !chosen.is_absolute() || !chosen.is_dir() {
        return Err("That is not a folder on this computer.".into());
    }
    let dir = app.path().app_data_dir().map_err(|e| e.to_string())?;
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    std::fs::write(dir.join(FOLDER_FILE), p.as_bytes())
        .map_err(|e| format!("Could not remember the folder: {e}"))?;
    Ok(Some(p))
}

/// Goes back to the default downloads folder.
#[tauri::command]
pub async fn reset_download_folder(app: tauri::AppHandle) -> Result<String, String> {
    use tauri::Manager;
    if let Ok(dir) = app.path().app_data_dir() {
        let _ = std::fs::remove_file(dir.join(FOLDER_FILE));
    }
    Ok(download_folder(&app)?.display().to_string())
}

/// Credentials from the saved login: the keystore holds the token, `state.json` the agent's address
/// and the certificate the user trusted.
/// `host` scopes the login to one saved host (`host:port`); `None` is the active session. The
/// transfer keeps it, so switching the active host never sends a transfer to another agent.
pub fn saved_login(data_dir: PathBuf, host: Option<String>) -> Creds {
    use crate::secrets::Offloaded;
    Arc::new(move || {
        let dir = data_dir.clone();
        let host = host.clone();
        Box::pin(async move {
            let saved = Offloaded::os_keystore()
                .scoped(host)
                .run(move |s| crate::flows::load_saved(&dir, s))
                .await?;
            if saved.token.is_empty() || saved.host.is_empty() {
                return Err("not signed in".to_string());
            }
            Ok(Conn {
                host: saved.host,
                fingerprint: saved.fingerprint,
                token: saved.token,
            })
        })
    })
}

fn app_ctx(app: &tauri::AppHandle, host: Option<String>) -> Result<Ctx, String> {
    use tauri::Manager;
    let data_dir = app.path().app_data_dir().map_err(|e| e.to_string())?;
    Ok(Ctx {
        creds: saved_login(data_dir, host),
        download_dir: download_folder(app)?,
    })
}

/// Like `app_ctx`, with the folder for this one download.
fn app_ctx_in(
    app: &tauri::AppHandle,
    host: Option<String>,
    folder: PathBuf,
) -> Result<Ctx, String> {
    let mut ctx = app_ctx(app, host)?;
    ctx.download_dir = folder;
    Ok(ctx)
}

#[tauri::command]
pub async fn transfer_download(
    app: tauri::AppHandle,
    state: tauri::State<'_, Transfers>,
    host: Option<String>,
    remote_path: String,
    ask_where: Option<bool>,
) -> Result<String, String> {
    let folder = download_target(&app, ask_where.unwrap_or(false)).await?;
    state
        .start_download(app_ctx_in(&app, host, folder)?, &remote_path)
        .await
}

#[tauri::command]
pub async fn transfer_upload(
    app: tauri::AppHandle,
    state: tauri::State<'_, Transfers>,
    host: Option<String>,
    local_path: String,
    remote_dir: String,
) -> Result<String, String> {
    state
        .start_upload(app_ctx(&app, host)?, &local_path, &remote_dir)
        .await
}

/// Uploads a folder (or one file) from this computer: the folder's own tree is made on the
/// computer first, then every file in it is queued. Returns the ids of the queued uploads.
#[tauri::command]
pub async fn transfer_upload_tree(
    app: tauri::AppHandle,
    state: tauri::State<'_, Transfers>,
    host: Option<String>,
    local_path: String,
    remote_dir: String,
) -> Result<Vec<String>, String> {
    validate_remote(
        &remote_dir,
        "enter the folder on the computer to upload into",
    )?;
    let lp = local_path.clone();
    let (items, folders) = tokio::task::spawn_blocking(move || crate::local::walk_all(&lp))
        .await
        .map_err(|e| e.to_string())??;
    if items.is_empty() && folders.is_empty() {
        return Err("that folder holds no files to upload".into());
    }
    let dir = {
        use tauri::Manager;
        app.path().app_data_dir().map_err(|e| e.to_string())?
    };
    let store = crate::secrets::Offloaded::os_keystore().scoped(host.clone());
    // Each folder of the tree, empty ones too, shallowest first. One that already exists is fine.
    for rel in &folders {
        let (up, step) = match rel.rsplit_once('/') {
            Some((parent, step)) => (join_remote(&remote_dir, parent), step),
            None => (remote_dir.clone(), rel.as_str()),
        };
        if crate::files::create_folder(&dir, &store, &up, step)
            .await
            .is_err()
        {
            // Already there is fine; anything else (a file in the way, no right to write) is not.
            let is_folder = folder_exists(&dir, &store, &up, step).await;
            if !is_folder {
                return Err(format!("cannot make the folder {step} on the computer"));
            }
        }
    }
    let mut ids = Vec::with_capacity(items.len());
    let queued: Result<(), String> = async {
        for (path, rel) in items {
            let target = match rel.rsplit_once('/') {
                Some((parent, _)) => join_remote(&remote_dir, parent),
                None => remote_dir.clone(),
            };
            let id = state
                .start_upload(app_ctx(&app, host.clone())?, &path, &target)
                .await?;
            ids.push(id);
        }
        Ok(())
    }
    .await;
    settle_queued(&state, queued, ids)
}

/// Whether `up` holds a folder named `step`, reading its pages in turn: a folder of thousands of
/// entries does not fit in the first one.
async fn folder_exists(
    dir: &Path,
    store: &crate::secrets::Offloaded,
    up: &str,
    step: &str,
) -> bool {
    let mut cursor: Option<String> = None;
    for _ in 0..500 {
        let Ok(page) = crate::files::list(dir, store, up, cursor.as_deref(), Some(1000)).await
        else {
            return false;
        };
        if page.entries.iter().any(|e| e.name == step && e.is_dir) {
            return true;
        }
        match page.next_cursor {
            Some(c) if !c.is_empty() => cursor = Some(c),
            _ => return false,
        }
    }
    false
}

/// The ids of a tree that was queued, or its error. When queuing stopped half way, what was already
/// started is cancelled: the window gets no ids for it, so it could never be shown or cancelled.
fn settle_queued(
    state: &Transfers,
    queued: Result<(), String>,
    ids: Vec<String>,
) -> Result<Vec<String>, String> {
    match queued {
        Ok(()) => Ok(ids),
        Err(e) => {
            for id in &ids {
                let _ = state.cancel(id);
            }
            Err(e)
        }
    }
}

/// Downloads a folder (or one file) from the computer, keeping its tree under the downloads
/// folder. Returns the ids of the queued downloads.
#[tauri::command]
pub async fn transfer_download_tree(
    app: tauri::AppHandle,
    state: tauri::State<'_, Transfers>,
    host: Option<String>,
    remote_path: String,
    is_dir: bool,
    ask_where: Option<bool>,
) -> Result<Vec<String>, String> {
    validate_remote(&remote_path, "enter the path of the file on the computer")?;
    let base = download_target(&app, ask_where.unwrap_or(false)).await?;
    if !is_dir {
        let id = state
            .start_download(app_ctx_in(&app, host, base)?, &remote_path)
            .await?;
        return Ok(vec![id]);
    }
    let dir = {
        use tauri::Manager;
        app.path().app_data_dir().map_err(|e| e.to_string())?
    };
    let store = crate::secrets::Offloaded::os_keystore().scoped(host.clone());
    let top = safe_name(&remote_path);
    // (remote folder, its path under the downloads folder)
    let mut stack = vec![(remote_path.clone(), top)];
    let mut ids = Vec::new();
    let queued: Result<(), String> = async {
        let mut folders = 0usize;
        while let Some((folder, rel)) = stack.pop() {
            // The folder is made even when nothing is in it.
            let here = base.join(&rel);
            std::fs::create_dir_all(&here)
                .map_err(|e| format!("cannot create {}: {e}", here.display()))?;
            let mut cursor: Option<String> = None;
            loop {
                let page = crate::files::list(&dir, &store, &folder, cursor.as_deref(), Some(1000))
                    .await
                    .map_err(|e| crate::files::user_message(&e))?;
                for e in page.entries {
                    if e.is_dir {
                        // A link to a folder may point back up the tree: it is not followed.
                        if !e.is_symlink {
                            stack.push((e.path.clone(), format!("{rel}/{}", safe_name(&e.path))));
                        }
                    } else {
                        // Checked per file, not per folder, so a huge folder is refused before it queues millions.
                        if ids.len() >= MAX_FOLDER_FILES {
                            return Err(
                                "that folder holds too many files to download at once".into()
                            );
                        }
                        let ctx = app_ctx_in(&app, host.clone(), here.clone())?;
                        ids.push(state.start_download(ctx, &e.path).await?);
                    }
                }
                match page.next_cursor {
                    Some(c) => cursor = Some(c),
                    None => break,
                }
            }
            folders += 1;
            if folders > 20_000 || ids.len() > MAX_FOLDER_FILES {
                return Err("that folder holds too many files to download at once".into());
            }
        }
        Ok(())
    }
    .await;
    // A folder with nothing in it is still a folder: it was made, and there is nothing to follow.
    settle_queued(&state, queued, ids)
}

/// The settings that change how transfers run. `limit_mbps` is millions of bytes per second, 0 for no
/// limit; `on_conflict` is `ask`, `replace`, `keep` or `skip`.
#[tauri::command]
pub async fn transfer_set_prefs(
    state: tauri::State<'_, Transfers>,
    parallel: usize,
    limit_mbps: u64,
    on_conflict: String,
    verify: bool,
) -> Result<(), String> {
    let policy = Policy::parse(&on_conflict).ok_or("that is not a way to settle a taken name")?;
    state.set_prefs(parallel, limit_mbps.min(10_000) * 1_000_000, policy, verify);
    Ok(())
}

/// The answer to a transfer that waits in the `conflict` state: `replace`, `keep` or `skip`.
#[tauri::command]
pub async fn transfer_resolve(
    state: tauri::State<'_, Transfers>,
    id: String,
    how: String,
) -> Result<(), String> {
    let how = Resolution::parse(&how).ok_or("that is not a way to settle a taken name")?;
    state.resolve(&id, how)
}

#[tauri::command]
pub async fn transfer_list(
    state: tauri::State<'_, Transfers>,
) -> Result<Vec<TransferView>, String> {
    Ok(state.list())
}

#[tauri::command]
pub async fn transfer_cancel(state: tauri::State<'_, Transfers>, id: String) -> Result<(), String> {
    state.cancel(&id)
}

#[tauri::command]
pub async fn transfer_pause(state: tauri::State<'_, Transfers>, id: String) -> Result<(), String> {
    state.pause(&id)
}

#[tauri::command]
pub async fn transfer_retry(state: tauri::State<'_, Transfers>, id: String) -> Result<(), String> {
    state.retry(&id)
}

#[tauri::command]
pub async fn transfer_clear_finished(state: tauri::State<'_, Transfers>) -> Result<usize, String> {
    Ok(state.clear_finished())
}

/// Where downloads go, for the screen to show.
#[tauri::command]
pub async fn transfer_folder(app: tauri::AppHandle) -> Result<String, String> {
    Ok(download_folder(&app)?.display().to_string())
}

#[cfg(test)]
mod session_tests {
    use super::*;

    fn body(chunk_size: usize, total_chunks: usize) -> SessionBody {
        SessionBody {
            id: "abc".into(),
            chunk_size,
            total_chunks,
            received_chunks: vec![],
            status: "open".into(),
        }
    }

    #[test]
    fn the_chunk_count_must_match_the_file_size() {
        assert_eq!(expected_chunks(0, 1024), 1);
        assert_eq!(expected_chunks(1, 1024), 1);
        assert_eq!(expected_chunks(1024, 1024), 1);
        assert_eq!(expected_chunks(1025, 1024), 2);
        assert!(!bad_session(&body(1024, 2), 1025));
        assert!(bad_session(&body(1024, 3), 1025), "too many chunks");
        assert!(bad_session(&body(1024, 1), 1025), "too few chunks");
        assert!(
            bad_session(&body(1024, usize::MAX), 10),
            "a huge count must be refused, not sent"
        );
        assert!(
            !bad_session(&body(1024, 1), 0),
            "an empty file has one chunk"
        );
    }
}

#[cfg(test)]
mod checksum_tests {
    use super::*;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};

    async fn serve(status: &'static str, body: &'static str) -> String {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            let (mut sock, _) = listener.accept().await.unwrap();
            let mut buf = [0u8; 2048];
            let _ = sock.read(&mut buf).await;
            let resp = format!(
                "HTTP/1.1 {status}\r\ncontent-type: application/json\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{body}",
                body.len()
            );
            let _ = sock.write_all(resp.as_bytes()).await;
        });
        format!("http://{addr}")
    }

    fn conn() -> Conn {
        Conn {
            host: "h".into(),
            fingerprint: "f".into(),
            token: "t".into(),
        }
    }

    async fn ask(status: &'static str, body: &'static str) -> Result<Option<String>, String> {
        let base = serve(status, body).await;
        remote_checksum(&reqwest::Client::new(), &base, &conn(), "/x").await
    }

    #[tokio::test]
    async fn only_an_agent_without_the_endpoint_leaves_a_download_unverified() {
        assert_eq!(ask("404 Not Found", "{}").await, Ok(None));
        assert_eq!(ask("501 Not Implemented", "{}").await, Ok(None));
        assert_eq!(
            ask("200 OK", r#"{"checksum":"ab"}"#).await,
            Ok(Some("ab".into()))
        );
    }

    #[tokio::test]
    async fn a_failed_or_garbled_check_is_an_error_not_unverified() {
        assert!(ask("500 Internal Server Error", "{}").await.is_err());
        assert!(ask("200 OK", "not json").await.is_err());
    }
}

#[cfg(test)]
mod resume_tests {
    use super::resume_is_unsafe;

    #[test]
    fn bytes_without_a_known_version_are_not_continued() {
        assert!(resume_is_unsafe(10, &None));
        assert!(!resume_is_unsafe(0, &None));
        assert!(!resume_is_unsafe(10, &Some("\"etag\"".into())));
    }
}

#[cfg(all(test, unix))]
mod non_utf8_tests {
    use super::*;
    use std::os::unix::ffi::OsStrExt;

    #[test]
    fn an_upload_source_with_a_non_utf8_name_is_opened_as_it_is() {
        let d = tempfile::tempdir().unwrap();
        let path = d.path().join(std::ffi::OsStr::from_bytes(b"caf\xe9.txt"));
        std::fs::write(&path, b"x").unwrap();
        assert!(open_source(&path).is_ok());
        assert!(path.to_str().is_none(), "the name really is not UTF-8");
    }
}
