// The Settings page is scaled up to the v5 mockup: size tokens and rules sit in one labelled block at the end of
// ui/app.css, add no colours, and the page still draws its navigation and sections on the stub core.
//
//   node --test desktop/ui-tests/settings-scale.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

import { readAllCss } from "./css-lib.mjs";
const css = readAllCss();
const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

const MARK = "v5 Settings page scale-up";
const start = css.indexOf(MARK);
// the block runs to the next labelled block (other branches add blocks after it)
const nextMark = css.slice(start + MARK.length).search(/\n\/\* (?:={3,}|-{3,}) /);
const block = nextMark < 0 ? css.slice(start) : css.slice(start, start + MARK.length + nextMark);

/** Reads a custom property's rem value from the labelled block. */
function rem(name) {
  const m = block.match(new RegExp("--" + name + ":([0-9.]+)rem"));
  assert.ok(m, "missing token --" + name);
  return parseFloat(m[1]);
}

test("the scale-up block is labelled and sits after the base settings rules", () => {
  assert.ok(css.includes(MARK), "block label");
  assert.equal(css.lastIndexOf(MARK), css.indexOf(MARK));
  assert.ok(css.indexOf(MARK) > css.indexOf(".setl{display:grid"), "block comes after the base settings rules");
});

test("size tokens are at least the v5 mockup scale (nav 44px, row 76px, text 15px, controls 36-40px)", () => {
  assert.ok(rem("set-nav-w") >= 15 && rem("set-nav-w") * 16 <= 280);
  assert.ok(rem("set-nav-h") >= 2.75, "nav button height");
  assert.ok(rem("set-row-h") >= 4.5, "row height");
  assert.ok(rem("set-text") >= 1, "row text");
  assert.ok(rem("set-small") >= 0.875, "hint text");
  assert.ok(rem("set-ctl-h") >= 2.5, "button/input height");
  assert.ok(rem("set-seg-h") >= 2.25, "segmented control height");
  assert.ok(rem("set-gap") >= 2, "space between nav and content");
  for (const sel of [".setn button{height:var(--set-nav-h)", ".sr{min-height:var(--set-row-h)", ".sr .seg{height:var(--set-seg-h)", ".sr .btn{height:var(--set-ctl-h)", ".sr input.tx{height:var(--set-ctl-h)"]) assert.ok(block.includes(sel), sel);
});

