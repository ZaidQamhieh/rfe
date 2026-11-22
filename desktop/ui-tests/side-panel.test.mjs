// The side panel (details of the selection, Local files) and the device details:
//  a. every action is a same-size bordered button with an icon and one short word; Delete is red text;
//  b. what a device may do is edited in place (switches that save on the click), no "Change"/Permissions dialog;
//  c. an Open button, and "Open without downloading" in the file menu.
//  d. nothing in the new rules is a coloured stripe, glow or gradient; the ids the panel hangs on still exist.
// Same jsdom harness as window.test.mjs.
//
//   node --test desktop/ui-tests/side-panel.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
import { readAllCss } from "./css-lib.mjs";
const css = readAllCss();
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const waitFor = async (fn, ms = 4000) => { for (let t = 0; t < ms; t += 25) { if (fn()) return; await sleep(25); } };
const NAS = "192.168.1.20:7443";
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

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
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
/** Browses a stub folder and selects its first file (`kind` "file") or folder (`kind` "dir"). */
async function select(ctx, kind) {
  const { A, E } = ctx; const dir = kind === "file" ? "/srv/projects/atlas/releases/2026.10" : "/srv/projects/atlas";
  await E.fs.load(NAS, dir); A.mainPane.go(dir); await sleep(100);
  const it = (E.fs.list(NAS, dir) || []).find((x) => x.t === kind);
  assert.ok(it, "the stub has a " + kind + " in " + dir);
  A.mainPane.sel = new Set([it.n]); A.mainPane.selChanged(); await sleep(50);
  return it;
}
/* The v5 panel: bordered `.k-btn` buttons (an icon and `span.bl` with the word) in `.k-dact`; Rename, Save to and Delete of a single item sit in the "…" menu (`.k-ib`). */
const buttons = (d) => [...d.querySelectorAll("#detail .k-dact .k-btn")];
const word = (b) => text(b.querySelector(".bl"));
/** Opens the panel's "…" menu and returns its items: label text and whether the item is the red (danger) one. */
async function moreMenu(ctx) {
  const { d } = ctx; d.querySelector('#detail [data-do="more"]').click(); await sleep(30);
  return [...d.querySelectorAll(".menu .mi")].map((m) => ({ label: text(m), danger: m.classList.contains("danger") }));
}

test("a. a selected file: every action is a bordered button with an icon and one short word; Delete is the red one in the … menu", async () => {
  const ctx = await open(); const { d, errs } = ctx;
  await select(ctx, "file");
  const bs = buttons(d);
  assert.deepEqual(bs.map((b) => b.dataset.do), ["download", "open", "share"]);
  for (const b of bs) {
    assert.ok(b.querySelector("svg, .ic"), b.dataset.do + " has an icon");
    assert.match(word(b), /^[A-Za-z]+( link…)?$/, b.dataset.do + " has a short label, got '" + word(b) + "'");
    assert.ok(word(b).split(" ").length <= 2);
  }
  assert.equal(d.querySelectorAll("#detail a[data-do]").length, 0, "no loose text link acts in the panel");
  const more = d.querySelector('#detail .k-row .k-ib[data-do="more"]');
  assert.ok(more && more.querySelector("svg, .ic") && more.getAttribute("aria-label") && more.title, "the … button is a square icon button with a name and a tooltip");
  const items = await moreMenu(ctx);
  for (const l of ["Rename", "Save to…", "Delete"]) assert.ok(items.some((i) => i.label.startsWith(l)), "the … menu has " + l + ": " + items.map((i) => i.label).join(", "));
  assert.deepEqual(items.filter((i) => i.danger).map((i) => i.label.replace(/Del$/, "")), ["Delete"], "Delete is the one red item");
  assert.deepEqual(errs, []);
});

test("a. a folder, a selection of several, the empty selection and the Local tab follow the same rule", async () => {
  const ctx = await open(); const { d, A, errs } = ctx;
  await select(ctx, "dir");
  assert.deepEqual(buttons(d).map((b) => b.dataset.do), ["download", "open"], "a folder has no Share link");
  A.mainPane.sel.clear(); A.mainPane.selChanged(); await sleep(50);
  assert.deepEqual(buttons(d).map((b) => b.dataset.do), ["newfolder", "upload"]);
  assert.deepEqual(buttons(d).map(word), ["New", "Upload"]);
  const items = A.E.fs.list(NAS, A.mainPane.path); A.mainPane.sel = new Set(items.slice(0, 2).map((x) => x.n)); A.mainPane.selChanged(); await sleep(50);
  const bs = buttons(d);
  assert.deepEqual(bs.map((b) => b.dataset.do), ["download", "delete"]);
  assert.ok(bs.every((b) => b.querySelector("svg, .ic")));
  assert.ok(bs[1].classList.contains("rm") && word(bs[1]) === "Delete", "Delete is the red one and says so in words");
  assert.ok(!bs[0].classList.contains("rm"));
  // the v5 page has no Local files tab: a file on "This computer" is selected in the tree and the same panel offers one bordered Upload button
  const lp = A.E.start("local"); await A.E.fs.load("local", lp); A.mainPane.setHost("local"); await sleep(100);
  const lf = (A.E.fs.list("local", A.mainPane.path) || []).find((x) => x.t === "file");
  assert.ok(lf, "the local folder has a file");
  A.mainPane.sel = new Set([lf.n]); A.mainPane.selChanged(); await sleep(50);
  const up = d.querySelector('#detail .k-dact .k-btn[data-do="download"]');
  assert.ok(up, "the panel has its upload button for a local file");
  assert.equal(word(up), "Upload");
  assert.ok(up.querySelector("svg, .ic"), "with an icon");
  assert.deepEqual(errs, []);
});

