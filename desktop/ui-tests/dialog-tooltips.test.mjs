// Tooltips inside dialogs open upward (above their trigger), so the dialog's edge and scrolling
// body no longer clip them, like the v5 mockup (mockups/v5/kit.css: ".k-dlg [data-tip]"). The
// tooltip is one fixed element on <body>, so no dialog overflow or stacking context can cut it;
// it flips below the trigger only when there is no room above.
//
//   node --test desktop/ui-tests/dialog-tooltips.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
import { readAllCss, readLegacyCss } from "./css-lib.mjs";
const css = readLegacyCss();
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The rules of the labelled tooltip block at the end of app.css, as a list of [selector, body]. */
function block() {
  const at = css.lastIndexOf("/* ===== v5 dialog tooltips");
  assert.ok(at > 0, "a labelled 'v5 dialog tooltips' block exists");
  const rest = css.slice(at);
  assert.equal(css.indexOf("/* =====", at + 10), -1, "the block is the last labelled block, at the end of app.css");
  const text = rest.replace(/\/\*[\s\S]*?\*\//g, "");
  return [...text.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => [m[1].trim(), m[2]]);
}
const rule = (sel) => block().find(([s]) => s.split(",").map((x) => x.trim()).includes(sel));

test("the tooltip is a fixed element above every dialog layer", () => {
  const r = rule(".tip");
  assert.ok(r, ".tip rule");
  assert.match(r[1], /position:\s*fixed/);
  assert.match(r[1], /pointer-events:\s*none/);
  const z = +/z-index:\s*(\d+)/.exec(r[1])[1];
  const before = css.slice(0, css.lastIndexOf("/* ===== v5 dialog tooltips"));
  for (const m of before.matchAll(/z-index:\s*(\d+)/g)) assert.ok(z > +m[1], "above z-index " + m[1]);
});

test("the dialog tooltip opens upward: its animation rises from the trigger and its arrow sits on the bottom edge", () => {
  const up = rule(".tip.up");
  assert.ok(up, ".tip.up rule");
  assert.match(up[1], /--tip-dy:\s*\.125rem/, "starts a little lower and rises into place");
  const down = rule(".tip.down");
  assert.ok(down, ".tip.down rule for the flipped case");
  assert.match(down[1], /--tip-dy:\s*-\.125rem/);
});

test("no dialog rule clips overflow in a way that could cut the tooltip, and the tooltip is not placed inside a dialog", () => {
  // the tooltip lives on <body>, outside .scrim/.dlg/.db, so their overflow cannot clip it
  const r = rule(".tip");
  assert.doesNotMatch(r[1], /position:\s*absolute/);
  assert.doesNotMatch(block().map((b) => b[0]).join(" "), /\.dlg|\.db|\.scrim/, "the block adds no dialog selector that could change dialog overflow");
  // the v5 foundation sheets (linked after the block) may name those classes only under the inert .d5 wrapper
  const v5 = readAllCss().slice(css.length).replace(/\/\*[\s\S]*?\*\//g, "").replace(/@font-face\{[^}]*\}/g, "");
  const loose = [...v5.matchAll(/([^{};]+)\{[^{}]*\}/g)].flatMap((m) => m[1].split(",")).map((s) => s.trim()).filter((s) => /\.dlg\b|\.db\b|\.scrim\b/.test(s) && !/^:where\(\.d5\)/.test(s) && !/^\.stage\.v5x /.test(s)); /* the Overview page's own .db blocks live under .stage.v5x, in the page, never in a dialog */
  assert.deepEqual(loose, [], "v5 sheets: every selector on a dialog class is scoped under .d5 or the Overview page (.stage.v5x)");
});

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
      // jsdom has no layout: the tooltip is 120 x 26 px; other boxes are set per test by box()
      const rect = w.Element.prototype.getBoundingClientRect;
      w.Element.prototype.getBoundingClientRect = function () { return this.classList && this.classList.contains("tip") ? { left: 0, top: 0, width: 120, height: 26, right: 120, bottom: 26, x: 0, y: 0 } : rect.call(this); };
      Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
      Object.defineProperty(w, "innerWidth", { value: 1400, configurable: true, writable: true });
      Object.defineProperty(w, "innerHeight", { value: 900, configurable: true, writable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, errs };
}

/** Gives an element a box (jsdom has no layout). */
function box(el, left, top, width, height) {
  el.getBoundingClientRect = () => ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top });
}
const hover = (w, el) => el.dispatchEvent(new w.MouseEvent("mouseover", { bubbles: true, relatedTarget: null }));
const tip = (d) => d.querySelector(".tip");

