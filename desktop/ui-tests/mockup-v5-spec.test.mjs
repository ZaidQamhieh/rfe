// Target: desktop/mockups/v5/ (01-rfe.html, base.js, base.css, kit.js, kit.css, _nav.js; _frame.css and _files.js load with them)
// Requirements covered: R1 to R15 (/tmp/claude-1000/qa-v5/requirements.md). Each block quotes its requirement.
// Run: node --test desktop/ui-tests/mockup-v5-spec.test.mjs   (from the repo root; needs `npm ci` in desktop/ui-tests)
//
// The expected values come from the requirement text. The design's code is read only to reach a control
// (its data-k, class or label). Nothing is loosened: where the design breaks a requirement, its block fails.
// Each walk through the design runs once per mode (sweep); the blocks that read it share the result.

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole, ResourceLoader } from "jsdom";
import { fileURLToPath, pathToFileURL } from "node:url";

const PAGE = fileURLToPath(new URL("../mockups/v5/01-rfe.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/* No network: any non-file URL (the Google Fonts stylesheet) gets an empty body; local scripts and CSS load. */
class Offline extends ResourceLoader {
  fetch(url, options) {
    if (String(url).startsWith("file:")) return super.fetch(url, options);
    return Promise.resolve(Buffer.from(""));
  }
}

/** The design in a window. query: "?v=full", "?v=empty" or "" (the viewer). hash: a starting state. */
async function open(query = "", hash = "") {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => {
    const m = String((e && e.detail && e.detail.stack) || (e && e.message) || e);
    if (!/Could not parse CSS/.test(m)) errs.push("script: " + m.split("\n").slice(0, 3).join(" | "));
  });
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 300)));
  const dom = await JSDOM.fromFile(PAGE, {
    url: pathToFileURL(PAGE).href + query + hash,
    runScripts: "dangerously",
    resources: new Offline(),
    pretendToBeVisual: true,
    virtualConsole: vc,
    beforeParse(w) { w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} }); },
  });
  const w = dom.window;
  live.push(w);
  if (w.document.readyState !== "complete") {
    await Promise.race([new Promise((r) => w.addEventListener("load", r, { once: true })), sleep(10000)]);
  }
  return { w, d: w.document, errs, close: () => w.close() };
}

