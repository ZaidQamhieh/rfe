// The Wake computer action follows the device's own wake grant (the agent's fileCapabilities.wake, read with the
// folders and kept as server.caps): a computer whose agent reports it as false offers no wake anywhere (menu, details
// button, command palette, automatic wake); true, absent or unknown hides nothing.
//
//   node --test desktop/ui-tests/wake-grant-hidden.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, with every script error collected in `errs`. `seed` pre-fills storage keys before the scripts run. */
async function open(seed = {}) {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1",
    beforeParse(w) {
      const mem = new Map(Object.entries(seed));
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (mem.has(k) ? mem.get(k) : null), setItem: (k, v) => mem.set(k, String(v)), removeItem: (k) => mem.delete(k) }, configurable: true });
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
  await sleep(300);
  return { w, d: w.document, A: w.A, E: w.A.E, T: w.A.v5Computers, errs };
}

/** An offline computer with a known MAC address and the given reported grants (null: none read yet). */
async function offline(caps) {
  const o = await open();
  const s = o.E.servers.find((x) => x.mac) || o.E.servers[0];
  s.mac = s.mac || "aa:bb:cc:dd:ee:ff";
  s.state = "offline"; s.caps = caps;
  return { o, s };
}
const labels = (o, s) => (o.A.serverMenuExtra(s) || []).filter(Boolean).map((m) => m.label);
const WAKE = "Wake computer";

test("A. the menu has no Wake computer when the agent reports the wake grant as false", async () => {
  const { o, s } = await offline({ browse: true, modify: true, wake: false });
  assert.ok(!labels(o, s).includes(WAKE), labels(o, s).join(" | "));
  assert.deepEqual(o.errs, []);
});

test("B. the menu keeps Wake computer when the grant is true, absent or not read yet", async () => {
  for (const caps of [{ browse: true, wake: true }, { browse: true }, { browse: true, wake: null }, null]) {
    const { o, s } = await offline(caps);
    assert.ok(labels(o, s).includes(WAKE), JSON.stringify(caps) + ": " + labels(o, s).join(" | "));
  }
});

test("C. the details panel and the command palette offer no wake for a computer without the grant, and do for one that has it", async () => {
  const { o, s } = await offline({ browse: true, wake: false });
  const btn = () => o.d.querySelector('.cpg [data-pa="wake"]');
  o.A.go("devices"); o.T.pick = s.id; o.A.renderAll(); await sleep(200);
  assert.equal(btn(), null, "no Wake computer button without the grant");
  const inPalette = () => { const got = []; o.A.paletteExtra.forEach((f) => f((icon, label) => got.push(label))); return got; };
  assert.ok(!inPalette().includes("Wake " + s.name), "no palette entry without the grant");
  s.caps = { browse: true, wake: true }; o.A.renderAll(); await sleep(150);
  assert.ok(btn(), "the button is back with the grant");
  assert.ok(inPalette().includes("Wake " + s.name), "the palette entry is back with the grant");
  assert.deepEqual(o.errs, []);
});

test("D. the engine keeps the wake grant the agent reported with the folders", async () => {
  const o = await open();
  const s = o.E.servers.find((x) => x.state === "online");
  o.w.StubBackend.cmds.files_roots = () => ({ locations: [], source: "roots", caps: { browse: true, wake: false } });
  await o.E.loadRoots(s);
  assert.equal(s.caps.wake, false);
  assert.ok(!labels(o, Object.assign(s, { state: "offline", mac: s.mac || "aa:bb:cc:dd:ee:ff" })).includes(WAKE));
});
