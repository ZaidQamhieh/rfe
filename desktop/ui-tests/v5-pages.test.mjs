// The v5 Settings, Tools (Trash, Share links), Transfers, Overview, Search and History pages in the mockup's words and pictures:
// Settings sections with their own icons, rows with the mockup's small lines and button words (no icons in buttons), Theme in the
// order System / Light / Dark, the update check as its own row, the Help shortcut list; the Trash banner "Delete old items from
// Trash?"; Copy link on share rows; arrow keys between rows; the Transfers emoji pictures, "Show in Files", the "left unfinished"
// banner and Pause for a transfer still being set up; the Overview chart legend; the Search page words; the History icons.
// The real window scripts (ui/*.js) run in jsdom on the stub core, like v5-wording.test.mjs.
//
//   node --test desktop/ui-tests/v5-pages.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";
import { readAllCss } from "./css-lib.mjs";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (fn, what) => { for (let i = 0; i < 100; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });
const text = (el) => (el ? el.textContent.replace(/\s+/g, " ").trim() : "");

/** A 1400 px window on the stub core with every script error in `errs` and every clipboard write in `copied`. */
async function open(view = "") {
  const errs = [], copied = [];
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
      w.navigator.clipboard = { writeText: (t) => { copied.push(t); return Promise.resolve(); } };
      Object.defineProperty(w, "innerWidth", { value: 1400, configurable: true, writable: true });
      const store = new Map();
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (store.has(k) ? store.get(k) : null), setItem: (k, v) => store.set(k, String(v)), removeItem: (k) => store.delete(k) }, configurable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs, copied };
}

/** The inner drawing of a named line icon, so an icon on the page can be compared with the one it should be. */
const iconOf = (o, name, size) => { const h = o.d.createElement("div"); h.innerHTML = o.A.ic(name, { size }); return h.querySelector("svg").innerHTML; };
const emojiName = (img) => ((img && img.getAttribute("src")) || "").match(/([a-z_]+)\.svg$/)?.[1];

/** Opens a Settings section. */
const tab = async (d, name) => { d.querySelector('[data-snav="' + name + '"]').click(); await sleep(60); };
/** The Settings row with this title, looked for in every section; opens the section it is in. */
async function settingsRow(d, title) {
  for (const n of [...d.querySelectorAll("[data-snav]")].map((b) => b.dataset.snav)) {
    await tab(d, n);
    const r = [...d.querySelectorAll(".k-sbody .k-set")].find((x) => text(x.firstElementChild.querySelector("b")) === title);
    if (r) return r;
  }
  return null;
}

// ---------------------------------------------------------------- Settings

test("Settings sections each have their own line icon", async () => {
  const o = await open("settings");
  const ICON = { Appearance: "sun", "Language and accessibility": "text", Transfers: "swap", Connection: "plug", Files: "folder", Security: "shield", Desktop: "laptop", Backup: "database", Troubleshooting: "activity", Help: "keyboard", About: "info" };
  const btns = [...o.d.querySelectorAll(".k-snav button")];
  assert.ok(btns.length >= 8, "the section list is drawn");
  for (const b of btns) {
    const want = ICON[b.dataset.snav];
    assert.ok(want, "a known section: " + b.dataset.snav);
    assert.equal(b.querySelector("svg").innerHTML, iconOf(o, want, 16), b.dataset.snav + " uses the " + want + " icon");
  }
  assert.deepEqual(o.errs, []);
});

test("buttons in Settings rows are words only, with the mockup's words", async () => {
  const o = await open("settings");
  const { d } = o;
  const words = [["Computers you trust", "Show…"], ["Sign out all phones", "Sign out all phones…"], ["Test notification", "Test notification"], ["Computer log", "Show…"], ["This computer’s identity", "New key…"], ["Welcome tour", "Show the welcome screen"]];
  for (const [title, word] of words) {
    const row = await settingsRow(d, title);
    assert.ok(row, title + " is a row");
    const b = row.querySelector("button.k-btn");
    assert.ok(b, title + " has a button");
    assert.equal(text(b), word, title);
    assert.equal(b.querySelector("svg"), null, title + ": no icon in the button");
    assert.equal(b.getAttribute("aria-label"), null, title + ": the visible word is the name");
  }
  for (const n of [...d.querySelectorAll("[data-snav]")].map((b) => b.dataset.snav)) {
    await tab(d, n);
    for (const b of d.querySelectorAll(".k-sbody .k-set .k-btn")) assert.equal(b.querySelector("svg"), null, n + ": " + text(b) + " has no icon");
  }
  assert.deepEqual(o.errs, []);
});

test("Settings rows carry the mockup's small lines; a name that already exists has none", async () => {
  const o = await open("settings");
  const { d } = o;
  const sub = async (title) => { const r = await settingsRow(d, title); assert.ok(r, title); const s = r.firstElementChild.querySelector(":scope > small"); return s ? text(s) : null; };
  assert.equal(await sub("Parallel transfers"), "The others wait for a free slot");
  assert.equal(await sub("Speed limit"), "Shared by all transfers");
  assert.equal(await sub("When a name already exists"), null, "no small line on this row");
  assert.equal(await sub("Reconnect automatically"), "Try a lost computer again and resume its transfers");
  assert.equal(await sub("Move deleted items to Trash"), "Off makes a delete permanent");
  assert.equal(await sub("Keep items in Trash for"), "Asked when you open Trash");
  assert.equal(await sub("Pairing code lifetime"), "A code also works only once");
  const ask = await settingsRow(d, "Ask where to save each download");
  assert.ok(ask, "the download question is called Ask where to save each download");
  assert.deepEqual(o.errs, []);
});

test("Theme offers System, Light, Dark in that order", async () => {
  const o = await open("settings");
  await tab(o.d, "Appearance");
  const order = [...o.d.querySelectorAll('[data-set="theme"] button')].map(text);
  assert.deepEqual(order, ["System", "Light", "Dark"]);
  assert.deepEqual(o.errs, []);
});

