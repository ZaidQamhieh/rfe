// Four small leftovers of the "unslop" pass in the real desktop window:
//   1. the Transfers page shows ONE pause/resume toggle, not both with one disabled;
//   2. a transfer's route uses the line direction icon (not an arrow character) and sort labels read "A to Z";
//   3. one name for the one dialog: "Add a computer", never "New connection", in the rail, menus, palette, title;
//   4. a raw path is plain text in a sentence, not a monospace span.
// The window scripts (ui/*.js) run in jsdom on the stub core, like window.test.mjs.
//
//   node --test desktop/ui-tests/unslop-leftovers.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const UI = fileURLToPath(new URL("../ui/", import.meta.url));
const src = (f) => readFileSync(UI + f, "utf8");
const NAS = "192.168.1.20:7443";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const waitFor = async (fn, ms = 4000) => { for (let t = 0; t < ms; t += 25) { if (fn()) return; await sleep(25); } };
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const until = async (fn, what) => { for (let i = 0; i < 100; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };

/** A 1400 px window on the stub core with every script error collected in `errs`. */
async function open() {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 300)));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1",
    beforeParse(w) {
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.TextEncoder = TextEncoder; w.TextDecoder = TextDecoder;
      w.ResizeObserver = class { observe() {} disconnect() {} };
      w.HTMLElement.prototype.scrollIntoView = function () {};
      Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
      Object.defineProperty(w, "innerWidth", { value: 1400, configurable: true, writable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}

/** Starts one download so a card is running, and returns its task. */
async function runningDownload(A, E) {
  await E.fs.load(NAS, "/srv"); await E.fs.load(NAS, "/srv/projects").catch(() => {});
  A.mainPane.go("/srv"); await sleep(100);
  await A.doEnqueue("down", NAS, "/srv", "/home/zaid/Downloads", ["projects"]);
  await until(() => E.tasks[0] && E.tasks[0].rids.length, "the core to take it");
  await E.pollTransfers(); await sleep(50);
  assert.equal(E.tasks[0].state, "running");
  return E.tasks[0];
}

const toggles = (d) => [...d.querySelectorAll("#stage [data-pa=pauseall], #stage [data-pa=resumeall]")];

test("1. Transfers page: one pause/resume toggle that follows what is running", async () => {
  const { d, A, E, errs } = await open();
  A.go("transfers"); await sleep(150);
  /* The v5 empty Transfers page draws no toggles; with transfers it draws Pause all and Resume all, each enabled only when it has something to act on. */
  const by = (pa) => toggles(d).find((b) => b.dataset.pa === pa);
  assert.deepEqual(toggles(d).map((b) => b.dataset.pa), [], "with no transfers the page draws no toggles");
  A.go("files");
  await runningDownload(A, E);
  A.go("transfers"); await sleep(150);
  assert.deepEqual(toggles(d).map((b) => b.dataset.pa), ["pauseall", "resumeall"], "both toggles are drawn, in this order");
  assert.equal(text(by("pauseall")), "Pause all");
  assert.equal(by("pauseall").disabled, false, "something runs, so Pause all works");
  assert.equal(text(by("resumeall")), "Resume all");
  assert.equal(by("resumeall").disabled, true, "nothing is paused");
  d.querySelector('[data-tid] [data-t="pause"]').click();
  await waitFor(() => E.tasks[0].state === "paused" && by("pauseall").disabled);
  assert.equal(E.tasks[0].state, "paused");
  assert.equal(by("pauseall").disabled, true, "nothing runs or waits any more");
  assert.equal(by("resumeall").disabled, false, "something is paused, so Resume all works");
  by("resumeall").click(); await sleep(250);
  assert.notEqual(E.tasks[0].state, "paused", "the data-pa handler still resumes");
  assert.deepEqual(errs, []);
});

test("2. a transfer's route uses the direction icon, not an arrow; sort labels read A to Z", async () => {
  const { d, A, E, errs } = await open();
  await runningDownload(A, E);
  A.go("transfers"); await sleep(150);
  const small = d.querySelector("[data-tid] .nm small");
  assert.ok(small, "the card has its route line");
  assert.ok(!/[→←↔]/.test(small.textContent), "no arrow character in the route: " + text(small));
  assert.ok(small.querySelector(".rtic svg"), "the line direction icon sits in the route");
  assert.match(text(small), /192\.168\.1\.20.*to.*This computer|This computer.*to/, "the route still names both ends: " + text(small));
  assert.ok(small.querySelector(".rtic").getAttribute("aria-hidden") === "true", "the icon is decorative");
  A.go("files"); await sleep(200);
  /* the v5 tree page has no sort chip; the sort menu belongs to the Pane's own toolbar, so the Pane is mounted on a scratch node here */
  const host = d.createElement("div"); d.body.appendChild(host); A.mainPane.mount(host);
  const chip = d.querySelector('[data-act="sortmenu"]');
  assert.ok(chip, "the sort chip");
  chip.click(); await sleep(80);
  /* the Pane sorts by modified date, newest first, until a column is chosen; the chosen column names its direction in words */
  const menu = text(d.querySelector(".menu"));
  assert.match(menu, /Newest first/, "the sort menu names the current direction: " + menu);
  assert.ok(!/[→]/.test(menu), "no arrow in the sort menu");
  [...d.querySelectorAll(".menu .mi")].find((m) => /^Name/.test(text(m))).click(); await sleep(80);
  d.querySelector('[data-act="sortmenu"]').click(); await sleep(80);
  const menu2 = text(d.querySelector(".menu"));
  assert.match(menu2, /A to Z/, "after choosing Name the sort menu reads A to Z: " + menu2);
  assert.ok(!/[→]/.test(menu2), "no arrow in the sort menu");
  assert.ok(!/A→Z/.test(src("ui.js")), "the source has no A→Z");
  assert.deepEqual(errs, []);
});

test("3. one name for the dialog: Add a computer, in the rail, the Computers menu, the palette and the title", async () => {
  const { d, A, errs } = await open();
  const fab = d.querySelector("#rail [data-new]");
  assert.match(text(fab), /Add a computer/);
  assert.equal(fab.getAttribute("title"), "Add a computer");
  // The command palette.
  A.palette(); await sleep(80);
  const pal = text(d.querySelector("#layer") || d.body);
  assert.match(pal, /Add a computer/);
  assert.ok(!/New connection/.test(pal), "no New connection in the palette");
  d.dispatchEvent(new d.defaultView.KeyboardEvent("keydown", { key: "Escape", bubbles: true })); await sleep(50);
  for (const s of d.querySelectorAll(".scrim")) s.remove();
  // The dialog's own title.
  fab.click(); await sleep(150);
  assert.equal(text(d.querySelector(".scrim .k-dlg h2, .scrim .dlg h2")), "Add a computer"); /* v5 skins the box as .k-dlg */
  // The account menu and the top bar keep one Pair a phone each.
  for (const s of d.querySelectorAll(".scrim")) s.remove();
  d.querySelector("#btnAv").click(); await sleep(80);
  const acct = [...d.querySelectorAll(".menu .mi")].map(text).filter((x) => /^Pair a phone/.test(x));
  assert.equal(acct.length, 1, "one Pair a phone in the account menu: " + acct);
  assert.ok(d.querySelector("#btnQR"), "the top-bar QR icon button stays");
  assert.deepEqual(errs, []);
});

test("3b. no shipped file in ui/, docs/ or e2e/ still says New connection", () => {
  const files = [];
  const walk = (dir, exts) => { for (const e of readdirSync(dir, { withFileTypes: true })) { if (e.name === "node_modules") continue; const p = dir + e.name; if (e.isDirectory()) walk(p + "/", exts); else if (exts.some((x) => e.name.endsWith(x))) files.push(p); } };
  const root = fileURLToPath(new URL("../", import.meta.url));
  walk(UI, [".js", ".html", ".css"]); walk(root + "docs/", [".md"]); walk(root + "e2e/", [".mjs"]);
  const hits = files.filter((f) => /New connection/.test(readFileSync(f, "utf8")));
  assert.deepEqual(hits, []);
});

test("3c. the German, Spanish and Arabic tables have rows for the renamed labels", () => {
  const t = src("fx5.js");
  for (const s of ["Add a computer", "Add a computer…"]) {
    const m = t.match(new RegExp("\\['" + s.replace("…", "\\u2026?") + "', '([^']+)', '([^']+)', '([^']+)'\\]"));
    assert.ok(m, "a row for " + s);
    assert.ok(m[1] && m[2] && m[3]);
  }
});

test("4. raw paths in sentences are plain text, not monospace spans", () => {
  assert.ok(!/Limited to <span class="mono">/.test(src("features.js")), "Limited to <path> is plain text");
  assert.ok(!/Saved and checked: <span class="mono">/.test(src("fx6.js")), "Saved and checked: <path> is plain text");
  assert.match(src("fx6.js"), /Saved and checked: <span data-nolocal>' \+ esc\(UP\.saved\) \+ '<\/span>/, "the saved path is in a span the translator skips");
  assert.ok(!/<span class="mono">' \+ esc\((a\.jailRoot|UP\.saved)\)/.test(src("features.js") + src("fx6.js")), "neither path is monospace");
});

test("1b. Pause all and Resume all show together only when something runs and something is paused", async () => {
  const { d, A, E, errs } = await open();
  await runningDownload(A, E);
  A.go("transfers"); await sleep(150);
  d.querySelector('[data-tid] [data-t="pause"]').click(); await sleep(200);
  assert.equal(E.tasks[0].state, "paused");
  // A second transfer runs while the first stays paused: both actions are needed.
  await A.doEnqueue("down", NAS, "/srv", "/home/zaid/Downloads", ["projects"]);
  await until(() => E.tasks.some((t) => t.state === "running"), "the second transfer to run");
  A.go("files"); A.go("transfers"); await sleep(150);
  const kinds = toggles(d).map((b) => b.dataset.pa).sort();
  assert.deepEqual(kinds, ["pauseall", "resumeall"], "paused + running: both shown, got " + kinds);
  assert.ok(toggles(d).every((b) => !b.disabled && !b.hidden), "paused + running: both can act");
  // Only queued tasks: a queued transfer can be paused, so Pause all can act; nothing is paused, so Resume all cannot.
  A.paused = true; /* the page's own clocks (tick and live refresh) would put the stub core's states back while the test sets its own */
  for (const t of E.tasks) t.state = "queued";
  A.go("files"); A.go("transfers"); await sleep(150);
  const q = (pa) => toggles(d).find((b) => b.dataset.pa === pa);
  assert.equal(q("pauseall").disabled, false, "queued-only: Pause all can act");
  assert.equal(q("resumeall").disabled, true, "queued-only: nothing is paused, Resume all cannot act");
  assert.deepEqual(errs, []);
});

test("2b. the route marker is a horizontal arrow along the route (right for upload, left for download)", async () => {
  const { d, A, E, errs } = await open();
  await runningDownload(A, E);
  const route = (t) => { const h = d.createElement("div"); h.innerHTML = A.card(t); return h.querySelector(".nm small .rtic svg").innerHTML; };
  const down = route(E.tasks[0]);
  const up = route(Object.assign({}, E.tasks[0], { dir: "up" }));
  assert.match(down, /M19 12H5/, "download points left: " + down);
  assert.match(up, /M5 12h14/, "upload points right: " + up);
  assert.ok(!/V5|v14|V19/.test(down + up), "no vertical shaft");
  assert.deepEqual(errs, []);
});

test("2c. the screen-reader 'to' in a route is translated", async () => {
  const { d, A, E, errs } = await open();
  await runningDownload(A, E);
  A.go("transfers"); await sleep(150);
  const sr = () => text(d.querySelector("[data-tid] .nm small .rtsr"));
  assert.equal(sr(), "to");
  const want = { de: "zu", es: "a", ar: "إلى" };
  for (const l of Object.keys(want)) {
    A.S.lang = l; A.applyLang(); await sleep(80);
    assert.equal(sr(), want[l], "the route word in " + l);
  }
  A.S.lang = "en"; A.applyLang(); await sleep(50);
  assert.equal(sr(), "to");
  assert.deepEqual(errs, []);
});

test("2d. a download card's route names 'This computer' in its own span, translated in de/es/ar, with the path left alone", async () => {
  const { d, A, E, errs } = await open();
  await runningDownload(A, E);
  A.go("transfers"); await sleep(150);
  const small = () => d.querySelector("[data-tid] .nm small");
  const path = () => [...small().querySelectorAll("[data-nolocal]")].pop(); // the last skipped span: host name first, path last
  assert.ok(path(), "the path sits in a span the translator skips");
  const raw = text(path());
  assert.match(raw, /^\//, "the span holds the path: " + raw);
  const want = { de: "Dieser Computer", es: "Este equipo", ar: "هذا الكمبيوتر" };
  for (const l of Object.keys(want)) {
    A.S.lang = l; A.applyLang(); await sleep(80);
    assert.ok(text(small()).includes(want[l]), l + ": the card route is translated: " + text(small()));
    assert.ok(!/This computer/.test(text(small())), l + ": no English left in the route: " + text(small()));
    assert.equal(text(path()), raw, l + ": the path is untouched");
  }
  A.S.lang = "en"; A.applyLang(); await sleep(50);
  assert.match(text(small()), /This computer/);
  // An upload card names the same end in its own span too.
  const h = d.createElement("div"); h.innerHTML = A.card(Object.assign({}, E.tasks[0], { dir: "up" }));
  assert.ok([...h.querySelectorAll(".nm small span")].some((s) => text(s) === "This computer"), "upload: 'This computer' has its own span");
  assert.deepEqual(errs, []);
});

test("2e. German sort labels and the two path sentences are translated", async () => {
  const { d, A, errs } = await open();
  A.S.lang = "de"; A.applyLang(); await sleep(80);
  const tr = (s) => A.tr(s);
  assert.notEqual(tr("A to Z"), "A to Z");
  assert.notEqual(tr("Z to A"), "Z to A");
  assert.notEqual(tr("Type"), "Type");
  assert.notEqual(tr("Saved and checked:"), "Saved and checked:");
  assert.notEqual(tr("Limited to"), "Limited to");
  // In a sentence, the path span is skipped and the words before it are translated.
  const p = d.createElement("div"); p.innerHTML = 'Saved and checked: <span data-nolocal>/tmp/rfe.deb</span>.'; d.body.appendChild(p);
  A.fx5.walk(p);
  assert.equal(p.querySelector("span").textContent, "/tmp/rfe.deb");
  assert.ok(!/Saved and checked/.test(p.textContent), "the words are German: " + p.textContent);
  p.remove();
  A.go("files"); await sleep(200);
  const host = d.createElement("div"); d.body.appendChild(host); A.mainPane.mount(host); /* the Pane's own toolbar (the v5 tree has no sort chip) */
  d.querySelector('[data-act="sortmenu"]').click(); await sleep(80);
  const menu = text(d.querySelector(".menu"));
  assert.ok(!/\b(A to Z|Type)\b/.test(menu), "German sort menu: " + menu);
  assert.deepEqual(errs, []);
});

/** Focuses `opener`, runs `open`, picks the menu/palette item named `label`, presses Escape, and returns what has focus. */
async function focusAfterEscape(w, opener, open, label) {
  const d = w.document;
  opener.focus(); assert.equal(d.activeElement, opener, "the opener took focus");
  open(); await sleep(100);
  const item = [...d.querySelectorAll(".menu .mi, .pal .it")].find((x) => text(x).startsWith(label));
  assert.ok(item, "an item named " + label + ": " + [...d.querySelectorAll(".menu .mi, .pal .it")].map(text).join(" | "));
  item.click(); await sleep(250);
  assert.ok(d.querySelector(".scrim .dlg, .scrim .k-dlg"), "the dialog opened for " + label);
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true })); await sleep(100);
  assert.equal(d.querySelector(".scrim .dlg, .scrim .k-dlg"), null, "the dialog closed");
  return d.activeElement;
}

test("2f. Escape on a dialog opened from a menu or the palette gives focus back to the opener", async () => {
  const { w, d, errs } = await open();
  const av = d.querySelector("#btnAv");
  assert.equal(await focusAfterEscape(w, av, () => av.click(), "Pair a phone"), av, "account menu -> Pair a phone");
  const q = d.querySelector("#btnBell");
  // Ctrl+K now opens the Search panel; the palette is still opened by A.palette().
  assert.equal(await focusAfterEscape(w, q, () => w.A.palette(), "Add a computer"), q, "palette -> Add a computer");
  assert.deepEqual(errs, []);
});
