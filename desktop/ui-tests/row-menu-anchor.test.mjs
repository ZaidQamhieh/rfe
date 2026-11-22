// The "more" button of a Files row opens its menu at the button, not in the window's corner (rfe-6wt.21): clicking the button of a
// row that is not selected yet redraws the rows, and the button must be measured before that (a detached button measures 0,0).
//
//   node --test desktop/ui-tests/row-menu-anchor.test.mjs

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

const MENU_W = 224, MENU_H = 96, BTN = { left: 900, bottom: 420 };

test("the more button of a computer row opens its menu under the button after the row is selected", async () => {
  const { w, d, A, errs } = await open();
  const more = d.querySelector('#stage .n.pc .ia [data-a="more"]');
  assert.ok(more, "an online computer row has a More button");
  /* jsdom has no layout: a button still in the page is at BTN, one the redraw removed measures 0,0 like in a browser, a menu has a size */
  w.Element.prototype.getBoundingClientRect = function () {
    const box = (l, t, wd, h) => ({ left: l, top: t, right: l + wd, bottom: t + h, width: wd, height: h, x: l, y: t });
    if (this.classList && this.classList.contains("menu")) return box(0, 0, MENU_W, MENU_H);
    if (this.isConnected && this.matches && this.matches('[data-a="more"]')) return box(BTN.left, BTN.bottom - 28, 28, 28);
    return box(0, 0, 0, 0);
  };
  more.click(); await sleep(80);
  const menu = d.querySelector(".menu");
  assert.ok(menu, "the menu opened");
  assert.equal(parseFloat(menu.style.top), BTN.bottom, "the menu starts at the button's bottom edge, not at the corner (8px)");
  assert.equal(parseFloat(menu.style.left), BTN.left - 150, "the menu is placed from the button's left edge");
  assert.deepEqual(errs, [], "no script errors");
});
