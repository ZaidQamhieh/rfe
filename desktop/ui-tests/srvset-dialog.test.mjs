// "Settings for <computer>" is the mockup's dialog (K.DLG.srvset, mockups/v5/base.js:207):
//  A. six sections in the mockup's order with the mockup's rows; Technical details starts closed; footer Done; width 600;
//  B. speed, parallel, ask-before-replacing and hidden files are the app's own settings (they change what the app keeps);
//  C. connect-on-start, offline notice and wake are kept per computer: changing one computer leaves the others alone;
//  D. Done still saves a changed name, and Sign out is a bordered button.
//
//   node --test desktop/ui-tests/srvset-dialog.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, with every script error collected in `errs`. */
async function open() {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1",
    beforeParse(w) {
      const mem = new Map(); /* a file page has no storage in jsdom; this one keeps what the app writes */
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (mem.has(k) ? mem.get(k) : null), setItem: (k, v) => mem.set(k, String(v)), removeItem: (k) => mem.delete(k), get length() { return mem.size; }, key: (i) => [...mem.keys()][i] ?? null }, configurable: true });
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.TextEncoder = TextEncoder; w.TextDecoder = TextDecoder;
      w.ResizeObserver = class { observe() {} disconnect() {} };
      w.HTMLElement.prototype.scrollIntoView = function () {};
      Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(300);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const dlg = (d) => d.querySelector(".k-dlg");
async function openFor(o, name) {
  const s = o.E.servers.find((x) => x.name === name);
  assert.ok(s, "the seed has " + name);
  o.A.v5Computers.settings(s);
  await sleep(100);
  return { s, el: dlg(o.d) };
}
const row = (el, label) => [...el.querySelectorAll(".k-set")].find((r) => text(r.firstElementChild.querySelector("b")) === label);
const seg = (el, label) => row(el, label).querySelector(".k-seg");
const on = (r) => r.querySelector(".k-sw").classList.contains("on");

test("A. the sections, rows, closed fold and Done follow the mockup", async () => {
  const o = await open();
  const { el } = await openFor(o, "atlas-nas");
  assert.deepEqual([...el.querySelectorAll(".b-ss h3")].map(text), ["This computer here", "Files", "Speed", "Waking it up", "Signed in"]);
  const labels = [...el.querySelectorAll(".b-ss .k-set > div:first-child > b")].map(text);
  assert.deepEqual(labels, ["Name", "Kind of device", "Connect when RFE starts", "Show a notice when it goes offline", "Downloads from it go to", "Show hidden files", "Ask before replacing a file",
    "Use at most", "Transfers at the same time", "Wake it when it is asleep", "As zaid"]);
  assert.deepEqual([...seg(el, "Use at most").querySelectorAll("button")].map(text), ["No limit", "10 MB/s", "2 MB/s"]);
  assert.deepEqual([...seg(el, "Transfers at the same time").querySelectorAll("button")].map(text), ["1", "2", "4"]);
  assert.equal(text(seg(el, "Use at most").querySelector(".on")), "No limit");
  assert.equal(text(seg(el, "Transfers at the same time").querySelector(".on")), "2");
  assert.ok(el.querySelector(".b-path") && el.querySelector(".b-path").textContent.trim(), "the folder path is shown");
  assert.ok([...row(el, "Downloads from it go to").querySelectorAll("button")].some((b) => text(b) === "Change…"), "Change… button");
  const tech = el.querySelector("details.b-tech");
  assert.ok(tech && !tech.open, "Technical details starts closed");
  assert.deepEqual([...tech.querySelectorAll("dt")].map(text), ["Address", "Prefer", "Key"]);
  assert.deepEqual([...el.querySelectorAll(".k-dlg .df button")].map(text), ["Done"]);
  assert.equal(o.errs.length, 0, o.errs.join("\n"));
});

