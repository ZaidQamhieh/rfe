// The app's own commands, one per line in commands.txt. Declaring them in the app manifest makes
// Tauri gate each one by a permission (`allow-<name>`), which capabilities/default.json lists
// explicitly; tests/capabilities.rs keeps the three in step.
fn main() {
    println!("cargo:rerun-if-changed=commands.txt");
    let names: Vec<&'static str> = include_str!("commands.txt")
        .lines()
        .map(str::trim)
        .filter(|l| !l.is_empty())
        .collect();
    let commands: &'static [&'static str] = Box::leak(names.into_boxed_slice());
    tauri_build::try_build(
        tauri_build::Attributes::new()
            .app_manifest(tauri_build::AppManifest::new().commands(commands)),
    )
    .expect("failed to run tauri-build");
}
