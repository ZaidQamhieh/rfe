//! What belongs to the desktop around the window: the tray icon and its menu, closing to the tray,
//! operating-system notifications and starting with the session.
//!
//! The page gets none of the plugins behind this. It asks through the few commands below, and each
//! one checks what it is given: a notification has a fixed set of kinds, a short title and a short
//! body with no control characters; the autostart entry is written for this very program and
//! nothing else; the tray menu only does what its four items say.

use crate::transfers::Transfers;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Emitter, Manager};

const TRAY_ID: &str = "rfe-tray";

/// The choices the window passes on, and whether the tray exists at all.
#[derive(Default)]
pub struct Desktop {
    close_to_tray: AtomicBool,
    notifications: AtomicBool,
    tray: AtomicBool,
}

impl Desktop {
    /// Closing the window hides it instead, but only when there is a tray icon to bring it back from.
    pub fn hides_on_close(&self) -> bool {
        self.close_to_tray.load(Ordering::SeqCst) && self.tray.load(Ordering::SeqCst)
    }
}

/// The kinds of notification the window may ask for.
pub const KINDS: [&str; 5] = ["done", "error", "pair", "update", "test"];

/// A title or body fit to hand to the system: no control or direction-changing characters, no angle
/// brackets (Linux notification servers render `<b>`, `<a href>` and `<img>` in a body, and device
/// names and file names come from outside), cut at a character boundary to `max` characters.
pub fn clean_text(text: &str, max: usize) -> String {
    let kept: String = text
        .chars()
        .filter(|c| {
            !c.is_control()
                && !matches!(c,
                    '\u{200b}'..='\u{200f}' | '\u{202a}'..='\u{202e}' | '\u{2060}'..='\u{2064}'
                    | '\u{2066}'..='\u{2069}' | '\u{feff}')
        })
        .map(|c| match c {
            '<' => '\u{2039}',
            '>' => '\u{203a}',
            c => c,
        })
        .collect();
    let kept = kept.trim();
    if kept.chars().count() <= max {
        return kept.to_string();
    }
    let cut: String = kept.chars().take(max.saturating_sub(1)).collect();
    format!("{}…", cut.trim_end())
}

// ---- starting with the session (Linux: an XDG autostart entry) ----

const AUTOSTART_FILE: &str = "rfe-desktop.desktop";

/// The text of the autostart entry for the program at `exe`. Two layers of escaping apply, as the
/// Desktop Entry specification has them: the path is quoted for the Exec line (a quote, backslash,
/// `$` or backtick in it gets a backslash, `%` is doubled), and then the whole value is written as a
/// string (every backslash doubled). Control characters cannot be written and are left out; the
/// program starts hidden.
pub fn autostart_entry(exe: &Path) -> String {
    let mut quoted = String::from("\"");
    for c in exe.to_string_lossy().chars().filter(|c| !c.is_control()) {
        match c {
            // Exec-level escape, then the string-level doubling of its backslash.
            '"' | '`' | '$' => quoted.push_str("\\\\"),
            '\\' => quoted.push_str("\\\\\\"),
            // `%` starts a field code in an Exec line; two of them are one literal percent sign.
            '%' => quoted.push('%'),
            _ => {}
        }
        quoted.push(c);
    }
    quoted.push('"');
    format!(
        "[Desktop Entry]\nType=Application\nName=RFE Desktop\nComment=Starts RFE Desktop hidden in the tray\nExec={quoted} --minimized\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
    )
}

fn autostart_path(config_dir: &Path) -> PathBuf {
    config_dir.join("autostart").join(AUTOSTART_FILE)
}

/// Turns the autostart entry on or off in `config_dir` (the user's XDG config folder).
pub fn set_autostart_in(config_dir: &Path, exe: &Path, on: bool) -> Result<(), String> {
    let file = autostart_path(config_dir);
    if !on {
        return match std::fs::remove_file(&file) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(format!("cannot remove the autostart entry: {e}")),
        };
    }
    if !exe.is_absolute() || exe.to_string_lossy().chars().any(|c| c.is_control()) {
        return Err("cannot find where this program is installed".into());
    }
    let dir = file.parent().unwrap_or(config_dir);
    std::fs::create_dir_all(dir).map_err(|e| format!("cannot create {}: {e}", dir.display()))?;
    std::fs::write(&file, autostart_entry(exe))
        .map_err(|e| format!("cannot write {}: {e}", file.display()))
}

