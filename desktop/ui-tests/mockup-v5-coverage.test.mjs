// Feature coverage of desktop/mockups/v5 against the real desktop app.
// The feature list comes from three real-app sources: desktop/src-tauri/commands.txt (every backend command),
// desktop/docs/user-guide.md (every section and bold control) and desktop/ui (pages, menus, dialogs).
// Each row opens the place in the rendered design (page, dialog, menu, settings section, tool) and asserts the
// feature's control or text is on screen there. A row that fails is a real-app feature the design lacks.
// Run: node --test desktop/ui-tests/mockup-v5-coverage.test.mjs   (needs `npm ci` in desktop/ui-tests)

import { test } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, ResourceLoader } from "jsdom";
import { fileURLToPath, pathToFileURL } from "node:url";

const PAGE = fileURLToPath(new URL("../mockups/v5/01-rfe.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
class Offline extends ResourceLoader {
  fetch(url, o) { return String(url).startsWith("file:") ? super.fetch(url, o) : Promise.resolve(Buffer.from("")); }
}
async function open(mode) {
  const dom = await JSDOM.fromFile(PAGE, {
    url: pathToFileURL(PAGE).href + "?v=" + mode, runScripts: "dangerously", resources: new Offline(), pretendToBeVisual: true,
    beforeParse(w) { w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} }); },
  });
  const w = dom.window;
  if (w.document.readyState !== "complete") await Promise.race([new Promise((r) => w.addEventListener("load", r, { once: true })), sleep(8000)]);
  await sleep(200);
  return w;
}
const T = (el) => (el ? el.textContent.replace(/\s+/g, " ") : "");
const esc = (w) => w.document.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
/* "devices" is a route of the one Devices page (nav item go:servers) that selects a phone in the panel */
const go = (w, v) => (v === "devices" ? w.K.go("devices") : w.document.querySelector(`#side [data-k="go:${v}"]`).click());
const layer = (w) => T(w.document.querySelector(".k-layer")) + T(w.document.querySelector(".k-menu"));
const stage = (w) => T(w.document.querySelector("#st")) + T(w.document.querySelector("#rail"));
const sidebar = (w) => T(w.document.querySelector("#side"));