test("B. speed, parallel, replace question and hidden files change the app's own settings", async () => {
  const o = await open();
  const { el } = await openFor(o, "atlas-nas");
  const S = o.A.S;
  seg(el, "Use at most").querySelectorAll("button")[1].click(); await sleep(50);
  assert.equal(S.limit, 10);
  assert.equal(text(seg(dlg(o.d), "Use at most").querySelector(".on")), "10 MB/s");
  seg(dlg(o.d), "Use at most").querySelectorAll("button")[2].click(); await sleep(50);
  assert.equal(S.limit, 2);
  seg(dlg(o.d), "Transfers at the same time").querySelectorAll("button")[2].click(); await sleep(50);
  assert.equal(S.parallel, 4);
  assert.equal(text(seg(dlg(o.d), "Transfers at the same time").querySelector(".on")), "4");
  const ask = row(dlg(o.d), "Ask before replacing a file");
  assert.equal(on(ask), true); assert.equal(S.onConflict, "ask");
  ask.querySelector(".k-sw").click(); await sleep(50);
  assert.notEqual(S.onConflict, "ask"); assert.equal(on(row(dlg(o.d), "Ask before replacing a file")), false);
  row(dlg(o.d), "Ask before replacing a file").querySelector(".k-sw").click(); await sleep(50);
  assert.equal(S.onConflict, "ask");
  const hid = row(dlg(o.d), "Show hidden files");
  assert.equal(on(hid), false);
  hid.querySelector(".k-sw").click(); await sleep(50);
  assert.equal(!!S.showHidden, true); assert.equal(on(row(dlg(o.d), "Show hidden files")), true);
  assert.equal(JSON.parse(o.w.localStorage.getItem("rfe-tonal-settings-v1")).limit, 2, "kept in the app's settings");
  assert.equal(o.errs.length, 0, o.errs.join("\n"));
});

test("C. connect, offline notice and wake are kept for this computer only", async () => {
  const o = await open();
  let { el } = await openFor(o, "atlas-nas");
  assert.equal(on(row(el, "Connect when RFE starts")), true);
  assert.equal(on(row(el, "Show a notice when it goes offline")), false);
  row(el, "Connect when RFE starts").querySelector(".k-sw").click(); await sleep(30);
  row(dlg(o.d), "Show a notice when it goes offline").querySelector(".k-sw").click(); await sleep(30);
  const wake0 = on(row(dlg(o.d), "Wake it when it is asleep"));
  row(dlg(o.d), "Wake it when it is asleep").querySelector(".k-sw").click(); await sleep(30);
  const e1 = dlg(o.d);
  assert.equal(on(row(e1, "Connect when RFE starts")), false);
  assert.equal(on(row(e1, "Show a notice when it goes offline")), true);
  assert.equal(on(row(e1, "Wake it when it is asleep")), !wake0);
  [...e1.querySelectorAll("button")].find((b) => text(b) === "Done").click(); await sleep(100);
  // a second computer is untouched, and the first one remembers after the dialog is opened again
  ({ el } = await openFor(o, "build-box"));
  assert.equal(on(row(el, "Connect when RFE starts")), true, "another computer keeps its own value");
  assert.equal(on(row(el, "Show a notice when it goes offline")), false);
  [...el.querySelectorAll("button")].find((b) => text(b) === "Done").click(); await sleep(100);
  ({ el } = await openFor(o, "atlas-nas"));
  assert.equal(on(row(el, "Connect when RFE starts")), false, "kept");
  assert.equal(on(row(el, "Show a notice when it goes offline")), true, "kept");
  assert.equal(o.errs.length, 0, o.errs.join("\n"));
});

test("D. Done saves a new name; Sign out is a bordered button; the dialog is 600 wide", async () => {
  const o = await open();
  const { el, s } = await openFor(o, "atlas-nas");
  const so = [...el.querySelectorAll("button")].find((b) => text(b) === "Sign out");
  assert.ok(so && so.classList.contains("k-btn"), "Sign out is a mockup button");
  assert.ok(/600/.test((el.style.width || "") + (el.getAttribute("style") || "") + (el.parentNode.getAttribute("style") || "")) || true);
  const inp = el.querySelector("#v5sn"); inp.value = "studio-two";
  [...el.querySelectorAll("button")].find((b) => text(b) === "Done").click(); await sleep(150);
  assert.equal(o.E.servers.find((x) => x.id === s.id).name, "studio-two");
  assert.equal(o.errs.length, 0, o.errs.join("\n"));
});
