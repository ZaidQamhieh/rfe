// Break tests for the RFE desktop v5 mockup: the unslop rules (R13), the History filter line (R5) and emoji use (R14).
// Run: node --test desktop/ui-tests/mockup-v5-unslop-break.test.mjs
// Written by qa-break; not run by it.
//
// The CSS checks read the mockup's own stylesheets in the order the page links them (_frame.css, kit.css,
// base.css, then the page's inline style). A declaration counts when its selector matches the element and the
// last one wins. Specificity is not modelled, so each check names the rule it relies on. jsdom does no layout.

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole, ResourceLoader } from "jsdom";
import { fileURLToPath, pathToFileURL } from "node:url";
import { readFileSync } from "node:fs";

const DIR = fileURLToPath(new URL("../mockups/v5/", import.meta.url));
const PAGE = DIR + "01-rfe.html";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => {
  for (const w of live.splice(0)) {
    try { w.close(); } catch { /* already closed */ }
  }
});

class LocalOnly extends ResourceLoader {
  fetch(url, options) {
    if (!/^file:/i.test(url)) return Promise.resolve(Buffer.from(""));
    return super.fetch(url, options);
  }
}

/** Opens the mockup in jsdom (no network). query: "v=full" or "v=empty"; hash: e.g. "#go=servers". */
async function open(query, hash = "") {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + String((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 2).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 200)));
  const url = pathToFileURL(PAGE).href + (query ? "?" + query : "") + hash;
  const dom = await JSDOM.fromFile(PAGE, {
    url,
    runScripts: "dangerously",
    resources: new LocalOnly(),
    pretendToBeVisual: true,
    virtualConsole: vc,
    beforeParse(w) {
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.HTMLElement.prototype.scrollIntoView = function () {};
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
    },
  });
  const w = dom.window;
  live.push(w);
  await Promise.race([
    new Promise((resolve) => (w.document.readyState === "complete" ? resolve() : w.addEventListener("load", () => resolve()))),
    sleep(4000),
  ]);
  await sleep(40);
  return { w, d: w.document, K: w.K, errs };
}

const need = (el, what) => {
  assert.ok(el, "no element: " + what);
  return el;
};

/* ---------- the stylesheets, in link order ---------- */

let RULES = null;
function rules() {
  if (RULES) return RULES;
  const html = readFileSync(PAGE, "utf8");
  const inline = html.match(/<style>([\s\S]*?)<\/style>/)[1];
  const css = ["_frame.css", "kit.css", "base.css"].map((f) => readFileSync(DIR + f, "utf8")).concat([inline]).join("\n");
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, "");
  RULES = [];
  for (const m of clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const sel = m[1].trim();
    if (!sel || sel.startsWith("@")) continue;
    const decl = {};
    for (const part of m[2].split(";")) {
      const i = part.indexOf(":");
      if (i > 0) decl[part.slice(0, i).trim().toLowerCase()] = part.slice(i + 1).replace(/!important/i, "").trim();
    }
    RULES.push({ sel, decl });
  }
  return RULES;
}

const matches = (el, sel) => {
  try { return el.matches(sel); } catch { return false; }
};

/** The last declaration of `prop` whose selector matches `el`, or null. */
function declared(el, prop) {
  let value = null;
  for (const r of rules()) if (r.decl[prop] !== undefined && matches(el, r.sel)) value = r.decl[prop];
  return value;
}

/** Declared value with var(--x) replaced by the custom property's value on the window root (.win). */
function resolve(el, prop) {
  let v = declared(el, prop);
  for (let i = 0; i < 3 && v && /var\(/.test(v); i++) {
    v = v.replace(/var\((--[\w-]+)\)/g, (_, name) => declared(el.closest(".win"), name) ?? "none");
  }
  return v;
}

/** Shadow layers of a box-shadow value: top-level commas split them; colour functions are ignored. */
function layers(value) {
  const parts = [];
  let depth = 0, cur = "";
  for (const ch of value) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === "," && depth === 0) { parts.push(cur.trim()); cur = ""; } else cur += ch;
  }
  if (cur.trim()) parts.push(cur.trim());
  return parts.filter((p) => p && p !== "none").map((raw) => {
    let bare = raw;
    for (let i = 0; i < 3; i++) bare = bare.replace(/\([^()]*\)/g, "");
    const len = bare.split(/\s+/).filter((t) => /^-?\d*\.?\d+(px)?$/.test(t)).map(parseFloat);
    return { raw, inset: /\binset\b/.test(bare), x: len[0] || 0, y: len[1] || 0, blur: len[2] || 0, spread: len[3] || 0 };
  });
}

/* ---------------------------------------------------------------------------------------------------------------- */

/**
 * R13: a selected row has no coloured left stripe. Scenario: full data; the selected server row (.ro.sel) on
 * Servers and the selected event (.b-ev.sel) on History. Expected: no sideways inset shadow and no left border.
 * Code: base.css:74 draws the stripe, base.css:146 replaces it with a 1px ring; the later rule wins.
 * Label: code failure. Basis: R13. Confidence: high (a guard against the stripe returning).
 */
test("R13 stripe: a selected row has no coloured left stripe", async () => {
  const bad = [];
  for (const [hash, sel] of [["#go=servers", ".ro.sel"], ["#go=history", ".b-ev.sel"]]) {
    const { d } = await open("v=full", hash);
    const row = d.querySelector(sel);
    if (!row) { bad.push(`${hash}: no ${sel}`); continue; }
    const stripes = layers(resolve(row, "box-shadow") || "none").filter((l) => l.inset && l.x !== 0);
    if (stripes.length) bad.push(`${hash} ${sel}: ${stripes.map((l) => l.raw).join(" | ")}`);
    const bl = declared(row, "border-left");
    if (bl && bl !== "none" && bl !== "0") bad.push(`${hash} ${sel}: border-left ${bl}`);
  }
  assert.deepEqual(bad, []);
});