test("About keeps Details on the version row and has its own Check for updates row with Check now", async () => {
  const o = await open("settings");
  const { d } = o;
  await tab(d, "About");
  const rows = [...d.querySelectorAll(".k-sbody .k-set")];
  const ver = rows.find((r) => /^RFE Desktop/.test(text(r.querySelector("b"))));
  const upd = rows.find((r) => text(r.querySelector("b")) === "Check for updates");
  assert.ok(ver && upd, "the version row and the update row");
  assert.equal(ver.querySelector('[data-sx="update"]'), null, "the update button left the version row");
  assert.ok(ver.querySelector('[data-sx="about"]'), "Details stays on the version row");
  assert.equal(rows.indexOf(upd), rows.indexOf(ver) + 1, "the update row follows the version row");
  const b = upd.querySelector('[data-sx="update"]');
  assert.equal(text(b), "Check now");
  assert.equal(b.querySelector("svg"), null);
  assert.deepEqual(o.errs, []);
});

test("About has the mockup's rows in its order: update channel in the update row, Check automatically, Report a problem, Welcome tour last", async () => {
  const o = await open("settings");
  const { d, A } = o;
  await tab(d, "About");
  const titles = () => [...d.querySelectorAll(".k-sbody .k-set")].map((r) => text(r.querySelector("b")).replace(/^RFE Desktop.*/, "RFE Desktop"));
  assert.deepEqual(titles(), ["RFE Desktop", "Check for updates", "Check automatically", "Report a problem", "Welcome tour"]);
  const upd = [...d.querySelectorAll(".k-sbody .k-set")][1];
  assert.deepEqual([...upd.querySelectorAll('[data-set="channel"] button')].map(text), ["Stable", "Beta"]);
  upd.querySelector('[data-set="channel"] [data-v="beta"]').click(); await sleep(60);
  assert.equal(A.S.channel, "beta", "the channel is stored where the update dialog reads it");
  assert.equal(d.querySelector('[data-set="channel"] [data-v="beta"]').getAttribute("aria-pressed"), "true");
  const sw = () => d.querySelector('.k-sbody [data-sw="autoUpdate"]');
  assert.equal(sw().getAttribute("aria-checked"), "false");
  sw().click(); await sleep(60);
  assert.equal(A.S.autoUpdate, true, "the daily update check is switched on");
  assert.equal(sw().getAttribute("aria-checked"), "true");
  d.querySelector('[data-fx="v5.report"]').click(); await sleep(80);
  assert.ok([...d.querySelectorAll("#layer .dlg h2, #layer [role=dialog] h2, #layer .dh")].some((h) => /Report a problem/.test(text(h))) || /Report a problem/.test(text(d.querySelector("#layer"))), "the report dialog opens");
  assert.deepEqual(o.errs, []);
});

test("Help lists the shortcuts in the mockup's order, with Alt+Up; Delete reads Move to Trash unless Trash is off", async () => {
  const o = await open("settings");
  const { d, A } = o;
  await tab(d, "Help");
  const own = (el) => [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join("").trim();
  const items = () => [...d.querySelectorAll(".k-sbody .k-keys > div")];
  assert.deepEqual(items().map(own), ["Search", "Collapse navigation", "Type a path", "Download", "Upload", "New folder", "Rename", "Move to Trash", "Refresh", "Back", "Forward", "Up", "Select all", "This list", "Clear selection or close"]);
  const keys = (i) => [...items()[i].querySelectorAll("kbd")].map(text);
  assert.deepEqual(keys(0), ["Ctrl", "K", "/"], "one Search row holds both Ctrl+K and /");
  assert.ok(!items().some((i) => /palette/i.test(own(i))), "no command palette row: Ctrl+K opens Search");
  assert.deepEqual(keys(11), ["Alt", "↑"], "Alt+Up goes up a folder");
  assert.deepEqual(keys(7), ["Delete"]);
  A.S.trash = false; A.pageSettings();
  assert.equal(own(items()[7]), "Delete", "with Trash off the key is a plain delete");
  A.S.trash = true;
  assert.deepEqual(o.errs, []);
});

// ---------------------------------------------------------------- Tools: Trash and Share links

/** Opens a Tools tab with its list already filled in (and marked fresh so the page does not ask the computer again). */
async function tools(o, tool, fill) {
  const { A, E } = o;
  A.X.trashAt = E.now(); A.X.sharesAt = E.now();
  fill();
  A.X.tool = tool; A.go("tools"); await sleep(150); A.pageTools();
}
const DAY = 864e5;

test("Trash: no 'stay here for N days' line; rows have the mockup's pictures and take arrow keys", async () => {
  const o = await open();
  const { d, w, A, E } = o;
  const h = E.servers.find((s) => s.state === "online").id;
  await tools(o, "trash", () => {
    A.S.trashDays = 30;
    A.X.trash = [
      { host: h, id: "t1", name: "notes.txt", path: "/home/a/notes.txt", at: Date.now() - 1000, b: 100, isDir: false },
      { host: h, id: "t2", name: "photos", path: "/home/a/photos", at: Date.now() - 2000, b: 0, isDir: true },
      { host: h, id: "t3", name: "song.mp3", path: "/home/a/song.mp3", at: Date.now() - 3000, b: 300, isDir: false },
    ];
  });
  assert.doesNotMatch(text(d.querySelector("#stage")), /stay here for/i);
  assert.equal(d.querySelector(".k-banner"), null, "nothing is old, so no banner");
  const rows = [...d.querySelectorAll(".k-trash .tr[tabindex]")];
  assert.equal(rows.length, 3);
  const pics = rows.map((r) => emojiName(r.querySelector("img.k-e")));
  assert.ok(pics[0] && pics[0] !== "file_folder", "a file has a file picture: " + pics[0]);
  assert.equal(pics[1], "file_folder", "a folder is a folder picture");
  assert.equal(pics[2], "musical_notes", "music is told apart by its extension");
  assert.equal(d.querySelector(".k-trash .tr .k-fic svg"), null, "no line icon beside a name");
  const key = (el, k) => el.dispatchEvent(new w.KeyboardEvent("keydown", { key: k, bubbles: true }));
  rows[0].focus(); key(rows[0], "ArrowDown");
  assert.equal(d.activeElement, rows[1], "Down moves to the next row");
  key(rows[1], "End");
  assert.equal(d.activeElement, rows[2], "End moves to the last row");
  key(rows[2], "ArrowUp");
  assert.equal(d.activeElement, rows[1], "Up moves back");
  key(rows[1], "Home");
  assert.equal(d.activeElement, rows[0], "Home moves to the first row");
  assert.deepEqual(o.errs, []);
});

test("Trash: the old-items question is a banner with Delete them and Keep them", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = E.servers.find((s) => s.state === "online").id;
  const ran = [];
  await tools(o, "trash", () => {
    A.S.trashDays = 30;
    A.X.trash = [
      { host: h, id: "t1", name: "fresh.txt", path: "/a/fresh.txt", at: Date.now() - 1000, b: 1, isDir: false },
      { host: h, id: "t2", name: "ancient.txt", path: "/a/ancient.txt", at: Date.now() - 40 * DAY, b: 1, isDir: false },
    ];
  });
  const shown = A.askOldTrash({ title: "Delete old items from Trash?", body: "", actions: [{ id: "k", label: "Keep them", cb: () => ran.push("keep") }, { id: "d", label: "Delete 1", cb: () => ran.push("delete") }] });
  assert.equal(shown, null, "no dialog is opened while the v5 pages are on");
  assert.equal(d.querySelector(".dlg"), null);
  A.pageTools();
  const ban = d.querySelector('.k-banner[aria-label="Old items in Trash"]');
  assert.ok(ban, "the banner is drawn");
  assert.match(text(ban), /Delete old items from Trash\? 1 item was deleted more than 30 days ago/);
  assert.deepEqual([...ban.querySelectorAll("button")].map(text), ["Delete them", "Keep them"]);
  ban.querySelector('[data-fx="v5.trashold|keep"]').click(); await sleep(60);
  assert.deepEqual(ran, [], "Keep them deletes nothing");
  assert.equal(d.querySelector(".k-banner"), null, "and closes the banner");
  A.askOldTrash({ title: "Delete old items from Trash?", body: "", actions: [{ id: "k", label: "Keep them", cb: () => ran.push("keep") }, { id: "d", label: "Delete 1", cb: () => ran.push("delete") }] });
  A.pageTools();
  d.querySelector('[data-fx="v5.trashold|del"]').click(); await sleep(60);
  assert.deepEqual(ran, ["delete"], "Delete them runs the dialog's own delete button");
  assert.equal(d.querySelector(".k-banner"), null);
  assert.deepEqual(o.errs, []);
});