/* where: page:<view> | dlg:<name>[:<tab>] | menu:<name> | set:<Settings section> | tool:<Tools tab> | side | src:<file> */
const ROWS = [
  // ---- the window (guide: The window)
  ["Rail: Files, Devices, Transfers, Search, Tools, History, Settings", "side", /Files[\s\S]*Devices[\s\S]*Transfers[\s\S]*Search[\s\S]*Tools[\s\S]*History[\s\S]*Settings/],
  ["New connection button", "side", /Add a computer/],
  ["Overview page", "page:home", /Recent files/],
  ["Command palette (Ctrl K)", "search", /Search|Command/],
  ["Welcome tour", "dlg:welcome", /Welcome to RFE/],
  ["Notifications menu", "menu:notes", /Notifications/],
  ["Account and theme menu", "menu:account", /Theme[\s\S]*Sign out/],
  // ---- connect and trust (commands: probe_agent, discover_agents, list_pins, forget_pin, saved_agent)
  ["Add a computer: find PCs on the network", "dlg:newconn:1", /same network/],
  ["Add a computer: manual address", "dlg:newconn:1", /address|Address/],
  ["Trust a computer's key (plain words)", "dlg:trust", /Same number|Do the numbers|match|Check/i],
  ["A computer's key changed", "dlg:keychanged", /different|changed/i],
  ["Trusted computers list (pins)", "dlg:pins", /trust|Trusted/i],
  ["Reconnect after a change", "dlg:reconnect", /Connect to nas again/],
  // ---- sign in (login, pair_with_code, request_pairing, poll_pairing, cancel_pairing)
  ["Sign in: account", "dlg:signin:Account", /Username[\s\S]*Password/],
  ["Sign in: pairing code", "dlg:signin:Pairing code", /Pairing code/],
  ["Sign in: ask the PC", "dlg:signin:Ask the PC", /Waiting for the PC/],
  ["Allow on the PC: wait with a number (poll/cancel)", "dlg:newconn:2", /Waiting for[\s\S]*left[\s\S]*Back|Cancel/],
  ["Create a new device key", "dlg:devkey", /new device/i],
  ["Accounts list", "dlg:accounts", /Accounts/],
  ["Edit a connection", "dlg:editconn", /Edit|Name/],
  ["Sign out", "menu:account", /Sign out/],
  // ---- pair a phone, requests (generate_pairing_code, list_pair_requests, answer_pair_request)
  ["Pair a phone: code and QR", "dlg:pairphone", /Pair a phone|code/i],
  ["Pairing requests list", "page:devices", /Asking to join/],
  ["Accept a pairing request", "dlg:accept", /Allow Pixel 9/],
  ["Reject a pairing request", "page:devices", /Refuse|Reject/],
  // ---- settings (guide: Settings; commands app_settings, desktop_*)
  ["Settings: Appearance theme, density, size, click mode", "set:Appearance", /Theme[\s\S]*Row density[\s\S]*Interface size[\s\S]*Open files and folders/],
  ["Settings: Language, reduced motion, high contrast", "set:Language and accessibility", /Language[\s\S]*Reduce|motion[\s\S]*contrast/i],
  ["Settings: Transfers parallel, speed limit, name clash, checksum, download folder", "set:Transfers", /Parallel transfers[\s\S]*Speed limit[\s\S]*name already exists[\s\S]*Check every download[\s\S]*Download folder/],
  ["Settings: Connection reconnect and in-app notices", "set:Connection", /Reconnect automatically[\s\S]*Notifications inside the app/],
  ["Settings: Files trash, keep days, ask where, hidden", "set:Files", /Move deleted items to Trash[\s\S]*Keep items in Trash[\s\S]*Ask where[\s\S]*hidden/],
  ["Settings: Security approve here, code lifetime, trusted, sign out phones", "set:Security", /Approve new devices here[\s\S]*Pairing code lifetime[\s\S]*Computers you trust[\s\S]*Sign out all phones/],
  ["Settings: Desktop tray, autostart, notifications, test", "set:Desktop", /Close to the system tray[\s\S]*Start RFE when I sign in[\s\S]*Desktop notifications[\s\S]*Test notification/],
  ["Settings: Backup and restore", "set:Backup", /Back up[\s\S]*Restore/],
  ["Settings: Troubleshooting log level, agent log, keystore, device key", "set:Troubleshooting", /Log detail[\s\S]*log[\s\S]*Check the keystore[\s\S]*Device key/],
  ["Settings: Help shortcuts", "set:Help", /CtrlK[\s\S]*Command palette/],
  ["Settings: About version, details, updates, tour", "set:About", /Version[\s\S]*Check for updates/],
  ["Back up settings dialog", "dlg:backup", /Back up settings[\s\S]*Password/],
  ["Report a problem", "dlg:report", /Report a problem/],
  ["Check for updates dialog", "dlg:updates", /Check for updates[\s\S]*Download/],
  // ---- files (guide: Files; commands files_*, local_*)
  ["Files page: tree of rows with actions", "page:files", /north-bed-after\.jpg|garden-plan/],
  ["Local files: This computer in Browse on", "menu:browse", /This computer/],
  ["Folders a computer shares (files_roots)", "dlg:roots", /Folders studio-pc shares/],
  ["Where this login may start (locations)", "page:files", /studio-pc/],
  ["New folder", "dlg:newfolder", /New folder/],
  ["Rename one or several", "dlg:batch", /Rename/],
  ["Copy or move", "dlg:copyto", /Copy|Move/],
  ["Delete to Trash", "dlg:delete", /Trash|Delete/],
  ["Properties", "dlg:props", /Properties|Size|Modified/i],
  ["Checksum", "dlg:checksum", /Checksum|SHA/i],
  ["Open a video or picture without downloading", "dlg:stream", /Nothing is saved on this computer[\s\S]*Save a copy/],
  ["Preview a picture", "dlg:preview", /north-bed|Preview|Download/],
  ["Edit a text file", "dlg:editor", /Save/],
  ["File menu: preview, download, share, offline, favorites, copy/move, rename, zip, checksum, properties, path, trash", "menu:file", /Preview[\s\S]*Download[\s\S]*Share link[\s\S]*offline[\s\S]*favorites[\s\S]*Copy or move[\s\S]*Rename[\s\S]*Compress[\s\S]*Checksum[\s\S]*Properties[\s\S]*Copy path[\s\S]*Trash/],
  ["Empty-area menu: new folder, refresh, select all, copy folder path", "menu:blank", /New folder[\s\S]*Refresh[\s\S]*Select all[\s\S]*Copy folder path/],
  ["Extract an archive", "src:kit.js", /Extract/],
  ["Upload to a computer", "dlg:upload", /Upload/],
  ["Changed lately (files_recent)", "dlg:recent", /Changed lately/],
  ["Make available offline", "menu:file", /offline/],
  // ---- search (files_search, local_search)
  ["Search panel with per-computer scope", "search", /Searching[\s\S]*of[\s\S]*computers/],
  ["Search filters (kind, changed, size)", "menu:sfilt", /Kind[\s\S]*Changed[\s\S]*Size/],
  ["Search: Show all table", "src:01-rfe.html", /show all/i],
  // ---- sharing (share_mint, share_links, share_revoke_link)
  ["Make a share link", "dlg:share", /Share/],
  ["Live share links list", "tool:share", /Live share links/],
  ["Turn a link off", "dlg:turnoff", /Turn off this link/],
  ["Turn a link off while its computer is offline", "dlg:offrevoke", /offline/],
  // ---- tools
  ["Favorites tool", "tool:fav", /Favorites|favorite/i],
  ["Storage tool", "tool:store", /Storage|free|used/i],
  ["Trash tool: restore, delete forever, empty (trash_*)", "tool:trash", /Restore/],
  ["Empty Trash confirm", "dlg:emptytrash", /Empty/],
  ["Sync rules", "tool:sync", /Sync rules/],
  ["New sync rule", "dlg:newrule", /rule|Rule/],
  ["Apps on the PC (list_host_apps)", "tool:apps", /Launch|Open/],
  ["Launch an app (launch_host_app)", "dlg:launch", /Open GIMP/],
  // ---- servers (list_hosts, switch_host, rename_host, remove_host, wake_computer, agent_health, diagnostics)
  ["Servers page", "page:servers", /studio-pc[\s\S]*nas[\s\S]*media-mini/],
  ["Status of a computer", "dlg:status", /How studio-pc is doing/],
  ["Run diagnostics", "dlg:diag", /Diagnos|Check the connection|Checking|connection/i],
  ["Settings for one computer (rename, folder, hidden, speed, wake, sign out)", "dlg:srvset", /Name[\s\S]*Downloads from it go to[\s\S]*Show hidden files[\s\S]*Wake it[\s\S]*Sign out/],
  ["Forget a computer", "dlg:forget", /Remove media-mini/],
  ["Wake a sleeping computer", "menu:server", /Wake computer/],
  ["Server menu: browse, disconnect, edit, status, diagnose, roots, wake, sign in", "menu:server", /Browse files[\s\S]*Disconnect[\s\S]*Edit connection[\s\S]*Status/],
  ["Switch computer (browse on)", "menu:browse", /Browse on/],
  ["Agent's own log", "dlg:agentlog", /studio-pc’s log/],
  // ---- devices (list_devices, device_access, set_device_access, revoke_device, remove_device)
  ["Devices page with the device list and security events", "page:devices", /desk-linux[\s\S]*This app[\s\S]*Recent security events/],
  ["Device permissions", "page:devices", /See files[\s\S]*Download[\s\S]*Upload[\s\S]*Delete[\s\S]*share links[\s\S]*apps/i],
  ["Folder limit per device", "page:devices", /Only this folder/],
  ["Revoke a device", "dlg:revoke", /Remove Pixel 8/],
  ["Remove a device record", "dlg:remove", /Remove|list/],
  ["Revoke this app's own row warning", "dlg:revokeme", /sign-in this app uses/],
  ["Sign out all phones", "dlg:signoutall", /Sign out all phones|phones/],
  ["Device menu: activity, revoke, remove", "menu:device", /Activity[\s\S]*Revoke[\s\S]*Remove/],
  // ---- transfers (transfer_* commands)
  ["Transfers page: pause, resume, retry failed, clear done", "page:transfers", /Pause all[\s\S]*Resume all[\s\S]*Retry failed[\s\S]*Clear done/],
  ["Transfer states: needs choice, paused, failed, moving, checking, waiting, done", "page:transfers", /Action required[\s\S]*Paused[\s\S]*Failed[\s\S]*Checking[\s\S]*Waiting[\s\S]*Done/],
  ["Name clash choices (transfer_resolve)", "src:base.js", /Keep both[\s\S]*Replace[\s\S]*Skip/],
  ["Stop a transfer", "dlg:cancelx", /Stop this transfer/],
  ["Unfinished transfers after a restart", "dlg:resume", /unfinished transfers/],
  ["Transfer journal banner", "page:transfers", /left unfinished/],
  // ---- history, audit
  ["History page and filters", "page:history", /History[\s\S]*Transfers[\s\S]*Connections[\s\S]*Security[\s\S]*Devices[\s\S]*Shares/],
  ["Export history", "src:base.js", /Export as CSV/],
  ["Clear history confirm", "dlg:clearhist", /Clear the history/],
  ["Agent log from History", "src:kit.js", /Agent log/],
  // ---- guide terms the design words differently on purpose (plain words for non-technical users)
  ["Guide 'Trust NAME?' -> trust dialog for a named computer", "dlg:newconn:2", /Allow this computer on|Waiting for/],
  ["Guide 'Ask for approval' -> Ask the PC button", "dlg:signin:Ask the PC", /Ask[\s\S]*for this computer/],
  ["Guide 'Copy to… / Move to…' -> one Copy or move dialog", "dlg:copyto", /Copy[\s\S]*Move/],
  ["Guide 'Add a server' -> Add a computer", "side", /Add a computer/],
  ["Guide 'Status of NAME' -> How NAME is doing", "dlg:status", /How studio-pc is doing/],
  ["Guide 'Ask where to save downloads' -> per-download switch", "src:kit.js", /Ask where to save each download/],
  ["Guide 'Clear completed' -> Clear done", "page:transfers", /Clear done/],
  ["Editor: unsaved-changes prompt (Discard / Keep editing)", "dlg:editleave", /Keep editing[\s\S]*Discard/],
  ["Preview: Fit and Open with default app", "dlg:preview", /Fit[\s\S]*Open with default app/],
];

