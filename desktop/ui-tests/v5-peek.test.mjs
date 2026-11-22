// Hover-to-peek: the icons-only sidebar opens over the page while the pointer rests on it and closes when it leaves.
//
//   node --test desktop/ui-tests/v5-peek.test.mjs

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

const app = (d) => d.getElementById("app");

test("a collapsed sidebar opens on hover, closes on leave, and stays collapsed after a redraw", async () => {
  const { w, d, A } = await open();
  A.toggleRail(); await sleep(50);
  if (!app(d).classList.contains("sbmin")) A.toggleRail();
  await sleep(50);
  assert.ok(app(d).classList.contains("sbmin"), "collapsed to start");
  const item = d.querySelector("#rail .k-navi");
  item.dispatchEvent(new w.MouseEvent("mouseover", { bubbles: true }));
  assert.ok(app(d).classList.contains("sbmin"), "not at once: a pass of the pointer does not open it");
  await sleep(300);
  assert.ok(app(d).classList.contains("sbpeek") && !app(d).classList.contains("sbmin"), "open over the page");
  A.renderAll(); await sleep(50);
  assert.ok(app(d).classList.contains("sbpeek"), "a redraw keeps it open while the pointer is on it");
  d.querySelector("#rail .k-navi").dispatchEvent(new w.MouseEvent("mouseout", { bubbles: true, relatedTarget: d.querySelector("#stage") }));
  assert.ok(!app(d).classList.contains("sbpeek") && app(d).classList.contains("sbmin"), "closed on leave");
});

test("a pass of the pointer shorter than the delay does not open it; Escape closes an open one", async () => {
  const { w, d, A } = await open();
  A.toggleRail(); await sleep(50);
  if (!app(d).classList.contains("sbmin")) A.toggleRail();
  await sleep(50);
  const item = d.querySelector("#rail .k-navi");
  item.dispatchEvent(new w.MouseEvent("mouseover", { bubbles: true }));
  item.dispatchEvent(new w.MouseEvent("mouseout", { bubbles: true, relatedTarget: d.querySelector("#stage") }));
  await sleep(300);
  assert.ok(!app(d).classList.contains("sbpeek"), "never opened");
  item.dispatchEvent(new w.MouseEvent("mouseover", { bubbles: true })); await sleep(300);
  assert.ok(app(d).classList.contains("sbpeek"));
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert.ok(app(d).classList.contains("sbmin"), "Escape closes it");
});