pub fn autostart_is_on(config_dir: &Path) -> bool {
    autostart_path(config_dir).is_file()
}

/// The program to start: the AppImage file when running from one, else this executable.
fn program_path() -> Result<PathBuf, String> {
    if let Some(p) = std::env::var_os("APPIMAGE") {
        let p = PathBuf::from(p);
        if p.is_absolute() && p.is_file() {
            return Ok(p);
        }
    }
    std::env::current_exe().map_err(|e| format!("cannot find where this program is installed: {e}"))
}

#[cfg(target_os = "linux")]
fn config_dir(app: &AppHandle) -> Result<PathBuf, String> {
    app.path().config_dir().map_err(|e| e.to_string())
}

#[tauri::command]
pub fn desktop_autostart_state(app: AppHandle) -> bool {
    #[cfg(target_os = "linux")]
    {
        config_dir(&app)
            .map(|d| autostart_is_on(&d))
            .unwrap_or(false)
    }
    #[cfg(not(target_os = "linux"))]
    {
        let _ = app;
        false
    }
}

#[tauri::command]
pub fn desktop_set_autostart(app: AppHandle, on: bool) -> Result<bool, String> {
    #[cfg(target_os = "linux")]
    {
        let dir = config_dir(&app)?;
        set_autostart_in(&dir, &program_path()?, on)?;
        Ok(autostart_is_on(&dir))
    }
    #[cfg(not(target_os = "linux"))]
    {
        let _ = (app, on, program_path);
        Err("Starting with the session is only set up on Linux so far.".into())
    }
}

// ---- the choices, the tray and the notifications ----

#[tauri::command]
pub fn desktop_set_prefs(
    state: tauri::State<'_, Desktop>,
    close_to_tray: bool,
    notifications: bool,
) {
    state.close_to_tray.store(close_to_tray, Ordering::SeqCst);
    state.notifications.store(notifications, Ordering::SeqCst);
}

/// The smallest layout the window is built for, in CSS pixels at 100% (`tauri.conf.json` has the same).
const MIN_WIDTH: f64 = 900.0;
const MIN_HEIGHT: f64 = 600.0;

/// The scale a size is clamped to: the interface sizes the window offers run from 90% to 130%.
pub fn clamp_scale(scale: f64) -> f64 {
    if scale.is_finite() {
        scale.clamp(0.9, 1.3)
    } else {
        1.0
    }
}

/// The page scales with the window by itself (the root size in the stylesheet); the interface size in
/// Settings multiplies that. The smallest window grows with it, so the layout always has the room it
/// was drawn for.
#[tauri::command]
pub fn desktop_set_scale(window: tauri::WebviewWindow, scale: f64) -> Result<f64, String> {
    let z = clamp_scale(scale);
    window
        .set_min_size(Some(tauri::LogicalSize::new(MIN_WIDTH * z, MIN_HEIGHT * z)))
        .map_err(|e| e.to_string())?;
    Ok(z)
}

#[tauri::command]
pub fn desktop_has_tray(state: tauri::State<'_, Desktop>) -> bool {
    state.tray.load(Ordering::SeqCst)
}

/// Shows a system notification. Only the kinds in [`KINDS`] are accepted, and nothing is shown
/// unless the person turned notifications on (the `test` kind ignores that, it is the button that
/// shows what they look like). Returns an error text when the system has no notification service.
#[tauri::command]
pub fn desktop_notify(
    app: AppHandle,
    state: tauri::State<'_, Desktop>,
    kind: String,
    title: String,
    body: String,
) -> Result<bool, String> {
    use tauri_plugin_notification::NotificationExt;
    if !KINDS.contains(&kind.as_str()) {
        return Err("that is not a kind of notification".into());
    }
    if kind != "test" && !state.notifications.load(Ordering::SeqCst) {
        return Ok(false);
    }
    let title = clean_text(&title, 80);
    let body = clean_text(&body, 240);
    if title.is_empty() {
        return Err("a notification needs a title".into());
    }
    app.notification()
        .builder()
        .title(title)
        .body(body)
        .show()
        .map_err(|e| format!("the system did not show the notification: {e}"))?;
    Ok(true)
}