test("a. the CSS gives the panel's buttons one size and a border, and uses the red text token for the destructive one", () => {
  const rule = (sel) => { const m = css.match(new RegExp("(?:^|\\n)" + sel.replace(/[.\\[\\]()]/g, "\\$&") + "\\{([^}]*)\\}")); return m && m[1]; };
  const base = rule(".da .btn");
  assert.ok(base, "a rule for the panel buttons");
  assert.match(base, /height:2\.5rem/); assert.match(base, /width:100%/); assert.match(base, /border:1px solid/);
  assert.match(rule(".da .btn.e"), /color:var\(--err\)/);
  assert.match(css, /--err:#b42318;--err-fill:#b42318/, "in the light theme the red text and the red fill are one colour");
  assert.match(rule(".stabs2 button.on"), /background:var\(--sec-c\);border-color:var\(--sel-bd\)/, "selected = light gray fill + thin border");
});

test("c. a file offers Open in the panel and 'Open without downloading' in its menu; Open shows the preview", async () => {
  const ctx = await open(); const { d, A, E, errs } = ctx;
  await select(ctx, "file"); /* opens the folder; its first file (an installer) has no viewer, so Open is disabled for it (see panel-visuals) */
  const file = (E.fs.list(NAS, "/srv/projects/atlas/releases/2026.10") || []).find((x) => x.n === "release-notes.md");
  A.mainPane.sel = new Set([file.n]); A.mainPane.selChanged(); await sleep(50);
  const menu = A.mainPane.ctxItems(true).filter((i) => i && i.label);
  const o = menu.find((i) => i.label === "Open without downloading");
  assert.ok(o, "menu item: " + menu.map((i) => i.label).join(", "));
  assert.ok(menu.some((i) => i.label === "Download"), "Download is still there");
  assert.ok(!menu.some((i) => i.label === "Preview"), "no second 'Preview' row for the same thing");
  assert.ok(o.icon, "with an icon");
  // the Open button hands the file to the preview, which streams it from the computer
  let got = null; const prev = A.previewDialog; A.previewDialog = (h, dir, n) => { got = n.n; };
  d.querySelector('#detail [data-do="open"]').click(); await sleep(30);
  A.previewDialog = prev;
  assert.equal(got, file.n);
  // a folder opens into the folder instead
  const dir = await select(ctx, "dir");
  const dm = A.mainPane.ctxItems(true).find((i) => i && i.label === "Open");
  assert.ok(dm, "a folder's menu says Open");
  d.querySelector('#detail [data-do="open"]').click(); await sleep(100);
  /* the v5 tree "goes into" a folder by opening it in place: its branch is now expanded (the old page changed the pane's path) */
  assert.ok(A.v5Files.open.has(NAS + "\u0001" + A.mainPane.path + "/" + dir.n), "Open opened the branch of " + dir.n + ": " + [...A.v5Files.open].join(" | "));
  assert.deepEqual(errs, []);
});

/** Opens the details of a fake device whose grants start as `grants`, recording every set_device_access call. */
async function deviceDetails(grants, over = {}) {
  const ctx = await open(); const { w, A, E } = ctx;
  const srv = E.servers.find((s) => s.id === NAS) || E.servers[0];
  const calls = []; const orig = E.call.bind(E);
  ctx.failNext = false;
  E.call = (name, args) => {
    if (name === "set_device_access") { calls.push(JSON.parse(JSON.stringify(args))); return ctx.failNext ? Promise.reject(new Error("the computer said no")) : Promise.resolve(null); }
    if (name === "list_devices") return Promise.resolve([]);
    return orig(name, args);
  };
  A.pairServer = () => srv;
  const dev = { id: "dev-1", name: "Pixel 9", last: E.now(), paired: E.now() - 864e5, a: { ...grants }, ...over };
  A.X.devices = [dev]; A.X.devAt = E.now();
  A.deviceDetails("dev-1"); await sleep(60);
  ctx.calls = calls; ctx.dev = dev; ctx.dlg = () => w.document.querySelector(".dlg");
  return ctx;
}

test("b. device details hold the switches: no Permissions dialog, no Save step, each click saves", async () => {
  const ctx = await deviceDetails({ download: true, upload: false, modify: false, delete: false, share: false, viewApps: false, launchApps: false, readOnly: false });
  const { d, calls, errs } = ctx;
  const dlg = ctx.dlg();
  assert.ok(dlg, "the details open");
  assert.equal(d.querySelectorAll(".dlg").length, 1);
  const sw = (k) => dlg.querySelector('[data-c="' + k + '"]');
  assert.equal(dlg.querySelectorAll(".sw[data-c]").length, 9, "eight grants and Read-only");
  assert.equal(sw("download").getAttribute("aria-checked"), "true");
  assert.equal(sw("upload").getAttribute("aria-checked"), "false");
  const labels = [...dlg.querySelectorAll(".btn")].map(text);
  assert.ok(!labels.some((l) => /Permissions|Change|Save$/.test(l)), "no Permissions/Change/Save button: " + labels.join(" | "));
  sw("upload").click(); await sleep(40);
  assert.equal(calls.length, 1, "one click, one call, nothing else to press");
  assert.deepEqual(calls[0].patch, { upload: true });
  assert.equal(calls[0].id, "dev-1");
  assert.equal(sw("upload").getAttribute("aria-checked"), "true");
  assert.equal(d.querySelectorAll(".dlg").length, 1, "no second dialog");
  sw("readOnly").click(); await sleep(40);
  assert.deepEqual(calls[1].patch, { readOnly: true });
  assert.deepEqual(errs, []);
});

test("b. Launch apps turns See apps on with it, and See apps off turns Launch off with it", async () => {
  const ctx = await deviceDetails({ download: true, viewApps: false, launchApps: false });
  const { calls } = ctx; const dlg = ctx.dlg();
  const sw = (k) => dlg.querySelector('[data-c="' + k + '"]');
  sw("launchApps").click(); await sleep(40);
  assert.deepEqual(calls[0].patch, { launchApps: true, viewApps: true });
  assert.equal(sw("viewApps").getAttribute("aria-checked"), "true");
  sw("viewApps").click(); await sleep(40);
  assert.deepEqual(calls[1].patch, { viewApps: false, launchApps: false });
  assert.equal(sw("launchApps").getAttribute("aria-checked"), "false");
});

test("b. a refused save puts the switch back and says why", async () => {
  const ctx = await deviceDetails({ download: true, delete: false });
  const dlg = ctx.dlg(); ctx.failNext = true;
  const b = dlg.querySelector('[data-c="delete"]');
  b.click(); await waitFor(() => !b.disabled && b.getAttribute("aria-checked") === "false" && /the computer said no/.test(text(ctx.d.body)));
  assert.equal(b.getAttribute("aria-checked"), "false", "back to what the device really may do");
  assert.ok(!b.disabled, "the switch can be tried again");
  assert.match(text(ctx.d.body), /the computer said no/);
});

test("b. the Permissions button and menu item of a device row open the same in-place details", async () => {
  const ctx = await deviceDetails({ download: true });
  for (const x of ctx.d.querySelectorAll(".scrim")) x.remove(); ctx.A.X.devices = [ctx.dev]; ctx.A.fxHandlers["dev.perm"]("dev-1"); await waitFor(() => ctx.dlg()?.querySelector('.sw[data-c="download"]'));
  assert.ok(ctx.dlg().querySelector('.sw[data-c="download"]'), "switches, not a second kind of dialog");
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 1);
});