test("the block adds no new colours, gradients, glows or stripes", () => {
  assert.doesNotMatch(block.replace(/\/\*[\s\S]*?\*\//g, ""), /#[0-9a-fA-F]{3,6}\b|gradient|rgba?\(|text-shadow|border-left|box-shadow/);
});

test("it still collapses to one column when the page is narrow", () => {
  assert.match(block, /@container \(max-width:\d+rem\)\{\.setl\{grid-template-columns:minmax\(0,1fr\)\}\}/);
});

const live_css = block.replace(/\/\*[\s\S]*?\*\//g, "");

test("the section heading is shown at 24px (1.5rem) and no dead h3 rule remains in the block", () => {
  const m = live_css.match(/\.sg\.setp>h3\{([^}]*)\}/);
  assert.ok(m, "heading rule for the open section");
  assert.match(m[1], /display:block/);
  assert.match(m[1], /font:500 1\.5rem/);
  assert.doesNotMatch(live_css, /\.sg h3\{/, "the old rule is overridden by display:none, so it must not come back");
  // the only thing hiding the heading is `.setp>h3{display:none}` (specificity 0,1,1); the new rule must outrank it
  assert.match(css, /\.setp>h3\{display:none\}/);
  assert.ok(css.indexOf(".setp>h3{display:none}") < css.indexOf(".sg.setp>h3{"), "shown rule comes later");
});

test("Settings rows have 1px dividers from an existing border token", () => {
  assert.match(live_css, /\.sr\+\.sr\{border-top:1px solid var\(--outline-var\)\}/);
  assert.match(css, /--outline-var:/);
});

test("the Settings page drops the 5.5rem bottom padding so the 11-item list needs no vertical scroll", () => {
  assert.match(live_css, /\.stage\.page:has\(\.setl\)\{padding-bottom:1\.5rem\}/);
  assert.ok(rem("set-nav-h") >= 2.5, "nav buttons stay at least 2.5rem");
});

// Item 1: the floating Transfers button sits about 6.25rem above the bottom of the page area (measured 100px at 16px
// per rem); the page padding (1.5rem) plus the content column's own margin must clear it once, not twice, so the
// 11-item list still fits at 1280x800 and 1280x720 while the end of a long section never sits under the button.
test("the end of the content column keeps room for the floating Transfers button (padding + margin, counted once)", () => {
  const pad = parseFloat(live_css.match(/\.stage\.page:has\(\.setl\)\{padding-bottom:([0-9.]+)rem\}/)[1]);
  const mm = live_css.match(/\.setl>\.setp\{margin-bottom:([0-9.]+)rem\}/);
  assert.ok(mm, "margin-bottom rule on the content column");
  const margin = parseFloat(mm[1]);
  assert.ok(pad + margin >= 6.75, "padding + margin clears the button (6.25rem) with a little gap: " + (pad + margin));
  assert.ok(pad + margin <= 8, "and no more than needed, so a short list stays unscrolled: " + (pad + margin));
  assert.equal(css.match(/\.setl>\.setp\{margin-bottom/g).length, 1, "one margin rule, not a second one elsewhere");
});

// Item 6: calmer shapes and no card around the section; item 5: hover is lighter than the selected row.
test("nav items and segmented controls use the 8px radius; only the floating button stays a pill", () => {
  assert.match(live_css, /\.setn button\{[^}]*border-radius:\.5rem\}/);
  assert.match(live_css, /\.sr \.seg\{[^}]*border-radius:\.5rem\}/);
  assert.doesNotMatch(live_css, /\.setn button\{[^}]*border-radius:(1|1\.\d+)rem/);
  assert.match(css, /\.spill\{[^}]*border-radius:1\.5rem/, "the floating Transfers button keeps its pill shape");
});

test("the section is plain content: no border or fill, row dividers also under the last row", () => {
  assert.match(live_css, /\.sg\.setp,:root\[data-contrast="high"\] \.sg\.setp\{background:none;border:0;/);
  assert.match(live_css, /\.sg\.setp \.sr:last-child\{border-bottom:1px solid var\(--outline-var\)\}/);
  assert.match(live_css, /\.sr\+\.sr\{border-top:1px solid var\(--outline-var\)\}/);
});

test("nav hover is lighter than the selected fill (selected keeps its fill and thin border)", () => {
  assert.match(live_css, /\.setn button:hover:not\(\.on\)\{background:var\(--sc\)\}/);
  assert.match(css, /\.setn button\.on\{background:var\(--sec-c\);[^}]*box-shadow:inset 0 0 0 1px var\(--sel-bd\)/);
});

// Item 2: the Security hint never stays on "Counting…" when the phone list cannot be loaded.
test("Security > Sign out all phones: a failed load shows 'Could not count phones', not 'Counting…'", async () => {
  const { d, A, errs } = await open();
  A.X.devTried = false; A.X.devAt = 0; A.X.devErr = ""; A.X.devFail = false;
  A.loadDevices = () => Promise.reject(new Error("offline"));
  d.querySelector('[data-snav="Security"]').click();
  await sleep(250);
  const hint = (t) => [...d.querySelectorAll(".k-sbody .k-set")].find((r) => r.firstElementChild.querySelector("b").textContent === "Sign out all phones").firstElementChild.querySelector("small").textContent;
  assert.equal(hint(), "Could not count phones");
  assert.doesNotMatch(d.querySelector(".k-sbody").textContent, /Counting…/);
  assert.deepEqual(errs, []);
});

// Item 4 and item 7: row names and hint lines.
test("Desktop has a 'Test notification' row (no 'Try it'); Appearance, Troubleshooting and Backup carry hint lines", async () => {
  const { d, errs } = await open();
  const rows = (sec) => { d.querySelector('[data-snav="' + sec + '"]').click(); return [...d.querySelectorAll(".k-sbody .k-set")].map((r) => [r.firstElementChild.querySelector("b").textContent, (r.firstElementChild.querySelector("small") || {}).textContent || ""]); };
  const desk = rows("Desktop").map((r) => r[0]);
  assert.ok(desk.includes("Test notification"), "row named Test notification");
  assert.ok(!desk.includes("Try it"), "no Try it row");
  const hinted = (sec) => rows(sec).filter((r) => r[1]).length;
  assert.ok(hinted("Appearance") >= 3, "Appearance hints");
  assert.ok(hinted("Troubleshooting") >= 3, "Troubleshooting hints");
  assert.ok(hinted("Backup") >= 1, "Backup hint");
  assert.deepEqual(errs, []);
});

// Item 3: plain words by default; the technical sentence sits in a closed "Technical details" fold.
test("Settings shows plain words: no Tauri, TLS, keystore, pinned or certificate outside a closed Technical details fold", async () => {
  const { d, errs } = await open();
  const names = [...d.querySelectorAll(".k-snav button")].map((b) => b.dataset.snav);
  for (const n of names) {
    d.querySelector('[data-snav="' + n + '"]').click();
    const clone = d.querySelector(".k-sbody").cloneNode(true);
    for (const f of clone.querySelectorAll("details")) f.remove();
    const txt = clone.textContent;
    assert.doesNotMatch(txt, /Tauri|\bTLS\b|keystore|pinned|certificate|checksum|Agent log|Device key/i, n + ": " + txt.slice(0, 120));
  }
  d.querySelector('[data-snav="About"]').click();
  const fold = d.querySelector(".k-sbody details.b-tech");
  assert.ok(fold, "About has a Technical details fold");
  assert.equal(fold.open, false, "closed to start with");
  assert.match(fold.textContent, /Tauri/);
  assert.match(fold.textContent, /pinned TLS/);
  assert.deepEqual(errs, []);
});

test("renamed rows have de/es/ar translations", () => {
  const src = readFileSync(fileURLToPath(new URL("../ui/fx5.js", import.meta.url)), "utf8");
  for (const s of ["Check every download", "Computers you trust", "Computer log", "Check secure storage", "This computer’s identity", "Could not count phones", "Technical details"]) {
    assert.ok(src.includes("['" + s + "', '"), "translation row for " + s);
  }
  assert.ok(!src.includes("['Try it'") && !src.includes("['Pinned certificates'") && !src.includes("['Verify checksums'"), "no row for a removed English string");
});

async function open() {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 300)));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1&view=settings",
    beforeParse(w) {
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.TextEncoder = TextEncoder; w.TextDecoder = TextDecoder;
      w.ResizeObserver = class { observe() {} disconnect() {} };
      w.HTMLElement.prototype.scrollIntoView = function () {};
      Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
    },
  });
  const w = dom.window; live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, errs };
}

test("the Settings page draws its section list and rows, in both themes", async () => {
  const { d, A, errs } = await open();
  for (const theme of ["light", "dark"]) {
    d.documentElement.setAttribute("data-theme", theme);
    A.pageSettings();
    const nav = [...d.querySelectorAll(".k-snav button")].map((b) => b.textContent);
    for (const want of ["Appearance", "Transfers", "Connection", "Help"]) assert.ok(nav.includes(want), theme + " nav: " + want);
    assert.equal(d.querySelectorAll(".k-snav button.on").length, 1, "one selected section");
    assert.ok(d.querySelectorAll(".k-sbody .k-set").length >= 1, "rows in the open section");
    assert.ok(d.querySelector(".k-sbody .k-set b"), "row title");
  }
  for (const name of ["Transfers", "Appearance", "Help"]) {
    d.querySelector('[data-snav="' + name + '"]').click();
    assert.ok(d.querySelector(name === "Help" ? ".k-sbody .k-keys > div" : ".k-sbody .k-set"), name + " has rows");
  }
  assert.deepEqual(errs, []);
});