test("Share links: Copy link copies a link made in this session; an old link's button is disabled with a reason; arrow keys move between rows", async () => {
  const o = await open();
  const { d, w, A, E, copied } = o;
  const h = E.servers.find((s) => s.state === "online").id;
  const made = await E.call("share_mint", { host: h, path: "/srv/report.pdf", expiresInSeconds: 3600 });
  assert.ok(made && made.url && made.tokenHash, "the stub makes a link");
  await tools(o, "share", () => {
    A.X.shares = [
      { host: h, hash: made.tokenHash, name: "report.pdf", path: "/srv/report.pdf", exp: E.now() + 3600e3 },
      { host: h, hash: "f".repeat(64), name: "older.pdf", path: "/srv/older.pdf", exp: E.now() + 3600e3 },
      { host: h, hash: "e".repeat(64), name: "gone.pdf", path: "/srv/gone.pdf", exp: E.now() - 1000 },
    ];
  });
  const rows = [...d.querySelectorAll(".k-list > .k-lrow[tabindex]")];
  assert.equal(rows.length, 3);
  const copy = (i) => rows[i].querySelector('[data-fx^="v5.sharecopy"]');
  assert.equal(text(copy(0)), "Copy link");
  assert.equal(copy(0).disabled, false, "made in this session, so it can be copied");
  assert.equal(copy(1).disabled, true, "an earlier session's link cannot be shown again");
  assert.ok(copy(1).getAttribute("title"), "and says why");
  assert.equal(copy(2), null, "an expired link has no Copy link");
  copy(0).click(); await sleep(80);
  assert.deepEqual(copied, [made.url]);
  assert.equal(text(d.querySelector(".snack")), "Link copied");
  A.fxHandlers["v5.sharecopy"]("1"); await sleep(30);
  assert.match(text(d.querySelector(".snack")), /can’t be shown again/);
  const key = (el, k) => el.dispatchEvent(new w.KeyboardEvent("keydown", { key: k, bubbles: true }));
  rows[0].focus(); key(rows[0], "ArrowDown");
  assert.equal(d.activeElement, rows[1], "Down moves to the next link");
  key(rows[1], "ArrowUp");
  assert.equal(d.activeElement, rows[0], "Up moves back");
  assert.deepEqual(o.errs, []);
});

// ---------------------------------------------------------------- Transfers

/** Starts one download so a transfer is running, and returns its task. */
async function runningDownload(o) {
  const { A, E } = o;
  const NAS = E.servers.find((s) => s.state === "online").id;
  await E.fs.load(NAS, "/srv"); await E.fs.load(NAS, "/srv/projects").catch(() => {});
  A.mainPane.go("/srv"); await sleep(100);
  await A.doEnqueue("down", NAS, "/srv", "/home/zaid/Downloads", ["projects"]);
  await until(() => E.tasks[0] && E.tasks[0].rids.length, "the core to take it");
  await E.pollTransfers(); await sleep(50);
  return E.tasks[0];
}

test("Transfers: the page wears the d5 class; rows and the panel use the inbox/outbox pictures; a running transfer offers Show in Files", async () => {
  const o = await open();
  const { d, A, E } = o;
  const t = await runningDownload(o);
  assert.equal(t.state, "running");
  A.go("transfers"); await sleep(200);
  assert.ok(d.querySelector("#stage").classList.contains("d5"), "the stage is a d5 page");
  const row = d.querySelector('#stage .ro[data-x="pick"] img.k-e');
  assert.equal(emojiName(row), "inbox_tray", "a download is an inbox tray");
  assert.equal(emojiName(d.querySelector("#stage .v5rail > img.k-e")), "inbox_tray", "the panel shows the same picture");
  assert.equal(d.querySelector("#stage .ro .b-ic"), null, "no line-icon tile in the rows");
  assert.equal(d.querySelector("#stage .v5rail .b-tile"), null, "no line-icon tile in the panel");
  const labels = [...d.querySelectorAll("#stage .k-dact button")].map(text);
  assert.deepEqual(labels, ["Pause", "Show in Files", "Cancel"]);
  assert.deepEqual(o.errs, []);
});

