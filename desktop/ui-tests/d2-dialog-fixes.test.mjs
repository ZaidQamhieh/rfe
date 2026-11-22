// Four dialog fixes in the desktop window: a pairing code typed for one attempt is never reused by a later connect;
// the "Ask the PC" approval poll stops when its dialog is closed (even while the request is still in flight);
// an address is read the same way for IPv4, host names, bracketed IPv6 and bare IPv6, and a zone id is refused;
// signing out from a computer's settings tells the person when it fails and leaves the computer signed in.
//
//   node --test desktop/ui-tests/d2-dialog-fixes.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The real window scripts on the stub core, with the approval poll shortened so a test need not wait seconds. */
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
  w.A.T.poll = 30;
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}
const countOf = (w, cmd) => w.StubBackend.calls.filter((c) => c === cmd).length;
const button = (d, label) => [...d.querySelectorAll(".scrim button")].find((b) => b.textContent.trim() === label);

test("1. a pairing code typed for a failed attempt is not sent when the computer is connected again", async () => {
  const { w, A, E, errs } = await open();
  const probe = w.StubBackend.cmds.probe_agent;
  w.StubBackend.cmds.probe_agent = () => Promise.reject("connect error: no answer");
  const id = A.addAndOpen({ name: "stale pc", host: "10.9.9.7:8765" }, "ABCD2345").id;
  await sleep(300);
  assert.equal(E.server(id).state, "offline", "the first attempt could not reach it");
  w.StubBackend.cmds.probe_agent = probe;
  w.StubBackend.calls.length = 0;
  A.watchConnect(id); E.retryNow(id);
  await sleep(400);
  assert.equal(E.server(id).state, "trust", "reachable now: its key waits to be trusted");
  E.trust(id); await sleep(300);
  assert.equal(countOf(w, "pair_with_code"), 0, "the old code is not sent again");
  assert.deepEqual(errs, []);
});

test("2a. closing the Ask-the-PC dialog stops the approval poll and cancels the request", async () => {
  const { w, d, A, E } = await open();
  w.StubBackend.cmds.request_pairing = () => Promise.resolve({ matchCode: "1234 5678" });
  const s = E.servers.find((x) => !x.signedIn) || E.servers[0];
  A.signInDialog(s, { mode: "ask" }); await sleep(100);
  button(d, "Ask").click(); await sleep(150);
  assert.ok(countOf(w, "poll_pairing") >= 1, "the poll runs while the dialog is open");
  button(d, "Not now").click(); await sleep(60);
  const n = countOf(w, "poll_pairing");
  await sleep(250);
  assert.equal(countOf(w, "poll_pairing"), n, "no poll after the dialog closed");
  assert.equal(countOf(w, "cancel_pairing"), 1, "the PC is told the request is withdrawn");
});

test("2b. closing the dialog while the request is still on its way never starts a poll", async () => {
  const { w, d, A, E } = await open();
  let answer;
  w.StubBackend.cmds.request_pairing = () => new Promise((r) => { answer = r; });
  const s = E.servers.find((x) => !x.signedIn) || E.servers[0];
  A.signInDialog(s, { mode: "ask" }); await sleep(100);
  button(d, "Ask").click(); await sleep(50);
  button(d, "Not now").click(); await sleep(50);
  answer({ matchCode: "1234 5678" });
  await sleep(250);
  assert.equal(countOf(w, "poll_pairing"), 0, "the dialog is gone: nothing polls");
  assert.equal(countOf(w, "cancel_pairing"), 1, "the request that arrived late is withdrawn");
});