const txt = (el) => (el ? el.textContent.replace(/\s+/g, " ").trim() : "");
const own = (el) => (el ? [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join("").replace(/\s+/g, " ").trim() : "");
const click = (el) => { assert.ok(el, "a control the test must click is missing"); el.click(); return el; };
const press = (w, key, extra = {}) => w.document.dispatchEvent(new w.KeyboardEvent("keydown", { key, bubbles: true, ...extra }));
/* Devices is one nav item (go:servers); the phones live in a section of that page, so "devices" is reached the way a user does: open Devices, then pick a phone (empty mode has none, and stays on the page). */
const nav = (d, view) => { if (view !== "devices") return click(d.querySelector(`#side [data-k="go:${view}"]`)); const el = click(d.querySelector('#side [data-k="go:servers"]')); const ph = d.querySelector('#st [data-k^="pick:devices:"]'); if (ph) click(ph); return el; };
const escRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const computers = (d) => [...d.querySelectorAll("#side button.pc")].filter((b) => b.dataset.k !== "dlg:newconn");
const dialogBtn = (d, label) => [...d.querySelectorAll(".k-layer .k-da button")].find((b) => txt(b) === label);

const PAGES = [
  { view: "home", label: "Overview", shown: (d) => !!d.querySelector("#st .db") },
  { view: "files", label: "Files", shown: (d) => !!d.querySelector("#st .ol") },
  { view: "servers", label: "Devices", shown: (d) => txt(d.querySelector("#st h1")) === "Devices" },
  { view: "devices", label: "Devices", shown: (d) => txt(d.querySelector("#st h1")) === "Devices" },
  { view: "transfers", label: "Transfers", shown: (d) => txt(d.querySelector("#st h1")) === "Transfers" },
  { view: "tools", label: "Tools", shown: (d) => txt(d.querySelector("#st h1")) === "Tools" },
  { view: "history", label: "History", shown: (d) => txt(d.querySelector("#st h1")) === "History" },
  { view: "settings", label: "Settings", shown: (d) => txt(d.querySelector("#st h1")) === "Settings" },
];
const TAB_SELECTOR = { tools: "#st [data-k^=\"tool:\"]", settings: "#st [data-k^=\"set:\"]" };
const LIST_ROWS = { servers: '#st [data-k^="pick:servers:"]', devices: '#st [data-k^="pick:devices:"]', transfers: "#st .ro", history: "#st .b-ev" };

/* ---------- CSS: the cascade as the browser applies it (last matching rule wins; no specificity) ---------- */
function flatRules(d) {
  const out = [];
  const walk = (list) => { for (const r of list || []) { if (r.media) walk(r.cssRules); else if (r.selectorText && r.style) out.push(r); } };
  for (const sheet of d.styleSheets) walk(sheet.cssRules);
  return out;
}
function cascade(el, prop, rules) {
  let value = null;
  for (const r of rules) {
    const hit = r.selectorText.split(",").some((s) => { try { return el.matches(s.trim()); } catch { return false; } });
    if (hit) { const v = r.style.getPropertyValue(prop); if (v && v.trim()) value = v.trim(); }
  }
  return value;
}
function expandVars(value, root, rules, depth = 0) {
  if (depth > 5) return "transparent";
  const whole = /^var\((--[\w-]+)\)$/.exec(value.trim());
  if (whole) {
    const v = cascade(root, whole[1], rules);
    if (v === null) throw new Error("unresolved custom property " + whole[1] + " (harness limit, not a design result)");
    return expandVars(v, root, rules, depth + 1);
  }
  return value.replace(/var\((--[\w-]+)\)/g, (_, name) => {
    const v = cascade(root, name, rules);
    return v === null ? "transparent" : expandVars(v, root, rules, depth + 1);
  });
}
function resolved(el, prop, root, rules) {
  const raw = cascade(el, prop, rules);
  return raw === null ? null : expandVars(raw, root, rules);
}
/** The layers of a box-shadow value: colour functions dropped, numbers read in order (x, y, blur, spread). */
function shadowLayers(value) {
  if (!value || /^\s*none\s*$/i.test(value)) return [];
  let f = value, prev;
  do { prev = f; f = f.replace(/\([^()]*\)/g, ""); } while (f !== prev);
  return f.split(",").map((s) => s.trim()).filter(Boolean).map((layer) => {
    const toks = layer.split(/\s+/);
    const nums = toks.filter((t) => /^-?(\d+\.?\d*|\.\d+)(px)?$/.test(t)).map((t) => parseFloat(t));
    return { inset: toks.includes("inset"), x: nums[0] ?? 0, spread: nums[3] ?? 0 };
  });
}
/** A coloured left stripe in a box-shadow: an inset layer pushed to the right by 2px or more. */
const hasStripe = (shadow) => shadowLayers(shadow).some((l) => l.inset && l.x >= 1.5);
/** A coloured left border of 2px or more. */
function borderStripe(v) {
  if (!v || /^\s*(none|hidden)\b/i.test(v)) return false;
  const m = /(-?\d*\.?\d+)px/.exec(v);
  return !!m && parseFloat(m[1]) >= 2;
}

/* ---------- visible text: what a person sees by default (closed disclosures and closed search panels hidden) ---------- */
function hiddenByDefault(node, root) {
  for (let el = node.nodeType === 3 ? node.parentElement : node; el && el !== root; el = el.parentElement) {
    if (el.hasAttribute("hidden")) return true;
    if (el.matches(".b-sr:not(.open)")) return true;
    if (el.tagName === "DETAILS" && !el.hasAttribute("open")) {
      const sum = [...el.children].find((c) => c.tagName === "SUMMARY");
      if (!(sum && sum.contains(node))) return true;
    }
  }
  return false;
}
function visibleText(root) {
  const parts = [];
  const walker = root.ownerDocument.createTreeWalker(root, 4); // SHOW_TEXT
  for (let n = walker.nextNode(); n; n = walker.nextNode()) if (!hiddenByDefault(n, root)) parts.push(n.textContent);
  for (const f of root.querySelectorAll("input, textarea")) if (f.value && !hiddenByDefault(f, root)) parts.push(f.value);
  return parts.join(" ").replace(/\s+/g, " ");
}
// Requirement R9 names these: key fingerprints, IP addresses, ports, millisecond latency, "agent", "TLS",
// "SHA-256", "certificate", "fingerprint", "rfe status". Patterns are written from those words.
const FORBIDDEN = [
  ["key fingerprint", /\b[0-9A-Fa-f]{4} [0-9A-Fa-f]{4} (?:[0-9A-Fa-f]{4}\b|…)/],
  ["key fingerprint", /\b[0-9a-f]{4}…[0-9a-f]{4}\b/],
  ["IP address", /\b\d{1,3}(?:\.\d{1,3}){3}\b/],
  ["port", /:\d{4,5}\b/],
  ["port", /\bports?\b/i],
  ["millisecond latency", /\b\d+ ?ms\b/],
  ["'agent'", /\bagent\b/i],
  ["'TLS'", /\bTLS\b/],
  ["'SHA-256'", /SHA-?256/i],
  ["'certificate'", /certificate/i],
  ["'fingerprint'", /fingerprint/i],
  ["'rfe status'", /\brfe status\b/i],
];
function leaks(root) {
  const text = visibleText(root);
  const out = [];
  for (const [what, re] of FORBIDDEN) {
    const m = re.exec(text);
    if (m) out.push(`${what} near "…${text.slice(Math.max(0, m.index - 30), m.index + m[0].length + 30)}…"`);
  }
  return out;
}

/* ---------- emoji (R14): the file, computer and device emoji the kit uses; anything else on a non-thing is a fault ---------- */
const THINGS = new Set(["file_folder", "open_file_folder", "framed_picture", "page_facing_up", "memo", "bar_chart", "film_frames",
  "card_file_box", "package", "house", "computer_disk", "musical_notes", "scroll", "desktop_computer", "laptop", "mobile_phone", "window", "penguin"]);
const NON_THING = "#side img.k-e, #top img.k-e, #st h1 img.k-e, #st h2 img.k-e, #st h3 img.k-e, #st h4 img.k-e, #st .k-navi img.k-e, " +
  "#st .k-chip img.k-e, #st .k-snav img.k-e, #st .k-ttabs img.k-e, #rail h2 img.k-e, #rail h3 img.k-e, .k-layer h2 img.k-e, " +
  ".k-layer h3 img.k-e, .b-sr.open .s-ch img.k-e";
function emojiBad(d) {
  return [...d.querySelectorAll(NON_THING)]
    .map((img) => ((img.getAttribute("src") || "").match(/emoji\/([^/]+)\.svg$/) || [])[1] || "?")
    .filter((n) => !THINGS.has(n));
}

/* ---------- one walk through every page, tab, row and dialog, shared by R9, R12 and R14 ---------- */
const SWEEP = new Map();
async function sweep(mode) {
  if (SWEEP.has(mode)) return SWEEP.get(mode);
  const out = { leaks: [], emoji: [], errors: [] };
  const h = await open(`?v=${mode}`);
  const { w, d } = h;
  out.errors.push(...h.errs.map((e) => `load: ${e}`));
  const visit = (label, thrown, errs) => {
    for (const f of leaks(d.querySelector(".win"))) out.leaks.push(`${label}: ${f}`);
    for (const n of emojiBad(d)) out.emoji.push(`${label}: ${n}`);
    if (thrown) out.errors.push(`${label}: ${thrown.message}`);
    for (const e of errs) out.errors.push(`${label}: ${e}`);
    if (!d.querySelector("#st").innerHTML.trim()) out.errors.push(`${label}: the page is blank`);
  };
  const run = async (label, action) => {
    const before = h.errs.length;
    let thrown = null;
    try { await action(); } catch (e) { thrown = e; }
    visit(label, thrown, h.errs.slice(before));
    press(w, "Escape");
  };
  await run("opened (default page)", () => {});
  for (const p of PAGES) {
    await run(p.label, () => nav(d, p.view));
    const tabSel = TAB_SELECTOR[p.view];
    if (tabSel) for (let i = 0, n = d.querySelectorAll(tabSel).length; i < n; i++) await run(`${p.label} tab ${i + 1}`, () => click(d.querySelectorAll(tabSel)[i]));
    const rowSel = LIST_ROWS[p.view];
    if (rowSel) for (let i = 0, n = d.querySelectorAll(rowSel).length; i < n; i++) await run(`${p.label} row ${i + 1}`, () => click(d.querySelectorAll(rowSel)[i]));
  }
  await run("Show all results page", () => w.K.go("results"));
  await run("search panel", () => nav(d, "search"));
  for (const name of Object.keys(w.K.DLG)) {
    if (name === "palette") continue; // the palette is the search menu: R6 and R7 cover it
    await run(`dialog ${name}`, () => { w.K.openDlg(name); if (!d.querySelector(".k-layer .k-dlg")) throw new Error("the dialog was not drawn"); });
  }
  h.close();
  SWEEP.set(mode, out);
  return out;
}

/* ================================ R1 ================================ */
test("R1 sidebar: the eight items in order (Computers and Devices are one), then a Computers list and Add a computer", async () => {
  // Requirement R1: "Left sidebar shows Overview, Files, Servers, Devices, Transfers, Search, Tools, History, Settings, then a Computers list and 'Add a computer'" (source: owner "left side menu [navigation] from 12 fixate")
  // Expected: nav labels exactly [Overview, Files, Servers, Devices, Transfers, Search, Tools, History, Settings]; the Computers heading after them; Add a computer last; catches: a missing, renamed or reordered item, or Add a computer above Computers
  const { d } = await open("?v=full");
  const navBtns = [...d.querySelectorAll("#side .k-navi")];
  assert.deepEqual(navBtns.map((b) => txt(b.querySelector("span"))), ["Overview", "Files", "Devices", "Transfers", "Search", "Tools", "History", "Settings"]);
  const side = [...d.querySelector("#side").children];
  const h4 = side.find((e) => e.tagName === "H4");
  assert.equal(txt(h4), "Computers");
  assert.ok(side.indexOf(h4) > side.indexOf(navBtns[navBtns.length - 1]), "Computers comes after the nav items");
  const pcs = [...d.querySelectorAll("#side button.pc")];
  assert.equal(own(pcs[pcs.length - 1]), "Add a computer", "Add a computer is last");
  assert.ok(side.indexOf(pcs[0]) > side.indexOf(h4), "the computers follow their heading");
});

test("R1 page items: clicking one shows that page and marks it current, in both modes", async () => {
  // Requirement R1: "clicking a page item shows that page and marks it current" (source: owner "left side menu [navigation] from 12 fixate")
  // Expected: after each click exactly one sidebar item is current, it is the clicked one, and the page is shown; catches: a click that changes nothing, or two items marked current
  for (const mode of ["full", "empty"]) {
    const { d } = await open(`?v=${mode}`);
    for (const p of PAGES) {
      nav(d, p.view);
      const on = [...d.querySelectorAll("#side .k-navi.on")];
      assert.equal(on.length, 1, `${mode} ${p.label}: exactly one item is current`);
      assert.equal(txt(on[0].querySelector("span")), p.label, `${mode}: the current item is ${p.label}`);
      assert.ok(p.shown(d), `${mode} ${p.label}: the page is shown`);
    }
  }
});

/* ================================ R2 ================================ */
test("R2 Files: one tree with every computer and no pairing-request rows", async () => {
  // Requirement R2: "Files is one tree across every computer, and the tree has no pairing-request rows ('asks to join', 'wants to take over')" (source: owner "my problem with the file system is this … remove that from the file system")
  // Expected: one tree; every computer listed in the sidebar has a row in it; no 'asks to join' or 'wants to take over' text and no pairing-request block on the page; catches: a tree per computer, a computer missing from the tree, or pairing rows left in the file list
  const { d } = await open("?v=full");
  nav(d, "files");
  assert.equal(d.querySelectorAll("#st .ol .tr").length, 1, "one tree");
  const rows = [...d.querySelectorAll("#st .tr .n.pc")].map(txt);
  const names = computers(d).map(own);
  assert.ok(names.length >= 2, "the sidebar lists computers");
  for (const n of names) assert.ok(rows.some((r) => r.includes(n)), `${n} has a row in the tree`);
  assert.doesNotMatch(txt(d.querySelector("#st")), /asks to join|wants to take over/i, "no pairing request text on Files");
  assert.equal(d.querySelectorAll("#st .k-req, #st .k-reqs").length, 0, "no pairing-request block on Files");
});

/* ================================ R3 ================================ */
test("R3 Overview: the default page, with four stats, a throughput chart and three cards", async () => {
  // Requirement R3: "Overview (default page) shows four stats, a throughput chart, 'Waiting for you', 'Computers' and 'Recent files'" (source: owner "overview from 2")
  // Expected: Overview is current on load; 4 stat tiles; one chart; cards headed Waiting for you, Computers, Recent files; catches: another default page, three or five stats, a missing chart or card
  const { d } = await open("?v=full");
  assert.equal(txt(d.querySelector("#side .k-navi.on span")), "Overview", "Overview is the default page");
  assert.equal(d.querySelectorAll("#st .db > .stat").length, 4, "four stats");
  assert.equal(d.querySelectorAll("#st .db > .chart svg").length, 1, "one throughput chart");
  const cards = [...d.querySelectorAll("#st .db > .card > h2")].map(txt);
  for (const want of ["Waiting for you", "Computers", "Recent files"]) assert.ok(cards.some((h) => h.startsWith(want)), `a "${want}" card`);
});

test("R3 empty Overview: each item shows an empty state, and no sample data", async () => {
  // Requirement R3: "in empty mode each shows an empty state and no sample data" (source: owner "present 2 of the same one, one empty and one full of data")
  // Expected: each stat shows an empty value (a dash or zero); each card has its empty block; the chart has its empty note and no data line; none of the full-mode sample values, computer names or recent file names appear; catches: sample data left on the first-run page, or a card with no empty state
  // Reading (see the report): an empty stat counts as its empty state when it shows a dash or zero.
  const full = await open("?v=full");
  const sample = [
    ...computers(full.d).map(own),
    ...[...full.d.querySelectorAll("#st .db > .stat b")].map(txt),
    ...[...full.d.querySelectorAll("#st .db .rc b")].map(txt),
  ];
  const { d } = await open("?v=empty");
  assert.ok(d.querySelector("#st .db"), "Overview is shown");
  const stats = [...d.querySelectorAll("#st .db > .stat b")].map(txt);
  assert.equal(stats.length, 4, "four stats");
  for (const s of stats) assert.match(s, /^(—|0)/, `stat "${s}" is an empty value`);
  const cards = [...d.querySelectorAll("#st .db > .card")];
  assert.equal(cards.length, 3, "three cards");
  for (const c of cards) assert.ok(c.querySelector(".nil"), `card "${txt(c).slice(0, 30)}" has its empty state`);
  assert.ok(d.querySelector("#st .db .chart .ov"), "the chart has its empty note");
  const data = [...d.querySelectorAll("#st .db .chart svg path")].filter((p) => p.getAttribute("stroke") !== "var(--line)");
  assert.equal(data.length, 0, "the chart draws no data line");
  const emText = txt(d.querySelector("#st .db"));
  for (const s of sample.filter(Boolean)) assert.doesNotMatch(emText, new RegExp("(^|\\W)" + escRe(s) + "(\\W|$)"), `sample "${s}" does not show`);
});

/* ================================ R4 ================================ */
async function rowsSelectInspector(view) {
  const { d } = await open("?v=full");
  nav(d, view);
  const rowSel = LIST_ROWS[view], n = d.querySelectorAll(rowSel).length;
  assert.ok(n >= 2, `${view} lists its rows`);
  for (let i = 0; i < n; i++) {
    const name = txt(d.querySelectorAll(rowSel)[i].querySelector("b"));
    click(d.querySelectorAll(rowSel)[i]);
    assert.equal(txt(d.querySelector(rowSel + ".sel b")), name, `${view}: the row "${name}" is the selected one`);
    /* Merged Devices page: the right panel stays the computer's; the picked device's panel is the .rail.cdd under the device list. */
    const panel = view === "devices" ? "#st .rail.cdd" : "#rail";
    assert.equal(txt(d.querySelector(panel + (view === "devices" ? " h2 .dnt" : " h2"))), name.replace(/ This app$/, ""), `${view}: the inspector shows "${name}"`);
    assert.ok(d.querySelectorAll(panel + " .k-dact button").length >= 1, `${view}: "${name}" has its actions in the inspector`);
  }
}
// Requirement R4 (all three): "Servers, Devices and Transfers are lists with an inspector panel on the right; clicking a row selects it and the inspector shows that row's item with its actions" (source: owner "devices from 4", "servers from 4", "transfer from 4")
test("R4 Servers: a row selects it and the inspector shows that computer with its actions", () => rowsSelectInspector("servers"));
test("R4 Devices: a row selects it and the inspector shows that device with its actions", () => rowsSelectInspector("devices"));
test("R4 Transfers: a row selects it and the inspector shows that transfer with its actions", () => rowsSelectInspector("transfers"));

/* ================================ R5 ================================ */
test("R5 History: the filters sit on one line that never wraps", async () => {
  // Requirement R5: "History has filters on one line that never wraps: a Filter field plus Kind, When, Computer, Device, Outcome" (source: owner "history is good but I dont want it to slit into 2 lines")
  // Expected: one filter bar whose direct children are the Filter field plus the five selects (1 + 5 = 6); the bar is a flex row with flex-wrap nowrap in the cascade; catches: a filter wrapped onto a second line, or a missing filter
  const { d } = await open("?v=full");
  nav(d, "history");
  const bar = d.querySelector("#st .b-filt");
  assert.ok(bar, "the filter bar exists");
  const kids = [...bar.children];
  assert.equal(kids.length, 1 + 5, "six filters, each a direct child of the bar");
  assert.ok(kids.every((k) => k.tagName === "LABEL"), "each filter is one labelled control");
  assert.ok(bar.querySelector('input[aria-label^="Filter"]'), "a Filter field");
  for (const label of ["Kind", "When", "Computer", "Device", "Outcome"]) assert.ok(bar.querySelector(`select[aria-label="${label}"]`), `a ${label} filter`);
  const rules = flatRules(d);
  assert.equal(cascade(bar, "display", rules), "flex", "the bar is a flex row");
  assert.equal(cascade(bar, "flex-wrap", rules), "nowrap", "the bar never wraps");
});

test("R5 History: clicking an event selects it and shows it in the inspector", async () => {
  // Requirement R5: "clicking an event selects it and shows it in the inspector" (source: owner "history is good but I dont want it to slit into 2 lines")
  // Expected: after each click that row is the selected one, and the inspector shows that event's text; catches: an inspector that keeps the first event, or a row not marked selected
  const { d } = await open("?v=full");
  nav(d, "history");
  const n = d.querySelectorAll("#st .b-ev").length;
  assert.ok(n >= 4, "the history lists events");
  for (const i of [0, 3, n - 1]) {
    click(d.querySelectorAll("#st .b-ev")[i]);
    const row = d.querySelectorAll("#st .b-ev")[i];
    assert.ok(row.classList.contains("sel"), `event ${i + 1} is selected`);
    const tx = own(row.querySelector(".tx"));
    assert.ok(txt(d.querySelector("#rail")).includes(tx), `the inspector shows "${tx}"`);
  }
});

/* ================================ R6 ================================ */
test("R6 top bar: no search bar", async () => {
  // Requirement R6: "There is no search bar in the top bar." (source: owner "I dont want a search bar to appear on the top I want a search menu")
  // Expected: the top bar holds no text field, select or search landmark; catches: a search field put back in the top bar
  const { d } = await open("?v=full");
  assert.equal(d.querySelectorAll("#top input, #top textarea, #top select, #top [role=search], #top .k-sbox").length, 0);
});

test("R6 search menu opens from the sidebar Search item, from Ctrl K and from /", async () => {
  // Requirement R6: "Search in the sidebar, Ctrl K and '/' open the search menu" (source: owner "I dont want a search bar to appear on the top I want a search menu")
  // Expected: each of the three opens the search menu (.b-sr.open); catches: a route that does nothing
  const a = await open("?v=full");
  nav(a.d, "search");
  assert.ok(a.d.querySelector(".b-sr.open"), "the sidebar Search item opens it");
  const b = await open("?v=full");
  press(b.w, "k", { ctrlKey: true });
  assert.ok(b.d.querySelector(".b-sr.open"), "Ctrl K opens it");
  const c = await open("?v=full");
  press(c.w, "/");
  assert.ok(c.d.querySelector(".b-sr.open"), "/ opens it");
});

test("R6 Escape and a click outside the panel close it; a click inside keeps it open", async () => {
  // Requirement R6: "Escape and a click outside the panel close it" (source: owner "I dont want a search bar to appear on the top I want a search menu")
  // Expected: a click inside the panel keeps it open; Escape closes it; a click outside closes it; catches: a panel that stays open after Escape or an outside click, or one that closes when clicked inside
  const { w, d } = await open("?v=full");
  nav(d, "search");
  click(d.querySelector(".b-sr .s-sc p"));
  assert.ok(d.querySelector(".b-sr.open"), "a click inside keeps it open");
  press(w, "Escape");
  assert.equal(d.querySelector(".b-sr.open"), null, "Escape closes it");
  nav(d, "search");
  click(d.querySelector("#st"));
  assert.equal(d.querySelector(".b-sr.open"), null, "a click outside closes it");
});

/* ================================ R7 ================================ */
test("R7 search panel: beside the sidebar, a field, a count line with one switch per computer, and a Filters menu", async () => {
  // Requirement R7: "The search menu is a panel beside the sidebar: a field, a line naming how many computers are searched with one switch per computer (offline or not-yet-approved ones disabled with the reason), a Filters menu" (source: owner search from 9, 6, 8 → UI/UX agent recommendation adopted)
  // Expected: the panel starts at the sidebar's right edge; a field; the line reads "Searching N of M computers" with N the switches on and M the switches; one switch per computer; an offline or waiting-for-OK computer is disabled and says why, the others are enabled; Filters opens a menu; catches: a switch on for a disabled computer, a count line that disagrees with the switches, a missing reason, a panel placed over the sidebar
  const { d } = await open("?v=full");
  nav(d, "search");
  assert.ok(d.querySelector(".b-sr.open .m"), "the panel is open");
  const root = d.querySelector(".win");
  const rules = flatRules(d);
  const gridFirst = resolved(root, "grid-template-columns", root, rules).split(/\s+/)[0];
  assert.equal(resolved(d.querySelector(".b-sr"), "left", root, rules), gridFirst, "the panel starts at the sidebar's right edge");
  assert.ok(d.querySelector(".b-sr .s-f input"), "a field");
  const scope = d.querySelector(".b-sr .s-sc");
  const switches = [...scope.querySelectorAll('[data-k="sr:pick"]')];
  const cs = computers(d);
  assert.equal(switches.length, cs.length, "one switch per computer");
  const m = /Searching (\d+) of (\d+) computers/.exec(txt(scope.querySelector("p")));
  assert.ok(m, "a line names how many computers are searched");
  assert.equal(Number(m[1]), switches.filter((b) => b.classList.contains("on")).length, "the count matches the switches on");
  assert.equal(Number(m[2]), switches.length, "and the total matches the switches");
  for (const c of cs) {
    const status = txt(c.querySelector("small")); // '' when connected, else the sidebar's reason
    const sw = switches.find((b) => txt(b).startsWith(own(c)));
    assert.ok(sw, `${own(c)} has a switch`);
    if (!status) assert.equal(sw.disabled, false, `${own(c)} can be searched`);
    else {
      assert.equal(sw.disabled, true, `${own(c)} (${status}) is disabled`);
      assert.match(txt(sw) + " " + (sw.title || ""), /offline|needs your ok|needs ok|waiting for your ok|approv/i, `${own(c)} says why`);
    }
  }
  const filters = [...scope.querySelectorAll('[data-k="menu:sfilt"]')];
  assert.equal(filters.length, 1, "a Filters control in the panel");
  click(filters[0]);
  assert.ok(d.querySelectorAll(".k-layer .k-menu button").length >= 1, "the Filters menu opens");
});

test("R7 search results: grouped by computer, each group followed by its results", async () => {
  // Requirement R7: "results grouped by computer" (source: owner search from 9, 6, 8 → UI/UX agent recommendation adopted)
  // Expected: one group heading per computer that has results (the count of distinct computers in the results data), each followed by its result rows; catches: one flat list, or a group with no rows
  const { w, d } = await open("?v=full");
  nav(d, "search");
  const groups = [...d.querySelectorAll(".b-sr .s-g")];
  const pcs = new Set(w.B.S.res.map((r) => r.pc));
  assert.ok(pcs.size >= 1, "the search has results");
  assert.equal(groups.length, pcs.size, "one group per computer with results");
  for (const g of groups) assert.ok(g.nextElementSibling && g.nextElementSibling.classList.contains("s-r"), `${own(g)} has its rows under it`);
});

test("R7 Show all: a results page whose table columns each filter", async () => {
  // Requirement R7: "'Show all' which opens a results page whose table columns each filter" (source: owner search from 9, 6, 8 → UI/UX agent recommendation adopted)
  // Expected: Show all opens the results page; each named column (Name, Computer, Folder, Changed, Size) has a filter control in its header; catches: a named column with no filter
  // Reading (see the report): the unnamed icon column has no filter; this block tests the named columns only.
  const { d } = await open("?v=full");
  nav(d, "search");
  click(d.querySelector('.b-sr .s-ft [data-k="sr:go:results"]'));
  assert.equal(txt(d.querySelector("#st h1")), "Search results", "the results page opens");
  const heads = [...d.querySelectorAll("#st .rt .hc")];
  for (const name of ["Name", "Computer", "Folder", "Changed", "Size"]) {
    const hc = heads.find((h) => txt(h.querySelector("b")) === name);
    assert.ok(hc, `a ${name} column`);
    assert.ok(hc.querySelector("input, select"), `the ${name} column filters`);
  }
});

test("R7 empty search: no results, an Add a computer action, and the scope This computer", async () => {
  // Requirement R7: "Empty mode: no results, an 'Add a computer' action, and the scope 'This computer'." (source: owner search from 9, 6, 8 → UI/UX agent recommendation adopted)
  // Expected: no result rows or groups; an Add a computer action that opens the add dialog; the scope line names This computer; a Filters menu is still in the panel (reading: the panel keeps it in both modes); catches: results shown with no computers, or an action that does nothing
  const { w, d } = await open("?v=empty");
  nav(d, "search");
  assert.equal(d.querySelectorAll(".b-sr .s-g, .b-sr .s-r").length, 0, "no results");
  const add = [...d.querySelectorAll(".b-sr .s-no button")].find((b) => txt(b) === "Add a computer");
  assert.ok(add, "an Add a computer action");
  assert.ok([...d.querySelectorAll(".b-sr .s-sc button")].some((b) => txt(b) === "This computer"), "the scope is This computer");
  assert.ok(d.querySelector('.b-sr .s-sc [data-k="menu:sfilt"], .b-sr .s-sc .fl'), "a Filters control in the panel");
  click(add);
  assert.ok(d.querySelector('.k-layer .k-dlg[aria-label="Add a computer"]'), "the action opens Add a computer");
  press(w, "Escape");
});

/* ================================ R8 ================================ */
test("R8 Add a computer: one dialog, three steps, no tabs; opened from the sidebar and from Servers", async () => {
  // Requirement R8: "'Add a computer' (sidebar item, Servers button) opens one dialog with three steps and no tabs: 1 Find your PC, 2 Allow it on the PC (one matching number shown), 3 Ready (name it, open its files)" (source: owner chose mockup 2 'Steps'; 'the new connections popup need a rework')
  // Expected: the steps read exactly Find your PC, Allow it on the PC, Ready; no tab bar; the sidebar and the Servers button both open it; step 2 shows one matching number; step 3 has a name field and Open its files; catches: a tab bar, another step list, two matching numbers, or an entry point that opens nothing
  const { w, d } = await open("?v=full");
  nav(d, "home");
  click(d.querySelector('#side [data-k="dlg:newconn"]'));
  const dlg = d.querySelector('.k-layer .k-dlg[aria-label="Add a computer"]');
  assert.ok(dlg, "the sidebar opens it");
  assert.deepEqual([...dlg.querySelectorAll(".b-sx > div > span")].map(own), ["Find your PC", "Allow it on the PC", "Ready"], "three steps");
  assert.equal(dlg.querySelectorAll(".k-tabs, [role=tab], .fxtabs").length, 0, "no tabs");
  press(w, "Escape");
  nav(d, "servers");
  click(d.querySelector('#st [data-k="dlg:newconn"]'));
  assert.ok(d.querySelector('.k-layer .k-dlg[aria-label="Add a computer"]'), "the Servers button opens it");
  press(w, "Escape");
  const s2 = await open("?v=full", "#dlg=newconn&tab=2");
  assert.equal(s2.d.querySelectorAll(".k-layer .b-num").length, 1, "step 2 shows one matching number");
  assert.match(txt(s2.d.querySelector(".k-layer .b-num")), /^\d{4} \d{4}$/, "the number is the matching number");
  const s3 = await open("?v=full", "#dlg=newconn&tab=3");
  assert.ok(s3.d.querySelector('.k-layer input[aria-label="Name"]'), "step 3 names the computer");
  assert.ok(dialogBtn(s3.d, "Open its files"), "step 3 has Open its files");
});

test("R8 Next and Back move between steps", async () => {
  // Requirement R8: "Next and Back move between steps" (source: owner chose mockup 2 'Steps'; 'the new connections popup need a rework')
  // Expected: Next on step 1 goes to step 2; Back on step 2 returns to step 1; catches: a Next or Back that does nothing
  const { d } = await open("?v=full");
  nav(d, "home");
  click(d.querySelector('#side [data-k="dlg:newconn"]'));
  const cur = () => own(d.querySelector(".k-layer .b-sx > div.on > span"));
  assert.equal(cur(), "Find your PC");
  click(dialogBtn(d, "Next"));
  assert.equal(cur(), "Allow it on the PC", "Next goes to step 2");
  click(dialogBtn(d, "Back"));
  assert.equal(cur(), "Find your PC", "Back returns to step 1");
});

test("R8 step 2 moves to step 3 by itself, with no Next button", async () => {
  // Requirement R8 (owner, 2 Nov): once the PC allows this computer, the dialog jumps to step 3, Ready, with no button press
  // Expected: step 2 has Back and no Next; after the wait the current step is Ready; catches: a step 2 that needs a click, or never moves on
  const { d } = await open("?v=full", "#dlg=newconn&tab=2");
  assert.equal(dialogBtn(d, "Next"), undefined, "step 2 has no Next");
  assert.ok(dialogBtn(d, "Back"), "step 2 keeps Back");
  await new Promise((r) => setTimeout(r, 5400));
  assert.equal(own(d.querySelector(".k-layer .b-sx > div.on > span")), "Ready", "it moved to step 3 by itself");
});

test("R8 empty mode: Next is disabled on step 1", async () => {
  // Requirement R8: "in empty mode Next is disabled" (source: owner chose mockup 2 'Steps')
  // Expected: the Next button of step 1 is disabled in empty mode; catches: a Next that can be pressed with no computer found
  const { d } = await open("?v=empty", "#dlg=newconn&tab=1");
  assert.equal(dialogBtn(d, "Next")?.disabled, true, "Next is disabled");
});

/* ================================ R9 ================================ */
test("R9 technical details sit only in a closed 'Technical details' disclosure", async () => {
  // Requirement R9: "Such details may exist only inside a closed 'Technical details' disclosure" (source: owner "not everyone who is gonna use the app is technical … check keys … needs to be removed or at least hidden")
  // Expected: the computer inspector folds its technical part under a closed Technical details disclosure; no disclosure on the window is open by default; catches: details shown open by default, or no folded technical part
  const { d } = await open("?v=full");
  nav(d, "servers");
  click(d.querySelector("#st .ro"));
  const tech = [...d.querySelectorAll("#rail details")].filter((x) => txt(x.querySelector("summary")) === "Technical details");
  assert.ok(tech.length >= 1, "the inspector folds its technical part under Technical details");
  for (const x of tech) assert.equal(x.hasAttribute("open"), false, "the Technical details disclosure is closed");
  assert.equal([...d.querySelectorAll(".win details")].filter((x) => x.hasAttribute("open")).length, 0, "no disclosure is open by default");
});

test("R9 full: no technical wording is visible by default on any page, inspector, sub-page or dialog", async () => {
  // Requirement R9: "No technical wording is visible by default anywhere: no key fingerprints, IP addresses, ports, millisecond latency, 'agent', 'TLS', 'SHA-256', 'certificate', 'fingerprint', 'rfe status'." (source: owner "not everyone who is gonna use the app is technical … check keys … needs to be removed or at least hidden")
  // Expected: no listed term in the visible text of any state; a closed Technical details disclosure does not count; catches: a fingerprint, address, port, latency, or the listed words shown by default
  const s = await sweep("full");
  assert.equal(s.leaks.length, 0, "technical wording visible in full mode:\n" + s.leaks.slice(0, 20).join("\n"));
});

test("R9 empty: no technical wording is visible by default on any page, inspector, sub-page or dialog", async () => {
  // Requirement R9: "No technical wording is visible by default anywhere" (source: owner "not everyone who is gonna use the app is technical … check keys … needs to be removed or at least hidden")
  // Expected: as the full-mode block, in the first-run design; catches: the listed terms in the empty pages (for example the Files steps) and in empty dialogs
  const s = await sweep("empty");
  assert.equal(s.leaks.length, 0, "technical wording visible in empty mode:\n" + s.leaks.slice(0, 20).join("\n"));
});

/* ================================ R10 ================================ */
test("R10 a computer that changed opens a plain dialog with exactly two actions", async () => {
  // Requirement R10: "A computer that changed ('nas') opens a dialog in plain words with exactly two actions: leave it, or reconnect." (source: same as R9)
  // Expected: clicking nas in the sidebar opens a dialog about nas; its action row holds exactly two buttons, one that leaves it and one that reconnects; catches: a third action, or no dialog
  // Reading (see the report): the dialog's header close control is not counted as an action.
  const { d } = await open("?v=full");
  click(computers(d).find((b) => own(b) === "nas"));
  const dlg = d.querySelector(".k-layer .k-dlg");
  assert.ok(dlg && /nas/.test(txt(dlg.querySelector("h2"))), "a dialog about nas opens");
  const acts = [...dlg.querySelectorAll(".k-da button")].map(txt);
  assert.equal(acts.length, 2, "exactly two actions: " + acts.join(" | "));
  assert.ok(acts.some((a) => /leave/i.test(a)), "one action leaves it");
  assert.ok(acts.some((a) => /reconnect|connect again/i.test(a)), "one action reconnects");
});

/* ================================ R11 ================================ */
test("R11 each computer's inspector opens its own settings", async () => {
  // Requirement R11: "Each computer has its own settings, opened from its inspector ('Settings for <name>…')" (source: owner "there shall be options/settings for each server")
  // Expected: for every computer row the inspector has the button "Settings for <name>…", and it opens that computer's settings with its own name in the Name field; catches: one shared settings dialog, or a button for another computer
  const { w, d } = await open("?v=full");
  nav(d, "servers");
  const rowSel = LIST_ROWS.servers, n = d.querySelectorAll(rowSel).length;
  assert.ok(n >= 2, "the computers are listed");
  for (let i = 0; i < n; i++) {
    click(d.querySelectorAll(rowSel)[i]);
    const name = txt(d.querySelector(rowSel + ".sel b"));
    const btn = [...d.querySelectorAll("#rail .k-dact button")].find((b) => txt(b) === `Settings for ${name}…`);
    assert.ok(btn, `${name}: its inspector has "Settings for ${name}…"`);
    click(btn);
    const dlg = d.querySelector(".k-layer .k-dlg");
    assert.ok(dlg && txt(dlg.querySelector("h2")).startsWith(`Settings for ${name}`), `${name}: its settings dialog opens`);
    assert.equal(d.querySelector('.k-layer input[aria-label="Name"]').value, name, `${name}: the dialog holds its own name`);
    press(w, "Escape");
  }
});

/* ================================ R12 ================================ */
test("R12 pages: every page and sub-page draws in full and empty mode with no script error", async () => {
  // Requirement R12: "Every page and dialog renders in both full and empty mode with no script error." (source: owner "one empty and one full of data")
  // Expected: no error while the page loads, and none while any page, Tools or Settings tab, list row, the results page or the search panel is shown, in either mode; the stage is never blank; catches: a page that throws, or a blank stage
  for (const mode of ["full", "empty"]) {
    const s = await sweep(mode);
    const bad = s.errors.filter((e) => !e.startsWith("dialog "));
    assert.equal(bad.length, 0, `${mode} pages:\n` + bad.slice(0, 20).join("\n"));
  }
});

test("R12 dialogs: every dialog draws in full and empty mode with no script error", async () => {
  // Requirement R12: "Every page and dialog renders in both full and empty mode with no script error." (source: owner "one empty and one full of data")
  // Expected: each dialog the design defines draws (is in the layer) and raises no error, in either mode; catches: a dialog that throws in one mode, or never draws
  for (const mode of ["full", "empty"]) {
    const s = await sweep(mode);
    const bad = s.errors.filter((e) => e.startsWith("dialog "));
    assert.equal(bad.length, 0, `${mode} dialogs:\n` + bad.slice(0, 20).join("\n"));
  }
});

/* ================================ R13 ================================ */
test("R13 a selected row has no coloured left stripe", async () => {
  // Requirement R13: "a selected row has no coloured left stripe" (source: owner "there is still lots of ai slop", screenshot of the stripe)
  // Expected: the box-shadow and the border of each selected row (Files, Servers, History, the add-a-computer pick, a search result) draw no left stripe, and no pseudo-element stripe is on a selected row; catches: an inset left shadow or a left border on a selected row
  const { w, d } = await open("?v=full");
  const root = d.querySelector(".win");
  const rules = flatRules(d);
  const row = (label, el) => {
    assert.ok(el, `${label}: a selected row is on screen`);
    const sh = resolved(el, "box-shadow", root, rules);
    assert.equal(hasStripe(sh), false, `${label}: box-shadow "${sh}" draws a left stripe`);
    const bl = resolved(el, "border-left", root, rules);
    assert.equal(borderStripe(bl), false, `${label}: border-left "${bl}" draws a stripe`);
  };
  nav(d, "files");
  row("Files", d.querySelector("#st .n.sel"));
  nav(d, "servers");
  click(d.querySelector("#st .ro"));
  row("Servers", d.querySelector("#st .ro.sel"));
  nav(d, "history");
  click(d.querySelector("#st .b-ev"));
  row("History", d.querySelector("#st .b-ev.sel"));
  w.K.openDlg("newconn", "1");
  row("Add a computer", d.querySelector(".k-layer .b-pc.on"));
  press(w, "Escape");
  nav(d, "search");
  row("Search result", d.querySelector(".b-sr .s-r.on"));
  press(w, "Escape");
  for (const r of rules) {
    if (!/::(before|after)/.test(r.selectorText) || !/\.(sel|on)\b/.test(r.selectorText)) continue;
    const wd = r.style.getPropertyValue("width");
    const bg = r.style.getPropertyValue("background") || r.style.getPropertyValue("background-color");
    assert.ok(!(/px/.test(wd) && parseFloat(wd) <= 4 && bg && !/none|transparent/.test(bg)), `a stripe pseudo-element on ${r.selectorText}`);
  }
});

test("R13 fields and cards carry at most one spread shadow (no glow ring)", async () => {
  // Requirement R13: "no glow ring (a second spread box-shadow) on fields or cards" (source: owner "there is still lots of ai slop", "use unslop to unslop the final version")
  // Expected: each field and card has at most one shadow layer with a spread; catches: a second spread shadow (a glow ring) added to a field or a card
  const { w, d } = await open("?v=full");
  const root = d.querySelector(".win");
  const rules = flatRules(d);
  const check = (label, el) => {
    assert.ok(el, `${label} is on screen`);
    const v = resolved(el, "box-shadow", root, rules);
    const rings = shadowLayers(v).filter((l) => l.spread !== 0);
    assert.ok(rings.length <= 1, `${label}: ${rings.length} spread shadows in "${v}"`);
  };
  nav(d, "home");
  check("Overview stat", d.querySelector("#st .db .stat"));
  check("Overview card", d.querySelector("#st .db .card"));
  check("Overview chart", d.querySelector("#st .db .chart"));
  nav(d, "devices");
  check("Devices card", d.querySelector("#st .k-card"));
  nav(d, "history");
  check("History filter field", d.querySelector("#st .b-filt .k-in"));
  check("History select", d.querySelector("#st .b-filt .k-sel"));
  nav(d, "search");
  check("search field", d.querySelector(".b-sr .s-f"));
  press(w, "Escape");
  w.K.openDlg("newconn", "1");
  check("Add a computer address field", d.querySelector(".k-layer .b-f"));
});

test("R13 primary buttons are flat: no raised or inset shadow", async () => {
  // Requirement R13: "primary buttons are flat (no raised/inset shadow)" (source: owner "use unslop to unslop the final version")
  // Expected: every primary button on the checked states has no box-shadow layer; catches: a raised or inset shadow on a primary button
  const { w, d } = await open("?v=full");
  const root = d.querySelector(".win");
  const rules = flatRules(d);
  let seen = 0;
  const check = (label) => {
    for (const el of d.querySelectorAll(".k-btn.pri")) {
      seen++;
      const v = resolved(el, "box-shadow", root, rules);
      assert.equal(shadowLayers(v).length, 0, `${label}: primary "${txt(el)}" has shadow "${v}"`);
    }
  };
  nav(d, "home");
  check("Overview");
  nav(d, "servers");
  click(d.querySelector("#st .ro"));
  check("Servers inspector");
  w.K.openDlg("newconn", "2");
  check("Add a computer, step 2");
  press(w, "Escape");
  assert.ok(seen > 0, "primary buttons were found to check");
});

test("R13 counts are not filled red pills", async () => {
  // Requirement R13: "counts are not filled red pills" (source: owner "there is still lots of ai slop", screenshot of the stripe)
  // Expected: each count (badge or section count) has no background fill; a red count is red text only; catches: a count drawn as a filled red pill
  const { d } = await open("?v=full");
  const root = d.querySelector(".win");
  const rules = flatRules(d);
  let seen = 0;
  const check = (label) => {
    for (const el of d.querySelectorAll(".k-badge, .k-count")) {
      seen++;
      for (const prop of ["background", "background-color"]) {
        const v = resolved(el, prop, root, rules);
        assert.ok(v === null || /^(none|transparent)$/i.test(v.trim()), `${label}: count "${txt(el)}" is filled (${prop}: ${v})`);
      }
    }
  };
  check("Overview");
  nav(d, "devices");
  check("Devices");
  assert.ok(seen > 0, "counts were found to check");
});

/* ================================ R14 ================================ */
test("R14 History: event kinds are inline line icons, not emoji images", async () => {
  // Requirement R14: "History event kinds use line icons (inline SVG), not emoji images" (source: owner choice 'Keep for things only')
  // Expected: no emoji image in the History list; every event row carries an inline SVG icon; catches: an emoji event kind left in History
  const { d } = await open("?v=full");
  nav(d, "history");
  assert.equal(d.querySelectorAll("#st img.k-e").length, 0, "no emoji image on History");
  const rows = [...d.querySelectorAll("#st .b-ev")];
  assert.ok(rows.length >= 2, "History lists events");
  for (const r of rows) assert.ok(r.querySelector(".b-ic svg"), `an inline icon on "${txt(r).slice(0, 40)}"`);
});

test("R14 emoji mark only files, computers and devices: navigation, tabs and headings carry none", async () => {
  // Requirement R14: "Emoji only mark things (files, computers, devices)" (source: owner choice 'Keep for things only')
  // Expected: in the sidebar, the top bar, tab bars, headings and inspector headings, every emoji image is a file, computer or device; catches: an emoji on a navigation item, a tab, a settings heading or a section heading
  // Reading (see the report): transfer direction icons, app icons and empty-state illustrations are not tested here.
  const a = await sweep("full");
  const b = await sweep("empty");
  const bad = [...new Set([...a.emoji.map((x) => "full " + x), ...b.emoji.map((x) => "empty " + x)])];
  assert.equal(bad.length, 0, "emoji on a non-thing:\n" + bad.slice(0, 20).join("\n"));
});

/* ================================ R15 ================================ */
test("R15 viewer: two frames of the same file, full then empty", async () => {
  // Requirement R15: "The viewer (no ?v) shows two frames of the same file, full then empty" (source: owner "present 2 of the same one, one empty and one full of data")
  // Expected: no design drawn directly; two frames, the first ?v=full and the second ?v=empty, both loading the same file; catches: one frame, the wrong order, or a different file
  const { d } = await open();
  assert.equal(d.querySelectorAll(".win").length, 0, "the design is not drawn in the viewer");
  const frames = [...d.querySelectorAll(".mk-pair iframe")];
  assert.equal(frames.length, 2, "two frames");
  assert.match(frames[0].getAttribute("src"), /^01-rfe\.html\?v=full/, "the first frame is the full page");
  assert.match(frames[1].getAttribute("src"), /^01-rfe\.html\?v=empty/, "the second frame is the empty page");
  assert.deepEqual([...d.querySelectorAll(".mk-pair > h2")].map(own), ["Full", "Empty"], "the frames are labelled Full, then Empty");
});

test("R15 strip buttons switch both frames to the same state", async () => {
  // Requirement R15: "its strip buttons switch both frames to the same state" (source: owner "present 2 of the same one, one empty and one full of data")
  // Expected: clicking Servers in the strip sets the same hash on both frames, keeping full then empty; the page's own hash follows; catches: only one frame switching, or the two frames on different states
  const { w, d } = await open();
  click([...d.querySelectorAll(".mk-views button")].find((b) => txt(b) === "Devices"));
  const srcs = [...d.querySelectorAll(".mk-pair iframe")].map((f) => f.getAttribute("src"));
  assert.match(srcs[0], /\?v=full#go=servers$/, "the full frame switches");
  assert.match(srcs[1], /\?v=empty#go=servers$/, "the empty frame switches to the same state");
  assert.equal(w.location.hash, "#go=servers", "the viewer's own hash follows");
});

test("sidebar collapses to icons and expands again", async () => {
  // Owner request (2 Nov): the sidebar can be collapsed
  const { d } = await open("?v=full");
  const frame = d.querySelector("#side").parentElement;
  assert.ok(!frame.classList.contains("sbmin"), "starts expanded");
  click(d.querySelector("#side .sb-t"));
  assert.ok(frame.classList.contains("sbmin"), "collapsed");
  assert.equal(d.querySelectorAll("#side .k-navi").length, 8, "every page is still reachable");
  nav(d, "devices");
  assert.ok(frame.classList.contains("sbmin"), "stays collapsed after the sidebar redraws");
  click(d.querySelector("#side .sb-t"));
  assert.ok(!frame.classList.contains("sbmin"), "expanded again");
});
