// A pairing server picked with a chip in the pairing dialog survives a redraw of the Computers page: the page points
// pairing at the computer it shows only when that computer changes, or when the picked one is no longer usable.
//
//   node --test desktop/ui-tests/d2-pair-chip-pick.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The real window scripts on the stub core, with two computers online; returns them as a (shown) and b (picked). */
async function open() {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
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
  const online = w.A.E.servers.filter((s) => s.state === "online");
  assert.ok(online.length >= 2, "the stub has two online computers");
  return { w, A: w.A, X: w.A.X, DV: w.A.v5Devices, a: online[0], b: online[1], errs };
}

test("a chip pick survives a redraw of the page that shows another computer", async () => {
  const { X, DV, a, b, errs } = await open();
  DV.sectionFor(a);
  assert.equal(X.pair.host, a.id, "the first draw points pairing at the shown computer");
  X.pair.host = b.id; X.pair.code = "B-CODE"; X.devices = [{ id: "dev-b" }]; // the chip pick, and what it loaded
  DV.sectionFor(a);
  DV.sectionFor(a);
  assert.equal(X.pair.host, b.id, "the picked computer is kept");
  assert.equal(X.pair.code, "B-CODE", "its code is kept");
  assert.equal(X.devices.length, 1, "its device list is kept");
  assert.deepEqual(errs, []);
});

test("showing a different computer points pairing at it and clears the code", async () => {
  const { X, DV, a, b } = await open();
  DV.sectionFor(a);
  X.pair.host = b.id; X.pair.code = "B-CODE";
  DV.sectionFor(b);
  assert.equal(X.pair.host, b.id);
  DV.sectionFor(a);
  assert.equal(X.pair.host, a.id, "the shown computer changed, so pairing follows it");
  assert.equal(X.pair.code, "", "the old code is dropped");
});

test("a picked computer that is no longer online is replaced by the shown one", async () => {
  const { X, DV, a, b } = await open();
  DV.sectionFor(a);
  X.pair.host = b.id; X.pair.code = "B-CODE";
  b.state = "offline";
  DV.sectionFor(a);
  assert.equal(X.pair.host, a.id, "pairing falls back to the shown computer");
  assert.equal(X.pair.code, "");
});