test("3. addresses: IPv4, host names, bracketed IPv6, bare IPv6, ports and zone ids", async () => {
  const { w } = await open();
  const parse = w.A.E.util.parseAddr;
  assert.equal(typeof parse, "function", "the engine reads typed addresses in one place");
  const ok = (text, host, port, port2) => { const r = parse(text, port2); assert.equal(r.error, undefined, text + ": " + r.error); assert.deepEqual([r.host, r.port, r.addr], [host, port, host + ":" + port], text); };
  ok("10.0.0.5", "10.0.0.5", 8765);
  ok("10.0.0.5:9000", "10.0.0.5", 9000);
  ok("  nas.local  ", "nas.local", 8765);
  ok("nas.local:1", "nas.local", 1);
  ok("[::1]:8765", "[::1]", 8765);
  ok("[::1]", "[::1]", 8765);
  ok("[2001:db8::7]:9443", "[2001:db8::7]", 9443);
  ok("::1", "[::1]", 8765);
  ok("2001:db8::7", "[2001:db8::7]", 8765);
  ok("2001:db8:0:0:0:0:0:7", "[2001:db8:0:0:0:0:0:7]", 8765);
  ok("::1", "[::1]", 9000, 9000);
  ok("nas.local", "nas.local", 9000, 9000);
  for (const bad of ["", "   ", "a b", "fe80::1%eth0", "[fe80::1%25eth0]:8765", "[fe80::1%eth0]", "host:", "host:0", "host:70000", "host:abc", "[::1]:", "[::1]:0", "[::1", "::1]", "[::g]:8765", "[nas.local]:8765", "1:2:3:4:5:6:7:8:9", ":::1", "[::1]:8765:1"]) {
    assert.ok(parse(bad).error, JSON.stringify(bad) + " is refused");
  }
  assert.match(parse("fe80::1%eth0").error, /zone/i, "a zone id is named as the reason");
});

test("3b. the engine splits saved addresses, including IPv6, into host and port", async () => {
  const { w } = await open();
  const split = w.A.E.util.splitAddr;
  assert.deepEqual([...split("[::1]:8765")], ["[::1]", 8765]);
  assert.deepEqual([...split("[::1]")], ["[::1]", 8765]);
  assert.deepEqual([...split("::1")], ["[::1]", 8765]);
  assert.deepEqual([...split("fe80::2")], ["[fe80::2]", 8765]);
  assert.deepEqual([...split("10.0.0.5:99")], ["10.0.0.5", 99]);
  assert.deepEqual([...split("nas")], ["nas", 8765]);
});

test("3c. an IPv6 address typed in the add dialog is added as [addr]:port and a bad one is refused in the dialog", async () => {
  const { w, d, E, errs } = await open();
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  const f = d.querySelector("#v5addr"); f.closest("details").open = true;
  const type = (v) => { f.value = v; f.dispatchEvent(new w.Event("input", { bubbles: true })); button(d, "Next").click(); };
  type("fe80::1%eth0"); await sleep(100);
  assert.match(d.querySelector("#v5adde").textContent, /zone/i, "the zone id is explained under the field");
  assert.equal(E.servers.some((s) => /fe80/.test(s.addr)), false, "nothing was added");
  type("2001:db8::7"); await sleep(200);
  const s = E.servers.find((x) => /2001/.test(x.addr));
  assert.ok(s, "the computer was added");
  assert.equal(s.addr, "[2001:db8::7]:8765", "the address keeps its brackets and gets the port");
  assert.equal(w.StubBackend.calls.includes("probe_agent"), true);
  assert.deepEqual(errs, []);
});

test("4. a failed sign-out from the computer's settings says so and leaves it signed in", async () => {
  const { w, d, A, E, errs } = await open();
  const s = E.servers.find((x) => x.signedIn && x.state === "online");
  assert.ok(s, "the seed has a signed-in computer");
  w.StubBackend.cmds.sign_out = () => Promise.reject("The agent says: it is busy (INTERNAL)");
  A.v5Computers.settings(s); await sleep(100);
  const so = d.querySelector("[data-so]");
  assert.equal(so.textContent.trim(), "Sign out");
  so.click(); await sleep(200);
  const snack = d.querySelector(".snack.err");
  assert.ok(snack, "an error message is shown");
  assert.ok(snack.textContent.includes("Could not sign out of " + s.name), snack.textContent);
  assert.equal(E.server(s.id).signedIn, true, "still signed in on this computer");
  assert.equal(E.server(s.id).state, "online", "and still connected");
  assert.deepEqual(errs, [], "no unhandled failure");
});

test("4b. a sign-out that works says so", async () => {
  const { w, d, A, E } = await open();
  const s = E.servers.find((x) => x.signedIn && x.state === "online");
  A.v5Computers.settings(s); await sleep(100);
  d.querySelector("[data-so]").click(); await sleep(300);
  assert.ok(w.StubBackend.calls.includes("sign_out"), "the core was asked to sign out");
  assert.ok(d.querySelector(".snack").textContent.includes("Signed out of " + s.name));
});
