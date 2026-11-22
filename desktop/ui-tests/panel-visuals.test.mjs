// Visual fixes found in the side-panel review, checked as CSS text and as DOM in the jsdom harness:
//  1. the preview tile is flat (no gradient); 2. labels truncate with an ellipsis, one column at the narrowest width;
//  3. Delete/Revoke/Remove borders match the other buttons; 4. the always-on switch has its own locked-on style;
//  5. "Save to" opens the folder picker in place; 6. Open is disabled, with a reason, when nothing can show the file;
//  7. room under the Transfers button on Devices; 8. the snackbar clears an open dialog;
//  9. de/es/ar rows for the words the device fold and the permissions list use.
//
//   node --test desktop/ui-tests/panel-visuals.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const here = (p) => fileURLToPath(new URL(p, import.meta.url));
const page = here("../ui-dev/harness.html");
import { readAllCss } from "./css-lib.mjs";
const css = readAllCss();
const fx5 = readFileSync(here("../ui/fx5.js"), "utf8");
const uijs = readFileSync(here("../ui/ui.js"), "utf8");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const NAS = "192.168.1.20:7443";
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The text of the first rule in app.css whose selector list is exactly `sel` (empty when there is none). */
function rule(sel) {
  const esc = sel.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const all = [...css.matchAll(new RegExp("(?:^|\\})\\s*" + esc + "\\s*\\{([^}]*)\\}", "gm"))];
  return all.map((m) => m[1]).join(";");
}

/** The window on the stub core, with every script error collected in `errs`. */
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
    },
  });
  const w = dom.window; live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}
/** Browses the stub release folder and selects the file called `name`. */
async function selectFile(ctx, name) {
  const { A, E } = ctx; const dir = "/srv/projects/atlas/releases/2026.10";
  await E.fs.load(NAS, dir); A.mainPane.go(dir); await sleep(100);
  const it = (E.fs.list(NAS, dir) || []).find((x) => x.n === name);
  assert.ok(it, "the stub has " + name);
  A.mainPane.sel = new Set([it.n]); A.mainPane.selChanged(); await sleep(50);
  return it;
}

test("1. the preview tile of an image or video is flat: no gradient, no hue colours, the kind's icon on a neutral tile", async () => {
  assert.ok(!/\.dprev\.art/.test(css), "no .dprev.art rule is left");
  assert.doesNotMatch(rule(".dprev"), /gradient/);
  assert.match(rule(".dprev"), /background:var\(--tert-c\)/);
  const m = uijs.match(/A\.previewHTML = [\s\S]*?\n  \};/);
  assert.ok(m, "found previewHTML");
  assert.doesNotMatch(m[0], /hsl\(|--a:|--b:|dprev art/, "the tile no longer carries hue colours");
  const ctx = await open();
  const img = await selectFile(ctx, "hero.png");
  const tile = ctx.d.querySelector("#detail .dprev");
  /* the v5 panel shows the picture itself once its thumbnail has arrived, and the kind's icon until then */
  assert.ok(tile && tile.querySelector("svg, .ic, img"), "the image has a preview area with its picture or an icon: " + img.n);
  assert.ok(!tile.getAttribute("style"), "no inline colours on the tile");
  assert.deepEqual(ctx.errs, []);
});

test("2. panel button labels truncate with an ellipsis and the five actions go one per row at the narrowest panel", async () => {
  const lab = rule(".da .btn .bl");
  assert.match(lab, /min-width:0/); assert.match(lab, /overflow:hidden/); assert.match(lab, /text-overflow:ellipsis/);
  assert.match(css, /\.detail\{container-type:inline-size\}/);
  assert.match(css, /@container \(max-width:19\.5rem\)\{\.detail \.da\{grid-template-columns:minmax\(0,1fr\)\}\}/);
  const ctx = await open();
  await selectFile(ctx, "build.log");
  assert.ok(ctx.d.querySelectorAll("#detail .k-dact .k-btn").length >= 2, "the panel has its action buttons");
  for (const b of ctx.d.querySelectorAll("#detail .k-dact .k-btn")) {
    assert.ok(b.querySelector("span.bl"), b.dataset.do + " wraps its word in span.bl");
    assert.ok(b.title, b.dataset.do + " has a tooltip so a cut word can still be read");
  }
  assert.deepEqual(ctx.errs, []);
});