/** Opens a dialog with one trigger carrying `attr` and returns it, laid out at (left, top). */
function dialogWith(A, w, attr, top) {
  A.dialog({ title: "Test", body: '<button class="ib" id="tt" ' + attr + ' aria-label="Remove">x</button>', actions: [{ label: "Close" }] });
  const t = w.document.querySelector("#tt");
  box(t, 600, top, 32, 32);
  return t;
}

test("a data-tip trigger in a dialog shows its label above the trigger, on <body>", async () => {
  const { w, d, A, errs } = await open();
  const t = dialogWith(A, w, 'data-tip="Remove this link"', 400);
  hover(w, t);
  await sleep(700);
  const p = tip(d);
  assert.ok(p, "a tooltip element exists");
  assert.equal(p.parentElement, d.body, "it is outside the dialog, so the dialog cannot clip it");
  assert.equal(p.textContent, "Remove this link");
  assert.ok(p.classList.contains("up") && !p.classList.contains("down"));
  assert.equal(p.getAttribute("role"), "tooltip");
  assert.equal(parseFloat(p.style.top), 400 - 6 - 26, "its bottom edge is 6px above the trigger");
  assert.equal(parseFloat(p.style.left), 600 + 16 - 60, "centred on the trigger");
  assert.deepEqual(errs, []);
});

test("it flips below the trigger only when there is no room above", async () => {
  const { w, d, A } = await open();
  const t = dialogWith(A, w, 'data-tip="Remove this link"', 10);
  hover(w, t);
  await sleep(700);
  const p = tip(d);
  assert.ok(p.classList.contains("down") && !p.classList.contains("up"), "no room above: below");
  assert.equal(parseFloat(p.style.top), 10 + 32 + 6);
});

test("the pure placement keeps the tooltip inside the window and prefers up in a dialog, down elsewhere", async () => {
  const { A } = await open();
  const at = (r, size, inDlg) => A.tipPlace(r, size, { w: 1000, h: 800 }, inDlg);
  const r = { left: 500, top: 300, right: 532, bottom: 332 };
  assert.equal(at(r, { w: 100, h: 24 }, true).side, "up");
  assert.equal(at(r, { w: 100, h: 24 }, false).side, "down");
  assert.equal(at({ left: 500, top: 5, right: 532, bottom: 37 }, { w: 100, h: 24 }, true).side, "down", "no room above");
  assert.equal(at({ left: 500, top: 770, right: 532, bottom: 800 }, { w: 100, h: 24 }, false).side, "up", "no room below");
  assert.equal(at({ left: 0, top: 300, right: 10, bottom: 320 }, { w: 100, h: 24 }, true).left, 4, "kept off the left edge");
  assert.equal(at({ left: 990, top: 300, right: 1000, bottom: 320 }, { w: 100, h: 24 }, true).left, 1000 - 100 - 4, "kept off the right edge");
  assert.equal(at({ left: 0, top: 5, right: 10, bottom: 15 }, { w: 100, h: 790 }, true).side, "up", "no room either way: keeps its preferred side");
});