test("b/a. device details: Revoke and Remove are bordered red-text buttons with an icon; a revoked device has no switches", async () => {
  const ctx = await deviceDetails({ download: true });
  const acts = [...ctx.dlg().querySelectorAll(".btn.e")];
  assert.deepEqual(acts.map(text), ["Revoke", "Remove"]);
  assert.ok(acts.every((b) => b.querySelector("svg, .ic")));
  const rv = await deviceDetails({ download: true }, { revoked: true });
  assert.equal(rv.dlg().querySelectorAll(".sw[data-c]").length, 0, "nothing to edit on a revoked device");
  assert.deepEqual([...rv.dlg().querySelectorAll(".btn.e")].map(text), ["Remove"]);
});

test("d. the ids the panel hangs on are still there, and the new rules have no stripe, glow or gradient", async () => {
  const { d, errs } = await open();
  for (const id of ["btnSide", "rszSide", "side"]) assert.ok(d.getElementById(id), "#" + id);
  const rules = ["\\.da \\.btn", "\\.da \\.btn\\.f", "\\.da \\.btn\\.e", "\\.stabs2 button\\.on", "\\.rsz-side:hover"]
    .map((s) => (css.match(new RegExp("(?:^|\\n)" + s + "\\{([^}]*)\\}")) || [])[1]);
  assert.ok(rules.every(Boolean), "all rules found");
  for (const r of rules) assert.doesNotMatch(r, /gradient|border-left|border-right|box-shadow:[^;]*(rgba|#)/, r);
  assert.deepEqual(errs, []);
});
