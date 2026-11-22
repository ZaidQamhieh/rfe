//! The desktop version is written in two places, Cargo.toml and tauri.conf.json. A release tag
//! `host-vX.Y.Z` (`desktop-vX.Y.Z` before) must match both, so they must not drift apart.

use std::path::Path;

/// `Ok` when both versions are the same plain `X.Y.Z` or `X.Y.Z-rc.N`.
fn versions_agree(cargo: &str, conf: &str) -> Result<(), String> {
    if cargo != conf {
        return Err(format!(
            "Cargo.toml says {cargo} but tauri.conf.json says {conf}; set both to the release version"
        ));
    }
    let (core, pre) = match cargo.split_once('-') {
        Some((core, pre)) => (core, Some(pre)),
        None => (cargo, None),
    };
    let numbers: Vec<_> = core.split('.').collect();
    let plain = numbers.len() == 3 && numbers.iter().all(|n| n.parse::<u32>().is_ok());
    let pre_ok = match pre {
        None => true,
        Some(p) => p
            .strip_prefix("rc.")
            .is_some_and(|n| n.parse::<u32>().is_ok()),
    };
    if plain && pre_ok {
        Ok(())
    } else {
        Err(format!(
            "{cargo} is not X.Y.Z or X.Y.Z-rc.N, the only forms a host-v tag can take"
        ))
    }
}

#[test]
fn cargo_and_tauri_conf_carry_the_same_release_version() {
    let conf =
        std::fs::read_to_string(Path::new(env!("CARGO_MANIFEST_DIR")).join("tauri.conf.json"))
            .unwrap();
    let conf: serde_json::Value = serde_json::from_str(&conf).unwrap();
    let conf_version = conf["version"].as_str().expect("tauri.conf.json version");
    if let Err(e) = versions_agree(env!("CARGO_PKG_VERSION"), conf_version) {
        panic!("{e}");
    }
}

/// Negative controls: the check can fail.
#[test]
fn drifted_or_malformed_versions_are_rejected() {
    assert!(versions_agree("1.2.3", "1.2.3").is_ok());
    assert!(versions_agree("1.2.3-rc.1", "1.2.3-rc.1").is_ok());
    assert!(versions_agree("1.2.3", "1.2.4")
        .unwrap_err()
        .contains("set both"));
    for bad in [
        "1.2",
        "1.2.3.4",
        "v1.2.3",
        "1.2.3-beta",
        "1.2.3-rc.",
        "1.x.3",
    ] {
        assert!(
            versions_agree(bad, bad).is_err(),
            "{bad} should be rejected"
        );
    }
}

/// The product name is written twice: `productName` in tauri.conf.json and `APP_NAME` in
/// `src/lib.rs`. The installer and the window title come from the first, the tray and the
/// autostart entry from the second; they must be the same text.
#[test]
fn the_product_name_is_one_string() {
    let conf: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string("tauri.conf.json").unwrap()).unwrap();
    assert_eq!(
        conf["productName"].as_str(),
        Some(rfe_desktop_lib::APP_NAME),
        "tauri.conf.json productName and APP_NAME differ"
    );
}

/// The window's smallest width is set in `tauri.conf.json` and again when the interface size
/// changes (`MIN_WIDTH` in `src/desktop.rs`); the two must be the same number.
#[test]
fn the_minimum_window_width_is_one_number() {
    let conf: serde_json::Value =
        serde_json::from_str(&std::fs::read_to_string("tauri.conf.json").unwrap()).unwrap();
    let from_conf = conf["app"]["windows"][0]["minWidth"]
        .as_f64()
        .expect("minWidth");
    let src = std::fs::read_to_string("src/desktop.rs").unwrap();
    let line = src
        .lines()
        .find(|l| l.starts_with("const MIN_WIDTH"))
        .expect("MIN_WIDTH");
    let from_src: f64 = line
        .split('=')
        .nth(1)
        .unwrap()
        .trim()
        .trim_end_matches(';')
        .parse()
        .unwrap();
    assert_eq!(
        from_conf, from_src,
        "tauri.conf.json minWidth and desktop.rs MIN_WIDTH differ"
    );
}