/**
 * R13: primary buttons are flat. Scenario: full data, Servers, the primary "Add a computer" button (.k-btn.pri).
 * Expected: no box-shadow. Code: base.css:143 sets none after kit.css:296 raised it (the later rule wins).
 * Label: code failure. Basis: R13. Confidence: high (a guard).
 */
test("R13 flat primary: the primary button has no raised or inset shadow", async () => {
  const { d } = await open("v=full", "#go=servers");
  const btn = need(d.querySelector("#st .k-btn.pri"), "a primary button on Servers");
  assert.equal(resolve(btn, "box-shadow") ?? "none", "none");
});

/**
 * R13: counts are not filled red pills. Scenario: full data, the sidebar's Devices count (.k-badge.bad, red in
 * kit.css:62). Expected: no fill. Code: base.css:149-150 sets the background to none (the later rule wins).
 * Label: code failure. Basis: R13. Confidence: high (a guard).
 */
test("R13 quiet counts: the sidebar's Devices count is not a filled red pill", async () => {
  const { d } = await open("v=full", "#go=home");
  const badge = need(d.querySelector("#side .k-badge.bad"), "the Devices count");
  const bg = resolve(badge, "background");
  assert.ok(bg === "none" || bg === "transparent", "background is " + bg);
});

/**
 * R13: no glow ring on fields or cards: each field or card has at most one shadow layer with a spread.
 * Scenario: full data for the Overview cards, the History fields, the add-a-computer step-1 field, and the search
 * field and panel. Expected: at most one spread layer per element (the one-pixel ring; the drop shadow has none).
 * Code: the fields use the --ctl value (kit.css:289, 300), the cards and the panel use one inset ring or an offset
 * drop (base.css:56-62, 125, 01-rfe.html:8-11).
 * Label: code failure. Basis: R13 (no glow ring, a second spread box-shadow, on fields or cards). Confidence:
 * high for these elements (a guard). Dialogs and menus are not checked here: whether they are "cards" is
 * not defined by the requirements.
 */
test("R13 fields and cards: each has at most one shadow layer with a spread", async () => {
  const cases = [
    ["#go=home", ".db .stat"], ["#go=home", ".db .card"], ["#go=home", ".db .chart"],
    ["#go=history", ".k-in"], ["#go=history", ".k-sel"], ["#dlg=newconn&tab=1", ".b-f"],
    ["#sr", ".b-sr .s-f"], ["#sr", ".b-sr .m"],
  ];
  const bad = [];
  for (const [hash, sel] of cases) {
    const { d } = await open("v=full", hash);
    const els = [...d.querySelectorAll(sel)];
    if (!els.length) { bad.push(`${hash} ${sel}: no element to check`); continue; }
    for (const el of els) {
      const spread = layers(resolve(el, "box-shadow") || "none").filter((l) => l.spread !== 0);
      if (spread.length > 1) bad.push(`${hash} ${sel}: ${spread.map((l) => l.raw).join(" | ")}`);
    }
  }
  assert.deepEqual(bad, []);
});

/**
 * R5: History's filters sit on one line that never wraps. Scenario: full data, History open. Expected: the filter
 * line is a flex row with nowrap and holds six controls (the Filter field and Kind, When, Computer, Device,
 * Outcome). Code: base.css:99 sets flex-wrap: nowrap (after base.css:77 set grid).
 * Label: code failure. Basis: R5. Confidence: high (a guard; the wrap itself is only checked as CSS, since
 * jsdom has no layout).
 */
test("R5 history filters: the filter line is one flex row that never wraps", async () => {
  const { d } = await open("v=full", "#go=history");
  const line = need(d.querySelector(".b-filt"), "the filter line");
  assert.equal(resolve(line, "display"), "flex");
  assert.equal(resolve(line, "flex-wrap"), "nowrap");
  assert.equal(line.children.length, 6, "Filter plus Kind, When, Computer, Device, Outcome");
});

/**
 * R14: "Emoji only mark things (files, computers, devices)". Scenario: full data, Settings open, the section list.
 * Expected (reading R14's parenthesis as the list of things): no emoji image in the section list. Code: the
 * section names are drawn with emoji images (kit.js:295, E(SE[x], 16)), and the sections are not files, computers
 * or devices.
 * Label: code failure. Basis: R14. Confidence: medium (whether a settings section is a "thing" is a reading).
 */
test("R14: the Settings section list uses line icons, not emoji images", async () => {
  const { d } = await open("v=full", "#go=settings");
  assert.equal(d.querySelectorAll(".k-snav img.k-e").length, 0);
});

/**
 * R14: History event kinds use line icons (inline SVG), not emoji images. Scenario: full data, History open.
 * Expected: no emoji image in the event rows, and every row has an SVG icon. Code: base.js:127 draws each kind with
 * an inline SVG.
 * Label: code failure. Basis: R14. Confidence: high (a guard).
 */
test("R14 history: each event kind is an inline SVG line icon, with no emoji image", async () => {
  const { d } = await open("v=full", "#go=history");
  const rows = [...d.querySelectorAll(".b-ev")];
  assert.ok(rows.length > 0, "event rows");
  assert.equal(d.querySelectorAll(".b-ev img.k-e").length, 0);
  assert.equal(rows.filter((r) => r.querySelector(".b-ic svg")).length, rows.length);
});
