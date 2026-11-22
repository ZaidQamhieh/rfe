// The folder limit of a device (Devices page, side panel) is set only through the system's folder dialog:
// the path field is read-only, a cancelled or empty pick changes nothing, and any folder (the root too) is allowed.
//
//   node --test desktop/ui-tests/device-folder-picker.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const waitFor = async (fn, ms = 4000) => { for (let t = 0; t < ms; t += 25) { if (fn()) return; await sleep(25); } };
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, with every script error collected in `errs`. */
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

const ALL = { download: true, upload: true, modify: true, delete: true, share: true, viewApps: false, launchApps: false, readOnly: false };

/** Opens the Devices page with one fake device; `pick` answers pick_folder, every call is recorded. */
async function devices(pick, jailRoot = "") {
  const ctx = await open(); const { A, E } = ctx;
  const srv = E.servers[0]; const calls = []; const orig = E.call.bind(E);
  E.call = (name, args) => {
    calls.push([name, JSON.parse(JSON.stringify(args || {}))]);
    if (name === "pick_folder") return Promise.resolve(pick);
    if (name === "set_device_access") return Promise.resolve({ id: "dev-1", ...ALL, ...args.patch });
    if (name === "list_devices" || name === "device_access_all") return Promise.resolve([]);
    return orig(name, args);
  };
  A.pairServer = () => srv;
  const dev = { id: "dev-1", name: "Pixel 9", last: E.now(), paired: E.now() - 864e5, addr: "192.168.1.57", ver: "1.4.0", a: { ...ALL, jailRoot } };
  A.X.devices = [dev]; A.X.devAt = E.now();
  A.go("devices"); await sleep(80);
  A.X.devices = [dev]; A.X.devAt = E.now(); A.v5Rerender(); await sleep(60);
  /* a slow CI runner renders later than a fixed sleep, and a late device refresh can replace the list: put it back and redraw until the panel shows */
  await waitFor(() => { if (ctx.d.querySelector("#stage [data-v5lim]")) return true; A.X.devices = [dev]; A.X.devAt = E.now(); A.v5Rerender(); return false; }, 8000);
  ctx.calls = calls; ctx.sets = () => calls.filter((c) => c[0] === "set_device_access");
  ctx.q = (s) => ctx.d.querySelector("#stage " + s);
  return ctx;
}

test("the path field cannot be typed into; a button opens the folder dialog", async () => {
  const ctx = await devices("/srv/photos", "/srv");
  const f = ctx.q("[data-v5lim]");
  assert.ok(f, "the folder limit field is in the panel");
  assert.equal(f.readOnly, true, "read-only");
  assert.equal(f.value, "/srv");
  assert.ok(ctx.q("[data-v5limpick]"), "a Choose folder button");
  assert.deepEqual(ctx.errs, []);
});

test("choosing a folder in the dialog sets the limit to it", async () => {
  const ctx = await devices("/srv/photos");
  ctx.q("[data-v5limpick]").click(); await sleep(80);
  assert.ok(ctx.calls.some((c) => c[0] === "pick_folder"), "the system dialog was asked");
  assert.equal(ctx.sets().length, 1);
  assert.deepEqual(ctx.sets()[0][1].patch, { jailRoot: "/srv/photos" });
});

test("the filesystem root can be chosen", async () => {
  const ctx = await devices("/");
  ctx.q("[data-v5limpick]").click(); await sleep(80);
  assert.deepEqual(ctx.sets()[0][1].patch, { jailRoot: "/" });
});

test("a cancelled or empty pick changes nothing", async () => {
  for (const answer of [null, "", "   ", undefined]) {
    const ctx = await devices(answer, "/srv");
    ctx.q("[data-v5limpick]").click(); await sleep(80);
    assert.equal(ctx.sets().length, 0, "no save for " + JSON.stringify(answer));
    assert.equal(ctx.q("[data-v5lim]").value, "/srv");
  }
});

test("No limit clears the limit without typing", async () => {
  const ctx = await devices("/x", "/srv");
  ctx.q("[data-v5limclear]").click(); await sleep(80);
  assert.deepEqual(ctx.sets()[0][1].patch, { jailRoot: "" });
});

test("limitFromPick: only a real, non-blank path is accepted", async () => {
  const { A } = await open(); const f = A.v5Devices.limitFromPick;
  assert.equal(typeof f, "function");
  assert.equal(f("/"), "/"); assert.equal(f(" C:\\Users "), "C:\\Users");
  for (const bad of [null, undefined, "", "  ", 5, {}]) assert.equal(f(bad), null);
});
