// The v5 Search panel (ui/v5-transfers.js, "Search panel"): a 27.5 rem panel (#srp.b-sr) that opens over the page from the
// sidebar's Search item or the / key, closes with Esc, a click on the scrim or the same sidebar item, shows results grouped by
// computer (3 each, then "N more on X"), moves with Up/Down, opens with Enter, and hands "Show all" to the Search page's table.
// The real window scripts (ui/*.js) run in jsdom on the stub core, like v5-wording.test.mjs.
//
//   node --test desktop/ui-tests/v5-search-panel.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";
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
  return { w, d: w.document, A: w.A, E: w.A.E, P: w.A.v5Pages.panel, errs };
}

const text = (el) => (el ? el.textContent.replace(/\s+/g, " ").trim() : "");
const until = async (fn, what) => { for (let i = 0; i < 150; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };
const key = (w, target, k, o = {}) => { const e = new w.KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true, ...o }); target.dispatchEvent(e); return e; };
const isOpen = (d) => !!d.querySelector("#srp.b-sr.open");
/** Waits for the search the panel started to finish and its rows to be drawn. */
const finished = async (A) => { await until(() => A.searchRun && A.searchRun.done, "the search to finish"); await sleep(60); };
/** The panel searching every connected computer for `q`, finished. */
async function searched(o, q) {
  o.P.open(q);
  await finished(o.A);
}

test("the top bar Search button opens the panel over the page and the same button closes it; the page stays", async () => {
  const { d, A, errs } = await open();
  const before = d.querySelector("#stage").className;
  d.querySelector('#top #btnSearch').click(); await sleep(50);
  assert.ok(isOpen(d), "the panel is open");
  assert.ok(d.querySelector("#app").classList.contains("sr-open"), "the app marks it");
  assert.equal(d.querySelector("#stage").className, before, "the page behind is not the Search page");
  assert.equal(d.querySelector("#stage.srch"), null);
  assert.equal(d.activeElement, d.querySelector("#srq"), "the cursor is in the field");
  assert.equal(text(d.querySelector("#srp .s-f kbd")), "Esc");
  assert.equal(d.querySelector("#srp .s-gear"), null, "there is no gear: nothing is chosen before searching");
  assert.equal(d.querySelectorAll("#srp .s-sc button").length, 0, "the counts appear only once a search has results");
  assert.deepEqual([...d.querySelectorAll("#srp .s-ft .k")].map(text), ["Enter open", "Ctrl D download"]);
  d.querySelector('#top #btnSearch').click(); await sleep(50);
  assert.equal(isOpen(d), false, "the button toggles it closed");
  assert.equal(d.querySelector("#app").classList.contains("sr-open"), false);
  assert.ok(A.v5Pages.panel.el, "the panel stays built for next time");
  assert.deepEqual(errs, []);
});

test("/ opens the panel (but not while typing in a field), Esc closes it, and so does a click on the scrim", async () => {
  const { w, d, errs } = await open();
  key(w, d.body, "/"); await sleep(30);
  assert.ok(isOpen(d), "/ opens it");
  key(w, d.querySelector("#srq"), "Escape");
  assert.equal(isOpen(d), false, "Esc closes it");
  d.querySelector("#q").dispatchEvent(new w.KeyboardEvent("keydown", { key: "/", bubbles: true, cancelable: true }));
  assert.equal(isOpen(d), false, "a slash typed in a field does not open it");
  key(w, d.body, "/"); await sleep(30);
  assert.ok(isOpen(d));
  d.querySelector("#srp").click();
  assert.equal(isOpen(d), false, "a click outside the box closes it");
  key(w, d.body, "/"); await sleep(30);
  d.querySelector("#srp .m").click();
  assert.ok(isOpen(d), "a click inside the box keeps it open");
  A_go(d, w);
  assert.deepEqual(errs, []);
});

test("Ctrl+K and Cmd+K open the panel from any page and from inside a field, not the command palette", async () => {
  const { w, d, A, errs } = await open();
  const noPalette = () => assert.equal(d.querySelector(".pal"), null, "the command palette did not open");
  key(w, d.body, "k", { ctrlKey: true }); await sleep(30);
  assert.ok(isOpen(d), "Ctrl+K opens it");
  noPalette();
  key(w, d.querySelector("#srq"), "Escape");
  assert.equal(isOpen(d), false);
  key(w, d.body, "K", { metaKey: true }); await sleep(30);
  assert.ok(isOpen(d), "Cmd+K opens it too");
  key(w, d.querySelector("#srq"), "Escape");
  // From another page.
  A.go("files"); await sleep(150);
  key(w, d.body, "k", { ctrlKey: true }); await sleep(30);
  assert.ok(isOpen(d), "opens over the Files page");
  assert.equal(d.querySelector("#stage.srch"), null, "the page behind is not the Search page");
  key(w, d.querySelector("#srq"), "Escape");
  // From inside a text field (a slash would not open it there).
  const q = d.querySelector("#q");
  assert.ok(q, "the top-bar field stays in the DOM");
  const e = key(w, q, "k", { ctrlKey: true }); await sleep(30);
  assert.ok(isOpen(d), "opens while the cursor is in a field");
  assert.equal(e.defaultPrevented, true, "the browser's own Ctrl+K is cancelled");
  noPalette();
  // A bare k is not the shortcut.
  key(w, d.querySelector("#srq"), "Escape");
  key(w, d.body, "k");
  assert.equal(isOpen(d), false, "a bare k does nothing");
  assert.deepEqual(errs, []);
});

