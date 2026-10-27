//! The checksum file, the verify script and the SBOM that go with a release, run for real on
//! temporary files and on this project's own Cargo.lock.

use serde_json::Value;
use std::path::{Path, PathBuf};
use std::process::{Command, Output};
use tempfile::TempDir;

fn scripts() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../scripts")
}

fn run(script: &str, args: &[&Path]) -> Output {
    Command::new(scripts().join(script))
        .args(args)
        .output()
        .unwrap_or_else(|e| panic!("{script}: {e}"))
}

fn text(o: &Output) -> String {
    format!(
        "{}{}",
        String::from_utf8_lossy(&o.stdout),
        String::from_utf8_lossy(&o.stderr)
    )
}

fn release_dir() -> TempDir {
    let d = TempDir::new().unwrap();
    std::fs::write(d.path().join("RFE Desktop_1.0.0_amd64.deb"), b"deb bytes").unwrap();
    std::fs::write(
        d.path().join("RFE Desktop_1.0.0_amd64.AppImage"),
        b"appimage bytes",
    )
    .unwrap();
    std::fs::write(d.path().join("rfe-desktop.sbom.json"), b"{}").unwrap();
    d
}

#[test]
fn sums_are_written_and_a_download_verifies_against_them() {
    let d = release_dir();
    let out = run("checksums.sh", &[d.path()]);
    assert!(out.status.success(), "{}", text(&out));
    let sums = std::fs::read_to_string(d.path().join("SHA256SUMS")).unwrap();
    assert_eq!(sums.lines().count(), 3, "{sums}");
    assert!(
        !sums.contains("SHA256SUMS"),
        "the sums file does not list itself"
    );
    for line in sums.lines() {
        let (hash, name) = line.split_once("  ").unwrap();
        assert_eq!(hash.len(), 64, "{line}");
        assert!(!name.contains('/'), "names are relative: {line}");
    }

    // A downloaded copy in another place, found by its name.
    let elsewhere = TempDir::new().unwrap();
    let copy = elsewhere.path().join("RFE Desktop_1.0.0_amd64.deb");
    std::fs::copy(d.path().join("RFE Desktop_1.0.0_amd64.deb"), &copy).unwrap();
    let out = run("verify-download.sh", &[&d.path().join("SHA256SUMS"), &copy]);
    assert!(out.status.success(), "{}", text(&out));
    assert!(
        text(&out).contains("RFE Desktop_1.0.0_amd64.deb: OK"),
        "{}",
        text(&out)
    );
}

#[test]
fn a_changed_missing_or_unlisted_download_fails() {
    let d = release_dir();
    assert!(run("checksums.sh", &[d.path()]).status.success());
    let sums = d.path().join("SHA256SUMS");

    let tampered = d.path().join("rfe-desktop.sbom.json");
    std::fs::write(&tampered, b"{ \"tampered\": true }").unwrap();
    let out = run("verify-download.sh", &[&sums, &tampered]);
    assert!(!out.status.success());
    assert!(
        text(&out).contains("rfe-desktop.sbom.json: FAILED"),
        "{}",
        text(&out)
    );

    let gone = d.path().join("not-downloaded.deb");
    let out = run("verify-download.sh", &[&sums, &gone]);
    assert!(!out.status.success());
    assert!(text(&out).contains("MISSING"), "{}", text(&out));

    let stranger = d.path().join("stranger.deb");
    std::fs::write(&stranger, b"never released").unwrap();
    let out = run("verify-download.sh", &[&sums, &stranger]);
    assert!(!out.status.success());
    assert!(text(&out).contains("NOT LISTED"), "{}", text(&out));

    // One bad file fails the whole run even when the others are fine.
    let good = d.path().join("RFE Desktop_1.0.0_amd64.deb");
    let out = run("verify-download.sh", &[&sums, &good, &stranger]);
    assert!(!out.status.success());
    assert!(
        text(&out).contains("RFE Desktop_1.0.0_amd64.deb: OK"),
        "{}",
        text(&out)
    );
}

#[test]
fn an_empty_directory_gets_no_sums() {
    let d = TempDir::new().unwrap();
    assert!(!run("checksums.sh", &[d.path()]).status.success());
}

#[test]
fn the_sbom_lists_every_crate_in_the_lock_file_with_its_hash() {
    let lock_path = Path::new(env!("CARGO_MANIFEST_DIR")).join("Cargo.lock");
    let d = TempDir::new().unwrap();
    let out_path = d.path().join("sbom.json");
    let out = run("sbom.py", &[&lock_path, &out_path]);
    assert!(out.status.success(), "{}", text(&out));
    let bom: Value = serde_json::from_slice(&std::fs::read(&out_path).unwrap()).unwrap();

    assert_eq!(bom["bomFormat"], "CycloneDX");
    assert_eq!(bom["specVersion"], "1.5");
    let app = &bom["metadata"]["component"];
    assert_eq!(app["name"], "rfe-desktop");
    assert_eq!(app["version"], env!("CARGO_PKG_VERSION"));

    let lock = std::fs::read_to_string(&lock_path).unwrap();
    let packages =
        lock.matches("\n[[package]]").count() + usize::from(lock.starts_with("[[package]]"));
    let components = bom["components"].as_array().unwrap();
    assert_eq!(
        components.len(),
        packages - 1,
        "every crate but the app itself"
    );

    // A crate the app certainly uses, with the version and checksum the lock file records.
    let tauri = components
        .iter()
        .find(|c| c["name"] == "tauri")
        .expect("tauri");
    assert_eq!(
        tauri["purl"],
        format!("pkg:cargo/tauri@{}", tauri["version"].as_str().unwrap())
    );
    let block = lock
        .split("[[package]]")
        .find(|b| b.contains("name = \"tauri\"\n"))
        .unwrap();
    let checksum = block
        .lines()
        .find_map(|l| l.strip_prefix("checksum = \""))
        .unwrap();
    assert_eq!(
        tauri["hashes"][0]["content"],
        checksum.trim_end_matches('"')
    );

    // Every component is the target of at least the app's dependency graph entry.
    let refs: Vec<&str> = bom["dependencies"]
        .as_array()
        .unwrap()
        .iter()
        .map(|d| d["ref"].as_str().unwrap())
        .collect();
    assert!(refs.contains(&app["purl"].as_str().unwrap()));
    assert_eq!(refs.len(), packages);
}
