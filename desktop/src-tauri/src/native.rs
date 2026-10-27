//! The system's own file dialogs. The window asks for a pick and gets paths back; it never gets
//! a file-system or dialog plugin of its own, so the page stays limited to this app's commands.

use serde::Deserialize;
use tauri_plugin_dialog::DialogExt;

/// A group of endings the dialog offers, such as "Backups" and `["rfebak"]`.
#[derive(Debug, Clone, Deserialize)]
pub struct Filter {
    pub name: String,
    pub extensions: Vec<String>,
}

fn paths(list: Option<Vec<tauri_plugin_dialog::FilePath>>) -> Vec<String> {
    list.unwrap_or_default()
        .into_iter()
        .filter_map(|p| p.into_path().ok())
        .map(|p| p.to_string_lossy().into_owned())
        .collect()
}

/// Lets the person choose files on this computer. An empty list means they cancelled.
#[tauri::command]
pub async fn pick_files(
    app: tauri::AppHandle,
    title: Option<String>,
    multiple: bool,
    start_dir: Option<String>,
    filters: Option<Vec<Filter>>,
) -> Result<Vec<String>, String> {
    tokio::task::spawn_blocking(move || {
        let mut d = app.dialog().file();
        if let Some(t) = title {
            d = d.set_title(t);
        }
        if let Some(s) = start_dir.filter(|s| std::path::Path::new(s).is_dir()) {
            d = d.set_directory(s);
        }
        for f in filters.unwrap_or_default() {
            let ext: Vec<&str> = f.extensions.iter().map(String::as_str).collect();
            d = d.add_filter(f.name, &ext);
        }
        if multiple {
            paths(d.blocking_pick_files())
        } else {
            paths(d.blocking_pick_file().map(|p| vec![p]))
        }
    })
    .await
    .map_err(|e| e.to_string())
}

/// Lets the person choose a folder on this computer. An empty answer means they cancelled.
#[tauri::command]
pub async fn pick_folder(
    app: tauri::AppHandle,
    title: Option<String>,
    start_dir: Option<String>,
) -> Result<Option<String>, String> {
    tokio::task::spawn_blocking(move || {
        let mut d = app.dialog().file();
        if let Some(t) = title {
            d = d.set_title(t);
        }
        if let Some(s) = start_dir.filter(|s| std::path::Path::new(s).is_dir()) {
            d = d.set_directory(s);
        }
        paths(d.blocking_pick_folder().map(|p| vec![p]))
            .into_iter()
            .next()
    })
    .await
    .map_err(|e| e.to_string())
}
