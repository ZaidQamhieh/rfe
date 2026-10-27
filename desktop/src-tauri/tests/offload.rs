//! Keystore calls must not block the async runtime while an unlock prompt is open, and a
//! keystore that never answers must end in a clear error instead of a hung command.

use rfe_desktop_lib::agent_client::AgentError;
use rfe_desktop_lib::flows;
use rfe_desktop_lib::secrets::{MemoryStore, Offloaded, SecretStore};
use std::time::{Duration, Instant};
use tempfile::TempDir;

/// A keystore whose every call sits on an "unlock prompt" for `0`.
struct SlowStore(Duration);

impl SecretStore for SlowStore {
    fn get(&self, _: &str) -> Result<Option<String>, String> {
        std::thread::sleep(self.0);
        Ok(None)
    }
    fn set(&self, _: &str, _: &str) -> Result<(), String> {
        std::thread::sleep(self.0);
        Ok(())
    }
    fn delete(&self, _: &str) -> Result<(), String> {
        std::thread::sleep(self.0);
        Ok(())
    }
}

const PROMPT: Duration = Duration::from_millis(400);

/// Time until a 20 ms timer fires on the same single-threaded runtime, measured from `t0`.
async fn tick_after(t0: Instant) -> Duration {
    tokio::time::sleep(Duration::from_millis(20)).await;
    t0.elapsed()
}

#[tokio::test(flavor = "current_thread")]
async fn an_open_unlock_prompt_does_not_stall_other_work() {
    let store = Offloaded::new(SlowStore(PROMPT));
    let t0 = Instant::now();
    let (call, tick) = tokio::join!(store.run(|s| s.get("a")), tick_after(t0));
    assert_eq!(call, Ok(None));
    assert!(t0.elapsed() >= PROMPT, "the keystore call really waited");
    assert!(
        tick < PROMPT / 2,
        "the timer fired after {tick:?}: the runtime was blocked"
    );
}

/// Negative control: the same wait done inline does stall the runtime, so the test above can fail.
#[tokio::test(flavor = "current_thread")]
async fn control_the_same_call_made_inline_does_stall_the_runtime() {
    let store = SlowStore(PROMPT);
    let t0 = Instant::now();
    let (_, tick) = tokio::join!(async { store.get("a") }, tick_after(t0));
    assert!(
        tick >= PROMPT,
        "expected a stall, timer fired after {tick:?}"
    );
}

#[tokio::test]
async fn a_keystore_that_never_answers_ends_in_a_clear_timeout_error() {
    let store = Offloaded::new(SlowStore(Duration::from_millis(600)))
        .with_timeout(Duration::from_millis(50));
    let err = store.run(|s| s.get("a")).await.unwrap_err();
    assert!(err.contains("did not answer within 50ms"), "{err}");
    assert!(err.contains("never saved to files"), "{err}");
}

/// Negative control: with enough time the same store answers, so the error above is the timeout.
#[tokio::test]
async fn control_a_slow_keystore_with_enough_time_succeeds() {
    let store =
        Offloaded::new(SlowStore(Duration::from_millis(100))).with_timeout(Duration::from_secs(5));
    assert_eq!(store.run(|s| s.get("a")).await, Ok(None));
}

const FP: &str = "abababababababababababababababababababababababababababababababab";

#[tokio::test]
async fn login_stops_at_a_hung_keystore_before_any_network_traffic() {
    let dir = TempDir::new().unwrap();
    let hung = Offloaded::new(SlowStore(Duration::from_millis(600)))
        .with_timeout(Duration::from_millis(50));
    let err = flows::login(dir.path(), "127.0.0.1:1", FP, "u", "p", "X", &hung)
        .await
        .unwrap_err();
    assert!(
        matches!(&err, AgentError::Local(m) if m.contains("did not answer")),
        "{err:?}"
    );
    assert!(
        std::fs::read_dir(dir.path()).unwrap().next().is_none(),
        "nothing was written"
    );
}

