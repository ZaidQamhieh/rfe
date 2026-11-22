// The per-computer choices of "Settings for <computer>" (stored by A.v5Computers.T.prefs / setPref) change what the app does:
//  A. connect: a computer set not to connect on start stays "disconnected" at startup (no health read), and Connect still works;
//  B. notify: an online -> offline change shows one notice, only when the choice is on;
//  C. wake: a computer that cannot be reached when it is opened gets the existing wake signal, only when the choice is on;
//  D. dir (the download folder) is kept and sent with each download as transfer_download_tree's destDir;
//  E. the MAC address read from a computer is kept in the window's storage, so a computer that is asleep at startup can still be woken.
//
//   node --test desktop/ui-tests/computer-prefs.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const enginePath = require.resolve("../ui/engine.js");
const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (fn, what) => { for (let i = 0; i < 300; i++) { if (fn()) return; await sleep(10); } assert.fail("timed out waiting for " + what); };
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

const PKEY = "rfe-v5-pcprefs-v1";
const NAS = "192.168.1.20:7443"; /* signed in and online in the stub core's seed */
const BUILD = "100.101.4.7:7443"; /* the same */

/** The window on the stub core. `prefs` is what the window's storage holds before it starts. */
async function open(prefs, extra) {
  const errs = [];
  let store = null;
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1",
    beforeParse(w) {
      const mem = new Map(prefs ? [[PKEY, JSON.stringify(prefs)]] : []);
      for (const [k, v] of Object.entries(extra || {})) mem.set(k, v);
      store = mem;
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
  await sleep(400);
  return { w, A: w.A, E: w.A.E, errs, cmds: w.StubBackend.cmds, store };
}

/** Makes the stub core answer a health read of every computer as unreachable (a network error, not a login error). */
const unplug = (o) => { o.cmds.agent_health = () => Promise.reject(Object.assign(new Error("connect error: network unreachable"), { code: "NETWORK" })); };

test("A. a computer set not to connect on start stays disconnected; the others connect; Connect works", async () => {
  const o = await open({ [NAS]: { connect: false } });
  const nas = o.E.server(NAS), build = o.E.server(BUILD);
  assert.equal(nas.state, "disconnected", "not connected at startup");
  assert.equal(build.state, "online", "a computer with no choice still connects on start");
  await sleep(300);
  assert.equal(nas.state, "disconnected", "the clock does not connect it either");
  assert.notEqual(o.A.mainPane.host, NAS, "the first view is not the computer that does not connect");
  await o.E.connect(NAS);
  assert.equal(nas.state, "online", "the user can still open it");
  assert.deepEqual(o.errs, []);
});

test("A2. the engine skips the startup health read when its hook says no, and connects when it says yes", async () => {
  async function boot(hook) {
    delete require.cache[enginePath];
    const E = require(enginePath);
    const calls = [];
    E.setInvoke(async (cmd) => {
      calls.push(cmd);
      switch (cmd) {
        case "transfer_folder": return "/dl";
        case "local_places": return [{ label: "Home", path: "/home/u", kind: "home" }];
        case "list_hosts": return [{ key: NAS, name: "nas", host: NAS, fingerprint: "ab".repeat(32), username: "u", signedIn: true }];
        case "agent_health": return { host: NAS, fingerprint: "ab".repeat(32), health: { status: "ok", name: "nas", version: "1.43.0", os: "Debian 12" }, status: null };
        case "files_roots": return { locations: [], source: "roots" };
        default: throw "unknown command " + cmd;
      }
    });
    if (hook) E.hooks.autoConnect = hook;
    await E.init();
    await sleep(60);
    return { E, calls };
  }
  const no = await boot(() => false);
  assert.equal(no.calls.includes("agent_health"), false, "no health read at startup");
  assert.equal(no.E.server(NAS).state, "disconnected");
  await no.E.connect(NAS);
  assert.equal(no.E.server(NAS).state, "online");
  const yes = await boot(() => true);
  assert.equal(yes.E.server(NAS).state, "online");
  const dflt = await boot(null);
  assert.equal(dflt.E.server(NAS).state, "online", "no hook set: it connects as before");
});

test("B. the offline notice shows once when the choice is on, and not when it is off", async () => {
  const o = await open({ [NAS]: { notify: true } });
  const shown = [];
  o.A.snack = (m) => shown.push(String(m));
  assert.equal(o.E.server(NAS).state, "online");
  unplug(o);
  await o.E.check(o.E.server(NAS));
  await o.E.check(o.E.server(BUILD));
  assert.notEqual(o.E.server(NAS).state, "online", "it went offline");
  assert.notEqual(o.E.server(BUILD).state, "online", "the other one too");
  const nas = o.E.server(NAS).name, build = o.E.server(BUILD).name;
  assert.equal(shown.filter((m) => m.includes(nas) && /went offline/.test(m)).length, 1, "one notice for the computer with the choice on: " + shown.join(" / "));
  assert.equal(shown.filter((m) => m.includes(build) && /went offline/.test(m)).length, 0, "none for the one with it off");
  await o.E.check(o.E.server(NAS));
  await o.E.check(o.E.server(NAS));
  assert.equal(shown.filter((m) => m.includes(nas) && /went offline/.test(m)).length, 1, "no repeat while it stays away");
  assert.deepEqual(o.errs, []);
});

test("C. a computer that cannot be reached when opened gets the wake signal when the choice is on", async () => {
  const o = await open({ [NAS]: { wake: true }, [BUILD]: { wake: false } });
  const sent = [];
  o.cmds.wake_computer = (a) => { sent.push(a.host); return null; };
  o.A.snack = () => {};
  for (const id of [NAS, BUILD]) { assert.ok(o.E.server(id).mac, "the MAC address was read while it was online"); o.E.disconnect(id); }
  unplug(o);
  await o.E.connect(NAS);
  await o.E.connect(BUILD);
  await sleep(100);
  assert.notEqual(o.E.server(NAS).state, "online");
  assert.deepEqual(sent, [NAS], "woken: only the one whose choice is on");
  assert.deepEqual(o.errs, []);
});

test("C2. no wake signal while the computer answers", async () => {
  const o = await open({ [NAS]: { wake: true } });
  const sent = [];
  o.cmds.wake_computer = (a) => { sent.push(a.host); return null; };
  o.E.disconnect(NAS);
  await o.E.connect(NAS);
  await sleep(100);
  assert.equal(o.E.server(NAS).state, "online");
  assert.deepEqual(sent, []);
});

/* The folder is kept per computer and sent with each download as destDir (transfers.rs transfer_download_tree). */
test("D. the download folder is kept and shown", async () => {
  const o = await open({ [NAS]: { dir: "/tmp/elsewhere" } });
  assert.equal(o.A.v5Computers.prefs(o.E.server(NAS)).dir, "/tmp/elsewhere", "it is kept");
});

test("D2. a download from that computer carries its folder as destDir", async () => {
  const o = await open({ [NAS]: { dir: "/tmp/elsewhere" } });
  assert.equal(o.E.destFor(NAS), "/tmp/elsewhere");
  assert.equal(o.E.destFor("nobody"), "");
});

test("D3. the transfer row names the folder chosen for that computer, not the app-wide one", async () => {
  const o = await open({ [NAS]: { dir: "/tmp/elsewhere" } });
  const DIR = "/srv/projects/atlas/releases/2026.10";
  await o.E.fs.load(NAS, DIR);
  const file = (o.E.fs.list(NAS, DIR) || []).find((n) => n.t !== "dir");
  assert.ok(file, "a file on the computer");
  const r = o.E.enqueue({ dir: "down", host: NAS, srcDir: DIR, dstDir: "", names: [file.n] });
  assert.equal(r.tasks[0].dstDir, "/tmp/elsewhere", "shown and revealed where it was really saved");
  const other = o.E.enqueue({ dir: "down", host: BUILD, srcDir: DIR, dstDir: "", names: [file.n] });
  if (other.tasks.length) assert.equal(other.tasks[0].dstDir, o.E.settings.downloadDir || "", "no folder chosen: the app-wide one");
  assert.deepEqual(o.errs, []);
});

/** Downloads the first file of one folder of the computer NAS from a window whose storage holds `prefs`; resolves the destDir the core was given. */
async function destDirSent(prefs) {
  const o = await open(prefs);
  const asked = [];
  o.cmds.transfer_download_tree = (a) => { asked.push(a); return ["r1"]; };
  const DIR = "/srv/projects/atlas/releases/2026.10";
  await o.E.fs.load(NAS, DIR);
  const file = (o.E.fs.list(NAS, DIR) || []).find((n) => n.t !== "dir");
  assert.ok(file, "a file on the computer");
  o.E.enqueue({ dir: "down", host: NAS, srcDir: DIR, dstDir: "", names: [file.n] });
  await until(() => asked.length === 1, "the download to reach the core");
  assert.deepEqual(o.errs, []);
  return asked[0].destDir;
}

test("D4. the download folder reaches the core with the download, and a computer with none sends nothing", async () => {
  assert.equal(await destDirSent({ [NAS]: { dir: "/tmp/elsewhere" } }), "/tmp/elsewhere");
  assert.equal(await destDirSent(), null, "no folder chosen: the core's own default");
});

test("E. a MAC address read from a computer is kept, and a later window knows it without a health read", async () => {
  const first = await open();
  const mac = first.E.server(NAS).mac;
  assert.ok(mac, "read while it was online");
  const kept = [...first.store.values()].filter((v) => v.includes(mac));
  assert.equal(kept.length, 1, "one stored value holds it");
  const later = await open({ [NAS]: { connect: false } }, Object.fromEntries([...first.store].filter(([, v]) => v.includes(mac))));
  assert.equal(later.E.server(NAS).state, "disconnected", "no health read this session");
  assert.equal(later.E.server(NAS).mac, mac, "known from the earlier session, so it can be woken");
  assert.deepEqual(later.errs, []);
});
