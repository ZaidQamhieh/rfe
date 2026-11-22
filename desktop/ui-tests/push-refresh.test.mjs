// Push refresh: the Rust core subscribes to each computer's GET /v1/events stream and tells the window
// (Tauri events "agent-event" {host, kind} and "agent-stream" {host, state}). The window re-reads the open
// page at once, and polls less often while every online computer's stream is connected.
//
//   node --test desktop/ui-tests/push-refresh.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const E = require("../ui/engine.js");

// ---- the schedule, pure ----
test("each event kind names only the jobs of the page that is open and shows that data", () => {
  const jobs = (kind, view) => E.liveEventJobs(kind, view);
  assert.deepEqual(jobs("audit", "history"), ["history"]);
  assert.deepEqual(jobs("audit", "devices"), ["history"], "the Devices page shows each device's recent activity");
  assert.deepEqual(jobs("devices", "devices"), ["devices"]);
  assert.deepEqual(jobs("files", "files"), ["files"]);
  assert.deepEqual(jobs("settings", "tools"), ["tools"]);
  assert.deepEqual(jobs("files", "history"), [], "another page's data is not read");
  assert.deepEqual(jobs("audit", "files"), []);
  assert.deepEqual(jobs("transfers", "transfers"), [], "the window's own transfer list comes from its own engine");
  assert.deepEqual(jobs("nonsense", "history"), []);
});

test("hello (a stream just opened, nothing replayed) refreshes whatever the open page shows", () => {
  assert.deepEqual(E.liveEventJobs("hello", "history"), ["history"]);
  assert.deepEqual(E.liveEventJobs("hello", "devices").sort(), ["devices", "history"]);
  assert.deepEqual(E.liveEventJobs("hello", "files"), ["files"]);
  assert.deepEqual(E.liveEventJobs("hello", "home"), []);
});

test("while the stream is connected the pushed pages poll slower; the others keep their pace", () => {
  for (const job of ["history", "devices", "files"]) assert.ok(E.liveMs(job, false, true) > E.liveMs(job, false, false), job);
  for (const job of ["transfers", "tools"]) assert.equal(E.liveMs(job, false, true), E.liveMs(job, false, false), job);
  assert.ok(E.liveMs("history", true, true) > E.liveMs("history", true, false), "the pushed interval wins over the busy-transfer one");
  assert.equal(E.liveMs("history", false), E.liveMs("history", false, false), "the old two-argument call is unchanged");
  const base = { view: "history", visible: true, active: false, now: 100000 };
  const slow = E.liveMs("history", false, true), fast = E.liveMs("history", false, false);
  assert.deepEqual(E.liveDue({ ...base, push: true, last: { history: 100000 - fast } }), []);
  assert.deepEqual(E.liveDue({ ...base, push: false, last: { history: 100000 - fast } }), ["history"]);
  assert.deepEqual(E.liveDue({ ...base, push: true, last: { history: 100000 - slow } }), ["history"]);
});

// ---- in the window ----
const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

async function open() {
  const vc = new VirtualConsole();
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
  return { w, A: w.A };
}
const onlineHosts = (A) => A.E.servers.filter((s) => s.signedIn && s.state === "online").map((s) => s.id);

test("the window listens for the core's two events and asks it to follow the saved computers", async () => {
  const { w, A } = await open();
  assert.ok(w.StubBackend.listening("agent-event"), "agent-event has a listener");
  assert.ok(w.StubBackend.listening("agent-stream"), "agent-stream has a listener");
  assert.ok(w.StubBackend.calls.includes("events_sync"), "the core was asked to follow the computers at start");
  const n = w.StubBackend.calls.filter((c) => c === "events_sync").length;
  w.StubBackend.calls.length = 0;
  A.E.emit("servers");
  await sleep(20);
  assert.equal(w.StubBackend.calls.filter((c) => c === "events_sync").length, 0, "no change in which computers are signed in: nothing to ask again (" + n + " at start)");
  const s = A.E.servers.find((x) => x.signedIn);
  s.signedIn = false; A.E.emit("servers");
  await sleep(20);
  assert.equal(w.StubBackend.calls.filter((c) => c === "events_sync").length, 1, "a computer signed out or removed: the core is asked to drop its stream");
});

