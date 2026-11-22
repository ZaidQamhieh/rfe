// The navigation rail collapses to icons and expands again: a button in the rail's top row and
// Ctrl+. do the same thing, and the choice is kept in the settings (railWide), also across a
// restart. The real window scripts (ui/*.js) in jsdom on the stub core, like window.test.mjs.
//
//   node --test desktop/ui-tests/rail-collapse.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** A window `width` px wide on the stub core. `store` stands in for the app's saved settings and can outlive the window. */
async function open(width = 1400, store = new Map()) {
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
      Object.defineProperty(w, "innerWidth", { value: width, configurable: true, writable: true });
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (store.has(k) ? store.get(k) : null), setItem: (k, v) => store.set(k, String(v)), removeItem: (k) => store.delete(k) }, configurable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, errs, store, done: () => w.close() };
}
const app = (d) => d.querySelector("#app");
const btn = (d) => d.querySelector("#rail [data-menu]");
const ctrlDot = (w, d, init = {}) => d.dispatchEvent(new w.KeyboardEvent("keydown", { key: ".", ctrlKey: true, bubbles: true, cancelable: true, ...init }));

test("the button in the rail's top row collapses the rail to icons and expands it again, and stays in view", async () => {
  const { d, A, errs, done } = await open();
  assert.equal(app(d).classList.contains("collapsed"), true, "icons only to start with");
  assert.equal(app(d).classList.contains("wide"), false);
  const first = d.querySelector("#rail").firstElementChild;
  assert.ok(first.classList.contains("lg") && first.contains(btn(d)), "the button sits in the rail's first row, the logo row");
  assert.equal(btn(d).tagName, "BUTTON", "a real button, so it takes the keyboard focus");
  assert.equal(btn(d).getAttribute("aria-expanded"), "false");
  btn(d).click();
  assert.equal(app(d).classList.contains("collapsed"), false, "the click expands it");
  assert.equal(app(d).classList.contains("wide"), true);
  assert.equal(btn(d).getAttribute("aria-expanded"), "true");
  assert.equal(d.activeElement, btn(d), "focus stays on the button");
  assert.ok(d.querySelector("#rail").firstElementChild.contains(btn(d)), "still in the first row when expanded");
  btn(d).click();
  assert.equal(app(d).classList.contains("collapsed"), true, "the next click collapses it");
  assert.equal(A.S.railWide, false);
  assert.equal(btn(d).hidden, false, "still visible when collapsed");
  assert.deepEqual(errs, []);
  done();
});

test("the button says what it does and names Ctrl+. in its label and tooltip", async () => {
  const { d, done } = await open();
  assert.equal(btn(d).getAttribute("aria-label"), "Expand navigation");
  assert.match(btn(d).title, /Expand navigation/);
  assert.match(btn(d).title, /Ctrl\+\./);
  assert.match(btn(d).title, /Ctrl\+\\/, "Ctrl+\\ is named too");
  assert.equal(btn(d).getAttribute("aria-keyshortcuts"), "Control+Period Control+Backslash");
  btn(d).click();
  assert.equal(btn(d).getAttribute("aria-label"), "Collapse navigation");
  assert.match(btn(d).title, /Collapse navigation \(Ctrl\+\. or Ctrl\+\\\)/);
  done();
});

test("Ctrl+. toggles the rail like the button, also while typing, and not with other modifiers or an open dialog", async () => {
  const { w, d, A, errs, done } = await open();
  const ev = ctrlDot(w, d);
  assert.equal(ev, false, "the shortcut is taken: the default action is prevented");
  assert.equal(app(d).classList.contains("collapsed"), false, "Ctrl+. expands");
  ctrlDot(w, d);
  assert.equal(app(d).classList.contains("collapsed"), true, "Ctrl+. collapses");
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: ".", metaKey: true, bubbles: true }));
  assert.equal(app(d).classList.contains("collapsed"), false, "Cmd+. does the same");
  ctrlDot(w, d, { shiftKey: true }); ctrlDot(w, d, { altKey: true });
  assert.equal(app(d).classList.contains("collapsed"), false, "Ctrl+Shift+. and Ctrl+Alt+. are not this shortcut");
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: ".", bubbles: true }));
  assert.equal(app(d).classList.contains("collapsed"), false, "a plain full stop does nothing");
  const q = d.querySelector("#q"); q.focus();
  q.dispatchEvent(new w.KeyboardEvent("keydown", { key: ".", ctrlKey: true, bubbles: true, cancelable: true }));
  assert.equal(app(d).classList.contains("collapsed"), true, "works from the search box too");
  A.dialog({ title: "Open", body: "x", actions: [{ label: "Close", kind: "f" }] });
  assert.ok(d.querySelector(".scrim"), "a dialog is open");
  ctrlDot(w, d);
  assert.equal(app(d).classList.contains("collapsed"), true, "a dialog keeps the keys to itself");
  assert.deepEqual(errs, []);
  done();
});

