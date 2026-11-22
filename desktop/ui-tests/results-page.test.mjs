// The "Search results" page (ui/v5-transfers.js, drawSearch): the mockup's page, one table whose six columns each filter,
// filled from the search the engine really ran (E.makeSearch, kept in A.searchRun) and reached from the Search panel's "Show all".
// It has the title, a line saying what was searched and what was left out, a "Save this search" button, the filter header
// (Kind, Name, Computer, Folder, Changed, Size) and a row per match: picture, highlighted name, computer, "a › b" folder,
// short date, size, open-folder button. The real window scripts run in jsdom on the stub core, like v5-search-panel.test.mjs.
//
//   node --test desktop/ui-tests/results-page.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const css = readFileSync(fileURLToPath(new URL("../ui/css/v5-transfers.css", import.meta.url)), "utf8");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const waitFor = async (fn, ms = 4000) => { for (let t = 0; t < ms; t += 25) { if (fn()) return; await sleep(25); } };
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
  return { w, d: w.document, A: w.A, E: w.A.E, P: w.A.v5Pages.panel, errs };
}

const text = (el) => (el ? el.textContent.replace(/\s+/g, " ").trim() : "");
const until = async (fn, what) => { for (let i = 0; i < 150; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };

/** Searches every connected computer for `q` in the panel, then opens the results page with Show all. */
async function resultsFor(o, q) {
  o.P.open(q);
  await until(() => o.A.searchRun && o.A.searchRun.done, "the search to finish");
  await sleep(60);
  o.d.querySelector('#srp .s-ft [data-x="pall"]').click(); await sleep(250);
}

/** The table's cells grouped into rows of six (the header cells are .hc, not .c). */
const rowsOf = (d) => {
  const cells = [...d.querySelectorAll("#stage .rt .c")], out = [];
  for (let i = 0; i < cells.length; i += 6) out.push(cells.slice(i, i + 6));
  return out;
};

test("the page has the mockup's parts: title, search line, Save button, six filters", async () => {
  const o = await open();
  const { d, A } = o;
  await resultsFor(o, "e");
  const n = A.searchRun.results.length;
  assert.ok(n > 0, "the stub core has matches for 'e'");
  assert.equal(text(d.querySelector("#stage h1")), "Search results");
  assert.match(text(d.querySelector("#stage .k-sub")), new RegExp("^" + n + "\\+? for “e” on "));
  const save = d.querySelector("#stage .k-ph-r .k-btn");
  assert.equal(text(save), "Save this search");
  assert.ok(save.querySelector("svg"), "the button has an icon");
  assert.deepEqual([...d.querySelectorAll("#stage .rt .hc b")].map(text), ["Kind", "Name", "Computer", "Folder", "Changed", "Size"]);
  assert.equal(d.querySelectorAll("#stage .rt .hc select, #stage .rt .hc input").length, 6, "each column has a filter control");
  assert.equal(d.querySelector('#stage .rt [data-rf="name"]').value, "e", "the Name filter starts as the search");
  assert.equal(d.querySelectorAll("#stage .s-sc, #stage .s-pl, #stage [data-sf], #stage [data-x=\"view\"]").length, 0, "no scope pills, kind pills or view switch on the page");
  assert.equal(rowsOf(d).length, n, "a row per match");
  assert.deepEqual(o.errs, []);
});

test("the line under the title names the computers searched and the ones left out", async () => {
  const o = await open();
  const { d, A, E } = o;
  await resultsFor(o, "e");
  const sr = A.searchRun, name = (id) => A.hostName(id);
  const list = (a) => (a.length < 2 ? a.join("") : a.slice(0, -1).join(", ") + " and " + a[a.length - 1]);
  const left = E.servers.filter((s) => !sr.hosts.includes(s.id)).map((s) => s.name);
  const want = sr.results.length + (sr.results.length >= 400 ? "+" : "") + " for “e” on " + list(sr.hosts.map(name)) + (left.length ? " · " + list(left) + (left.length === 1 ? " was" : " were") + " left out" : "");
  assert.equal(text(d.querySelector("#stage #sprog")), want);
  assert.deepEqual(o.errs, []);
});

test("each row shows picture, highlighted name, computer, 'a › b' folder, short date, size and an open-folder button", async () => {
  const o = await open();
  const { d, A } = o;
  await resultsFor(o, "e");
  const sr = A.searchRun, rows = rowsOf(d);
  assert.equal(rows.length, sr.results.length);
  rows.forEach((cells, i) => {
    const r = sr.results[i];
    assert.ok(cells[0].querySelector("img.k-e"), "a colour picture for " + r.node.n);
    assert.equal(text(cells[1]), r.node.n);
    assert.ok(cells[1].querySelector("mark"), "the match is marked in " + r.node.n);
    assert.equal(text(cells[2]), A.hostName(r.host));
    assert.match(text(cells[3]), /^[^›]+( › [^›]+)?$/, "folder as 'a › b'");
    assert.match(text(cells[4]), /^\d{1,2} [A-Z][a-z]{2}( \d{4})?$/, "short date, got " + text(cells[4]));
    assert.match(text(cells[5]), /^(\d+(\.\d)? (B|KB|MB|GB|TB)|\d+\+? items?|—)$/, "size, got " + text(cells[5]));
    const btns = [...cells[5].querySelectorAll("button")];
    assert.equal(btns.length, 1, "only the open-folder button");
    assert.equal(btns[0].getAttribute("aria-label"), "Open the folder");
    assert.ok(btns[0].querySelector("svg"), "with an icon");
  });
  assert.deepEqual(o.errs, []);
});

test("the open-folder button opens the match's folder in Files, and a column filter narrows the rows", async () => {
  const o = await open();
  const { d, A } = o;
  await resultsFor(o, "e");
  const all = rowsOf(d).length;
  const dir = A.searchRun.results.filter((r) => r.host !== 'local').map((r) => r.dir.split("/").filter(Boolean).pop()).find(Boolean);
  const f = d.querySelector('#stage .rt [data-rf="folder"]');
  f.value = dir; f.dispatchEvent(new o.w.Event("input", { bubbles: true })); await sleep(60);
  const kept = rowsOf(d);
  assert.ok(kept.length >= 1 && kept.length <= all, "the Folder filter keeps the matching rows: " + kept.length + " of " + all + " for [" + dir + "] " + A.searchRun.results[0].dir);
  assert.ok(kept.filter((c) => text(c[2]) !== "This computer").every((c) => text(c[3]).toLowerCase().includes(dir.toLowerCase())), "every other computer's row shows the folder");
  kept[0][5].querySelector("button").click();
  await waitFor(() => A.state.view === "files");
  assert.equal(A.state.view, "files", "Files opened");
  assert.deepEqual(o.errs, []);
});

test("with no search yet the page still shows its title and filter header", async () => {
  const o = await open();
  const { d, A } = o;
  A.searchRun = null;
  A.go("search"); await sleep(250);
  assert.equal(text(d.querySelector("#stage h1")), "Search results");
  assert.equal(d.querySelectorAll("#stage .rt .hc").length, 6, "the six filters");
  assert.equal(rowsOf(d).length, 0, "no rows");
  assert.ok(!/Type in the box at the top/.test(text(d.querySelector("#stage"))), "no pointer at a box that is not on screen");
  assert.deepEqual(o.errs, []);
});

test("with no computer the page says so under the same title and keeps the Add a computer button", async () => {
  const o = await open();
  const { d, A, E } = o;
  A.searchRun = null;
  E.servers.splice(0);
  A.go("search"); await sleep(250);
  assert.equal(text(d.querySelector("#stage h1")), "Search results");
  assert.equal(text(d.querySelector("#stage .k-sub")), "Nothing to search yet");
  assert.equal(d.querySelectorAll("#stage .rt .hc").length, 6);
  assert.equal(text(d.querySelector('#stage .s-no [data-x="add"]')), "Add a computer");
  assert.deepEqual(o.errs, []);
});

test("the page's title and sub line use the Devices/Computers page size, and the match mark is the browser's own", () => {
  assert.match(css, /\.stage\.srch \.k-title\s*\{[^}]*font:\s*500 1\.75rem\/1\.15/);
  assert.match(css, /\.stage\.srch \.k-sub\s*\{[^}]*font-size:\s*0\.875rem/);
  assert.match(css, /\.stage\.srch \.rt mark\s*\{[^}]*background:\s*Mark/);
});