test("Transfers: with nothing moving the empty page has the up-down arrow picture", async () => {
  const o = await open();
  o.E.tasks.splice(0);
  o.A.go("transfers"); await sleep(200);
  const e = o.d.querySelector("#stage .b-empty");
  assert.ok(e, "the empty state");
  assert.equal(emojiName(e.querySelector("img.k-e")), "updown_arrow");
  assert.equal(text(e.querySelector("b")), "Nothing moving");
  assert.deepEqual(o.errs, []);
});

test("Transfers: unfinished transfers from the last session show as a banner whose Carry on… opens the resume question", async () => {
  const o = await open();
  const { d, A } = o;
  const NAS = o.E.servers[0].id;
  const kept = (n) => ({ dir: "down", host: NAS, name: n, srcDir: "/srv", dstDir: "/home/zaid/Downloads", bytes: 10, done: 1 });
  A.journal.retained = [kept("a.bin"), kept("b.bin")];
  let asked = null; A.resumeDialog = (j) => { asked = j; };
  o.E.tasks.splice(0);
  A.go("transfers"); await sleep(200);
  const ban = d.querySelector("#stage .k-banner");
  assert.ok(ban, "the banner is drawn");
  assert.match(text(ban), /^2 transfers were left unfinished when RFE closed\./);
  const b = ban.querySelector('[data-x="carry"]');
  assert.equal(text(b), "Carry on…");
  b.click();
  assert.ok(asked && asked.tasks.length === 2, "the resume question gets the two kept transfers");
  A.journal.retained = [kept("a.bin")];
  A.go("home"); A.go("transfers"); await sleep(200);
  assert.match(text(d.querySelector("#stage .k-banner")), /^1 transfer was left unfinished/);
  A.journal.retained = [];
  A.go("home"); A.go("transfers"); await sleep(200);
  assert.equal(d.querySelector("#stage .k-banner"), null, "nothing kept, no banner");
  assert.deepEqual(o.errs, []);
});

test("Transfers: the old Transfer journal card is hidden on this page (the banner replaces it)", () => {
  const css = readAllCss();
  assert.match(css, /\.stage\.v5x \.jcard\s*\{\s*display:\s*none/);
  assert.ok(readFileSync(fileURLToPath(new URL("../ui/v5-transfers.js", import.meta.url)), "utf8").includes("left unfinished"));
});

test("Transfers: Pause on a transfer still being set up holds it as Paused, and Resume lets it go on", async () => {
  const o = await open();
  const { d, A, E } = o;
  const NAS = E.servers[0].id;
  E.tasks.splice(0);
  const t = { id: "q-setup", dir: "down", host: NAS, name: "big.iso", srcDir: "/srv", dstDir: "/home/zaid/Downloads", state: "queued", rids: [], bytes: 100, done: 0, speed: 0, queuedAt: Date.now() };
  E.tasks.push(t);
  A.go("transfers"); await sleep(200);
  d.querySelector('#stage .ro[data-id="q-setup"]').click(); await sleep(60);
  const pause = d.querySelector('#stage [data-tid="q-setup"] [data-t="pause"]');
  assert.ok(pause, "a queued transfer offers Pause");
  pause.click(); await sleep(60);
  assert.equal(t.state, "paused", "held at once, without asking the engine");
  assert.equal(t.held, true);
  const resume = d.querySelector('#stage [data-tid="q-setup"] [data-t="resume"]');
  assert.ok(resume, "and now offers Resume");
  t.starting = true;
  resume.click(); await sleep(60);
  assert.equal(t.held, false);
  assert.equal(t.state, "queued", "a transfer whose setup is running goes back to waiting");
});

// ---------------------------------------------------------------- Overview and Search

test("Overview: the stage is a d5 page and the speed chart's legend uses the .lgd marks", async () => {
  const o = await open();
  o.A.go("home"); await sleep(300);
  const st = o.d.querySelector("#stage");
  assert.ok(st.classList.contains("d5"), "the stage wears d5");
  const lg = st.querySelectorAll(".chart h2 i.lgd");
  assert.equal(lg.length, 2);
  assert.ok(lg[0].classList.contains("dn") && lg[1].classList.contains("up"), "Download then Upload");
  assert.equal(st.querySelector(".chart h2 i.lg"), null, "no inline-coloured legend dots");
  assert.deepEqual(o.errs, []);
});

test("Search with no computer says 'Add a computer to search it' and offers an Add a computer button", async () => {
  const o = await open();
  const { d, A, E } = o;
  A.searchRun = null;
  E.servers.splice(0);
  A.go("search"); await sleep(200);
  const no = d.querySelector("#stage .s-no");
  assert.ok(no, "the empty state");
  assert.equal(text(no.querySelector("b")), "Add a computer to search it");
  const b = no.querySelector('button[data-x="add"]');
  assert.ok(b, "a button");
  assert.equal(text(b), "Add a computer");
});

test("Search results: titled 'Search results', with the line of what was searched; the scope and 'N more on <computer>' belong to the Search panel", async () => {
  const o = await open();
  const { d, A, E } = o;
  A.runQuery("e");
  await until(() => A.searchRun && A.searchRun.done, "the search to finish");
  await sleep(150);
  assert.equal(text(d.querySelector("#stage h1")), "Search results");
  assert.match(text(d.querySelector("#stage #sprog")), /^\d+\+? for “e” on /);
  assert.equal(d.querySelector("#stage .s-sc"), null, "the page has no scope buttons (the panel has them)");
  assert.equal(d.querySelector("#stage .s-mo"), null, "the page is one table, not folded groups");
  assert.ok(d.querySelector("#stage .rt"), "the table");
  assert.ok(E.servers.length >= 1);
});

// ---------------------------------------------------------------- History

test("History: Transfers events use the swap icon, Security the lock, and the filter field the filter icon", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = E.servers[0].id;
  E.history.push(
    { id: "v5-h-tr", at: Date.now() - 1000, kind: "transfer", text: "Downloaded report.pdf", host: h },
    { id: "v5-h-sec", at: Date.now() - 2000, kind: "audit", text: "Sign in failed", host: h },
  );
  A.go("history"); await sleep(300);
  const icon = (id) => d.querySelector('#stage [data-v5ev="' + id + '"] .b-ic svg').innerHTML;
  assert.equal(icon("v5-h-tr"), iconOf(o, "swap", 16), "Transfers is the swap icon");
  assert.equal(icon("v5-h-sec"), iconOf(o, "lock", 16), "Security is the lock icon");
  assert.equal(d.querySelector("#stage .b-fq svg").innerHTML, iconOf(o, "filter", 14), "the filter field has the filter icon");
  assert.deepEqual(o.errs, []);
});

