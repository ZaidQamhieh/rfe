// Tools, Favorites and Available offline (the v5 mockup details):
//  - every favorite row ends in a red trash-can button (aria-label "Remove from favorites") that
//    removes it from X.favs and saves the change;
//  - an available-offline item shows its progress in the same cell as its name, not further down the row.
// The real window scripts (ui/*.js) in jsdom on the stub core, like rail-collapse.test.mjs.
//
//   node --test desktop/ui-tests/fav-offline.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
import { readLegacyCss } from "./css-lib.mjs";
const css = readLegacyCss();
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** A window on the stub core; `store` stands in for the app's saved settings. */
async function open(store = new Map()) {
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
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (store.has(k) ? store.get(k) : null), setItem: (k, v) => store.set(k, String(v)), removeItem: (k) => store.delete(k) }, configurable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, errs, store };
}

const FAVS = [
  { host: "h1", path: "/home/sam/Projects/garden-plan", name: "garden-plan", t: "dir" },
  { host: "h1", path: "/home/sam/Photos", name: "Photos", t: "dir" },
  { host: "h2", path: "/Users/sam/Documents/Thesis", name: "Thesis", t: "dir" },
];
const saved = (store) => JSON.parse(store.get("rfe-tonal-user-v1") || "{}");

/** Opens Tools, Favorites with the given favorites (and offline items). */
async function toolsPage(extra = {}) {
  const o = await open();
  o.A.X.favs.splice(0, o.A.X.favs.length, ...FAVS.map((f) => ({ ...f })));
  if (extra.pins) o.A.X.pins.splice(0, o.A.X.pins.length, ...extra.pins);
  o.A.X.tool = "fav";
  o.A.go("tools");
  await sleep(150);
  o.A.pageTools();
  return o;
}

test("each favorite has a trash-can remove button at the far end of its row", async () => {
  const { d, errs } = await toolsPage();
  const rows = Array.from(d.querySelectorAll(".k-lrow")).filter((r) => r.querySelector('[data-fx^="fav.rm"]'));
  assert.equal(rows.length, FAVS.length, "one row per favorite");
  for (const r of rows) {
    const b = r.querySelector('[data-fx^="fav.rm"]');
    assert.equal(b.tagName, "BUTTON", "a real button, reachable by keyboard");
    assert.equal(b.getAttribute("aria-label"), "Remove from favorites");
    assert.ok(r.lastElementChild.classList.contains("k-hov") && r.lastElementChild.lastElementChild === b, "the last control in the row (in its hover cell)");
    assert.ok(b.querySelector(".ic"), "carries an icon");
    assert.match(b.innerHTML, /M10 11v6M14 11v6|M4 7h16/, "the trash-can icon, not the old cross");
    assert.ok(!b.hasAttribute("tabindex") || Number(b.getAttribute("tabindex")) >= 0, "not taken out of the tab order");
    assert.ok(!b.hidden && !b.disabled);
  }
  assert.deepEqual(errs, []);
});

test("pressing the trash can removes that favorite from X.favs and saves it", async () => {
  const { d, A, store, errs } = await toolsPage();
  d.querySelectorAll('[data-fx="fav.rm|1"]')[0].click();
  await sleep(50);
  assert.deepEqual(Array.from(A.X.favs, (f) => f.name), ["garden-plan", "Thesis"]);
  assert.deepEqual(saved(store).favs.map((f) => f.name), ["garden-plan", "Thesis"], "persisted");
  assert.equal(d.querySelectorAll('[data-fx^="fav.rm"]').length, 2, "the page redraws without it");
  assert.ok(!d.body.textContent.includes("Photos"), "the removed favorite is gone from the page");
  assert.deepEqual(errs, []);
});

test("the remove button is red and only fades in on hover or focus, in a separate CSS block at the end", () => {
  const i = css.lastIndexOf("v5 favorites and offline");
  assert.ok(i > css.length - 4000, "own block near the end of app.css");
  const block = css.slice(i);
  assert.match(block, /\.fvrm[^{]*\{[^}]*var\(--err\)/, "uses the destructive token");
  assert.match(block, /\.lrow:hover \.fvrm|\.lrow:focus-within \.fvrm/, "shown on row hover / focus");
  assert.match(block, /\.fvrm:focus-visible/, "visible when focused");
  assert.doesNotMatch(block, /display:\s*none/, "never display:none, so keyboard users can reach it");
  assert.doesNotMatch(block, /gradient|box-shadow:[^;]*(rgba|#)[^;]*\d+px [^;]*\d+px|border-left/, "no stripe, glow or gradient");
  assert.doesNotMatch(block, /#[0-9a-fA-F]{3,8}\b/, "tokens only, no literal colours");
});

test("an available-offline item shows its progress in the same cell as its name", async () => {
  const pins = [
    { host: "h1", path: "/a/bed-layout.pdf", name: "bed-layout.pdf", b: 2400000, at: 1, tid: "t-running" },
    { host: "h1", path: "/a/planting.md", name: "planting.md", b: 6000, at: 1, tid: null },
  ];
  const o = await open();
  o.A.E.task = (id) => (id === "t-running" ? { id, state: "running", done: 62, bytes: 100 } : null);
  o.A.X.pins.splice(0, o.A.X.pins.length, ...pins);
  o.A.X.tool = "fav"; o.A.go("tools"); await sleep(150); o.A.pageTools();
  const d = o.d;
  const rows = Array.from(d.querySelectorAll(".k-lrow")).filter((r) => r.querySelector('[data-fx^="pin.rm"]'));
  assert.equal(rows.length, 2);
  const [run, done] = rows;
  const name = run.querySelector("b");
  assert.equal(name.textContent, "bed-layout.pdf");
  const prog = run.querySelector(".k-offb");
  assert.ok(prog, "a progress cell");
  assert.equal(prog.parentElement, run, "the progress cell is one cell of the same row as the name");
  assert.match(run.querySelector("small").textContent, /^Downloading 62% of /, "the line under the name says how far it is");
  assert.equal(prog.querySelector("em").textContent, "62%");
  assert.ok(prog.querySelector(".k-bar i"), "a thin bar");
  assert.equal(prog.querySelector("i").style.width, "62%");
  assert.equal(prog.querySelector('[role="progressbar"]').getAttribute("aria-valuenow"), "62");
  assert.equal(done.querySelector(".k-bar"), null, "a finished item has no progress bar");
  assert.ok(done.querySelector(".k-offb.ok"), "a finished item shows the done mark");
  assert.deepEqual(o.errs, []);
});