/// Brings the main window back from the tray (or from behind other windows).
fn show_main(app: &AppHandle) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.unminimize();
        let _ = w.set_focus();
    }
}

/// Makes the tray icon with its menu. Without a working tray (no status-notifier host, no
/// indicator library) the app simply runs without one and closing the window closes the app.
pub fn setup_tray(app: &AppHandle) {
    let built = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| build_tray(app)));
    match built {
        Ok(Ok(())) => {
            app.state::<Desktop>().tray.store(true, Ordering::SeqCst);
            crate::applog::info("tray icon ready");
        }
        Ok(Err(e)) => crate::applog::info(&format!("no tray icon: {e}")),
        Err(_) => crate::applog::info("no tray icon: the system has no indicator library"),
    }
}

fn build_tray(app: &AppHandle) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "open", "Open RFE", true, None::<&str>)?;
    let running = MenuItem::with_id(app, "running", "No transfers running", false, None::<&str>)?;
    let pause = MenuItem::with_id(app, "pause", "Pause all transfers", true, None::<&str>)?;
    let pair = MenuItem::with_id(app, "pair", "Pair a phone…", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit RFE", true, None::<&str>)?;
    let sep = PredefinedMenuItem::separator(app)?;
    let menu = Menu::with_items(app, &[&open, &running, &pause, &pair, &sep, &quit])?;
    let mut tray = TrayIconBuilder::with_id(TRAY_ID)
        .tooltip("RFE Desktop")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "open" => show_main(app),
            "pause" => {
                let n = app.state::<Transfers>().pause_all();
                crate::applog::info(&format!("tray: paused {n} transfers"));
            }
            "pair" => {
                show_main(app);
                let _ = app.emit("tray-action", "pair");
            }
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;
    // The "running" line follows the list of transfers.
    let handle = app.clone();
    tauri::async_runtime::spawn(async move {
        let mut last = usize::MAX;
        loop {
            tokio::time::sleep(std::time::Duration::from_secs(2)).await;
            let n = handle.state::<Transfers>().running_count();
            if n != last {
                last = n;
                let text = match n {
                    0 => "No transfers running".to_string(),
                    1 => "1 transfer running".to_string(),
                    n => format!("{n} transfers running"),
                };
                let _ = running.set_text(text);
            }
        }
    });
    Ok(())
}

/// Hides the window at start when the program was started by the session (`--minimized`) and a tray
/// icon is there to bring it back.
pub fn start_hidden_if_asked(app: &AppHandle) {
    let asked = std::env::args().any(|a| a == "--minimized");
    if asked && app.state::<Desktop>().tray.load(Ordering::SeqCst) {
        if let Some(w) = app.get_webview_window("main") {
            let _ = w.hide();
        }
    }
}

/// Called for the close button: with the choice made and a tray to return from, the window hides.
pub fn on_close_requested(window: &tauri::Window, api: &tauri::CloseRequestApi) {
    let app = window.app_handle();
    if window.label() == "main" && app.state::<Desktop>().hides_on_close() {
        api.prevent_close();
        let _ = window.hide();
    }
}

/// Brings the window forward when a second copy of the program is started.
pub fn show_for_second_start(app: &AppHandle) {
    show_main(app);
}

#[cfg(test)]
mod scale_tests {
    use super::clamp_scale;

    #[test]
    fn the_interface_size_stays_between_90_and_130_percent() {
        assert_eq!(clamp_scale(1.15), 1.15);
        assert_eq!(clamp_scale(0.2), 0.9);
        assert_eq!(clamp_scale(9.0), 1.3);
        assert_eq!(clamp_scale(f64::NAN), 1.0);
        assert_eq!(clamp_scale(f64::INFINITY), 1.0);
    }
}