test("a title inside a dialog becomes the styled tooltip (the native one is not clipped but is slow and un-themed), and keeps its accessible name", async () => {
  const { w, d, A } = await open();
  A.dialog({ title: "Test", body: '<button class="ib" id="tt" title="Copy the link"></button>', actions: [{ label: "Close" }] });
  const t = d.querySelector("#tt");
  box(t, 600, 400, 32, 32);
  hover(w, t);
  await sleep(700);
  assert.equal(t.hasAttribute("title"), false, "no double native tooltip");
  assert.equal(t.getAttribute("data-tip"), "Copy the link");
  assert.equal(t.getAttribute("aria-label"), "Copy the link", "the name that title gave is kept");
  assert.equal(tip(d).textContent, "Copy the link");
});

test("a title outside any dialog is left alone", async () => {
  const { w, d } = await open();
  const b = d.querySelector("#rail [data-menu]");
  const before = b.getAttribute("title");
  assert.ok(before);
  hover(w, b);
  await sleep(700);
  assert.equal(b.getAttribute("title"), before);
  assert.equal(tip(d), null);
});

test("the tooltip goes away when the pointer leaves, a key is pressed, or the dialog closes", async () => {
  const { w, d, A } = await open();
  const t = dialogWith(A, w, 'data-tip="Remove this link"', 400);
  hover(w, t);
  await sleep(700);
  assert.equal(tip(d).classList.contains("on"), true);
  t.dispatchEvent(new w.MouseEvent("mouseout", { bubbles: true, relatedTarget: d.body }));
  assert.equal(tip(d).classList.contains("on"), false, "gone on mouse out");
  hover(w, t);
  await sleep(700);
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert.equal(tip(d).classList.contains("on"), false, "gone on a key");
  hover(w, t);
  await sleep(700);
  t.closest(".scrim").remove();
  await sleep(50);
  assert.equal(tip(d).classList.contains("on"), false, "gone when its trigger is removed");
});

test("an icon-only dialog button with only an aria-label gets its label as the tooltip, and keeps its aria-label", async () => {
  const { w, d, A, errs } = await open();
  A.dialog({ title: "Preview", body: '<button class="ib" id="zo" aria-label="Zoom out"></button>', actions: [{ label: "Close" }] });
  const t = d.querySelector("#zo");
  box(t, 600, 400, 32, 32);
  hover(w, t);
  await sleep(700);
  assert.equal(tip(d).textContent, "Zoom out", "the aria-label is the label");
  assert.equal(tip(d).parentElement, d.body, "it is outside the dialog");
  assert.ok(tip(d).classList.contains("up"));
  assert.equal(t.getAttribute("aria-label"), "Zoom out", "the aria-label is not changed");
  assert.equal(t.hasAttribute("title"), false, "no native tooltip is added");
  assert.equal(t.hasAttribute("data-tip"), false, "no data-tip is added");
  assert.deepEqual(errs, []);
});

test("a dialog button with visible text and an aria-label gets no tooltip", async () => {
  const { w, d, A } = await open();
  A.dialog({ title: "Preview", body: '<button class="ib" id="tx" aria-label="Open file">Open</button>', actions: [{ label: "Close" }] });
  const t = d.querySelector("#tx");
  box(t, 600, 400, 32, 32);
  hover(w, t);
  await sleep(700);
  assert.equal(tip(d), null, "no tooltip element is created");
  assert.equal(t.getAttribute("aria-label"), "Open file");
});

test("precedence: data-tip beats aria-label, and a title beats aria-label", async () => {
  const { w, d, A } = await open();
  A.dialog({ title: "Preview", body: '<button class="ib" id="dt" data-tip="Fit page" aria-label="Zoom in"></button><button class="ib" id="ti" title="Copy link" aria-label="Other name"></button>', actions: [{ label: "Close" }] });
  const dt = d.querySelector("#dt"), ti = d.querySelector("#ti");
  box(dt, 600, 400, 32, 32);
  box(ti, 600, 400, 32, 32);
  hover(w, dt);
  await sleep(700);
  assert.equal(tip(d).textContent, "Fit page");
  hover(w, ti);
  await sleep(700);
  assert.equal(tip(d).textContent, "Copy link");
  assert.equal(ti.getAttribute("aria-label"), "Other name", "the title does not change the aria-label");
});