/// Negative control: with a working keystore the same login gets as far as the network.
#[tokio::test]
async fn control_login_with_a_working_keystore_reaches_the_network() {
    let dir = TempDir::new().unwrap();
    let err = flows::login(
        dir.path(),
        "127.0.0.1:1",
        FP,
        "u",
        "p",
        "X",
        &Offloaded::new(MemoryStore::default()),
    )
    .await
    .unwrap_err();
    assert!(matches!(err, AgentError::Network(_)), "{err:?}");
}

/// Records the order in which writes reach the keystore; `set` sits on a prompt for `0`.
struct OrderStore {
    log: std::sync::Arc<std::sync::Mutex<Vec<&'static str>>>,
    slow_set: Duration,
}

impl SecretStore for OrderStore {
    fn get(&self, _: &str) -> Result<Option<String>, String> {
        std::thread::sleep(self.slow_set);
        Ok(None)
    }
    fn set(&self, _: &str, _: &str) -> Result<(), String> {
        std::thread::sleep(self.slow_set);
        self.log.lock().unwrap().push("set");
        Ok(())
    }
    fn delete(&self, _: &str) -> Result<(), String> {
        self.log.lock().unwrap().push("delete");
        Ok(())
    }
}

/// A `set` that timed out is still sitting on the prompt. A later `delete` must not overtake it,
/// or the stale `set` lands last and the token the user just removed comes back.
#[tokio::test]
async fn a_timed_out_write_cannot_land_after_a_later_one() {
    let log = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
    let store = Offloaded::new(OrderStore {
        log: log.clone(),
        slow_set: Duration::from_millis(300),
    })
    .with_timeout(Duration::from_millis(50));
    assert!(store.run(|s| s.set("a", "t")).await.is_err());
    let later = store.clone().with_timeout(Duration::from_secs(5));
    later.run(|s| s.delete("a")).await.unwrap();
    assert_eq!(*log.lock().unwrap(), vec!["set", "delete"]);
}

/// A call that gave up while still queued behind a stuck one never runs: its caller was told it
/// failed, so it must not save anything later.
#[tokio::test]
async fn a_call_abandoned_before_it_started_never_runs() {
    let log = std::sync::Arc::new(std::sync::Mutex::new(Vec::new()));
    let store = Offloaded::new(OrderStore {
        log: log.clone(),
        slow_set: Duration::from_millis(300),
    })
    .with_timeout(Duration::from_millis(50));
    assert!(store.run(|s| s.get("a")).await.is_err()); // stuck on the prompt
    assert!(store.run(|s| s.delete("a")).await.is_err()); // queued behind it, then abandoned
    tokio::time::sleep(Duration::from_millis(600)).await;
    assert!(log.lock().unwrap().is_empty(), "{:?}", log.lock().unwrap());
}

/// Every Tauri command builds its own handle, so the one-at-a-time gate has to be shared by all
/// of them or concurrent commands would not serialize.
#[tokio::test]
async fn separate_os_keystore_handles_serialize_through_one_gate() {
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;
    let running = Arc::new(AtomicUsize::new(0));
    let overlap = Arc::new(AtomicUsize::new(0));
    let mut calls = Vec::new();
    for _ in 0..4 {
        let (running, overlap) = (Arc::clone(&running), Arc::clone(&overlap));
        calls.push(tokio::spawn(async move {
            Offloaded::os_keystore()
                .run(move |_| {
                    if running.fetch_add(1, Ordering::SeqCst) > 0 {
                        overlap.fetch_add(1, Ordering::SeqCst);
                    }
                    std::thread::sleep(Duration::from_millis(50));
                    running.fetch_sub(1, Ordering::SeqCst);
                    Ok(())
                })
                .await
        }));
    }
    for c in calls {
        c.await.unwrap().unwrap();
    }
    assert_eq!(overlap.load(Ordering::SeqCst), 0, "calls ran concurrently");
}
