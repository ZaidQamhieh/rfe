//! The launcher entry, icons and AppStream metadata the .deb ships agree with each other and with
//! the app's configuration. These are the things a launcher shows (name, icon) and a software
//! centre reads (id, version), checked without installing anything.

use serde_json::Value;
use std::path::{Path, PathBuf};

fn root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).to_path_buf()
}

fn conf() -> Value {
    serde_json::from_str(&std::fs::read_to_string(root().join("tauri.conf.json")).unwrap()).unwrap()
}

fn read(rel: &str) -> String {
    std::fs::read_to_string(root().join(rel)).unwrap_or_else(|e| panic!("{rel}: {e}"))
}

/// Width and height from a PNG's header.
fn png_size(path: &Path) -> (u32, u32) {
    let b = std::fs::read(path).unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    assert_eq!(
        &b[..8],
        b"\x89PNG\r\n\x1a\n",
        "{} is not a PNG",
        path.display()
    );
    let n = |i: usize| u32::from_be_bytes(b[i..i + 4].try_into().unwrap());
    (n(16), n(20))
}

/// The text between `<tag ...>` and `</tag>` (first match), or the value of an attribute.
fn between<'a>(xml: &'a str, open: &str, close: &str) -> &'a str {
    let start = xml
        .find(open)
        .unwrap_or_else(|| panic!("no {open} in the metainfo"))
        + open.len();
    let len = xml[start..].find(close).unwrap();
    &xml[start..start + len]
}

#[test]
fn every_icon_exists_and_is_the_size_its_name_says() {
    let conf = conf();
    let icons: Vec<&str> = conf["bundle"]["icon"]
        .as_array()
        .unwrap()
        .iter()
        .map(|v| v.as_str().unwrap())
        .collect();
    let mut sizes = Vec::new();
    for rel in &icons {
        let path = root().join(rel);
        assert!(path.exists(), "{rel} is listed but missing");
        let name = path.file_stem().unwrap().to_str().unwrap();
        if !rel.ends_with(".png") {
            continue;
        }
        let (w, h) = png_size(&path);
        assert_eq!(w, h, "{rel} is not square");
        // `NxN.png` is N pixels, `NxN@2x.png` is 2N, `icon.png` is the largest.
        if let Some(stem) = name.strip_suffix("@2x") {
            let n: u32 = stem.split('x').next().unwrap().parse().unwrap();
            assert_eq!(w, n * 2, "{rel} should be {}px", n * 2);
        } else if name != "icon" {
            let n: u32 = name.split('x').next().unwrap().parse().unwrap();
            assert_eq!(w, n, "{rel} should be {n}px");
        }
        sizes.push(w);
    }
    for needed in [16, 24, 32, 48, 64, 128, 256, 512] {
        assert!(sizes.contains(&needed), "no {needed}px icon in {sizes:?}");
    }
}

#[test]
fn the_desktop_template_names_the_app_icon_and_window_class() {
    let conf = conf();
    let template = read(
        conf["bundle"]["linux"]["deb"]["desktopTemplate"]
            .as_str()
            .expect("the deb uses our desktop template"),
    );
    for line in [
        "Type=Application",
        "Name={{name}}",
        "Exec={{exec}}",
        "Icon={{icon}}",
        "Categories={{categories}}",
        // GNOME and KDE match a window to its launcher by this class; it is the binary name.
        "StartupWMClass={{exec}}",
        "Terminal=false",
    ] {
        assert!(
            template.lines().any(|l| l == line),
            "template lacks {line:?}"
        );
    }
    assert!(template.contains("Keywords="), "no search keywords");
}

#[test]
fn the_metainfo_matches_the_app_it_describes() {
    let conf = conf();
    let xml = read("packaging/linux/app.rfe.desktop.metainfo.xml");

    assert_eq!(
        between(&xml, "<id>", "</id>"),
        conf["identifier"].as_str().unwrap()
    );
    assert_eq!(
        between(&xml, "<name>", "</name>"),
        conf["productName"].as_str().unwrap()
    );
    // The bundler names the launcher file after the product name; the metainfo must point at it.
    assert_eq!(
        between(&xml, "<launchable type=\"desktop-id\">", "</launchable>"),
        format!("{}.desktop", conf["productName"].as_str().unwrap())
    );
    assert_eq!(between(&xml, "<binary>", "</binary>"), "rfe-desktop");

    // The newest release entry is this build. A version bump has to touch it.
    let release = between(&xml, "<release version=\"", "\"");
    assert_eq!(
        release,
        env!("CARGO_PKG_VERSION"),
        "packaging/linux/app.rfe.desktop.metainfo.xml lists release {release} but the app is {}",
        env!("CARGO_PKG_VERSION")
    );
}

#[test]
fn the_deb_installs_the_metainfo_from_a_file_that_exists() {
    let conf = conf();
    let files = conf["bundle"]["linux"]["deb"]["files"].as_object().unwrap();
    let (dest, src) = files.iter().next().expect("a metainfo entry in deb.files");
    assert_eq!(dest, "/usr/share/metainfo/app.rfe.desktop.metainfo.xml");
    assert!(
        root().join(src.as_str().unwrap()).exists(),
        "{src} is missing"
    );
}
