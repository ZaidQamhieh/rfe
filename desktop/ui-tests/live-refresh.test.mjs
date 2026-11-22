// The window refreshes the page that is open by itself: the schedule is a pure function of the
// engine (liveDue), and the window runs what is due once a second while it is visible.
//
//   node --test desktop/ui-tests/live-refresh.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const E = require("../ui/engine.js");
const due = (o) => E.liveDue({ view: "history", visible: true, active: false, now: 100000, last: {}, ...o });

test("a visible page whose data was never read is due at once, only the data that page shows", () => {
  assert.deepEqual(due({ view: "history" }), ["history"]);
  assert.deepEqual(due({ view: "devices" }), ["devices"]);
  assert.deepEqual(due({ view: "transfers" }), ["transfers"]);
  assert.deepEqual(due({ view: "files" }), ["files"]);
  assert.deepEqual(due({ view: "tools" }), ["tools"]);
  assert.deepEqual(due({ view: "settings" }), []);
});

test("a hidden window refreshes nothing", () => {
  for (const view of ["history", "devices", "transfers", "files", "tools", "home"]) assert.deepEqual(due({ view, visible: false }), [], view);
});

test("a job is not due again before its interval, and is due after it", () => {
  const ms = E.liveMs("history", false);
  assert.ok(ms > 0);
  assert.deepEqual(due({ last: { history: 100000 - ms + 1 } }), []);
  assert.deepEqual(due({ last: { history: 100000 - ms } }), ["history"]);
  assert.deepEqual(due({ last: { devices: 100000 } }), ["history"], "another page's clock does not hold this one back");
});

test("Transfers and History refresh faster while a transfer is active; the other pages do not", () => {
  assert.ok(E.liveMs("transfers", true) < E.liveMs("transfers", false));
  assert.ok(E.liveMs("history", true) < E.liveMs("history", false));
  assert.equal(E.liveMs("devices", true), E.liveMs("devices", false));
  const t = E.liveMs("transfers", true);
  assert.deepEqual(due({ view: "transfers", active: true, last: { transfers: 100000 - t } }), ["transfers"]);
  assert.deepEqual(due({ view: "transfers", active: false, last: { transfers: 100000 - t } }), []);
});

test("the Overview refreshes the transfer list too; the clock going backwards makes nothing due twice", () => {
  assert.deepEqual(due({ view: "home" }), ["transfers"]);
  assert.deepEqual(due({ last: { history: 100001 } }), []);
});

// ---- in the window ----
const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

async function open() {
  const vc = new VirtualConsole();
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
  return { w, A: w.A };
}

test("the open History page reads the computers' log again on the clock, and not while the window is hidden", async () => {
  const { w, A } = await open();
  let n = 0;
  A.syncAudit = async () => { n++; return 0; };
  A.go("history"); await sleep(100);
  assert.ok(n >= 1, "opening History reads the computers' log at once");
  n = 0;
  const t0 = Date.now() + 1e6;
  A.liveTick(t0);
  assert.equal(n, 1, "due at once on first sight");
  A.liveTick(t0 + 10);
  assert.equal(n, 1, "not twice inside the interval");
  await sleep(5); /* the first run has finished */
  A.liveTick(t0 + E.liveMs("history", false) + 10);
  assert.equal(n, 2, "due again after the interval");
  await sleep(5);
  Object.defineProperty(w.document, "visibilityState", { value: "hidden", configurable: true });
  A.liveTick(t0 + 10 * E.liveMs("history", false));
  assert.equal(n, 2, "hidden: nothing runs");
  Object.defineProperty(w.document, "visibilityState", { value: "visible", configurable: true });
  A.liveTick(t0 + 10 * E.liveMs("history", false) + 1);
  assert.equal(n, 3, "visible again: runs without waiting for a full interval of visible time");
});

test("the open Devices page reloads its list on the clock", async () => {
  const { A } = await open();
  let n = 0;
  A.loadDevices = async () => { n++; };
  A.go("devices"); await sleep(100);
  const before = n;
  A.liveTick(Date.now() + 2e6);
  assert.equal(n, before + 1);
});