// ---------------------------------------------------------------- Tools rows, Apps, Files panel, radar, breadcrumb bar

const onlineId = (E) => E.servers.find((s) => s.state === "online").id;

test("Tools: Favorites, Recent files and Available offline rows lead with a colour picture of the file or folder", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = onlineId(E);
  await tools(o, "fav", () => {
    A.X.favs = [{ host: h, path: "/srv/projects", name: "projects", t: "dir" }, { host: h, path: "/srv/song.mp3", name: "song.mp3", t: "file" }];
    A.X.recents = [{ host: h, path: "/srv/sheet.csv", name: "sheet.csv", at: E.now() }];
    A.X.pins = [{ host: h, path: "/srv/clip.flac", name: "clip.flac", b: 1000 }];
  });
  const pic = (row) => { assert.ok(row, "the row is drawn"); assert.ok(row.firstElementChild.matches("img.k-e"), "the row starts with a picture"); return emojiName(row.firstElementChild); };
  const favs = [...d.querySelectorAll('.k-lrow[data-fx^="fav.open"]')];
  assert.equal(favs.length, 2);
  assert.equal(pic(favs[0]), "file_folder", "a favorite folder");
  assert.equal(pic(favs[1]), "musical_notes", "a favorite song");
  assert.equal(pic(d.querySelector('.k-lrow[data-fx^="rec.open"]')), "bar_chart", "a recent spreadsheet");
  const pin = [...d.querySelectorAll(".k-lrow")].find((r) => /clip\.flac/.test(text(r)));
  assert.equal(pic(pin), "musical_notes", "an offline song");
  assert.equal(d.querySelector(".k-lrow .k-fic svg"), null, "no line icon beside a name");
  assert.deepEqual(o.errs, []);
});

test("Tools: Largest files, Duplicates and Share links rows use colour pictures, not line icons", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = onlineId(E);
  A.X.tool = "store"; A.go("tools"); await sleep(400);
  await until(() => !A.X.bigBusy, "the scan to end");
  A.X.bigHost = h;
  A.X.big = [{ p: "/srv/data.csv", name: "data.csv", b: 6e8 }, { p: "/srv/notes.txt", name: "notes.txt", b: 5e8 }];
  A.X.dupes = [[{ name: "a.mp3", size: 1e6, path: "/srv/a.mp3" }, { name: "a.mp3", size: 1e6, path: "/srv/old/a.mp3" }]];
  A.pageTools(); await sleep(60);
  const big = [...d.querySelectorAll('.k-lrow[data-fx^="store.show"]')];
  assert.equal(big.length, 2);
  assert.equal(emojiName(big[0].querySelector(".k-fic img.k-e")), "bar_chart", "a big spreadsheet in its small box");
  assert.ok(emojiName(big[1].querySelector(".k-fic img.k-e")), "a big text file has a picture");
  assert.equal(big[0].querySelector("svg"), null, "no line icon in a Largest files row");
  const dup = d.querySelector(".k-lrow .k-dup").parentElement;
  assert.equal(emojiName(dup.firstElementChild), "musical_notes", "a duplicate group starts with its file picture");
  await tools(o, "share", () => {
    A.X.shares = [{ host: h, hash: "a".repeat(64), name: "sheet.csv", path: "/srv/sheet.csv", exp: E.now() + 3600e3 }, { host: h, hash: "b".repeat(64), name: "old.csv", path: "/srv/old.csv", exp: E.now() - 1000 }];
  });
  const rows = [...d.querySelectorAll(".k-list > .k-lrow[tabindex]")];
  assert.equal(rows.length, 2);
  assert.equal(emojiName(rows[0].firstElementChild), "bar_chart", "a share link starts with its file picture");
  assert.ok(rows[1].firstElementChild.matches("img.k-e"), "an expired link keeps its picture");
  assert.equal(d.querySelector(".k-list > .k-lrow .k-fic"), null, "no boxed line icon in Share links");
  assert.deepEqual(o.errs, []);
});

test("Tools: Apps cards show the mockup picture chosen from the program's name and category, a toolbox when nothing fits", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = onlineId(E);
  await tools(o, "apps", () => {
    A.X.apps[h] = { at: E.now(), cat: { launchAllowed: true, apps: [
      { id: "a0", name: "GIMP", category: "Graphics", description: "Edit pictures", launchable: true },
      { id: "a1", name: "Firefox", category: "Web browser", description: "Browse", launchable: true },
      { id: "a2", name: "VLC", category: "Media player", description: "Play", launchable: true },
      { id: "a3", name: "Zzz", category: "Other", description: "Misc", launchable: false },
    ] } };
  });
  const cards = [...d.querySelectorAll(".k-apps .k-app")];
  assert.equal(cards.length, 4);
  const pic = (name) => emojiName(cards.find((c) => text(c.querySelector("b")) === name).querySelector(".k-appic img.k-e"));
  assert.equal(pic("GIMP"), "artist_palette");
  assert.equal(pic("Firefox"), "globe_with_meridians");
  assert.equal(pic("VLC"), "film_frames");
  assert.equal(pic("Zzz"), "toolbox");
  assert.equal(cards[3].disabled, true, "a program that cannot be launched is disabled");
  assert.deepEqual(o.errs, []);
});

