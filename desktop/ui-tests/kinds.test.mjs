// Device kinds (PC, Laptop, Server, Mobile) on the Devices tab (ui/v5-computers.js):
//  A. a computer's kind defaults to PC, is kept per computer in the window's storage, and an unknown stored value reads as PC;
//  B. the kind icon (inline SVG, no emoji) is in the list row and the details header, and follows the kind;
//  C. the add dialog offers the four kinds and saves the chosen one for the new computer;
//  D. "Settings for <computer>" has the same choice, changes the kind, and hides what does not fit it
//     (Server and Mobile: waking; Mobile: the download folder) while PC and Laptop keep every row.
//
//   node --test desktop/ui-tests/kinds.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });
const PKEY = "rfe-v5-pcprefs-v1";
const KINDS = ["pc", "laptop", "server", "mobile"];

/** The window on the stub core, with every script error collected in `errs`. `seed` pre-fills storage keys before the scripts run. */
async function open(seed = {}) {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1",
    beforeParse(w) {
      const mem = new Map(Object.entries(seed));
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (mem.has(k) ? mem.get(k) : null), setItem: (k, v) => mem.set(k, String(v)), removeItem: (k) => mem.delete(k) }, configurable: true });
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.TextEncoder = TextEncoder; w.TextDecoder = TextDecoder;
      w.ResizeObserver = class { observe() {} disconnect() {} };
      w.HTMLElement.prototype.scrollIntoView = function () {};
      Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
      Object.defineProperty(w, "innerWidth", { value: 1400, configurable: true, writable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(300);
  return { w, d: w.document, A: w.A, E: w.A.E, T: w.A.v5Computers, errs };
}
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const dlg = (d) => d.querySelector(".k-dlg");
const stored = (w) => JSON.parse(w.localStorage.getItem(PKEY) || "{}");
const row = (el, label) => [...el.querySelectorAll(".k-set")].find((r) => text(r.firstElementChild.querySelector("b")) === label);
/** Whether an element is shown: neither it nor a parent is hidden. */
const shown = (el) => !!el && !el.closest("[hidden]");
const kindBtns = (el) => [...el.querySelectorAll('.k-seg[data-kd] button')];
const pressed = (el) => kindBtns(el).filter((b) => b.getAttribute("aria-pressed") === "true").map((b) => b.dataset.v);
async function settingsOf(o, name) {
  const s = o.E.servers.find((x) => x.name === name);
  assert.ok(s, "the seed has " + name);
  o.T.settings(s); await sleep(100);
  return { s, el: dlg(o.d) };
}
const pick = (el, kind) => { kindBtns(el).find((b) => b.dataset.v === kind).click(); };

test("A. the kind defaults to pc, is kept per computer, and an unknown value reads as pc", async () => {
  const o = await open();
  const [a, b] = o.E.servers;
  assert.equal(o.T.kind(a), "pc", "default");
  assert.equal(o.T.kind(b), "pc", "default");
  assert.equal(stored(o.w)[a.id], undefined, "nothing is written until a kind is chosen");
  o.T.setKind(a, "server");
  assert.equal(o.T.kind(a), "server");
  assert.equal(o.T.kind(b), "pc", "another computer is untouched");
  assert.equal(stored(o.w)[a.id].kind, "server", "kept in the per-computer prefs under " + PKEY);
  o.T.setPref(a, "dir", "/tmp/x");
  assert.equal(o.T.kind(a), "server", "other prefs do not clear it");
  for (const k of KINDS) { o.T.setKind(b, k); assert.equal(o.T.kind(b), k); }
  o.T.setKind(b, "spaceship");
  assert.equal(o.T.kind(b), "pc", "an unknown kind passed to the setter reads as pc");
  assert.deepEqual(o.errs, []);
});

test("A2. a kind kept before a restart is read back; an unknown or odd stored value is pc", async () => {
  const probe = await open();
  const [a, b, c] = probe.E.servers;
  assert.ok(a && b, "the seed has two computers");
  const seed = { [PKEY]: JSON.stringify({ [a.id]: { kind: "laptop" }, [b.id]: { kind: "toaster" }, ...(c ? { [c.id]: { kind: 7 } } : {}) }) };
  const o = await open(seed);
  assert.equal(o.T.kind(o.E.server(a.id)), "laptop", "kept");
  assert.equal(o.T.kind(o.E.server(b.id)), "pc", "unknown string");
  if (c) assert.equal(o.T.kind(o.E.server(c.id)), "pc", "not a string");
  assert.deepEqual(o.errs, []);
});

test("B. the kind icon is an inline SVG in the list row and the details header and follows the kind", async () => {
  const o = await open();
  o.A.go("devices"); await sleep(200);
  const s = o.E.servers[0];
  const rowOf = () => o.d.querySelector('.cpg .ro[data-id="' + s.id + '"]');
  const icon = (el) => el.querySelector(".k-kind");
  assert.ok(icon(rowOf()), "the row has a kind icon");
  assert.ok(icon(rowOf()).querySelector("svg"), "it is an SVG");
  assert.equal(icon(rowOf()).dataset.kind, "pc");
  assert.equal(text(rowOf().querySelector("b")), s.name, "the icon adds no text to the name");
  const head = () => [...o.d.querySelectorAll(".cpg h2")].find((h) => text(h) === s.name);
  assert.ok(icon(head()), "the details header has a kind icon");
  assert.equal(text(head()), s.name, "the icon adds no text to the header");
  const shapes = new Set();
  for (const k of KINDS) {
    o.T.setKind(s, k); o.A.renderAll(); await sleep(150);
    assert.equal(icon(rowOf()).dataset.kind, k, "row icon for " + k);
    assert.equal(icon(head()).dataset.kind, k, "header icon for " + k);
    shapes.add(icon(rowOf()).innerHTML);
    assert.ok(!/[\u{1F300}-\u{1FAFF}☀-➿]/u.test(icon(rowOf()).textContent), "no emoji");
  }
  assert.equal(shapes.size, 4, "each kind has its own drawing");
  assert.deepEqual(o.errs, []);
});

test("C. the add dialog offers four kinds (pc chosen) and saves the chosen kind for the new computer", async () => {
  const o = await open();
  o.d.querySelector("#rail [data-new]").click(); await sleep(200);
  const el = dlg(o.d);
  assert.deepEqual(kindBtns(el).map((b) => b.dataset.v), KINDS);
  assert.deepEqual(kindBtns(el).map(text), ["PC", "Laptop", "Server", "Mobile"]);
  assert.deepEqual(pressed(el), ["pc"], "pc is the default choice");
  pick(el, "server"); await sleep(30);
  assert.deepEqual(pressed(dlg(o.d)), ["server"], "the choice shows");
  const f = o.d.querySelector("#v5addr");
  f.closest("details").open = true; f.value = "10.9.9.7";
  f.dispatchEvent(new o.w.Event("input", { bubbles: true }));
  assert.deepEqual(pressed(dlg(o.d)), ["server"], "typing the address keeps the choice");
  [...o.d.querySelectorAll(".scrim .df button")].find((b) => text(b) === "Next").click(); await sleep(500);
  const added = o.E.server("10.9.9.7:8765");
  assert.ok(added, "the computer was added");
  assert.equal(o.T.kind(added), "server", "its kind is the one chosen");
  assert.equal(stored(o.w)[added.id].kind, "server");
  assert.deepEqual(o.errs, []);
});

test("C2. adding with the default choice keeps pc, and adding does not change an existing computer's kind", async () => {
  const o = await open();
  const [a] = o.E.servers;
  o.T.setKind(a, "laptop");
  o.d.querySelector("#rail [data-new]").click(); await sleep(200);
  const f = o.d.querySelector("#v5addr");
  f.closest("details").open = true; f.value = "10.9.9.8";
  f.dispatchEvent(new o.w.Event("input", { bubbles: true }));
  [...o.d.querySelectorAll(".scrim .df button")].find((b) => text(b) === "Next").click(); await sleep(500);
  assert.equal(o.T.kind(o.E.server("10.9.9.8:8765")), "pc");
  assert.equal(o.T.kind(o.E.server(a.id)), "laptop");
  assert.deepEqual(o.errs, []);
});

test("D. the settings dialog shows the kind, changes it, and keeps it per computer", async () => {
  const o = await open();
  let { s, el } = await settingsOf(o, "atlas-nas");
  const r = row(el, "Kind of device");
  assert.ok(r, "a Kind of device row");
  assert.deepEqual(kindBtns(r).map((b) => b.dataset.v), KINDS);
  assert.deepEqual(pressed(r), ["pc"]);
  pick(el, "laptop"); await sleep(30);
  assert.deepEqual(pressed(dlg(o.d)), ["laptop"]);
  assert.equal(o.T.kind(s), "laptop", "saved at once");
  [...dlg(o.d).querySelectorAll("button")].find((b) => text(b) === "Done").click(); await sleep(100);
  ({ el } = await settingsOf(o, "atlas-nas"));
  assert.deepEqual(pressed(el), ["laptop"], "kept when the dialog is opened again");
  [...el.querySelectorAll("button")].find((b) => text(b) === "Done").click(); await sleep(100);
  ({ el } = await settingsOf(o, "build-box"));
  assert.deepEqual(pressed(el), ["pc"], "another computer keeps its own");
  assert.deepEqual(o.errs, []);
});

test("E. per-kind options: pc and laptop show everything; server and mobile hide waking; mobile also hides the download folder", async () => {
  const o = await open();
  const { el } = await settingsOf(o, "atlas-nas");
  const wake = () => shown(row(dlg(o.d), "Wake it when it is asleep"));
  const wakeHead = () => shown([...dlg(o.d).querySelectorAll(".b-ss h3")].find((h) => text(h) === "Waking it up"));
  const folder = () => shown(row(dlg(o.d), "Downloads from it go to"));
  const expect = { pc: [true, true], laptop: [true, true], server: [false, true], mobile: [false, false] };
  for (const k of ["pc", "server", "mobile", "laptop", "mobile", "pc"]) {
    pick(dlg(o.d), k); await sleep(30);
    const [w, f] = expect[k];
    assert.equal(wake(), w, k + ": wake row");
    assert.equal(wakeHead(), w, k + ": waking heading goes with its row");
    assert.equal(folder(), f, k + ": download folder row");
    for (const lbl of ["Name", "Connect when RFE starts", "Show a notice when it goes offline", "Show hidden files", "Ask before replacing a file", "Use at most", "Transfers at the same time"]) {
      assert.ok(shown(row(dlg(o.d), lbl)), k + ": " + lbl + " stays");
    }
  }
  assert.ok(el, "dialog opened");
  // opening the dialog for a computer that already has a kind starts with the right rows hidden
  [...dlg(o.d).querySelectorAll("button")].find((b) => text(b) === "Done").click(); await sleep(100);
  const s = o.E.servers.find((x) => x.name === "atlas-nas");
  o.T.setKind(s, "mobile");
  await settingsOf(o, "atlas-nas");
  assert.equal(wake(), false); assert.equal(folder(), false);
  assert.deepEqual(o.errs, []);
});

test("F. a hidden option stays off: no automatic wake for a server or a mobile, and the details panel offers no wake button for them", async () => {
  const o = await open();
  const s = o.E.servers.find((x) => x.mac) || o.E.servers[0];
  s.mac = s.mac || "aa:bb:cc:dd:ee:ff";
  s.state = "offline";
  o.A.go("devices"); o.T.pick = s.id; o.A.renderAll(); await sleep(200);
  const wakeBtn = () => o.d.querySelector('.cpg [data-pa="wake"]');
  assert.ok(wakeBtn(), "a pc offers Wake computer");
  o.T.setKind(s, "laptop"); o.A.renderAll(); await sleep(150);
  assert.ok(wakeBtn(), "a laptop offers Wake computer");
  for (const k of ["server", "mobile"]) {
    o.T.setKind(s, k); o.A.renderAll(); await sleep(150);
    assert.equal(wakeBtn(), null, k + " offers no Wake computer");
  }
  assert.deepEqual(o.errs, []);
});

test("G. the new words have German, Spanish and Arabic rows", async () => {
  const o = await open();
  const words = ["PC", "Laptop", "Server", "Mobile", "Kind of device", "What kind of device is it?", "Hides the options that do not fit it"];
  for (const lang of ["de", "es", "ar"]) {
    o.A.S.lang = lang;
    for (const w of words) {
      const t = o.A.tr(w);
      assert.ok(t && t.length, lang + ": " + w);
      if (w.length > 6) assert.notEqual(t, w, lang + ": " + w + " is translated");
    }
  }
  o.A.S.lang = "en";
  assert.deepEqual(o.errs, []);
});
