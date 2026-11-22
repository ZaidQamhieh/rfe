// Four fixes in the desktop window: a pushed change that arrives during a timer-driven read is read afterwards;
// the History Kind filter keeps its choice when the counts change; CSV exports cannot start a spreadsheet formula;
// an audit line belongs to a device only when it names the whole device name.
//
//   node --test desktop/ui-tests/d1-ui-fixes.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

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
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}

test("a change pushed while a timer-driven read runs is read once more when that read ends", async () => {
  const { w, A, E } = await open();
  const host = E.servers.find((s) => s.signedIn && s.state === "online").id;
  A.go("history"); await sleep(150);
  let n = 0, release;
  A.syncAudit = () => { n++; return new Promise((r) => { release = r; }); };
  A.liveTick(Date.now() + 1e7);
  assert.equal(n, 1, "the clock started a read");
  w.StubBackend.emit("agent-event", { host, kind: "audit" });
  await sleep(20);
  assert.equal(n, 1, "the read is still running: no second one yet");
  release(0); await sleep(30);
  assert.equal(n, 2, "the push that came meanwhile is not lost");
  release(0); await sleep(30);
  assert.equal(n, 2, "and then it settles");
});

test("the History Kind filter keeps its choice when the count in the option text changes", async () => {
  const { w, d, A, E, errs } = await open();
  const h = E.servers[0].id;
  E.history.push({ id: "d1-a", at: Date.now() - 2000, kind: "transfer", text: "Downloaded a.pdf", host: h });
  E.history.push({ id: "d1-b", at: Date.now() - 1000, kind: "transfer", text: "Downloaded b.pdf", host: h });
  A.go("history"); await sleep(300);
  const sel = () => d.querySelector('#stage select[data-v5f="Kind"]');
  const opt = [...sel().options].find((o) => o.textContent.startsWith("Transfers"));
  sel().value = opt.value; sel().dispatchEvent(new w.Event("change", { bubbles: true }));
  assert.match(sel().selectedOptions[0].textContent, /^Transfers · 2$/);
  E.history.push({ id: "d1-c", at: Date.now(), kind: "transfer", text: "Downloaded c.pdf", host: h });
  A.go("history"); await sleep(300);
  assert.match(sel().selectedOptions[0].textContent, /^Transfers · 3$/, "still Transfers after the count went 2 -> 3");
  assert.deepEqual(errs, []);
});

test("the History CSV neutralises a leading formula character and leaves other values alone", async () => {
  const { w, d, A, E } = await open();
  const h = E.servers[0].id;
  E.history.splice(0);
  const at = Date.now() - 1000;
  E.history.push({ id: "d1-f1", at, kind: "transfer", text: "=HYPERLINK(\"http://x\")", host: h });
  E.history.push({ id: "d1-f2", at: at - 1, kind: "transfer", text: "@cmd", host: h });
  E.history.push({ id: "d1-f3", at: at - 2, kind: "transfer", text: "\tTabbed", host: h });
  E.history.push({ id: "d1-f4", at: at - 3, kind: "transfer", text: "Downloaded plain \"quoted\".pdf", host: h });
  E.history.push({ id: "d1-f5", at: at - 4, kind: "transfer", text: "+1 and -2", host: h });
  A.go("history"); await sleep(300);
  let saved = null;
  A.saveLocal = (name, body) => { saved = body; return Promise.resolve({ name, dir: "/tmp" }); };
  d.querySelector('#stage [data-v5="csv"]').click(); await sleep(30);
  assert.ok(saved, "a file was saved");
  assert.ok(saved.includes(',"\'=HYPERLINK(""http://x"")"'), saved);
  assert.ok(saved.includes(',"\'@cmd"'), saved);
  assert.ok(saved.includes(',"\'\tTabbed"'), saved);
  assert.ok(saved.includes(',"\'+1 and -2"'), saved);
  assert.ok(saved.includes(',"Downloaded plain ""quoted"".pdf"'), "normal text is unchanged apart from quote doubling");
});