test("Files: the details panel's preview tile and title use colour pictures for a folder and for a multi-selection", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = onlineId(E);
  await E.fs.load(h, "/srv");
  A.go("files"); await sleep(400);
  A.setMainHost(h); A.mainPane.go("/srv"); await sleep(200);
  A.mainPane.refreshItems();
  A.mainPane.sel = new Set(["projects"]); A.renderDetail();
  assert.equal(emojiName(d.querySelector("#detail .dprev img.k-e")), "file_folder", "a folder's preview tile");
  assert.equal(emojiName(d.querySelector("#detail h3 img.k-e")), "file_folder", "the small picture before its name");
  assert.equal(d.querySelector("#detail .dprev svg"), null, "no line icon in the tile");
  A.mainPane.sel = new Set(["projects", "backups"]); A.renderDetail();
  assert.match(text(d.querySelector("#detail h3")), /2 items selected/);
  assert.equal(emojiName(d.querySelector("#detail .dprev img.k-e")), "card_file_box", "a multi-selection is a card box");
  assert.equal(d.querySelector("#detail .dprev svg"), null, "no line icon in the tile");
  const lis = [...d.querySelectorAll("#detail ul.names li:not(.more)")];
  assert.equal(lis.length, 2);
  for (const li of lis) assert.ok(li.querySelector("img.k-e"), "each listed name has its picture");
  assert.deepEqual(o.errs, []);
});

test("Find on network has the radar icon on the empty Files page and the empty Computers page", async () => {
  const o = await open();
  const { d, A, E } = o;
  E.servers.splice(0);
  A.go("files"); await sleep(300);
  const find = (root) => [...d.querySelectorAll(root + " button")].find((b) => text(b) === "Find on network");
  assert.ok(find("#stage"), "Files offers it");
  assert.equal(find("#stage").querySelector("svg").innerHTML, iconOf(o, "radar", 16), "Files: the radar icon");
  A.go("servers"); await sleep(300);
  assert.ok(find("#stage"), "Computers offers it");
  assert.equal(find("#stage").querySelector("svg").innerHTML, iconOf(o, "radar", 16), "Computers: the radar icon");
  assert.deepEqual(o.errs, []);
});

test("Files: the bar above the tree has Up, the computer with its dot, clickable path steps, New folder and Upload", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = onlineId(E);
  await E.fs.load(h, "/srv/projects");
  A.go("files"); await sleep(400);
  A.setMainHost(h); A.mainPane.go("/srv/projects"); await sleep(200);
  A.mainPane.sel = new Set();
  A.v5Files.paintTop();
  const bar = d.querySelector("#stage #ftop");
  assert.ok(bar && bar.classList.contains("k-ftop"), "the bar is drawn");
  const up = bar.querySelector('[data-top="up"]');
  assert.equal(up.querySelector("svg").innerHTML, iconOf(o, "arrow-up", 16), "Up has the arrow-up icon");
  assert.equal(up.disabled, false, "not at the top, so Up is on");
  const trail = bar.querySelector('nav.k-trail[aria-label="Path"]');
  assert.ok(trail, "the path");
  const host = trail.querySelector('button.host[data-top="host"]');
  assert.equal(text(host), A.hostName(h), "the computer's name");
  assert.ok(host.firstElementChild, "with its status dot");
  assert.deepEqual([...trail.querySelectorAll('button[data-top="crumb"]')].map(text), ["srv"], "earlier steps are buttons");
  assert.equal(text(trail.querySelector("b")), "projects", "the current folder is plain bold text");
  assert.equal(bar.querySelector('[data-top="mk"]').getAttribute("aria-label"), "New folder");
  assert.equal(text(bar.querySelector('[data-top="upload"]')), "Upload");
  assert.deepEqual(o.errs, []);
});

// ---------------------------------------------------------------- Empty and error states (small .k-empty in a card, whole-page .b-empty)

/** What one empty note shows: its colour picture, the picture's width, its title and its hint line. */
const note = (el) => ({ pic: emojiName(el.querySelector("img.k-e")), size: el.querySelector("img.k-e").getAttribute("width"), title: text(el.querySelector("b")), hint: text(el.querySelector("small, p")) });

test("the colour pictures the empty states use exist as files", () => {
  for (const n of ["star", "counterclockwise_arrows_button", "check_mark_button", "hourglass_not_done", "pushpin", "scroll", "warning", "mobile_phone", "desktop_computer", "wastebasket", "file_cabinet", "toolbox", "link", "bar_chart", "page_facing_up"]) {
    assert.ok(existsSync(fileURLToPath(new URL("../ui/emoji/" + n + ".svg", import.meta.url))), n + ".svg");
  }
});

test("Tools: with no favorites, recent files or offline copies, Favorites is one whole-page .b-empty (40 px picture, title, hint, Add current folder)", async () => {
  const o = await open();
  const { d, A } = o;
  await tools(o, "fav", () => { A.X.favs = []; A.X.recents = []; A.X.pins = []; });
  const e = d.querySelectorAll(".k-tbody .b-empty");
  assert.equal(e.length, 1, "one page-level empty");
  assert.deepEqual(note(e[0]), { pic: "star", size: "40", title: "No favorites", hint: "Star a folder in Files to keep it here." });
  assert.equal(d.querySelectorAll(".k-tbody .k-empty").length, 0, "no small notes beside it");
  assert.ok(e[0].querySelector('[data-fx="fav.add"]'), "keeps the Add current folder button");
  assert.deepEqual(o.errs, []);
});

test("Tools: when only some of Favorites, Recent files and Available offline are empty, each empty one is a small .k-empty note (32 px picture, plain title, hint)", async () => {
  const o = await open();
  const { d, A } = o;
  await tools(o, "fav", () => { A.X.favs = []; A.X.recents = [{ name: "a.txt", host: "x", path: "/a.txt", at: Date.now() }]; A.X.pins = []; });
  const notes = [...d.querySelectorAll(".k-tbody .k-empty")].map(note);
  assert.deepEqual(notes, [
    { pic: "star", size: "32", title: "No favorites", hint: "Star a folder in Files to keep it here." },
    { pic: "pushpin", size: "32", title: "Nothing offline", hint: "Right-click a file and choose Make available offline." },
  ]);
  assert.equal(d.querySelector(".k-tbody .b-empty"), null, "inside cards, never the page-level empty");
  assert.deepEqual(o.errs, []);
});

