// A computer whose start folder is unknown shows a quiet note, not a row named null.
//
//   node --test desktop/ui-tests/files-no-start-folder.test.mjs

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

const rowTexts = (d) => [...d.querySelectorAll(".tr .n")].map((n) => n.textContent.replace(/\s+/g, " ").trim());

test("a computer with no start folder shows a note, not a row named null", async () => {
  const { d, A, E, errs } = await open();
  const s = E.servers[0];
  assert.equal(s.state, "online", "the first computer of the stub core is online");
  const before = rowTexts(d);
  assert.ok(before.length > 1, "before: the tree has rows");
  s.virtualRoot = null; /* the seeded computer lists places; a computer without them starts from its start folder */
  const real = E.start;
  E.start = (h) => (h === s.id ? null : real(h));
  A.renderAll(); await sleep(80);
  const rows = rowTexts(d);
  assert.ok(rows.some((t) => t.includes("No folders to show on this computer yet")), "the quiet note shows: " + rows.join(" | "));
  assert.ok(!rows.some((t) => /(^|\s)null(\s|$)/.test(t)), "no row named null: " + rows.join(" | "));
  assert.deepEqual(errs, [], "no script errors");
});