test("3. destructive buttons use the same border colour as the other buttons", () => {
  for (const sel of [".btn.e", ".da .btn.e"]) {
    const r = rule(sel);
    assert.match(r, /color:var\(--err\)/, sel + " keeps the red text");
    assert.match(r, /border-color:var\(--outline\)(;|$)/, sel + " uses --outline, like .btn");
    assert.doesNotMatch(r, /outline-var/, sel + " no longer uses the faint divider colour");
  }
  assert.match(rule(".btn"), /border:1px solid var\(--outline\)/);
  assert.match(rule(".da .btn"), /border:1px solid var\(--outline\)/);
});

test("4. the always-on switch has a locked-on style that reads as on", async () => {
  const lock = rule(".sw.lock,.sw.lock:disabled");
  assert.match(lock, /background:var\(--accent\)/, "filled like a switch that is on");
  assert.match(lock, /opacity:\.[6-9]/, "not the .38 fade of a disabled control");
  assert.match(rule(".sw.lock::after"), /background:var\(--on-accent\)/);
  const ctx = await open();
  const html = ctx.A.grantRows({ a: {}, viaLogin: false });
  const host = ctx.d.createElement("div"); host.innerHTML = html;
  const lk = host.querySelector(".sw.lock");
  assert.ok(lk && lk.classList.contains("on") && lk.disabled && lk.getAttribute("aria-checked") === "true", "Browse files is on, locked and says so");
  assert.equal(host.querySelectorAll(".sw.lock").length, 1, "only the always-on row is locked");
});

test("5. Save to opens the folder picker in place and keeps the file selected", async () => {
  const ctx = await open(); const { A, E, d } = ctx;
  const f = await selectFile(ctx, "build.log");
  const calls = []; const orig = E.call.bind(E); E.call = (n, a) => { calls.push(n); return orig(n, a); };
  const view = A.state.view;
  /* in the v5 panel "Save to…" is an item of the "…" menu */
  d.querySelector('#detail [data-do="more"]').click(); await sleep(30);
  const item = [...d.querySelectorAll(".menu .mi")].find((m) => m.textContent.trim() === "Save to…");
  assert.ok(item, "the … menu has Save to…");
  item.click(); await sleep(150);
  assert.ok(calls.includes("choose_download_folder"), "the picker command ran: " + calls.join(","));
  assert.equal(A.state.view, view, "still on the file list, not Settings");
  assert.deepEqual([...A.mainPane.selected()].map((x) => x.n), [f.n], "the file is still selected");
  assert.match(d.querySelector("#detail").textContent, /Saves to\s*\S*Chosen/, "the Saves to row (in Technical details) shows the folder just picked");
  assert.deepEqual(ctx.errs, []);
});

test("6. Open is disabled, with the reason as its tooltip, when no viewer can show the file", async () => {
  const ctx = await open(); const { A, d } = ctx;
  await selectFile(ctx, "atlas-2.0.0-release.apk");
  let o = d.querySelector('#detail [data-do="open"]');
  assert.ok(o.disabled, "an installer package has nothing to open");
  assert.match(o.title, /Nothing to show/, "the tooltip says why: " + o.title);
  let hit = 0; const prev = A.previewDialog; A.previewDialog = () => { hit++; };
  o.click(); await sleep(30); A.previewDialog = prev;
  assert.equal(hit, 0, "a disabled Open does not open the preview");
  await selectFile(ctx, "release-notes.md");
  o = d.querySelector('#detail [data-do="open"]');
  assert.ok(!o.disabled, "a small text file still opens: " + o.title);
  await selectFile(ctx, "build.log");
  o = d.querySelector('#detail [data-do="open"]');
  assert.ok(o.disabled && /Too large/.test(o.title), "a 2 MB log is too large to preview: " + o.title);
  const big = { n: "huge.txt", t: "file", b: 5 << 20, perm: "-rw-r--r--", own: "me", mod: 1 };
  assert.match(A.openBlock(NAS, big), /Too large/, "a text file over 1 MB is blocked as too large");
  assert.equal(A.openBlock(NAS, { t: "dir", n: "x" }), "", "a folder always opens");
  assert.deepEqual(ctx.errs, []);
});

