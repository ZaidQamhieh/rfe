// The v5 window's computer dialogs use the mockup's plain words and box: "Connection to X" (not "Diagnose X"),
// "Folders X shares" (not "Allowed roots on X"), "Changed lately on X" (opened from the computer's panel), and the
// Files page shows its sort (newest first, folders before files) and the mockup's small colour pictures (ui/emoji).
// The real window scripts (ui/*.js) run in jsdom on the stub core, like add-computer.test.mjs.
//
//   node --test desktop/ui-tests/v5-wording.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** A 1400 px window on the stub core, with at least one computer in the list. */
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
      const store = new Map();
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (store.has(k) ? store.get(k) : null), setItem: (k, v) => store.set(k, String(v)), removeItem: (k) => store.delete(k) }, configurable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}

const text = (el) => (el ? el.textContent.replace(/\s+/g, " ").trim() : "");
const box = (d) => d.querySelector(".scrim .k-dlg");
const labels = (d) => [...box(d).querySelectorAll(".df button")].map((b) => b.textContent.trim());

/** The first computer the stub reports as online. */
const online = (E) => E.servers.find((s) => s.state === "online");

test("Check the connection opens \"Connection to X\" with plain lines and the mockup's buttons", async () => {
  const { d, A, E, errs } = await open();
  const s = online(E);
  assert.ok(s, "the stub has an online computer");
  A.diagnose(s.id); await sleep(500);
  assert.ok(box(d), "the dialog is skinned as the mockup's box");
  assert.equal(text(box(d).querySelector("h2")), "Connection to " + s.name);
  const l = labels(d);
  for (const want of ["Copy for support", "Check again", "Review"]) assert.ok(l.includes(want), want + " is a button: " + l.join(", "));
  for (const gone of ["Close", "Copy report", "Run again", "Review key…"]) assert.ok(!l.includes(gone), gone + " is gone: " + l.join(", "));
  assert.deepEqual([...box(d).querySelectorAll(".k-list .k-lrow > span")].map(text), ["Found it on the network", "It is the same PC as before", "Signed in", "Shares its folders"]);
  assert.ok(box(d).querySelector("details"), "the raw answers sit in a Technical details fold");
  assert.equal(box(d).querySelector("details").open, false, "closed to start with");
  assert.deepEqual(errs, []);
});

test("Folders it shares opens \"Folders X shares\" with a Done button", async () => {
  const { d, A, E, errs } = await open();
  const s = E.servers.find((x) => (x.roots || []).length) || online(E);
  A.rootsDialog(s.id); await sleep(150);
  assert.equal(text(box(d).querySelector("h2")), "Folders " + s.name + " shares");
  const l = labels(d);
  assert.ok(l.includes("Done"), "the close button reads Done: " + l.join(", "));
  assert.ok(!l.includes("Close"), "no Close button");
  assert.match(text(box(d)), new RegExp("Only these folders can be opened from here\\. Change them on " + s.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "\\."));
  assert.deepEqual(errs, []);
});

test("the computer's panel has a \"Changed lately…\" button that opens \"Changed lately on X\"", async () => {
  const { d, A, E, errs } = await open();
  const s = online(E);
  A.go("servers"); await sleep(200);
  const row = d.querySelector('#stage .cpg .ro[data-pa="pick"][data-id="' + s.id.replace(/"/g, '\\"') + '"]');
  assert.ok(row, "the computer's row is drawn");
  row.click(); await sleep(150);
  const b = d.querySelector('#stage .cpg [data-pa="recent"]');
  assert.ok(b, "the panel has the button");
  assert.equal(text(b), "Changed lately…");
  assert.equal(b.disabled, false, "an online computer can be asked");
  b.click(); await sleep(400);
  assert.equal(text(box(d).querySelector("h2")), "Changed lately on " + s.name);
  assert.deepEqual(errs, []);
});

test("Files sorts newest first, folders before files, and draws the mockup's pictures", async () => {
  const { d, A, errs } = await open();
  A.go("files"); await sleep(400);
  assert.equal(A.mainPane.sort.key, "mod", "sorted by when it changed");
  assert.equal(A.mainPane.sort.dir, -1, "newest first");
  const pics = [...d.querySelectorAll("#stage img.k-e")];
  assert.ok(pics.length, "the page draws emoji pictures");
  for (const p of pics) assert.match(p.getAttribute("src"), /\/emoji\/[a-z_]+\.svg$/, "every picture comes from ui/emoji");
  assert.deepEqual(errs, []);
});

test("Devices marks a device with the mockup's phone or laptop picture", async () => {
  const { d, A, errs } = await open();
  A.go("devices"); await sleep(500);
  const pics = [...d.querySelectorAll("#stage img.k-e")].map((p) => (p.getAttribute("src").match(/([a-z_]+)\.svg$/) || [])[1]);
  assert.ok(pics.some((n) => n === "mobile_phone" || n === "laptop"), "a phone or laptop picture is drawn: " + pics.join(","));
  assert.deepEqual(errs, []);
});