import fs from "node:fs";
const rd = (f) => fs.readFileSync(new URL(f, import.meta.url), "utf8");
const SRC = { "kit.js": rd("../mockups/v5/kit.js"), "base.js": rd("../mockups/v5/base.js"), "01-rfe.html": rd("../mockups/v5/01-rfe.html") };

function shown(w, where) {
  const r = where.slice(where.indexOf(":") + 1), kind = where.split(":")[0];
  const i = r.indexOf(":"), a = i < 0 ? r : r.slice(0, i), b = i < 0 ? undefined : r.slice(i + 1);
  let text = "";
  try {
    if (kind === "side") text = sidebar(w);
    else if (kind === "src") text = SRC[a];
    else if (kind === "page") { go(w, a); text = stage(w); }
    else if (kind === "dlg") { w.K.openDlg(a, b); text = layer(w); esc(w); }
    else if (kind === "menu") { w.K.menu(a, 300, 200); text = layer(w); esc(w); }
    else if (kind === "set") { go(w, "settings"); w.document.querySelector(`[data-k="set:${a}"]`).click(); text = stage(w); }
    else if (kind === "tool") { go(w, "tools"); const t = w.document.querySelector(`[data-k="tool:${a}"]`); if (t) t.click(); text = stage(w); }
    else if (kind === "search") { go(w, "search"); text = T(w.document.querySelector(".b-sr")); esc(w); }
  } catch (e) { text = "ERROR " + e.message; }
  return text;
}