test("Tools: Share links with none is a whole-page .b-empty; Sync rules and Apps with nothing to list use a .k-empty note", async () => {
  const o = await open();
  const { d, A, E } = o;
  const h = onlineId(E);
  await tools(o, "share", () => { A.X.shares = []; A.X.sharesBusy = false; A.X.sharesErr = ""; });
  const be = d.querySelectorAll(".k-tbody .b-empty");
  assert.equal(be.length, 1, "one page-level empty");
  assert.deepEqual(note(be[0]), { pic: "link", size: "40", title: "No share links", hint: "Make one from a file: Share link…" });
  assert.ok(be[0].querySelector('[data-fx="share.new"]'), "keeps the New link button");
  const only = () => { assert.equal(d.querySelector(".k-tbody .b-empty"), null, "not the page-level empty"); const n = [...d.querySelectorAll(".k-tbody .k-empty")]; assert.equal(n.length, 1); return note(n[0]); };
  await tools(o, "sync", () => { A.X.rules = []; });
  assert.deepEqual(only(), { pic: "counterclockwise_arrows_button", size: "32", title: "No sync rules", hint: "Pair a folder here with one on a computer." });
  await tools(o, "apps", () => { A.X.appQ = ""; A.X.apps[h] = { at: E.now(), cat: { launchAllowed: true, apps: [] } }; });
  assert.deepEqual(only(), { pic: "toolbox", size: "32", title: "No apps to show", hint: "This computer lists no apps to start." });
  A.X.appQ = "zzz"; A.pageTools();
  assert.deepEqual(only(), { pic: "toolbox", size: "32", title: "No app matches", hint: "Try another word in the filter." });
  A.X.appQ = ""; A.X.apps[h] = { at: E.now(), err: "denied" }; A.pageTools();
  assert.deepEqual(only(), { pic: "warning", size: "32", title: "Could not list apps", hint: "denied" });
  A.X.apps[h] = { at: E.now() }; A.pageTools();
  assert.deepEqual(only(), { pic: "hourglass_not_done", size: "32", title: "Loading…", hint: "Asking " + E.server(h).name + " for its apps." });
  assert.deepEqual(o.errs, []);
});

test("Tools: Storage with nothing found draws .k-empty notes for Largest files and Duplicates", async () => {
  const o = await open();
  const { d, A, E } = o;
  A.X.tool = "store"; A.go("tools"); await sleep(400);
  await until(() => !A.X.bigBusy, "the scan to end");
  A.X.bigHost = onlineId(E); A.X.big = []; A.X.dupes = []; A.X.dupeScanned = 7;
  A.pageTools(); await sleep(60);
  const notes = [...d.querySelectorAll(".k-tbody .k-empty")].map(note);
  const big = notes.find((n) => n.title === "No big files"), dup = notes.find((n) => n.title === "No duplicates");
  assert.ok(big && dup, "both notes: " + JSON.stringify(notes));
  assert.equal(big.pic, "bar_chart"); assert.equal(big.size, "32"); assert.match(big.hint, /^Nothing over .+ was found\.$/);
  assert.equal(dup.pic, "check_mark_button"); assert.equal(dup.size, "32"); assert.match(dup.hint, /^Of the 7 files over 1 MB checked on .+, none share a name and size\.$/);
  A.X.big = null; A.pageTools();
  const first = [...d.querySelectorAll(".k-tbody .k-empty")].map(note).find((n) => n.title === "Not scanned yet");
  assert.ok(first, "before any scan");
  assert.equal(first.pic, "bar_chart"); assert.equal(first.hint, "Press Scan again to look for the largest files.");
  assert.deepEqual(o.errs, []);
});

test("Tools: an empty Trash and Storage or Apps with no connected computer use the whole-page .b-empty (40 px picture, hint)", async () => {
  const o = await open();
  const { d, A, E } = o;
  const whole = () => { assert.equal(d.querySelector(".k-tbody .k-empty"), null, "not a card-sized note"); const n = d.querySelectorAll(".k-tbody > .b-empty"); assert.equal(n.length, 1, "one page-level empty directly in the page body"); assert.equal(n[0].querySelector(".k-row"), null, "no button"); return note(n[0]); };
  await tools(o, "trash", () => { A.X.trash = []; A.X.trashErr = ""; A.X.trashBusy = false; A.S.trash = true; });
  assert.deepEqual(whole(), { pic: "wastebasket", size: "40", title: "Trash is empty", hint: "Deleted files wait here before they go for good." });
  A.S.trash = false; A.pageTools();
  assert.equal(whole().hint, "Trash is turned off in Settings, so deletes are permanent.");
  E.servers.splice(0);
  A.X.tool = "store"; A.go("tools"); await sleep(200);
  assert.deepEqual(whole(), { pic: "file_cabinet", size: "40", title: "No storage to show", hint: "Disk use appears once a computer is connected." });
  A.X.tool = "apps"; A.pageTools();
  assert.deepEqual(whole(), { pic: "toolbox", size: "40", title: "No apps to show", hint: "A connected computer lists the apps it lets you start." });
  assert.deepEqual(o.errs, []);
});

test("Devices: no computer, still loading, a failed list and no paired devices each have a colour picture; the empty list offers Pair a phone", async () => {
  const o = await open();
  const { d, A, E } = o;
  let pairs = 0, adds = 0; A.pairDialog = () => { pairs++; }; A.serverDialog = () => { adds++; };
  A.loadDevices = () => {}; /* the page must not reload the list under the test */
  const empty = () => { const n = d.querySelectorAll("#stage .b-empty"); assert.equal(n.length, 1); return n[0]; };
  const fresh = (over) => { Object.assign(A.X, { devices: [], devAt: E.now(), devBusy: false, devErr: "", auditAt: E.now() }, over); A.go("devices"); A.pageDevices(); };
  fresh({}); await sleep(150); A.pageDevices();
  let e = empty();
  assert.deepEqual(note(e), { pic: "mobile_phone", size: "40", title: "No paired devices", hint: "Pair a phone to see it here." });
  assert.equal(d.querySelectorAll('#stage [data-v5="pair"]').length, 2, "the header button and the one in the empty note");
  const b = e.querySelector('button[data-v5="pair"]');
  assert.equal(text(b), "Pair a phone");
  b.click(); assert.equal(pairs, 1, "it opens the pairing dialog");
  fresh({ devBusy: true }); e = empty();
  assert.deepEqual(note(e), { pic: "hourglass_not_done", size: "40", title: "Loading…", hint: "Reading the list of devices." });
  assert.equal(e.querySelector("button"), null, "nothing to press while loading");
  fresh({ devErr: "The list could not be refreshed: offline" }); e = empty();
  assert.deepEqual(note(e), { pic: "warning", size: "40", title: "Could not list devices", hint: "The list could not be refreshed: offline" });
  E.servers.splice(0);
  fresh({}); e = empty();
  /* the devices now live on the Computers page, so with no computer saved the page says so there */
  assert.deepEqual(note(e), { pic: "desktop_computer", size: "40", title: "No devices yet", hint: "Install RFE on a PC and open it once. Then add it here: it shows up by itself on the same network." });
  const add = e.querySelector('button[data-pa="add"]');
  assert.equal(text(add), "Add a computer");
  add.click(); assert.equal(adds, 1);
  const local = e.querySelector('button[data-v5="addlocal"]');
  assert.equal(text(local), "Add this PC");
  local.click(); assert.equal(adds, 2);
  assert.deepEqual(o.errs, []);
});

