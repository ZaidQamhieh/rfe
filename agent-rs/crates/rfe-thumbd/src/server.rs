//! The request loop: frames in on stdin, thumbnails out on stdout, decoded by a fixed pool of threads that all
//! exist before the sandbox closes (a sandboxed process cannot start new ones).

use crate::decode::DecodeError;
use crate::render::{self, RenderError};
use rfe_proto::{
    code, error_response, read_frame, write_frame, Envelope, Hello, KIND_BIN, KIND_JSON,
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::io::{Read, Write};
use std::sync::atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering};
use std::sync::mpsc::{channel, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

pub const NAME: &str = "rfe-thumbd";
/// Bytes of source images waiting for a worker; more is answered with BUSY.
pub const MAX_QUEUED_BYTES: usize = 256 << 20;
/// A render that takes longer than this ends the process (the agent restarts it and falls back meanwhile).
pub const WATCHDOG: Duration = Duration::from_secs(30);

struct Job {
    id: u64,
    max_size: u32,
    body: Vec<u8>,
    cancel: Arc<AtomicBool>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct RenderReq {
    #[serde(default)]
    max_size: u32,
}

type Out<W> = Arc<Mutex<W>>;

fn send<W: Write>(out: &Out<W>, v: &Value, body: Option<&[u8]>) {
    let bytes = serde_json::to_vec(v).unwrap_or_default();
    let mut w = out.lock().unwrap();
    // A write failure means the agent is gone; the read loop sees EOF and exits.
    let _ = write_frame(&mut *w, KIND_JSON, &bytes);
    if let Some(b) = body {
        let _ = write_frame(&mut *w, KIND_BIN, b);
    }
    let _ = w.flush();
}

fn run_job(job: &Job) -> Result<(Value, Vec<u8>), (&'static str, String)> {
    if job.cancel.load(Ordering::Relaxed) {
        return Err((code::CANCELED, "canceled".into()));
    }
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        render::render(&job.body, job.max_size)
    }));
    match result {
        Ok(Ok(t)) => Ok((
            json!({
                "width": t.width, "height": t.height, "srcWidth": t.src_width, "srcHeight": t.src_height,
                "format": t.format,
            }),
            t.jpeg,
        )),
        Ok(Err(RenderError::Decode(DecodeError::Unsupported(m)))) => Err((code::NOT_SUPPORTED, m)),
        Ok(Err(RenderError::Decode(DecodeError::TooLarge(m)))) => Err((code::TOO_LARGE, m)),
        Ok(Err(RenderError::Internal(m))) => Err((code::INTERNAL, m)),
        Err(_) => Err((code::INTERNAL, "render panicked".into())),
    }
}

fn worker<W: Write + Send + 'static>(
    rx: Arc<Mutex<Receiver<Job>>>,
    out: Out<W>,
    cancels: Arc<Mutex<HashMap<u64, Arc<AtomicBool>>>>,
    queued: Arc<AtomicUsize>,
    started: Arc<AtomicU64>,
    epoch: Instant,
) {
    loop {
        let job = match rx.lock().unwrap().recv() {
            Ok(j) => j,
            Err(_) => return,
        };
        queued.fetch_sub(job.body.len(), Ordering::Relaxed);
        started.store(epoch.elapsed().as_millis() as u64 + 1, Ordering::Relaxed);
        let result = run_job(&job);
        started.store(0, Ordering::Relaxed);
        cancels.lock().unwrap().remove(&job.id);
        match result {
            Ok((mut v, jpeg)) => {
                v["id"] = json!(job.id);
                v["ok"] = json!(true);
                v["body"] = json!(true);
                send(&out, &v, Some(&jpeg));
            }
            Err((c, m)) => send(&out, &error_response(job.id, c, &m), None),
        }
    }
}

