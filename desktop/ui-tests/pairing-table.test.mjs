// The Devices page lists waiting pairing requests as ONE compact table (Device, Number, Time left,
// actions) and shows a plain empty line when nobody is asking. Same jsdom harness as window.test.mjs.
//
//   node --test desktop/ui-tests/pairing-table.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, started on the Devices page, with every script error collected in `errs`. */
async function open() {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 300)));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1&view=devices",
    beforeParse(w) {
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
  await sleep(150);
  return { w, d: w.document, A: w.A, errs };
}
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const REQS = [
  { id: "r1", host: "x", label: "Pixel 9", address: "192.168.1.77", matchCode: "4829 1307", ageSeconds: 18, expiresInSeconds: 102 },
  { id: "r2", host: "x", label: "sam-thinkpad", address: "10.0.0.5", matchCode: "0316 8842", ageSeconds: 51, expiresInSeconds: 69, replaces: "work-laptop" },
];
/** Puts `reqs` in the inbox and draws the Devices page. */
const show = (A, reqs) => { A.X.inbox = reqs.map((q) => ({ ...q })); A.pageDevices(); };

test("one table row per waiting request, with Device, Number and Time left", async () => {
  const { d, A, errs } = await open();
  show(A, REQS);
  const tables = d.querySelectorAll("#stage table.prt");
  assert.equal(tables.length, 1, "one table for all requests");
  assert.deepEqual([...tables[0].querySelectorAll("thead th")].slice(0, 3).map(text), ["Device", "Number", "Time left"]);
  const rows = tables[0].querySelectorAll("tbody tr");
  assert.equal(rows.length, 2);
  assert.match(text(rows[0]), /Pixel 9/); assert.match(text(rows[0]), /4829 1307/); assert.match(text(rows[0]), /1:42/);
  assert.match(text(rows[1]), /sam-thinkpad/); assert.match(text(rows[1]), /0316 8842/); assert.match(text(rows[1]), /1:09/);
  assert.match(text(rows[1]), /Replace work-laptop/);
  assert.equal(d.querySelectorAll("#stage .fxc.warn").length, 0, "the old big cards are gone");
  assert.deepEqual(errs, []);
});

test("without a lifetime the Time left column falls back to how long the request has waited", async () => {
  const { d, A } = await open();
  show(A, [{ id: "r1", host: "x", label: "Pixel 9", address: "1.2.3.4", matchCode: "1111 2222", ageSeconds: 7 }]);
  assert.match(text(d.querySelector("#stage table.prt tbody tr")), /just now|7 s/);
});

test("every action has a border class and an icon; Refuse is the only destructive control", async () => {
  const { d, A } = await open();
  show(A, REQS);
  const btns = [...d.querySelectorAll("#stage table.prt button")];
  assert.equal(btns.length, 4);
  for (const b of btns) { assert.ok(b.classList.contains("k-btn"), "a bordered button"); assert.ok(b.querySelector("svg"), "has an icon: " + text(b)); }
  const allow = btns.filter((b) => /^Allow…$/.test(text(b))), refuse = btns.filter((b) => /^Refuse$/.test(text(b)));
  assert.equal(allow.length, 2); assert.equal(refuse.length, 2);
  assert.ok(refuse.every((b) => b.classList.contains("warn")), "Refuse is destructive-styled");
  assert.ok(allow.every((b) => !b.classList.contains("warn")), "Allow is not");
});

test("Allow… and Refuse call the existing approve and deny actions for their own row", async () => {
  const { d, A } = await open();
  show(A, REQS);
  const rows = d.querySelectorAll("#stage table.prt tbody tr");
  const hooks = [...rows].map((r) => [...r.querySelectorAll("button")].map((b) => b.getAttribute("data-fx")));
  assert.deepEqual(hooks, [["dev.approve|0", "dev.deny|0"], ["dev.approve|1", "dev.deny|1"]]);
  // Clicking runs the real handlers: Allow… opens the approve dialog, Refuse the refuse dialog, for that row's device.
  rows[1].querySelector('[data-fx="dev.approve|1"]').click();
  await sleep(50);
  assert.match(text(d.querySelector(".dlg")), /Accept sam-thinkpad\?/);
  d.querySelector(".dlg").closest(".scrim").remove();
  rows[0].querySelector('[data-fx="dev.deny|0"]').click();
  await sleep(50);
  assert.match(text(d.querySelector(".dlg")), /Refuse Pixel 9\?/);
});

test("the address is not shown by default; it sits in a closed Technical details fold", async () => {
  const { d, A } = await open();
  show(A, REQS);
  const fold = d.querySelector("#stage .k-card.prq details");
  assert.ok(fold, "a details fold");
  assert.equal(fold.open, false);
  assert.match(text(fold.querySelector("summary")), /Technical details/);
  assert.match(text(fold), /192\.168\.1\.77/);
  const tbl = d.querySelector("#stage table.prt");
  assert.doesNotMatch(text(tbl), /192\.168\.1\.77|10\.0\.0\.5/, "the table itself has no address");
  assert.ok(!fold.closest("table"));
});

test("with no requests the page says so in plain words and has no table", async () => {
  const { d, A, errs } = await open();
  show(A, []);
  assert.equal(d.querySelectorAll("#stage table.prt").length, 0);
  assert.match(text(d.querySelector("#stage")), /Nobody is asking to join/);
  assert.deepEqual(errs, []);
});

test("the cannot-answer note and the three-waiting note are kept", async () => {
  const { d, A } = await open();
  const three = [1, 2, 3].map((n) => ({ ...REQS[0], id: "q" + n, label: "Phone " + n }));
  show(A, three);
  assert.equal(d.querySelectorAll("#stage table.prt tbody tr").length, 3);
  assert.match(text(d.querySelector("#stage")), /holds at most 3 waiting requests/);
  A.X.inboxDenied = new Set([A.pairServer().id]);
  show(A, []);
  assert.match(text(d.querySelector("#stage")), /This login cannot answer pairing requests/);
  assert.doesNotMatch(text(d.querySelector("#stage")), /Nobody is asking to join/);
});