test("History: filters that hide every event show a 'No events match' note and Clear filters brings the list back; with no history it says events are recorded as they happen", async () => {
  const o = await open();
  const { d, w, A, E } = o;
  const h = E.servers[0].id;
  E.history.push({ id: "v5-h-one", at: Date.now() - 1000, kind: "transfer", text: "Downloaded report.pdf", host: h });
  A.go("history"); await sleep(300);
  assert.equal(d.querySelector("#stage .k-empty"), null, "events are listed, no note");
  const q = d.querySelector('#stage [data-v5f="q"]');
  q.value = "zzz-no-such-event"; q.dispatchEvent(new w.Event("input", { bubbles: true }));
  const e = d.querySelector("#stage .k-empty");
  assert.ok(e, "the no-match note");
  assert.deepEqual(note(e), { pic: "scroll", size: "32", title: "No events match", hint: "Clear the filters to see them all." });
  assert.doesNotMatch(text(d.querySelector("#stage")), /recorded here as they happen/, "history exists, so no 'recorded' line");
  const clear = d.querySelector('#stage [data-v5="clearf"]');
  assert.ok(clear, "Clear filters is offered");
  clear.click();
  assert.equal(d.querySelector("#stage .k-empty"), null, "cleared");
  assert.ok(d.querySelector('#stage [data-v5ev="v5-h-one"]'), "the event is back");
  E.history.splice(0);
  const realCall = E.call; E.call = (c, a) => (c === "audit_page" ? Promise.resolve({ entries: [] }) : realCall(c, a)); /* no agent log entries: the page syncs them on open */
  A.go("files"); A.go("history"); await sleep(300);
  E.call = realCall;
  assert.match(text(d.querySelector("#stage .v5-note")), /recorded here as they happen\.$/);
  assert.equal(d.querySelector("#stage .k-empty"), null, "no 'No events match' without any history");
  assert.deepEqual(o.errs, []);
});

test("Computers: the empty page's Add a computer and Find on network buttons carry data-pa add and find, and Find on network opens the Add dialog", async () => {
  const o = await open();
  const { d, A, E } = o;
  let opened = 0; A.serverDialog = () => { opened++; };
  E.servers.splice(0);
  A.go("servers"); await sleep(300);
  const e = d.querySelector("#stage .cpg .b-empty");
  assert.ok(e, "the empty page");
  assert.equal(emojiName(e.querySelector("img.k-e")), "desktop_computer");
  assert.equal(text(e.querySelector("b")), "No devices yet");
  const add = e.querySelector('button[data-pa="add"]'), find = e.querySelector('button[data-pa="find"]');
  assert.equal(text(add), "Add a computer");
  assert.equal(text(find), "Find on network");
  find.click(); assert.equal(opened, 1, "Find on network opens the Add dialog");
  add.click(); assert.equal(opened, 2);
  A.go("files"); await sleep(300);
  const files = d.querySelector('#stage .b-empty button[data-a="find"]');
  assert.ok(files, "the Files empty page's button");
  assert.equal(text(files), text(find), "same wording on both pages");
  assert.deepEqual(o.errs, []);
});

test("Files details panel: a computer that is not connected shows the desktop computer picture and 'Not connected'", async () => {
  const o = await open();
  const { d, A, E } = o;
  A.go("files"); await sleep(300);
  const p = A.mainPane;
  p.online = () => false;
  A.renderDetail();
  const dnc = d.querySelector("#detail .dnc");
  assert.ok(dnc, "the not-connected panel");
  assert.equal(emojiName(dnc.querySelector("img.k-e")), "desktop_computer");
  assert.equal(dnc.querySelector("svg"), null, "no line icon");
  assert.match(text(dnc), /Not connected\. Details appear here once you connect\.$/);
  assert.deepEqual(o.errs, []);
});

test("Files with no computer selected says so, never the word null", async () => {
  const o = await open();
  const { A } = o;
  A.go("files"); await sleep(300);
  const p = A.mainPane;
  p.host = null;
  const html = p.stateHTML();
  assert.doesNotMatch(html, /null|undefined/);
  assert.match(html, /No computer selected/);
  assert.deepEqual(o.errs, []);
});


test("a computer record with no name or address says so in Files, never null and never a stray dot", async () => {
  const o = await open();
  const { A, E } = o;
  A.go("files"); await sleep(300);
  const s = E.servers[0];
  assert.ok(s, "a seeded computer");
  const p = A.mainPane;
  p.host = s.id;
  s.state = "disconnected";
  for (const [name, host] of [[null, "10.0.0.7"], [undefined, ""], ["", null]]) {
    s.name = name; s.host = host;
    const html = p.stateHTML();
    assert.doesNotMatch(html, /null|undefined/, "name " + name + ", host " + host);
    assert.doesNotMatch(html, /· \./, "no dot after an empty address");
    assert.match(html, /Unnamed computer/);
  }
  s.name = null; s.host = "10.0.0.7";
  assert.match(p.stateHTML(), /Unnamed computer<\/b> · 10\.0\.0\.7\./);
  assert.equal(A.hostName(s.id), "Unnamed computer");
  assert.deepEqual(o.errs, []);
});