test(`coverage: feature rows (${ROWS.length})`, async () => {
  const w = await open("full");
  const missing = ROWS.filter(([, where, re]) => !re.test(shown(w, where))).map(([n, wh, re]) => `${n} [${wh}] ${re}`);
  assert.deepEqual(missing, []);
});

/* Every backend command of the real app maps to a place in the design that shows its control. */
const CMD = {
  files_roots: ["dlg:roots", /Folders studio-pc shares/], files_list: ["page:files", /north-bed-after/], files_meta: ["dlg:props", /Size/],
  files_create_folder: ["dlg:newfolder", /New folder/], files_create_file: ["menu:blank", /New folder/], files_rename: ["dlg:batch", /Rename/],
  files_trash: ["dlg:delete", /Trash/], files_search: ["search", /Searching/], files_recent: ["dlg:recent", /Changed lately/],
  files_copy: ["dlg:copyto", /Copy/], files_move: ["dlg:copyto", /Move/], files_compress: ["menu:file", /Compress/], files_extract: ["src:kit.js", /Extract/],
  files_archive_list: ["src:kit.js", /Extract/], files_checksum: ["dlg:checksum", /Checksum|SHA/i],
  files_checksums: ["dlg:checksum", /Checksum|SHA/i], files_chmod: ["dlg:props", /Permissions|Properties/i],
  share_mint: ["dlg:share", /Share/], share_links: ["tool:share", /Live share links/], share_revoke_link: ["dlg:turnoff", /Turn off this link/],
  trash_items: ["tool:trash", /Restore/], trash_restore_items: ["tool:trash", /Restore/], trash_empty_items: ["dlg:emptytrash", /Empty/],
  files_read_text: ["dlg:editor", /Save/], files_write_text: ["dlg:editor", /Save/], files_thumb: ["dlg:preview", /north-bed/], files_video: ["dlg:preview", /north-bed/],
  wake_computer: ["menu:server", /Wake computer/], saved_agent: ["dlg:editconn", /Name|Edit/], list_pins: ["dlg:pins", /Trusted|trust/i],
  forget_pin: ["dlg:pins", /Trusted|trust|Forget|Remove/i], probe_agent: ["dlg:newconn:1", /address|Address/], login: ["dlg:signin:Account", /Password/],
  pair_with_code: ["dlg:signin:Pairing code", /Pairing code/], request_pairing: ["dlg:signin:Ask the PC", /Waiting for the PC/],
  poll_pairing: ["dlg:signin:Ask the PC", /Waiting for the PC/], cancel_pairing: ["dlg:signin:Ask the PC", /Not now/],
  reset_device_key: ["dlg:devkey", /new device/i], list_devices: ["page:devices", /desk-linux/], sign_out: ["menu:account", /Sign out/],
  app_settings: ["set:Appearance", /Theme/], discover_agents: ["dlg:newconn:1", /same network/], diagnostics: ["dlg:diag", /connection|Diagnos/i],
  set_log_level: ["set:Troubleshooting", /Log detail/], check_keystore: ["set:Troubleshooting", /Check the keystore/],
  list_host_apps: ["tool:apps", /Launch|Open/], launch_host_app: ["dlg:launch", /Open GIMP/], agent_health: ["dlg:status", /How studio-pc is doing/],
  generate_pairing_code: ["dlg:pairphone", /code/i], generate_local_pairing_code: ["dlg:pairphone", /code/i], local_agent_admin: ["page:devices", /Pair a phone/], audit_page: ["page:devices", /Recent security events/], agent_log: ["dlg:agentlog", /studio-pc’s log/], backups_status: ["set:Backup", /Restore/],
  device_access: ["page:devices", /See files[\s\S]*Only look, never change/], device_access_all: ["page:devices", /Only this folder/], set_device_access: ["page:devices", /Only look, never change/],
  revoke_device: ["dlg:revoke", /Remove Pixel 8/], remove_device: ["dlg:remove", /Remove/], list_pair_requests: ["page:devices", /Asking to join/],
  account_server_status: ["dlg:acctserver", /Account server|account server/], account_server_key: ["dlg:acctserver", /Look up its key/], account_server_enable: ["dlg:acctserver", /Account server|account server/], account_server_trust: ["dlg:acctserver", /Trust this account server/], account_server_untrust: ["dlg:acctuntrust", /Stop trusting/],
  answer_pair_request: ["dlg:accept", /Allow Pixel 9/], list_hosts: ["page:servers", /studio-pc[\s\S]*nas/], switch_host: ["menu:browse", /Browse on/],
  rename_host: ["dlg:srvset", /Name/], remove_host: ["dlg:forget", /Remove media-mini/],
  transfer_download: ["menu:file", /Download/], transfer_upload: ["dlg:upload", /Upload/], transfer_upload_tree: ["dlg:upload", /Upload/],
  transfer_download_tree: ["menu:file", /Download/], local_places: ["menu:browse", /This computer/], local_list: ["menu:browse", /This computer/],
  local_search: ["menu:browse", /This computer/], local_create_folder: ["menu:browse", /This computer/], local_rename: ["menu:browse", /This computer/],
  local_delete: ["menu:browse", /This computer/], local_read_text: ["menu:browse", /This computer/], local_read_image: ["menu:browse", /This computer/], local_read_video: ["menu:browse", /This computer/],
  local_open: ["menu:browse", /This computer/], local_save_text: ["menu:browse", /This computer/],
  pick_files: ["dlg:upload", /Upload/], pick_folder: ["dlg:upload", /Upload/],
  transfer_list: ["page:transfers", /Action required/], transfer_cancel: ["dlg:cancelx", /Stop this transfer/], transfer_pause: ["page:transfers", /Pause all/],
  transfer_retry: ["page:transfers", /Retry failed/], transfer_set_prefs: ["set:Transfers", /Parallel transfers/], transfer_resolve: ["src:base.js", /Keep both/],
  transfer_clear_finished: ["page:transfers", /Clear done/], transfer_folder: ["set:Transfers", /Download folder/],
  desktop_set_prefs: ["set:Desktop", /Close to the system tray/], desktop_has_tray: ["set:Desktop", /Close to the system tray/],
  desktop_set_scale: ["set:Appearance", /Interface size/], desktop_limits: ["menu:browse", /This computer/], desktop_notify: ["set:Desktop", /Test notification/],
  desktop_autostart_state: ["set:Desktop", /Start RFE when I sign in/], desktop_set_autostart: ["set:Desktop", /Start RFE when I sign in/],
  update_check: ["dlg:updates", /Check for updates/], update_download: ["dlg:updates", /Download/],
  choose_download_folder: ["set:Transfers", /Change…/], reset_download_folder: ["set:Transfers", /Reset/],
  events_sync: ["page:devices", /Recent security events/], /* no control of its own: it makes the Devices page (and the others) refresh when a computer pushes a change */
};
test("coverage: all backend commands map to a rendered control", async () => {
  const w = await open("full");
  const cmds = rd("../src-tauri/commands.txt").split(/\s+/).filter(Boolean);
  const unmapped = cmds.filter((c) => !CMD[c]);
  const stale = Object.keys(CMD).filter((c) => !cmds.includes(c));
  const bad = cmds.filter((c) => CMD[c] && !CMD[c][1].test(shown(w, CMD[c][0]))).map((c) => `${c} [${CMD[c][0]}]`);
  assert.deepEqual({ unmapped, stale, bad }, { unmapped: [], stale: [], bad: [] });
});