test("an audit event on the History page reads that computer's log at once, and a files event does not", async () => {
  const { w, A } = await open();
  const host = onlineHosts(A)[0]; assert.ok(host, "the stub has an online computer");
  const seen = [];
  A.syncAudit = async (ids) => { seen.push(ids); return 0; };
  A.go("history"); await sleep(100);
  seen.length = 0;
  w.StubBackend.emit("agent-event", { host, kind: "files" });
  w.StubBackend.emit("agent-event", { host, kind: "devices" });
  await sleep(20);
  assert.equal(seen.length, 0, "other kinds are not History's data");
  w.StubBackend.emit("agent-event", { host, kind: "audit" });
  await sleep(20);
  assert.equal(JSON.stringify(seen), JSON.stringify([[host]]), "only the computer that said so is read, with no wait for the clock");
});

test("a burst of events while a read runs makes exactly one more read, not one per event", async () => {
  const { w, A } = await open();
  const host = onlineHosts(A)[0];
  let n = 0, release;
  A.syncAudit = () => { n++; return new Promise((r) => { release = r; }); };
  A.go("history"); await sleep(100);
  n = 0;
  for (let i = 0; i < 5; i++) w.StubBackend.emit("agent-event", { host, kind: "audit" });
  await sleep(20);
  assert.equal(n, 1, "a read is already running");
  release(0); await sleep(20);
  assert.equal(n, 2, "the changes that came meanwhile are read once more");
  release(0); await sleep(20);
  assert.equal(n, 2, "and then it settles");
});

test("a devices event on the Devices page reloads the list; a files event refreshes the open folder of that computer only", async () => {
  const { w, A } = await open();
  const host = onlineHosts(A)[0];
  let dev = 0; A.loadDevices = async () => { dev++; };
  A.go("devices"); await sleep(100); dev = 0;
  w.StubBackend.emit("agent-event", { host, kind: "devices" });
  await sleep(20);
  assert.equal(dev, 1);
  const got = [];
  A.E.fs.refresh = async (h, p) => { got.push([h, p]); };
  A.go("files"); await sleep(100); got.length = 0;
  const pane = A.mainPane;
  pane.host = host; pane.path = "/srv";
  w.StubBackend.emit("agent-event", { host: "10.9.9.9:1", kind: "files" });
  await sleep(20);
  assert.equal(got.length, 0, "a folder of another computer is not touched");
  w.StubBackend.emit("agent-event", { host, kind: "files" });
  await sleep(20);
  assert.equal(JSON.stringify(got), JSON.stringify([[host, "/srv"]]));
});

test("polling slows down only while every online computer's stream is connected, and speeds up again when one drops", async () => {
  const { w, A } = await open();
  const hosts = onlineHosts(A); assert.ok(hosts.length >= 1);
  let n = 0;
  A.syncAudit = async () => { n++; return 0; };
  A.go("history"); await sleep(100);
  const fast = E.liveMs("history", false, false);
  const run = async (t) => { n = 0; A.liveTick(t); const first = n; await sleep(5); A.liveTick(t + fast + 10); const second = n; await sleep(5); return [first, second]; };
  let t = Date.now() + 1e6;
  assert.deepEqual(await run(t), [1, 2], "no stream: the old pace");
  for (const h of hosts.slice(0, -1)) w.StubBackend.emit("agent-stream", { host: h, state: "connected" });
  t += 1e6;
  if (hosts.length > 1) assert.deepEqual(await run(t), [1, 2], "one computer has no stream: the old pace");
  w.StubBackend.emit("agent-stream", { host: hosts[hosts.length - 1], state: "connected" });
  t += 1e6;
  assert.deepEqual(await run(t), [1, 1], "all connected: not read again at the old interval");
  w.StubBackend.emit("agent-stream", { host: hosts[0], state: "unsupported" });
  t += 1e6;
  assert.deepEqual(await run(t), [1, 2], "an agent that answers 404 or 501 falls back to the old pace");
});
