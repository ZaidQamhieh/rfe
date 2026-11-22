// Settings audit (rfe-2ls): every control on the Settings page must change what the app does, the first time it is pressed.
// The audit table is in the task notes; these tests pin the two switches that did nothing.
//
//   node --test desktop/ui-tests/settings-wired.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, started on Settings, with every script error collected in `errs` and every setInterval recorded in `w.__intervals`. */
async function open() {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 300)));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1&view=settings",
    beforeParse(w) {
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.TextEncoder = TextEncoder; w.TextDecoder = TextDecoder;
      w.ResizeObserver = class { observe() {} disconnect() {} };
      w.HTMLElement.prototype.scrollIntoView = function () {};
      Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
      const store = new Map();
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (store.has(k) ? store.get(k) : null), setItem: (k, v) => store.set(k, String(v)), removeItem: (k) => store.delete(k) }, configurable: true });
      w.__intervals = []; const si = w.setInterval.bind(w);
      w.setInterval = (fn, ms, ...rest) => { w.__intervals.push([fn, ms]); return si(fn, ms, ...rest); };
    },
  });
  const w = dom.window; live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}
const until = async (fn, what) => { for (let i = 0; i < 100; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };
/** Opens one Settings section and returns a finder for its switch by the setting's name. */
function section(ctx, name) {
  ctx.A.state.setTab = name; ctx.A.pageSettings();
  return (key) => ctx.d.querySelector('#stage [data-sw="' + key + '"]');
}
const click = (w, el) => el.dispatchEvent(new w.MouseEvent("click", { bubbles: true, cancelable: true }));

/** A window that is not the one being looked at, with desktop notifications on, recording every desktop_notify call. */
async function away() {
  const ctx = await open(); const { A, E, d } = ctx;
  d.hasFocus = () => false; A.S.desktopNotif = true;
  const sent = []; const orig = E.call.bind(E);
  E.call = (name, args) => { if (name === "desktop_notify") { sent.push(args); return Promise.resolve(true); } return orig(name, args); };
  ctx.sent = sent; return ctx;
}

test("New device requests and Updates: shown on when they are on (the default), and the first press turns them off", async () => {
  const ctx = await away(); const { w, A, sent, errs } = ctx;
  let sw = section(ctx, "Desktop");
  for (const [key, kind] of [["nPair", "pair"], ["nUpdate", "update"]]) {
    assert.ok(sw(key), key + " switch is on the page (Desktop notifications is on)");
    A.osNotify(kind, "x");
    assert.equal(sent.filter((s) => s.kind === kind).length, 1, key + ": the notification is sent by default");
    assert.equal(sw(key).getAttribute("aria-checked"), "true", key + ": so the switch must read on");
    click(w, sw(key)); sw = section(ctx, "Desktop");
    assert.equal(sw(key).getAttribute("aria-checked"), "false", key + ": one press turns it off");
    A.osNotify(kind, "y");
    assert.equal(sent.filter((s) => s.kind === kind).length, 1, key + ": and nothing more is sent");
    click(w, sw(key)); sw = section(ctx, "Desktop");
    A.osNotify(kind, "z");
    assert.equal(sent.filter((s) => s.kind === kind).length, 2, key + ": pressed again, it is sent again");
  }
  assert.deepEqual(errs, []);
});

test("Check automatically: turning it on checks now (not at the next start), turning it off does not, and the check repeats while the app runs", async () => {
  const ctx = await open(); const { w, A, E, errs } = ctx;
  const checks = []; const orig = E.call.bind(E);
  E.call = (name, args) => { if (name === "update_check") { checks.push(args); return Promise.resolve({ available: false, latest: "", current: "1.0.0" }); } return orig(name, args); };
  A.S.autoUpdate = false; A.S.lastUpdateCheck = 0;
  let sw = section(ctx, "About");
  assert.ok(sw("autoUpdate"), "the switch is on the About page");
  click(w, sw("autoUpdate"));
  await until(() => checks.length === 1, "an update check right after turning Check automatically on");
  assert.equal(A.S.autoUpdate, true);
  await until(() => A.S.lastUpdateCheck > 0, "the time of the check to be remembered");
  sw = section(ctx, "About"); click(w, sw("autoUpdate")); await sleep(100);
  assert.equal(A.S.autoUpdate, false); assert.equal(checks.length, 1, "turning it off asks nothing");
  /* the daily check: an hourly tick looks again once a day has passed, never sooner */
  const ticks = w.__intervals.filter(([, ms]) => ms === A.T.hour).map(([fn]) => fn);
  assert.ok(ticks.length >= 1, "an hourly tick is scheduled");
  A.S.autoUpdate = true; A.S.lastUpdateCheck = Date.now();
  ticks.forEach((f) => f()); await sleep(100);
  assert.equal(checks.length, 1, "checked a moment ago: the tick waits");
  A.S.lastUpdateCheck = Date.now() - A.T.day - 1000;
  ticks.forEach((f) => f());
  await until(() => checks.length === 2, "the tick to check once a day has passed");
  assert.deepEqual(errs, []);
});