/* Every bold term of the user guide: verbatim in the design, or an explicit exception with a reason. */
const ALIAS = { // guide wording -> where the design shows it in plain words
  "Trust NAME?": ["dlg:newconn:2", /Allow this computer on/], "Ask for approval": ["dlg:signin:Ask the PC", /Ask[\s\S]*for this computer/],
  "Copy to…": ["dlg:copyto", /Copy/], "Move to…": ["dlg:copyto", /Move/], "Add a server": ["side", /Add a computer/],
  "Status of NAME": ["dlg:status", /How studio-pc is doing/], "Ask where to save downloads": ["src:kit.js", /Ask where to save each download/],
  "Clear completed": ["page:transfers", /Clear done/], "This computer's own row": ["page:devices", /This app/],
  "Devices → Security events": ["page:devices", /Recent security events/], "Settings → Troubleshooting → Computer log": ["dlg:agentlog", /log/],
  "Backup status": ["set:Backup", /Restore/], "Computer log": ["set:Troubleshooting", /studio-pc’s log/], "Check secure storage": ["set:Troubleshooting", /Check the keystore/],
  "This computer's identity": ["set:Troubleshooting", /Device key/], "Reset…": ["set:Troubleshooting", /New key…/],
  "They match: trust": ["dlg:newconn:2", /number/i], "Settings → Files → Show hidden files": ["set:Files", /hidden/],
};
const NOT_DESKTOP = { // not a control of the desktop window
  "RFE for Android → Add computer → Scan QR": "phone app, not the desktop window",
  " and a ": "sentence fragment between two bold words", " button. ": "sentence fragment between two bold words",
};
test("coverage: every bold term of the user guide is in the design or explained", async () => {
  const w = await open("full");
  // all rendered text: every page, settings section, tool, dialog, menu
  let all = "";
  for (const v of ["home", "files", "servers", "devices", "transfers", "search", "tools", "history", "settings"]) all += shown(w, "page:" + v);
  for (const s of ["Appearance", "Language and accessibility", "Transfers", "Connection", "Files", "Security", "Desktop", "Backup", "Troubleshooting", "Help", "About"]) all += shown(w, "set:" + s);
  for (const t of ["fav", "share", "store", "sync", "trash", "apps"]) all += shown(w, "tool:" + t);
  for (const k of Object.keys(w.K.DLG)) for (const tab of [undefined, "1", "2", "Account", "Pairing code", "Ask the PC"]) all += shown(w, "dlg:" + k + (tab ? ":" + tab : ""));
  for (const k of Object.keys(w.K.MENU)) all += shown(w, "menu:" + k);
  all += SRC["kit.js"] + SRC["base.js"] + SRC["01-rfe.html"];
  const n = (x) => x.toLowerCase().replace(/[’']/g, "'").replace(/\s+/g, " ");
  const A = n(all);
  const terms = [...new Set([...rd("../docs/user-guide.md").matchAll(/\*\*([^*\n]{2,60})\*\*/g)].map((m) => m[1].replace(/[.:]+$/, "")))];
  const bad = [];
  for (const t of terms) {
    if (A.includes(n(t)) || NOT_DESKTOP[t]) continue;
    const al = ALIAS[t];
    if (al && al[1].test(shown(w, al[0]))) continue;
    // navigation path: every segment must exist in the design
    if (t.includes("→") && t.split("→").every((seg) => A.includes(n(seg.trim())))) continue;
    bad.push(t);
  }
  assert.deepEqual(bad, [], "guide terms with no place in the design");
});

/* Every user-visible label written in the real app's UI source (desktop/ui/*.js) must be in the design as a whole phrase,
   or be mapped to the plain-word control that replaces it. The list is read from the source, not typed here. */
const LABEL_ALIAS = {
  "Change this device’s own access?": ["page:devices", /Only this folder/], "Go to Servers": ["dlg:offrevoke", /Go to Computers/], "Save to…": ["set:Transfers", /Download folder/],
  "Copy to…": ["dlg:copyto", /Copy/], "Move to…": ["dlg:copyto", /Move/], "Move": ["dlg:copyto", /Move/], "Create link": ["dlg:share", /Share|link/i],
  "Deny": ["page:devices", /Refuse/], "Manage devices": ["dlg:pairphone", /Done|Pair/], "Sign out": ["menu:account", /Sign out/],
  "Sign in to": ["dlg:signin:Account", /Sign in/], "Trust the new key for": ["dlg:keychanged", /Trust|Connect/i], "Diagnose": ["menu:server", /Diagnos/i],
  "Allowed roots on": ["dlg:roots", /Folders studio-pc shares/], "Get started": ["dlg:welcome", /Add a computer/], "Save as…": ["dlg:editor", /Save as copy/],
  "Queue unfinished transfers again?": ["dlg:resume", /unfinished transfers/], "Save folder limit": ["page:devices", /Only this folder/],
  "Choose a settings backup": ["set:Backup", /Restore/], "Clear journal": ["dlg:resume", /unfinished|Clear|Forget|Discard/i], "About RFE": ["set:About", /RFE Desktop/],
  "Can’t revoke while offline": ["dlg:offrevoke", /Can't turn this link off yet|offline/], "Show again": ["set:About", /Show the welcome screen/],
  "Apply permissions": ["page:devices", /Only look, never change/], "Permissions…": ["page:devices", /Only look, never change/], "Recently changed on": ["dlg:recent", /Changed lately/], "Status of": ["dlg:status", /How studio-pc is doing/],
  "Backups of": ["set:Backup", /Restore/], "Agent log of": ["dlg:agentlog", /studio-pc’s log/], "Create a new device key?": ["dlg:devkey", /new device/i], "RFE test notification": ["set:Desktop", /Test notification/],
  "Theme:": ["menu:account", /Theme/], "Accounts…": ["dlg:accounts", /Accounts/], "Keyboard shortcuts": ["set:Help", /Command palette/],
  "Search in": ["search", /Searching/], "Manage computers…": ["page:servers", /studio-pc/], "Forget computer": ["dlg:forget", /Remove media-mini/], "This file changed on the computer": ["dlg:editor", /file changed on the/], "Add a computer…": ["side", /Add a computer/],
  "Clear history?": ["dlg:clearhist", /Clear the history/], "Clear completed": ["page:transfers", /Clear done/], "Cancel this transfer?": ["dlg:cancelx", /Stop this transfer/],
  "Cancel transfer": ["dlg:cancelx", /Stop this transfer/], "Go up": ["page:files", /Up|studio-pc/], "Clear filter": ["menu:sfilt", /Clear|Reset|Kind/],
  "Delete": ["dlg:delete", /Delete|Trash/], "Rename": ["dlg:batch", /Rename/], "Launch": ["dlg:launch", /Open GIMP/], "Revoke": ["dlg:revoke", /Remove Pixel 8/],
  "Reset…": ["set:Troubleshooting", /New key…/], "Remove": ["dlg:remove", /Remove/], "Pairing QR code for": ["dlg:pairphone", /code/i], "They match: trust": ["dlg:newconn:2", /number/i], "Discard": ["dlg:editleave", /Discard/],
};
test("coverage: every label in the real app's UI source is in the design", async () => {
  const w = await open("full");
  let all = "";
  for (const v of ["home", "files", "servers", "devices", "transfers", "search", "tools", "history", "settings"]) all += shown(w, "page:" + v);
  for (const s of ["Appearance", "Language and accessibility", "Transfers", "Connection", "Files", "Security", "Desktop", "Backup", "Troubleshooting", "Help", "About"]) all += shown(w, "set:" + s);
  for (const t of ["fav", "share", "store", "sync", "trash", "apps"]) all += shown(w, "tool:" + t);
  for (const k of Object.keys(w.K.DLG)) for (const tab of [undefined, "1", "2", "Account", "Pairing code", "Ask the PC"]) all += shown(w, "dlg:" + k + (tab ? ":" + tab : ""));
  for (const k of Object.keys(w.K.MENU)) all += shown(w, "menu:" + k);
  all += SRC["kit.js"] + SRC["base.js"] + SRC["01-rfe.html"];
  const n = (x) => x.toLowerCase().replace(/[’']/g, "'").replace(/\s+/g, " ");
  const A = n(all);
  const whole = (t) => new RegExp("(^|[^a-z0-9])" + n(t).replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "($|[^a-z0-9])").test(A);
  const dir = fileURLToPath(new URL("../ui/", import.meta.url));
  const labels = new Set();
  for (const f of fs.readdirSync(dir).filter((f) => f.endsWith(".js"))) {
    const src = fs.readFileSync(dir + f, "utf8");
    for (const m of src.matchAll(/\b(?:label|title|aria-label|placeholder)\s*[:=]\s*'([^'\n]{3,50})'/g)) labels.add(m[1]);
    for (const m of src.matchAll(/<button[^>]*>([A-Z][A-Za-z ,'’…]{2,40})<\/button>/g)) labels.add(m[1]);
  }
  assert.ok(labels.size > 120, "label extraction found " + labels.size);
  const bad = [];
  for (const raw of labels) {
    if (/[+<>{}$]/.test(raw)) continue;
    const t = raw.replace(/[ “"]+$/, "").trim();
    if (whole(t)) continue;
    const al = LABEL_ALIAS[t];
    if (al && al[1].test(shown(w, al[0]))) continue;
    bad.push(raw);
  }
  assert.deepEqual(bad, [], "UI labels of the real app with no place in the design");
});
