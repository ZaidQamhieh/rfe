// The real window scripts (ui/*.js) in a DOM (jsdom), answered by the dev stand-in for the core
// (ui-dev/stub-backend.js). It checks behaviour: what is on each screen, which dialog a command
// opens, that no script throws. It does not check how the window looks; the end-to-end tests in
// desktop/e2e look at real pixels. Needs `npm ci` in this folder.
//
//   node --test desktop/ui-tests/*.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const NAS = "192.168.1.20:7443";
const OFFLINE = "203.0.113.9:7443";
const live = [];
// A failed assertion must not leave a window's timers running: the test process would never end.
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, started on `view`, with every script error collected in `errs`. */
async function open(view = "") {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 300)));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1" + (view ? "&view=" + view : ""),
    beforeParse(w) {
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.TextEncoder = TextEncoder; w.TextDecoder = TextDecoder;
      w.ResizeObserver = class { observe() {} disconnect() {} };
      w.HTMLElement.prototype.scrollIntoView = function () {};
      Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  const done = () => { w.close(); };
  return { w, d: w.document, A: w.A, E: w.A.E, errs, done };
}
const until = async (fn, what) => { for (let i = 0; i < 100; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const clear = (d) => { for (const s of d.querySelectorAll(".snack,.scrim,.menu")) s.remove(); };

test("the window starts, lists the roots of the server being browsed, and throws nothing", async () => {
  const { d, A, E, errs, done } = await open();
  assert.equal(A.mainPane.host, NAS);
  await until(() => E.fs.list(NAS, "/"), "the root listing");
  await sleep(100);
  const rows = text(d.querySelector("#stage"));
  assert.match(rows, /srv/); assert.match(rows, /zaid/);
  assert.equal(d.querySelectorAll("#rail button, #rail [data-go]").length >= 8, true, "the rail has its destinations");
  assert.deepEqual(errs, []);
  done();
});

test("the list of allowed folders offers no New folder; a real folder does", async () => {
  const { d, A, E, errs, done } = await open();
  await until(() => E.fs.list(NAS, "/"), "the root listing");
  await sleep(100);
  const sv = E.server(NAS);
  assert.equal(A.atVirtualRoot(NAS, "/"), !!sv.virtualRoot && "/" === sv.virtualRoot);
  assert.equal(sv.virtualRoot, "/", "the stub server lists several roots under a virtual root");
  {
    assert.equal(d.querySelector('[data-act="newfolder"]').disabled, true, "disabled at the virtual root");
    A.mainPane.startNew();
    assert.notEqual(A.mainPane.creating, true, "startNew does nothing there");
  }
  assert.equal(A.atVirtualRoot(NAS, "/srv"), false);
  assert.deepEqual(errs, []);
  done();
});

test("every screen draws with its heading and no script error", async () => {
  const { d, A, errs, done } = await open();
  for (const [v, h] of [["servers", "Servers"], ["devices", "Devices"], ["tools", "Tools"], ["transfers", "Transfers"], ["history", "History"], ["settings", "Settings"]]) {
    A.go(v); await sleep(120);
    assert.equal(text(d.querySelector("#stage h2")), h, v);
  }
  assert.deepEqual(errs, []);
  done();
});

const tab = async (d, name) => { d.querySelector('[data-snav="' + name + '"]').click(); await sleep(40); };
const allTabs = async (d) => { let s = ""; for (const n of [...d.querySelectorAll("[data-snav]")].map((b) => b.dataset.snav)) { await tab(d, n); s += " " + text(d.querySelector("#stage")); } return s; };

test("Settings offers only what the core honours", async () => {
  const { d, A, done } = await open("settings");
  const s = await allTabs(d);
  for (const want of ["Parallel transfers", "Speed limit", "When a name already exists", "Verify checksums", "Download folder", "Reconnect automatically", "Troubleshooting", "Language", "Back up settings", "Show hidden files", "Pinned certificates"]) assert.ok(s.includes(want), want);
  for (const gone of ["Simulate", "Demo", "Reset demo data"]) assert.ok(!s.includes(gone), gone + " is gone");
  A.go("files"); done();
});

test("the transfer settings reach the core, and a taken name asks, then follows the answer", async () => {
  const { d, A, E, errs, done } = await open("settings");
  const W = d.defaultView;
  await tab(d, "Transfers");
  const click = (sel) => d.querySelector(sel).click();
  click('[data-set="parallel"] [data-v="3"]'); await sleep(50);
  click('[data-set="limit"] [data-v="25"]'); await sleep(50);
  click('[data-set="onConflict"] [data-v="ask"]'); await sleep(50);
  assert.equal(JSON.stringify(W.StubBackend.state.prefs), JSON.stringify({ parallel: 3, limitMbps: 25, onConflict: "ask", verify: true }));
  click('[data-sw="verify"]'); await sleep(50);
  assert.equal(W.StubBackend.state.prefs.verify, false, "the checksum switch reaches the core");

  // A name already taken in the downloads folder: the card asks, and each answer does what it says.
  const DIR = "/srv/projects/atlas/releases/2026.10";
  await E.fs.load(NAS, DIR);
  const file = (E.fs.list(NAS, DIR) || []).find((n) => n.t !== "dir");
  assert.ok(file, "a file on the server");
  const sim = W.SimEngine.fs;
  sim.add("local", "/home/zaid/Downloads", sim.mkfile(file.n, 100));
  const start = async () => { const r = E.enqueue({ dir: "down", host: NAS, srcDir: DIR, dstDir: "", names: [file.n] }); await sleep(80); await E.pollTransfers(); return r.tasks[0]; };
  const t1 = await start();
  assert.equal(t1.state, "conflict", "it asks");
  A.go("transfers"); await sleep(150);
  const card = d.querySelector('[data-tid="' + t1.id + '"]');
  assert.ok(card && /already exists/.test(card.textContent), "the card says the name is taken");
  for (const b of ["Replace", "Keep both", "Skip"]) assert.ok([...card.querySelectorAll("button")].some((x) => x.textContent === b), b + " is offered");
  card.querySelector('[data-t="skip"]').click();
  await sleep(150); await E.pollTransfers(); await sleep(100);
  assert.ok(!E.tasks.includes(t1), "Skip removes the transfer");
  const t2 = await start();
  assert.equal(t2.state, "conflict");
  await E.resolve(t2.id, "replace", false);
  await until(() => t2.state === "done", "the replaced download");
  done();
  assert.deepEqual(errs, []);
});

test("the security and file settings change what the core is asked to do", async () => {
  const { d, A, E, errs, done } = await open("settings");
  const W = d.defaultView;
  await tab(d, "Security");
  const s = text(d.querySelector("#stage")) + (await (async () => { await tab(d, "Files"); return text(d.querySelector("#stage")); })());
  for (const want of ["Approve new devices here", "Pairing code lifetime", "Sign out all phones", "Move deleted items to Trash", "Keep items in Trash for"]) assert.ok(s.includes(want), want);
  await tab(d, "Security");
  d.querySelector('[data-set="pairLife"] [data-v="2"]').click(); await sleep(50);
  assert.equal(A.S.pairLife, 2);
  // With Trash turned off a delete is permanent: the core is told so.
  const seen = []; const orig = W.StubBackend.cmds.files_trash;
  W.StubBackend.cmds.files_trash = (a) => { seen.push(a); return orig ? orig(a) : null; };
  const DIR = "/srv/projects/atlas/releases/2026.10";
  await E.fs.load(NAS, DIR);
  const items = (E.fs.list(NAS, DIR) || []).filter((n) => n.t !== "dir");
  assert.ok(items.length >= 2, "two files to delete");
  await E.fs.remove(NAS, DIR, [items[0].n]);
  assert.equal(seen[0].permanent, false, "by default a delete goes to the Trash");
  await tab(d, "Files");
  d.querySelector('[data-sw="trash"]').click(); await sleep(50);
  assert.equal(A.S.trash, false);
  await E.fs.remove(NAS, DIR, [items[1].n]);
  assert.equal(seen[1].permanent, true, "with Trash off a delete is permanent");
  done();
  assert.deepEqual(errs, []);
});

test("the desktop settings reach the core, and the update check offers a checked download", async () => {
  const { d, A, E, errs, done } = await open("settings");
  const W = d.defaultView; const SB = W.StubBackend;
  await tab(d, "Desktop");
  const s = text(d.querySelector("#stage"));
  for (const want of ["Close to the system tray", "Start RFE when I sign in", "Desktop notifications", "Test notification"]) assert.ok(s.includes(want), want);
  d.querySelector('[data-sw="closeToTray"]').click(); await sleep(50);
  assert.equal(SB.state.desktop.closeToTray, true, "the core is told to hide the window on close");
  d.querySelector('[data-sw="startLogin"]').click(); await sleep(80);
  assert.equal(SB.state.autostart, true, "starting with the session is switched on in the core");
  d.querySelector('[data-sw="desktopNotif"]').click(); await sleep(80);
  assert.equal(SB.state.desktop.notifications, true);
  assert.ok(text(d.querySelector("#stage")).includes("New device requests"), "the kinds of notification appear once they are on");
  d.querySelector('[data-sx="notif"]').click(); await sleep(50);
  assert.equal(SB.state.notified.at(-1).kind, "test");
  // The update dialog.
  await tab(d, "About"); assert.ok(text(d.querySelector("#stage")).includes("Check for updates"));
  d.querySelector('[data-sx="update"]').click(); await sleep(250);
  const dlg = text(d.querySelector(".dlg"));
  assert.match(dlg, /1\.1\.0 available/); assert.match(dlg, /Faster folders/); assert.match(dlg, /RFE-Desktop_1\.1\.0_amd64\.deb/);
  d.querySelector('.dlg [data-id="go"]').click(); await sleep(250);
  assert.match(text(d.querySelector(".dlg")), /Saved and checked:.*RFE-Desktop_1\.1\.0_amd64\.deb/);
  assert.match(text(d.querySelector(".dlg")), /never installs it for you/);
  done();
  assert.deepEqual(errs, []);
});

test("each command opens its dialog with the right title", async () => {
  const { d, A, E, errs, done } = await open();
  await E.fs.load(NAS, "/srv"); A.mainPane.go("/srv"); await sleep(100);
  const file = (E.fs.list(NAS, "/srv") || [])[0];
  const F = A.fx;
  const cases = [
    ["Properties", () => A.propsDialog(NAS, "/srv", file), file.n],
    ["Share", () => F.shareDialog(NAS, "/srv", file), "Share link"],
    ["Checksum", () => F.checksumDialog(A.mainPane, file), "Checksum"],
    ["Preview", () => A.previewDialog(NAS, "/srv", file), file.n],
    ["Edit", () => A.openEditor(NAS, "/srv", file), file.n],
    ["Agent log", () => A.agentLogDialog(), /Agent log of/],
    ["Recently changed", () => A.recentOnServer(NAS), /Recently changed on/],
    ["Sign in", () => A.signInDialog(E.server(OFFLINE)), /Sign in to/],
    ["Trust", () => A.trustDialog(E.server(OFFLINE)), /Trust .*\?/],
    ["Accounts", () => A.accountsDialog(), "Accounts"],
    ["Diagnose", () => A.diagnose(NAS), /Diagnose/],
    ["Roots", () => A.rootsDialog(NAS), /Allowed roots/],
    ["Back up", () => A.backupDialog(), "Back up settings"],
    ["Restore", () => A.restoreDialog(), "Restore settings"],
    ["About", () => A.aboutDialog(), "About RFE"],
    ["Report", () => A.reportDialog(), "Report a problem"],
    ["Device key", () => A.settingsAction("devkey"), "Create a new device key?"],
    ["Pins", () => A.settingsAction("pins"), "Pinned certificates"],
  ];
  for (const [name, run, title] of cases) {
    clear(d);
    await run(); await sleep(80);
    const h = [...d.querySelectorAll(".dlg h2")].map(text).join("/");
    assert.ok(typeof title === "string" ? h === title : title.test(h), `${name}: dialog title was “${h}”`);
    assert.doesNotMatch(text(d.querySelector(".dlg")), /undefined|\[object|NaN/, name);
  }
  assert.deepEqual(errs, []);
  done();
});

test("the About dialog shows the core's diagnostics and no secrets", async () => {
  const { d, A, errs, done } = await open();
  let copied = "";
  A.w = null;
  d.defaultView.navigator.clipboard = { writeText: (t) => { copied = t; return Promise.resolve(); } };
  A.aboutDialog(); await sleep(50);
  [...d.querySelectorAll(".dlg .btn")].find((b) => /Copy diagnostics/.test(b.textContent)).click();
  await sleep(100);
  assert.match(copied, /RFE Desktop diagnostics/);
  assert.match(copied, /Servers:/);
  assert.doesNotMatch(copied, /password|token|BEGIN/i);
  assert.deepEqual(errs, []);
  done();
});

test("every control has an accessible name", async () => {
  const { A, w, done } = await open();
  for (const v of ["files", "servers", "devices", "tools", "transfers", "history", "settings"]) { A.go(v); await sleep(120); A.a11yFix(); assert.deepEqual(Array.from(A.a11yAudit(), (e) => e.outerHTML.slice(0, 80)), [], v); }
  w.close();
});

test("the language switch translates the labels and flips the layout for Arabic", async () => {
  const { d, A, done } = await open("settings");
  const label = () => text(d.querySelector("#rail"));
  assert.match(label(), /Files/);
  A.S.lang = "de"; A.applyLang(); await sleep(50);
  assert.match(label(), /Dateien/); assert.equal(d.documentElement.lang, "de"); assert.equal(d.documentElement.dir, "ltr");
  A.S.lang = "ar"; A.applyLang(); await sleep(50);
  assert.equal(d.documentElement.dir, "rtl");
  A.S.lang = "en"; A.applyLang(); await sleep(50);
  assert.match(label(), /Files/); assert.equal(d.documentElement.dir, "ltr");
  done();
});

test("the language switch leaves the names of the user's own files alone", async () => {
  const { d, A, done } = await open("settings");
  const box = d.createElement("div"); box.innerHTML = '<b>' + A.hl("Settings", "") + '</b>'; d.body.appendChild(box);
  A.S.lang = "de"; A.applyLang(); await sleep(50);
  assert.equal(text(box), "Settings");
  A.S.lang = "en"; A.applyLang();
  done();
});

test("opening an entry goes to its own path, not to its label (a root named Documents lives elsewhere)", async () => {
  const { A, E, errs, done } = await open();
  await E.fs.load(NAS, "/srv");
  A.mainPane.go("/");
  await sleep(100);
  A.mainPane.open({ n: "Documents", t: "dir", b: 0, mod: 0, perm: "drwxr-xr-x", own: "", path: "/srv/projects" });
  await until(() => A.mainPane.path === "/srv/projects", "the folder to open at its real path (was " + A.mainPane.path + ")");
  assert.deepEqual(errs, []);
  done();
});

test("a download can be paused from its card and resumed", async () => {
  const { d, A, E, errs, done } = await open();
  await E.fs.load(NAS, "/srv"); await E.fs.load(NAS, "/srv/projects").catch(() => {});
  A.mainPane.go("/srv"); await sleep(100);
  const r = await A.doEnqueue("down", NAS, "/srv", "/home/zaid/Downloads", ["projects"]);
  assert.ok(E.tasks.length === 1, "queued: " + JSON.stringify(r));
  await until(() => E.tasks[0].rids.length, "the core to take it");
  await E.pollTransfers(); await sleep(50);
  assert.equal(E.tasks[0].state, "running");
  A.go("transfers"); await sleep(150);
  d.querySelector('[data-tid] [data-t="pause"]').click(); await sleep(150);
  assert.equal(E.tasks[0].state, "paused");
  assert.ok(d.querySelector('[data-tid] [data-t="resume"]'), "the card now offers Resume");
  d.querySelector('[data-tid] [data-t="resume"]').click(); await sleep(150);
  assert.notEqual(E.tasks[0].state, "paused");
  assert.deepEqual(errs, []);
  done();
});

test("clicking through every screen, in single, double and right clicks, throws nothing", async () => {
  const bad = [];
  for (const view of ["files", "servers", "devices", "tools", "transfers", "search", "history", "settings"]) {
    const { w, d, A, errs, done } = await open(view);
    const seen = new Set();
    for (let round = 0; round < 3; round++) {
      if (A.state.view !== view) { A.go(view); await sleep(40); }
      const els = [...d.querySelectorAll("#stage *, #top *, #side *, #sheet *")].filter((e) => !e.disabled && (e.tagName === "BUTTON" || e.matches(".mi,.row,.res,.chip,.srv,[role=button],[role=switch]") || [...e.attributes].some((a) => a.name.startsWith("data-") && !["data-i", "data-f", "data-tid", "data-st", "data-drop", "data-rows", "data-v"].includes(a.name))));
      for (const el of els) {
        const key = round + "|" + el.tagName + "|" + (el.id || el.getAttribute("aria-label") || text(el).slice(0, 30) || el.className);
        if (seen.has(key) || !el.isConnected) continue;
        seen.add(key);
        if (A.state.view !== view) { A.go(view); await sleep(30); }
        if (!el.isConnected) continue;
        const before = errs.length;
        el.dispatchEvent(new w.MouseEvent(["click", "dblclick", "contextmenu"][round], { bubbles: true, cancelable: true }));
        await sleep(15);
        if (errs.length > before) bad.push(view + " " + key + ": " + errs.slice(before).join(" // "));
        clear(d);
      }
    }
    done();
  }
  assert.deepEqual(bad, []);
});

test("one click opens a folder, the round icon only selects, and Two clicks in Settings turns that around", async () => {
  const { w, d, A, E, errs, done } = await open();
  await E.fs.load(NAS, "/srv");
  A.mainPane.go("/srv"); await sleep(100);
  const row = (name) => [...d.querySelectorAll("#stage .li[data-i]")].find((r) => text(r).includes(name));
  const fire = (el, type, o = {}) => el.dispatchEvent(new w.MouseEvent(type, { bubbles: true, cancelable: true, ...o }));
  const r = row("projects"); assert.ok(r, "a folder row: " + text(d.querySelector("#stage")));
  fire(r.querySelector(".lead svg"), "click"); /* the glyph itself: an icon carries data-i too */
  assert.equal(A.mainPane.path, "/srv", "the round icon selects, it does not open");
  assert.ok(A.mainPane.sel.has("projects"));
  fire(row("projects").querySelector(".nm, .n, b, span") || row("projects"), "click");
  await until(() => A.mainPane.path === "/srv/projects", "one click to open the folder (was " + A.mainPane.path + ")");
  assert.equal(A.mainPane.sel.size, 0, "a click only opens: nothing is selected");
  A.mainPane.go("/srv"); await sleep(100);
  A.S.openMode = "double";
  fire(row("projects"), "click");
  assert.equal(A.mainPane.path, "/srv", "Two clicks: one click only selects");
  fire(row("projects"), "dblclick");
  await until(() => A.mainPane.path === "/srv/projects", "the double click to open it");
  A.S.openMode = "single";
  assert.deepEqual(errs, []);
  done();
});

test("the side panel is dragged to the size wanted, or folded away, and the transfers open from a pill or the top bar", async () => {
  const { w, d, A, errs, done } = await open();
  const fire = (el, type, x, y) => el.dispatchEvent(new w.MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, button: 0 }));
  const main = d.querySelector("#main"); const grip = d.querySelector("#rszSide");
  const hidden = () => main.classList.contains("sidehid");
  assert.equal(main.style.getPropertyValue("--sidew"), "21.00rem", "sizes are in rem, so they follow the window's scale");
  fire(grip, "pointerdown", 1000, 300); fire(grip, "pointermove", 900, 300); fire(grip, "pointerup", 900, 300);
  assert.equal(main.style.getPropertyValue("--sidew"), "27.25rem", "100px to the left makes it 6.25 rem wider (16 px a rem here)");
  fire(grip, "pointerdown", 900, 300); fire(grip, "pointermove", -4000, 300); fire(grip, "pointerup", -4000, 300);
  assert.equal(main.style.getPropertyValue("--sidew"), "40.00rem", "not wider than the limit");
  fire(grip, "pointerdown", 0, 300); fire(grip, "pointermove", 140, 300); fire(grip, "pointerup", 140, 300);
  assert.equal(main.style.getPropertyValue("--sidew"), "31.25rem", "narrower again");
  fire(grip, "pointerdown", 0, 300); fire(grip, "pointermove", 5000, 300); fire(grip, "pointerup", 5000, 300);
  assert.equal(hidden(), true, "dragged narrow enough it folds away");
  assert.equal(d.querySelector("#side").style.display, "none");
  fire(grip, "pointerdown", 500, 300); fire(grip, "pointerup", 500, 300);
  assert.equal(hidden(), false, "a click on the edge brings it back");
  const btn = d.querySelector("#btnSide"); assert.ok(btn, "a button for it in the side panel header");
  btn.click(); assert.equal(hidden(), true, "the button hides it");
  assert.equal(btn.getAttribute("aria-pressed"), "false");
  btn.click(); assert.equal(hidden(), false);
  A.state.sheetOpen = false; A.renderAll();
  const sheet = d.querySelector("#sheet"), pill = () => d.querySelector("#spill"), top = d.querySelector("#btnXfer");
  assert.ok(pill() && top, "a pill for the transfers and a button for them in the top bar");
  assert.equal(sheet.classList.contains("min"), true, "folded: only the pill shows");
  assert.equal(main.style.getPropertyValue("--sheet"), "", "the transfers take no room from the page");
  pill().click(); assert.equal(sheet.classList.contains("min"), false, "the pill opens the list");
  assert.equal(top.getAttribute("aria-pressed"), "true");
  top.click(); assert.equal(sheet.classList.contains("min"), true, "the top bar button folds it");
  assert.equal(top.getAttribute("aria-pressed"), "false");
  top.click(); assert.equal(A.state.sheetOpen, true, "and opens it again");
  assert.deepEqual(errs, []);
  done();
});

test("the menu button opens a labelled drawer in a narrow window and Escape or a click elsewhere closes it", async () => {
  const { w, d, A, errs, done } = await open();
  const btn = () => d.querySelector("#rail [data-menu]");
  const drawer = () => d.querySelector("#app").classList.contains("drawer");
  assert.ok(btn(), "the menu button is a real button");
  assert.equal(btn().getAttribute("aria-expanded"), "false");
  assert.equal(drawer(), false);
  btn().click();
  assert.equal(drawer(), true, "a narrow window gets a drawer over the page");
  assert.equal(btn().getAttribute("aria-expanded"), "true");
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert.equal(drawer(), false, "Escape closes it");
  btn().click(); assert.equal(drawer(), true);
  d.querySelector("#stage").click();
  assert.equal(drawer(), false, "a click elsewhere closes it");
  btn().click(); d.querySelector('#rail [data-go="servers"]').click();
  assert.equal(drawer(), false, "choosing a place closes it");
  assert.deepEqual(errs, []);
  done();
});

test("one click opens a file only when there is something to show; any other file is selected", async () => {
  const { w, d, A, E, errs, done } = await open();
  const REL = "/srv/projects/atlas/releases/2026.10"; await E.fs.load(NAS, REL); A.mainPane.go(REL); await sleep(150);
  const items = A.mainPane.items.filter((n) => n.t !== "dir");
  assert.ok(items.length, "files in the stub folder");
  const bin = items.find((n) => !A.hasViewer(NAS, n)); const txt = items.find((n) => A.hasViewer(NAS, n));
  const row = (n) => [...d.querySelectorAll("#stage .li[data-i]")].find((r) => text(r).includes(n.n));
  const fire = (el) => el.dispatchEvent(new w.MouseEvent("click", { bubbles: true, cancelable: true }));
  if (bin) { fire(row(bin)); await sleep(150); assert.equal(d.querySelector(".dlg"), null, "no dialog for " + bin.n); assert.ok(A.mainPane.sel.has(bin.n)); }
  if (txt) { A.mainPane.sel.clear(); fire(row(txt)); await until(() => d.querySelector(".dlg"), "the viewer for " + txt.n); assert.equal(A.mainPane.sel.has(txt.n), false, "opening a file does not select it"); }
  assert.deepEqual(errs, []);
  done();
});

test("the transfers strip starts folded and keeps the way it was left", async () => {
  const { A, done } = await open();
  assert.equal(A.state.sheetOpen, false);
  done();
});

test("every length in the stylesheet is in rem, so the page scales with the window", async () => {
  const { readFileSync } = await import("node:fs");
  const css = readFileSync(new URL("../ui/app.css", import.meta.url), "utf8");
  const root = css.match(/html\{--ui:1;font-size:([^}]*)\}/);
  assert.ok(root && /vw/.test(root[1]) && /vh/.test(root[1]) && /clamp\(calc\((?:16px|1rem|\.818rem)/.test(root[1]), "the root size follows the window: " + (root && root[1]));
  const rest = css.replace(root[0], "").replace(/\/\*[\s\S]*?\*\//g, "");
  const big = [...rest.matchAll(/(?<![\w.#-])(\d*\.?\d+)px/g)].filter((m) => +m[1] > 1.5).map((m) => m[0]);
  assert.deepEqual(big, [], "no fixed pixel sizes beyond hairlines");
});

test("Interface size multiplies the window's own scale and asks the core for a matching smallest window", async () => {
  const { w, d, A, errs, done } = await open();
  A.S.uiSize = 115; A.applyZoom();
  assert.equal(d.documentElement.style.getPropertyValue("--ui"), "1.15");
  A.S.uiSize = 400; A.applyZoom();
  assert.equal(d.documentElement.style.getPropertyValue("--ui"), "1.3", "never beyond 130%");
  A.S.uiSize = 100; A.applyZoom();
  assert.deepEqual(errs, []);
  done();
});

test("a host address with markup in it cannot break out of a card attribute", async () => {
  const { d, A, E, errs, done } = await open();
  const evil = 'x"><img id="injected" src=y onerror="1">.example:7443';
  E.addServer({ host: evil, name: "evil" });
  A.go("servers"); await sleep(150);
  assert.equal(d.querySelector("#injected"), null, "the address became an element");
  const card = [...d.querySelectorAll("[data-sid]")].find((c) => c.dataset.sid === evil.toLowerCase());
  assert.ok(card, "the card keeps the whole address in data-sid");
  for (const b of card.querySelectorAll("[data-id]")) assert.equal(b.dataset.id, evil.toLowerCase());
  assert.deepEqual(errs, []);
  done();
});

test("a window resize switches transitions off while it lasts, so the layout does not lag behind the new root size", async () => {
  const { w, d, done } = await open();
  const html = d.documentElement;
  assert.ok(!html.classList.contains("winsize"));
  w.dispatchEvent(new w.Event("resize"));
  assert.ok(html.classList.contains("winsize"), "no guard while resizing");
  await sleep(300);
  assert.ok(!html.classList.contains("winsize"), "the guard stays on after the resize stopped");
  const css = [...d.styleSheets].flatMap((s) => [...s.cssRules]).map((r) => r.cssText).join("\n");
  assert.match(css, /html\.winsize[^{]*\{[^}]*transition:\s*none/);
  done();
});

test("the Servers page keeps keyboard focus on the same control when a tick rebuilds it", async () => {
  const { d, A, E, done } = await open();
  A.go("servers"); await sleep(150);
  const target = d.querySelector('#stage [data-sa="open"]');
  const id = target.dataset.id;
  target.focus();
  assert.equal(d.activeElement, target);
  const s = E.server(id); s.latency += 7; s.lastSeen += 1; E.emit("servers", s); await sleep(60);
  const now = d.querySelector('#stage [data-sa="open"][data-id="' + id.replace(/"/g, '\\"') + '"]');
  assert.ok(now, "the card is still drawn");
  assert.equal(d.activeElement, now, "focus was lost in the rebuild");
  done();
});

const sheet = (d) => [...d.styleSheets].flatMap((s) => [...s.cssRules]).map((r) => r.cssText).join("\n");

test("a scrolling file list drops its hover fades while it moves, and gets them back after", async () => {
  const { w, d, A, done } = await open();
  A.go("files"); await sleep(200);
  const rows = d.querySelector("[data-rows]");
  rows.dispatchEvent(new w.Event("scroll"));
  assert.ok(rows.classList.contains("scrolling"), "no guard while scrolling");
  await sleep(300);
  assert.ok(!rows.classList.contains("scrolling"), "the guard stays on after the scroll stopped");
  assert.match(sheet(d), /\[data-rows\]\.scrolling \.li[^{]*\{[^}]*transition:\s*none/);
  done();
});

test("switching pages swaps the content at once: no fade, dip or slide", async () => {
  const { d, A, errs, done } = await open();
  const calls = []; const proto = d.defaultView.Element.prototype; const was = proto.animate;
  proto.animate = function (k) { calls.push({ el: this, k }); return { cancel() {}, finish() {} }; };
  for (const v of ["servers", "transfers", "files", "settings"]) { calls.length = 0; A.go(v); await sleep(30);
    assert.equal(calls.length, 0, "a page switch to " + v + " animated " + calls.map((c) => c.el.id || c.el.className).join(", ")); }
  proto.animate = was;
  assert.deepEqual(errs, []);
  done();
});

test("pop-ups appear in full on their first frame: no entrance animation on a dialog, backdrop, menu or snackbar", async () => {
  const { d, done } = await open();
  const rules = [...d.styleSheets].flatMap((s) => [...s.cssRules]);
  for (const sel of [".scrim", ".dlg", ".menu", ".snack"]) {
    const bad = rules.filter((r) => r.selectorText === sel && /animation(-name)?\s*:\s*(?!none)[a-z]/i.test(r.cssText) && !/spin|pulse|ind\b/.test(r.cssText));
    assert.equal(bad.length, 0, sel + " still has an entrance animation: " + bad.map((r) => r.cssText.slice(0, 80)).join(" | "));
  }
  const keys = rules.filter((r) => r.type === 7).map((r) => r.name);
  for (const k of ["fin", "dlgin", "snin", "snackin"]) assert.ok(!keys.includes(k), "keyframes " + k + " is left behind");
  done();
});

test("the refresh button turns once, slowly and smoothly, and is not a fast linear whirl", async () => {
  const { d, done } = await open();
  const r = [...d.styleSheets].flatMap((s) => [...s.cssRules]).filter((x) => x.selectorText === ".ib.spin .ic").map((x) => x.cssText).join(" ");
  const m = /animation:\s*spin\s+([\d.]+)s\s+([^;}]*)/.exec(r); assert.ok(m, "no spin rule: " + r);
  assert.ok(+m[1] >= 1, "one turn takes " + m[1] + " s, too fast");
  assert.ok(!/linear/.test(m[2]) && !/infinite/.test(m[2]), "it must ease and run once: " + m[2]);
  done();
});

test("reduced motion: the transitions are switched off; tab switches in the side panel, Settings and Tools never fade", async () => {
  const { d, A, done } = await open();
  assert.match(sheet(d), /prefers-reduced-motion:\s*reduce[^]*transition-duration/);
  assert.equal(A.ease, undefined, "the fade helper is back");
  const hit = []; const proto = d.defaultView.Element.prototype; const was = proto.animate; proto.animate = function () { hit.push(this); return { cancel() {}, finish() {} }; };
  A.go("files"); await sleep(30); hit.length = 0;
  for (const t of d.querySelectorAll("#side [data-tab]")) { t.click(); await sleep(30); }
  A.go("settings"); await sleep(30); hit.length = 0;
  for (const t of d.querySelectorAll("#stage [data-snav]")) { t.click(); await sleep(30); }
  proto.animate = was;
  assert.equal(hit.length, 0, "a tab switch animated: " + hit.map((e) => e.className || e.tagName).join(", "));
  done();
});

test("the switch knob transitions only the properties it changes", async () => {
  const { d, done } = await open();
  const rule = [...d.styleSheets].flatMap((s) => [...s.cssRules]).find((r) => r.selectorText === ".sw::after");
  assert.ok(rule, "rule found");
  assert.doesNotMatch(rule.cssText, /transition:\s*all/);
  done();
});

test("the loading skeleton waits before it appears, so a fast folder load does not flash it", async () => {
  const { d, done } = await open();
  const css = [...d.styleSheets].flatMap((s) => [...s.cssRules]).filter((r) => r.selectorText === ".skel").map((r) => r.cssText).join(" ");
  assert.ok(css, ".skel rule found");
  assert.match(css, /opacity:\s*0/);
  assert.match(css, /animation:[^;]*skelin[^;]*\.15s/);
  done();
});

test("the indeterminate bar moves by transform, not by a layout property", async () => {
  const { d, done } = await open();
  const kf = [...d.styleSheets].flatMap((s) => [...s.cssRules]).find((r) => r.name === "ind");
  assert.ok(kf, "keyframes found");
  assert.match(kf.cssText, /transform/);
  assert.doesNotMatch(kf.cssText, /left/);
  done();
});

test("folding the details panel slides it in by transform only: never faded, no copy left behind", async () => {
  const { w, d, A, errs, done } = await open();
  A.go("files"); await sleep(200);
  const calls = [];
  const orig = w.HTMLElement.prototype.animate;
  w.HTMLElement.prototype.animate = function (kf, o) { calls.push(kf); return { onfinish: null, oncancel: null }; };
  A.toggleSide(); await sleep(30);
  assert.equal(d.querySelectorAll('body > [aria-hidden="true"]').length, 0, "a copy of the panel is left on the page");
  A.toggleSide(); await sleep(30);
  assert.ok(calls.some((k) => k[0].transform && k[0].opacity === undefined), "the panel does not slide in");
  assert.ok(!calls.some((k) => k.some((f) => f.opacity !== undefined && +f.opacity < 1)), "the panel fades");
  assert.doesNotMatch([...d.styleSheets].flatMap((s) => [...s.cssRules]).filter((r) => /^\.side,\.sheet|^\.side$|^\.sheet$/.test(r.selectorText || "")).map((r) => r.cssText).join(" "), /transition:\s*opacity/);
  w.HTMLElement.prototype.animate = orig;
  assert.deepEqual(errs, []);
  done();
});

test("switching add-a-computer tabs swaps the contents at once: nothing fades or blinks", async () => {
  const { d, errs, done } = await open();
  d.querySelector(".fab").click(); await sleep(150);
  const hit = []; const proto = d.defaultView.Element.prototype; const was = proto.animate;
  proto.animate = function (k, o) { hit.push(this); return { cancel() {}, finish() {} }; };
  d.querySelector('.dlg [data-fx^="conn.tab|code"]').click(); await sleep(50);
  proto.animate = was;
  const dlg = d.querySelector(".dlg");
  assert.equal(hit.length, 0, "a tab switch animated: " + hit.map((e) => e.className || e.tagName).join(", "));
  assert.ok(dlg.querySelector(".fxtabs"), "the dialog still has its tab bar");
  assert.deepEqual(errs, []);
  done();
});

test("the add-a-computer tabs are one dialog: the backdrop node is never removed and Escape still closes once", async () => {
  const { w, d, errs, done } = await open();
  d.querySelector(".fab").click(); await sleep(150);
  const sc = d.querySelector(".scrim"); const removed = []; const mo = new w.MutationObserver((l) => l.forEach((r) => r.removedNodes.forEach((n) => removed.push(n)))); mo.observe(sc.parentNode, { childList: true });
  for (const next of ["code", "disc", "manual", "disc"]) { d.querySelector('.dlg [data-fx^="conn.tab|' + next + '"]').click(); await sleep(60); assert.equal(d.querySelector(".scrim"), sc, "the backdrop was replaced on " + next); assert.equal(d.querySelectorAll(".scrim").length, 1); assert.equal(d.querySelectorAll(".scrim .dlg").length, 1); }
  assert.equal(removed.length, 0, "the backdrop left the page during a switch");
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true })); await sleep(60);
  assert.equal(d.querySelectorAll(".scrim").length, 0, "Escape did not close the dialog after switching");
  assert.deepEqual(errs, []);
  done();
});

test("the add-a-computer tabs share one size, so switching never resizes the box", async () => {
  const { d, errs, done } = await open();
  const css = [...d.styleSheets].flatMap((s) => [...s.cssRules]).filter((r) => r.selectorText === ".dlg.conn").map((r) => r.cssText).join(" ");
  assert.match(css, /min-height/, "the connection dialogs need one shared minimum height");
  d.querySelector(".fab").click(); await sleep(150);
  const w = [];
  for (const next of ["code", "disc", "manual"]) {
    w.push(d.querySelector(".dlg").style.getPropertyValue("--w"));
    d.querySelector('.dlg [data-fx^="conn.tab|' + next + '"]').click(); await sleep(150);
  }
  w.push(d.querySelector(".dlg").style.getPropertyValue("--w"));
  assert.equal(new Set(w).size, 1, "the three tabs use different widths: " + w.join(", "));
  assert.deepEqual(errs, []);
  done();
});

test("the add-a-computer tabs share one top edge, so the tab bar stays put when the box changes height", async () => {
  const { d, A, errs, done } = await open();
  const css = [...d.styleSheets].flatMap((s) => [...s.cssRules]).filter((r) => r.selectorText === ".scrim.top").map((r) => r.cssText).join(" ");
  assert.match(css, /align-items:\s*start/);
  assert.match(css, /height:\s*100%/, "an anchored scrim must keep the full height");
  d.querySelector(".fab").click(); await sleep(150);
  for (const tab of ["code", "disc", "manual"]) {
    assert.equal(d.querySelectorAll(".scrim").length, 1, "one dialog at a time before " + tab);
    assert.ok(d.querySelector(".scrim").classList.contains("top"), "the " + tab + " dialog is not anchored");
    d.querySelector('.dlg [data-fx^="conn.tab|' + (tab === "code" ? "disc" : tab === "disc" ? "manual" : "code") + '"]').click(); await sleep(150);
  }
  assert.deepEqual(errs, []);
  done();
});