test("the audit CSV neutralises a leading formula character and leaves other values alone", async () => {
  const { A, E } = await open();
  const h = E.servers[0].id;
  const rows = ["=1+1", "-cmd", "\rCR", "Paired Pixel 7"].map((text, i) => ({ id: "d1-c" + i, at: Date.now(), kind: "audit", text, host: h }));
  const csv = A.fx4.toCSV(rows);
  const lines = csv.split("\n");
  assert.ok(lines[1].includes(',"\'=1+1",'), lines[1]);
  assert.ok(lines[2].includes(',"\'-cmd",'), lines[2]);
  assert.ok(csv.includes(',"\'\rCR",'));
  assert.ok(csv.includes(',"Paired Pixel 7",'), "a normal value is unchanged");
});

test("an audit line belongs to the device whose whole name it holds; an empty name matches nothing", async () => {
  const { A, E } = await open();
  const h = E.servers[0].id;
  const at = Date.now();
  E.history.splice(0);
  E.history.push({ id: "d1-p7", at, kind: "audit", text: "device_revoke · Pixel 7", host: h });
  E.history.push({ id: "d1-p", at: at - 1, kind: "audit", text: "device_revoke · Pixel", host: h });
  E.history.push({ id: "d1-x", at: at - 2, kind: "audit", text: "login · someone", host: h });
  A.X.devices = [{ id: "a", name: "Pixel" }, { id: "b", name: "Pixel 7" }, { id: "c", name: "" }];
  const ids = (dev) => { A.X4.audit.dev = dev; return Array.from(A.fx4.auditRows(), (r) => r.id); };
  assert.deepEqual(ids("Pixel"), ["d1-p"], "Pixel does not match Pixel 7");
  assert.deepEqual(ids("Pixel 7"), ["d1-p7"]);
  assert.deepEqual(ids(""), [], "an empty name matches no line");
  assert.deepEqual(ids("all").length, 3);
});

test("the device dialog lists only the activity that names that device", async () => {
  const { w, A, E } = await open();
  const h = E.servers[0].id;
  E.history.splice(0);
  E.history.push({ id: "d1-q7", at: Date.now(), kind: "audit", text: "device_revoke · Pixel 7", host: h });
  A.X.devices = [{ id: "a", name: "Pixel", last: E.now(), paired: E.now(), a: {} }, { id: "b", name: "Pixel 7", last: E.now(), paired: E.now(), a: {} }, { id: "c", name: "", last: E.now(), paired: E.now(), a: {} }];
  A.pairServer = () => E.servers[0];
  A.deviceDetails("a"); await sleep(60);
  assert.equal(w.document.querySelectorAll(".dlg .evrow").length, 0, "Pixel is not Pixel 7");
  w.document.querySelector(".dlg").remove();
  A.deviceDetails("c"); await sleep(60);
  assert.equal(w.document.querySelectorAll(".dlg .evrow").length, 0, "an empty name matches nothing");
});

test("pure helpers: csvCell and mentionsDevice", () => {
  const E = require("../ui/engine.js");
  assert.equal(E.csvCell("plain"), '"plain"');
  assert.equal(E.csvCell(null), '""');
  assert.equal(E.csvCell(5), '"5"');
  assert.equal(E.csvCell('a"b'), '"a""b"');
  for (const c of ["=", "+", "-", "@", "\t", "\r"]) assert.equal(E.csvCell(c + "x"), '"\'' + c + 'x"', JSON.stringify(c));
  assert.equal(E.mentionsDevice("revoked Pixel 7", "Pixel", ["Pixel", "Pixel 7"]), false);
  assert.equal(E.mentionsDevice("revoked Pixel 7", "Pixel 7", ["Pixel", "Pixel 7"]), true);
  assert.equal(E.mentionsDevice("revoked Pixel", "Pixel", ["Pixel", "Pixel 7"]), true);
  assert.equal(E.mentionsDevice("Pixel 7 and Pixel", "Pixel", ["Pixel", "Pixel 7"]), true, "a free-standing occurrence counts");
  assert.equal(E.mentionsDevice("anything", "", []), false);
  assert.equal(E.mentionsDevice("Pixels", "Pixel", ["Pixel"]), false, "part of a longer word");
});