test("a narrow window gets the labels as a drawer from the button and from Ctrl+., and Escape closes it", async () => {
  const { w, d, A, errs, done } = await open(1024);
  ctrlDot(w, d);
  assert.equal(app(d).classList.contains("drawer"), true);
  assert.equal(app(d).classList.contains("collapsed"), false);
  assert.equal(A.S.railWide, false, "a drawer is for the moment: nothing saved");
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert.equal(app(d).classList.contains("collapsed"), true);
  assert.deepEqual(errs, []);
  done();
});

test("the choice survives a redraw, a page change and a restart", async () => {
  const { w, d, A, errs, store, done } = await open();
  btn(d).click();
  A.renderAll();
  assert.equal(app(d).classList.contains("collapsed"), false, "a redraw keeps it expanded");
  assert.equal(btn(d).getAttribute("aria-expanded"), "true");
  A.go("settings"); A.go("files");
  assert.equal(app(d).classList.contains("collapsed"), false, "changing the page keeps it");
  const saved = JSON.parse(store.get("rfe-tonal-settings-v1"));
  assert.equal(saved.railWide, true, "written to the saved settings");
  done();
  const again = await open(1400, store);
  assert.equal(again.A.S.railWide, true, "read back at the next start");
  assert.equal(app(again.d).classList.contains("collapsed"), false, "the next start opens expanded");
  ctrlDot(again.w, again.d);
  assert.equal(app(again.d).classList.contains("collapsed"), true);
  assert.equal(JSON.parse(store.get("rfe-tonal-settings-v1")).railWide, false, "and the shortcut saves too");
  assert.deepEqual([...errs, ...again.errs], []);
  again.done();
});

test("the shortcut is listed in the keyboard shortcuts and the command palette; Ctrl+K opens Search", async () => {
  const { w, d, A, done } = await open();
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "F1", bubbles: true, cancelable: true }));
  const dlg = d.querySelector(".dlg");
  assert.ok(dlg, "the shortcuts dialog");
  const kbd = [...dlg.querySelectorAll(".kbd")].find((k) => k.textContent === "Ctrl+.");
  assert.ok(kbd, "Ctrl+. is in the list");
  assert.match(kbd.nextElementSibling.textContent, /navigation/i);
  const find = [...dlg.querySelectorAll(".kbd")].find((k) => k.textContent === "Ctrl+K or /");
  assert.ok(find, "Ctrl+K is listed on the one Search row");
  assert.equal(find.nextElementSibling.textContent, "Search");
  assert.ok(![...dlg.querySelectorAll(".kbd")].some((k) => k.textContent === "Ctrl+K"), "no separate Ctrl+K palette row");
  for (const s of d.querySelectorAll(".scrim")) s.remove();
  // Ctrl+K opens the Search panel, not the palette; the palette is still reachable as A.palette().
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "k", ctrlKey: true, bubbles: true, cancelable: true }));
  await sleep(50);
  assert.ok(d.querySelector("#srp.b-sr.open"), "Ctrl+K opens the Search panel");
  assert.equal(d.querySelector("#pl"), null, "and not the command palette");
  A.v5Pages.panel.close(); await sleep(50);
  A.palette();
  await sleep(50);
  const item = [...d.querySelectorAll("#pl [data-i]")].find((i) => /Collapse or expand the navigation/.test(i.textContent));
  assert.ok(item, "the palette lists it");
  assert.match(item.textContent, /Ctrl\+\./, "with its shortcut");
  item.click();
  assert.equal(app(d).classList.contains("collapsed"), false, "choosing it expands the rail");
  done();
});