test("7. the Devices page keeps room under the floating Transfers button", () => {
  assert.match(rule(".stage.page:has(.dvg)"), /padding-bottom:(\d+(\.\d+)?)rem/);
  const rem = Number(rule(".stage.page:has(.dvg)").match(/padding-bottom:([\d.]+)rem/)[1]);
  assert.ok(rem >= 7.5, "at least the height of the pill plus its offset, got " + rem + "rem");
});

test("8. a snackbar sits below an open dialog instead of covering it", () => {
  assert.match(rule("body:has(.scrim .dlg) .snacks"), /bottom:\.75rem/);
  const dl = rule("body:has(.scrim .dlg):has(.snack) .scrim .dlg");
  assert.match(dl, /max-height:calc\(100vh - 9rem\)/); assert.match(dl, /margin-bottom:5\.5rem/);
});

test("9. German, Spanish and Arabic rows exist for the device fold and the permissions list", async () => {
  const ctx = await open(); const { MAP } = ctx.A.fx5;
  for (const k of ["Technical details", "Allowed", "Signed in with the account: file actions are always allowed", "All files (account)", "No access. Revoked.", "The app does not play video.", "Nothing to show for this type of file. Download it instead.", "Too large to preview. Download it instead.", "Download to", "Choose the folder downloads are saved in", "Address", "Done"]) {
    for (const l of ["de", "es", "ar"]) assert.ok(MAP[l][k] && MAP[l][k] !== k, l + " has a row for “" + k + "”");
  }
  assert.equal(MAP.de["Technical details"], "Technische Details");
  assert.match(fx5, /'Technical details', 'Technische Details'/);
  assert.deepEqual(ctx.errs, []);
});

test("2. the reason Open is greyed out is a visible line under the buttons, tied to Open with aria-describedby", async () => {
  const ctx = await open(); const { A, d } = ctx;
  await selectFile(ctx, "atlas-2.0.0-release.apk");
  const o = d.querySelector('#detail [data-do="open"]'); const why = d.querySelector("#detail #dwhy");
  assert.ok(o.disabled && why, "Open is off and a line explains it");
  assert.equal(o.getAttribute("aria-describedby"), "dwhy");
  assert.match(why.textContent, /Nothing to show for this type of file/);
  assert.equal(why.textContent, o.title, "the visible line and the tooltip say the same");
  assert.ok(why.closest(".k-dact"), "it sits with the action buttons, below them");
  assert.ok(why.previousElementSibling && why.previousElementSibling.matches(".k-btn"), "after the last button");
  await selectFile(ctx, "release-notes.md");
  assert.equal(d.querySelector("#detail #dwhy"), null, "no line when Open works");
  assert.equal(d.querySelector('#detail [data-do="open"]').getAttribute("aria-describedby"), null);
  assert.deepEqual(ctx.errs, []);
});

test("2. a video says The app does not play video. as the reason", async () => {
  const ctx = await open(); const { A, d } = ctx;
  assert.equal(A.openBlock(NAS, { n: "clip.mkv", t: "file", b: 1e6, perm: "-rw-r--r--", own: "me", mod: 1 }), "The app does not play video.");
  /* the 412 MB mp4 is a type the player handles but is over the 256 MiB cap, so its reason is the size one */
  await selectFile(ctx, "demo-walkthrough.mp4");
  assert.equal(d.querySelector("#detail #dwhy").textContent, "This video is too big to play here. Download it instead.");
});

test("2. the reason line and the Download to row are translated", async () => {
  const ctx = await open(); const { A, d } = ctx;
  A.S.lang = "de"; A.applyLang();
  await selectFile(ctx, "atlas-2.0.0-release.apk"); await sleep(80);
  const det = d.querySelector("#detail");
  assert.match(det.querySelector("#dwhy").textContent, /Für diesen Dateityp/);
  assert.match(det.textContent, /Speichert in/, "the Saves to row is translated");
  assert.match(det.textContent, /Vollständiger Pfad/, "so are the other Technical details rows");
  assert.equal(det.querySelector('[data-do="more"]').title, "Umbenennen, Speicherort, Löschen und mehr");
  A.S.lang = "en"; A.applyLang();
});