test("Ctrl+K is ignored while a dialog is open", async () => {
  const { w, d, errs } = await open();
  const scrim = d.createElement("div"); scrim.className = "scrim"; d.body.appendChild(scrim);
  key(w, d.body, "k", { ctrlKey: true }); await sleep(30);
  assert.equal(isOpen(d), false, "the dialog keeps the keys");
  scrim.remove();
  key(w, d.body, "k", { ctrlKey: true }); await sleep(30);
  assert.ok(isOpen(d), "and the shortcut works again once it is gone");
  assert.deepEqual(errs, []);
});

test("the top-bar search field is hidden by the stylesheet under the v5 theme (the field itself stays in the DOM)", async () => {
  const css = readFileSync(fileURLToPath(new URL("../ui/css/v5-search.css", import.meta.url)), "utf8");
  assert.match(css, /:where\(\.d5\)\s+\.top\s+\.sbar[^{}]*\{\s*display:\s*none\s*;?\s*\}/, ".sbar is display:none under .d5, also when [hidden] is lifted");
  assert.match(css, /\.sbar\[hidden\][^{}]*\{\s*display:\s*none/, "the [hidden] form is covered too");
  const { d, errs } = await open();
  assert.ok(d.querySelector("#q") && d.querySelector("#q").closest(".sbar"), "#q is still in the .sbar");
  assert.deepEqual(errs, []);
});

/** A page change (here Files) closes an open panel. */
function A_go(d, w) { w.A.go("files"); assert.equal(isOpen(d), false, "going to another page closes the panel"); }

test("results are grouped by computer: 3 rows each, then \"N more on X\"", async () => {
  const o = await open();
  const { d, A } = o;
  await searched(o, "e");
  const sr = A.searchRun;
  const hosts = sr.hosts.filter((h) => sr.results.some((r) => r.host === h));
  assert.ok(hosts.length >= 1, "something was found");
  const groups = []; let g = null;
  for (const el of d.querySelector("#srp .s-ls").children) {
    if (el.classList.contains("s-g")) groups.push((g = { name: text(el.querySelector("span[data-nolocal]")), rows: 0, more: "" }));
    else if (g && el.classList.contains("s-r")) g.rows++;
    else if (g && el.classList.contains("s-mo")) g.more = text(el);
  }
  assert.equal(groups.length, hosts.length, "one group per computer with results");
  let folded = 0;
  hosts.forEach((h, i) => {
    const n = sr.results.filter((r) => r.host === h).length;
    assert.equal(groups[i].name, A.hostName(h));
    assert.equal(groups[i].rows, Math.min(3, n), "at most 3 rows for " + A.hostName(h));
    if (n > 3) { folded++; assert.equal(groups[i].more, (n - 3) + " more on " + A.hostName(h)); } else assert.equal(groups[i].more, "", "a short group is not folded");
  });
  assert.ok(folded >= 1, "the stub data has a computer with more than 3 matches for 'e'");
  assert.match(text(d.querySelector("#srp .s-ft")), new RegExp("Save Show all " + sr.results.length + "$"));
  assert.deepEqual(o.errs, []);
});

test("Up/Down move the chosen row and Enter in the field opens it in Files and closes the panel", async () => {
  const o = await open();
  const { w, d, A } = o;
  await searched(o, "e");
  const rows = () => [...d.querySelectorAll("#srp .s-ls .s-r")];
  const on = () => rows().findIndex((r) => r.classList.contains("on"));
  assert.equal(on(), 0, "the first row is chosen at first");
  const inp = d.querySelector("#srq");
  key(w, inp, "ArrowDown"); assert.equal(on(), 1);
  key(w, inp, "ArrowDown"); assert.equal(on(), 2);
  key(w, inp, "ArrowUp"); assert.equal(on(), 1);
  key(w, inp, "ArrowUp"); key(w, inp, "ArrowUp"); assert.equal(on(), 0, "stops at the first row");
  key(w, inp, "ArrowDown");
  assert.equal(rows()[1].getAttribute("aria-current"), "true");
  const want = A.searchRun.results[+rows()[1].dataset.i];
  const calls = []; A.openPath = (...a) => calls.push(a);
  key(w, inp, "Enter");
  assert.equal(calls.length, 1, "Enter opened one result");
  assert.equal(calls[0][0], want.host, "on the result's computer");
  assert.equal(isOpen(d), false, "the panel closed");
  assert.deepEqual(o.errs, []);
});

test("Enter on new text runs the search first; Enter again opens the chosen result", async () => {
  const o = await open();
  const { w, d, A, P } = o;
  P.open();
  const inp = d.querySelector("#srq");
  assert.equal(A.searchRun, null, "nothing ran yet");
  inp.value = "e";
  key(w, inp, "Enter");
  await finished(A);
  assert.ok(A.searchRun.results.length > 0, "Enter ran the search");
  assert.ok(isOpen(d), "and kept the panel open");
  assert.ok(d.querySelector("#srp .s-ls .s-r.on"));
  const calls = []; A.openPath = (...a) => calls.push(a);
  key(w, inp, "Enter");
  assert.equal(calls.length, 1);
  assert.equal(isOpen(d), false);
  assert.deepEqual(o.errs, []);
});

test("Show all hands the search to the Search page's table and closes the panel", async () => {
  const o = await open();
  const { d, A } = o;
  await searched(o, "e");
  const n = A.searchRun.results.length;
  d.querySelector('#srp .s-ft [data-x="pall"]').click(); await sleep(250);
  assert.equal(isOpen(d), false, "the panel closed");
  assert.ok(d.querySelector("#stage.srch"), "the Search page is showing");
  assert.equal(text(d.querySelector("#stage h1")), "Search results");
  assert.ok(d.querySelector('#stage .rt [data-rf="pc"]'), "as the table");
  assert.equal(d.querySelector('#stage .rt [data-rf="pc"]').value, "Any", "every computer");
  assert.equal(A.searchRun.results.length, n, "the same search");
  assert.deepEqual(o.errs, []);
});

test("\"N more on X\" opens the table narrowed to that computer", async () => {
  const o = await open();
  const { d, A } = o;
  await searched(o, "e");
  const more = d.querySelector("#srp .s-ls .s-mo");
  assert.ok(more, "a folded group");
  const host = more.dataset.h;
  more.click(); await sleep(250);
  assert.equal(isOpen(d), false);
  assert.equal(d.querySelector('#stage .rt [data-rf="pc"]').value, host, "the table shows only " + A.hostName(host));
  assert.deepEqual(o.errs, []);
});

test("the empty states: no computer, and no matches", async () => {
  const o = await open();
  const { d, A, E, P } = o;
  A.searchRun = null;
  E.servers.splice(0);
  P.open(); await sleep(50);
  const no = d.querySelector("#srp .s-ls .s-no");
  assert.ok(no, "the empty state is drawn");
  assert.equal(text(no.querySelector("b")), "Add a computer to search it");
  assert.equal(text(no.querySelector('button[data-x="padd"]')), "Add a computer");
  assert.equal(text(d.querySelector("#srp .s-sc")), "", "no counts before a search");
  assert.equal(d.querySelector("#srp .s-ft [data-x=\"pall\"]"), null, "no Show all without results");
  assert.deepEqual(o.errs, []);
});

test("a search with no matches says so and offers neither Save nor Show all", async () => {
  const o = await open();
  const { d, A, P } = o;
  P.open("zzqqxx-no-such-name"); await finished(A);
  assert.equal(A.searchRun.results.length, 0);
  assert.equal(text(d.querySelector("#srp .s-ls .s-no b")), "No matches");
  assert.equal(d.querySelector('#srp .s-ft [data-x="psave"]'), null);
  assert.equal(d.querySelector('#srp .s-ft [data-x="pall"]'), null);
  assert.deepEqual(o.errs, []);
});

test("A.runQuery runs inside the panel while it is open", async () => {
  const o = await open();
  const { d, A, P } = o;
  P.open();
  A.runQuery("e"); await finished(A);
  assert.ok(isOpen(d), "the panel is still open");
  assert.equal(d.querySelector("#srq").value, "e");
  assert.equal(d.querySelector("#stage.srch"), null, "the Search page was not opened");
  assert.ok(d.querySelectorAll("#srp .s-ls .s-r").length > 0);
  assert.deepEqual(o.errs, []);
});

test("after a search the counts by kind and computer appear, and one click narrows the list already found", async () => {
  const o = await open();
  const { d, A, P } = o;
  P.open("e"); await finished(A);
  const fk = [...d.querySelectorAll('#srp .s-sc [data-x="pfk"]')];
  assert.equal(text(fk[0]).replace(/\d+$/, "").trim(), "All", "All comes first");
  assert.equal(fk[0].getAttribute("aria-pressed"), "true");
  const before = d.querySelectorAll("#srp .s-ls .s-r").length;
  if (fk.length > 1) {
    fk[1].click(); await sleep(30);
    assert.equal(d.querySelector('#srp .s-sc [data-x="pfk"][aria-pressed="true"]').dataset.v, fk[1].dataset.v);
    assert.ok(d.querySelectorAll("#srp .s-ls .s-r").length <= before, "narrowing never adds rows");
    d.querySelector('#srp .s-sc [data-x="pfk"][data-v=""]').click(); await sleep(30);
    assert.equal(d.querySelectorAll("#srp .s-ls .s-r").length, before, "All brings them back");
  }
  assert.deepEqual(o.errs, []);
});
