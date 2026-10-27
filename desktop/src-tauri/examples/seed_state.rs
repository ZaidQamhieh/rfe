//! Dev helper for screenshots: signs `--state-dir` in to a throwaway agent so
//! the window opens on the device list, and adds a second device.
//! Usage: seed_state <host:port> <user> <password> <state-dir> <second-state-dir>

use rfe_desktop_lib::{
    agent_client::capture_fingerprint,
    flows,
    secrets::{Offloaded, OsKeystore},
};
use std::path::Path;

#[tokio::main]
async fn main() {
    let a: Vec<String> = std::env::args().skip(1).collect();
    let [host, user, pw, dir, dir2] = &a[..] else {
        panic!("usage: seed_state <host:port> <user> <password> <state-dir> <second-state-dir>");
    };
    let fp = capture_fingerprint(host).await.expect("probe");
    flows::login(
        Path::new(dir2),
        host,
        &fp,
        user,
        pw,
        "Phone (test)",
        &Offloaded::new(OsKeystore::new()),
    )
    .await
    .expect("second device");
    flows::login(
        Path::new(dir),
        host,
        &fp,
        user,
        pw,
        "RFE Desktop",
        &Offloaded::new(OsKeystore::new()),
    )
    .await
    .expect("login");
    println!("{fp}");
}