/// Serves requests until the input closes. `before_read` runs once the threads exist and before the first request
/// is read: the binary enters its sandbox there.
pub fn serve<R: Read, W: Write + Send + 'static>(
    mut input: R,
    output: W,
    workers: usize,
    before_read: impl FnOnce() -> Result<(), String>,
) -> std::io::Result<()> {
    let out: Out<W> = Arc::new(Mutex::new(output));
    let (tx, rx): (Sender<Job>, Receiver<Job>) = channel();
    let rx = Arc::new(Mutex::new(rx));
    let cancels: Arc<Mutex<HashMap<u64, Arc<AtomicBool>>>> = Arc::new(Mutex::new(HashMap::new()));
    let queued = Arc::new(AtomicUsize::new(0));
    let epoch = Instant::now();
    let slots: Vec<Arc<AtomicU64>> = (0..workers.max(1))
        .map(|_| Arc::new(AtomicU64::new(0)))
        .collect();
    let mut handles = Vec::new();
    for slot in &slots {
        let (rx, out, cancels, queued, slot) = (
            Arc::clone(&rx),
            Arc::clone(&out),
            Arc::clone(&cancels),
            Arc::clone(&queued),
            Arc::clone(slot),
        );
        handles.push(std::thread::spawn(move || {
            worker(rx, out, cancels, queued, slot, epoch)
        }));
    }
    let stop = Arc::new(AtomicBool::new(false));
    {
        let (slots, stop) = (slots.clone(), Arc::clone(&stop));
        std::thread::spawn(move || {
            while !stop.load(Ordering::Relaxed) {
                std::thread::sleep(Duration::from_millis(500));
                let now = epoch.elapsed().as_millis() as u64;
                for s in &slots {
                    let t = s.load(Ordering::Relaxed);
                    if t != 0 && now.saturating_sub(t - 1) > WATCHDOG.as_millis() as u64 {
                        crate::sandbox::say(&format!(
                            "rfe-thumbd: a render ran longer than {WATCHDOG:?}; exiting"
                        ));
                        std::process::exit(3);
                    }
                }
            }
        });
    }
    if let Err(e) = before_read() {
        crate::sandbox::say(&format!("rfe-thumbd: sandbox failed: {e}"));
        return Err(std::io::Error::other(e));
    }
    send(
        &out,
        &serde_json::to_value(Hello::new(
            NAME,
            rfe_proto::release_version(env!("CARGO_PKG_VERSION")),
        ))
        .unwrap(),
        None,
    );

    while let Some(frame) = read_frame(&mut input)? {
        if frame.kind != KIND_JSON {
            continue;
        }
        let Ok(env) = serde_json::from_slice::<Envelope>(&frame.payload) else {
            send(
                &out,
                &error_response(0, code::BAD_REQUEST, "malformed request"),
                None,
            );
            continue;
        };
        // A request that announces a body is followed by its BIN frame; it is always consumed to stay in sync.
        let body = if env.body {
            match read_frame(&mut input)? {
                Some(f) if f.kind == KIND_BIN => Some(f.payload),
                Some(_) | None => {
                    send(
                        &out,
                        &error_response(env.id, code::BAD_REQUEST, "missing body frame"),
                        None,
                    );
                    continue;
                }
            }
        } else {
            None
        };
        match env.op.as_str() {
            "ping" => send(&out, &json!({"id": env.id, "ok": true}), None),
            "cancel" => {
                if let Some(flag) = cancels.lock().unwrap().get(&env.target) {
                    flag.store(true, Ordering::Relaxed);
                }
                send(&out, &json!({"id": env.id, "ok": true}), None);
            }
            "thumb.render" => {
                let Some(body) = body else {
                    send(
                        &out,
                        &error_response(env.id, code::BAD_REQUEST, "thumb.render needs a body"),
                        None,
                    );
                    continue;
                };
                let Ok(req) = serde_json::from_slice::<RenderReq>(&frame.payload) else {
                    send(
                        &out,
                        &error_response(env.id, code::BAD_REQUEST, "bad request body"),
                        None,
                    );
                    continue;
                };
                if queued.load(Ordering::Relaxed) + body.len() > MAX_QUEUED_BYTES {
                    send(
                        &out,
                        &error_response(env.id, code::BUSY, "render queue is full"),
                        None,
                    );
                    continue;
                }
                queued.fetch_add(body.len(), Ordering::Relaxed);
                let cancel = Arc::new(AtomicBool::new(false));
                cancels.lock().unwrap().insert(env.id, Arc::clone(&cancel));
                let _ = tx.send(Job {
                    id: env.id,
                    max_size: req.max_size,
                    body,
                    cancel,
                });
            }
            other => send(
                &out,
                &error_response(env.id, code::BAD_REQUEST, &format!("unknown op {other:?}")),
                None,
            ),
        }
    }
    drop(tx);
    stop.store(true, Ordering::Relaxed);
    for h in handles {
        let _ = h.join();
    }
    Ok(())
}
