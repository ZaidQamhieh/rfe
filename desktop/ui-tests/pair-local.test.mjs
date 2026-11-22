// Pair a phone with no computer added: the code and QR come from the agent on this PC.
//
//   node --test desktop/ui-tests/pair-local.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
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
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}

const text = (el) => (el ? el.textContent.replace(/\s+/g, " ").trim() : "");
const box = (d) => d.querySelector(".scrim .k-dlg");
const labels = (d) => [...box(d).querySelectorAll(".df button")].map((b) => b.textContent.trim());


test("with no computer connected the pairing card shows this PC's code and QR", async () => {
  const { d, A, E } = await open();
  const calls = [];
  const call0 = E.call; E.call = (name, args) => { calls.push(name); return call0.call(E, name, args); };
  for (const s of E.servers) s.state = "offline";
  for (let i = 0; i < 100 && !(A.localPC && A.localPC.on()); i++) await sleep(60);
  A.pairDialog(); await sleep(500);
  assert.ok(d.querySelector(".fxpd #pairQR svg"), "the QR is drawn");
  assert.match(text(d.querySelector(".fxpd #pairCode")), /DB5Q ZMHJ/);
  assert.ok(calls.includes("generate_local_pairing_code"), "the code comes from this PC's own agent: " + calls.join());
});

test("with no computer added the Devices page is the full page, read from this PC's agent", async () => {
  const { d, A, E } = await open();
  E.servers.length = 0;
  const calls = [];
  const call0 = E.call; E.call = (name, args) => { calls.push([name, args && args.args ? args.args.join(" ") : ""]); return call0.call(E, name, args); };
  for (let i = 0; i < 100 && !(A.localPC && A.localPC.on()); i++) await sleep(60);
  assert.ok(A.localPC.on(), "this PC's agent was found");
  A.go("devices");
  for (let i = 0; i < 100 && !(/Pixel 8/.test(text(d.querySelector("#stage"))) && /Galaxy S24/.test(text(d.querySelector("#stage")))); i++) await sleep(60);
  const first = d.querySelector("#stage [data-v5pick]"); if (first) first.click();
  for (let i = 0; i < 40 && !d.querySelector("[data-v5perms]"); i++) await sleep(60);
  const page = text(d.querySelector("#stage"));
  assert.match(page, /Devices on this PC/);
  assert.match(page, /Mobile App/);
  assert.match(page, /Pixel 8/);
  assert.match(page, /Galaxy S24/, "the phone asking to join is listed");
  assert.match(page, /Recent security events/);
  const side = d.querySelector("[data-v5perms]");
  assert.ok(side, "the right panel has the access switches");
  assert.ok(side.querySelector('[data-c="download"]').classList.contains("on"), "download is on for the first phone");
  assert.ok(!side.querySelector('[data-c="upload"]').classList.contains("on"), "upload is off");
  side.querySelector('[data-c="upload"]').click();
  for (let i = 0; i < 40 && !calls.some((c) => c[1].startsWith("allow")); i++) await sleep(50);
  assert.ok(calls.some((c) => c[1] === "allow e13a4291 browse,download,upload"), JSON.stringify(calls.filter((c) => c[0] === "local_agent_admin")));
});

test("with no computer added, History reads this PC's own audit log", async () => {
  const { d, A, E } = await open();
  E.servers.length = 0;
  E.history.length = 0;
  const calls = [];
  const call0 = E.call; E.call = (name, args) => { calls.push([name, args && args.args ? args.args.join(" ") : ""]); return call0.call(E, name, args); };
  for (let i = 0; i < 100 && !(A.localPC && A.localPC.on()); i++) await sleep(60);
  assert.ok(A.localPC.on(), "this PC's agent was found");
  await A.syncAudit();
  assert.ok(calls.some((c) => c[1].startsWith("audit")), "the log is read from this PC's agent: " + JSON.stringify(calls));
  assert.ok(E.history.some((h) => h.kind === "audit" && h.host === A.localPC.id), "its events are in History");
});
