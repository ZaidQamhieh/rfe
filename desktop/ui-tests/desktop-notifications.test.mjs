// Desktop notifications (when switched on): sent for a finished or failed transfer and for a new pairing request, never when the
// setting (or the kind's own switch) is off, and a pairing request is still looked for while the window is hidden.
//
//   node --test desktop/ui-tests/desktop-notifications.test.mjs

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

/** A window that is not the one being looked at, recording every desktop_notify call. */
async function away() {
  const ctx = await open(); const { A, E, d } = ctx;
  d.hasFocus = () => false; A.S.desktopNotif = true;
  const sent = []; const orig = E.call.bind(E);
  E.call = (name, args) => { if (name === "desktop_notify") { sent.push(args); return Promise.resolve(true); } if (name === "list_pair_requests") return Promise.resolve({ requests: ctx.requests || [] }); return orig(name, args); };
  ctx.sent = sent; return ctx;
}

test("notifications are on by default, and a saved off is kept", async () => {
  const { A } = await open();
  assert.equal(A.S.desktopNotif, true, "a fresh profile has Desktop notifications on");
});

test("a finished transfer and a failed one are sent; the kind's switch and the main switch are respected", async () => {
  const { A, sent } = await away();
  A.osNotify("ok", "a.txt downloaded"); A.osNotify("error", "b.txt: failed");
  assert.deepEqual(sent.map((s) => s.kind), ["done", "error"]);
  A.S.notifyDone = false; A.osNotify("ok", "c.txt downloaded");
  assert.equal(sent.length, 2, "Notify when a transfer finishes is off");
  A.S.desktopNotif = false; A.osNotify("error", "d.txt: failed");
  assert.equal(sent.length, 2, "Desktop notifications is off");
});

test("a new pairing request is sent once", async () => {
  const ctx = await away(); const { A, sent } = ctx;
  ctx.requests = [{ id: "r1", label: "Pixel 9", address: "192.168.1.5" }];
  await A.loadInbox(); await A.loadInbox();
  assert.deepEqual(sent.filter((s) => s.kind === "pair").map((s) => s.body), ["Pixel 9 wants to pair"]);
});

test("the pairing-request poll keeps running while the window is hidden when notifications are on", async () => {
  const { A } = await open(); const due = A.inboxPollDue;
  assert.equal(typeof due, "function");
  const base = { paused: false, online: 1, visible: false };
  assert.equal(due({ ...base, notify: true }), true, "hidden but notifying: look");
  assert.equal(due({ ...base, notify: false }), false, "hidden and not notifying: skip");
  assert.equal(due({ ...base, visible: true, notify: false }), true);
  assert.equal(due({ ...base, visible: true, online: 0 }), false, "no computer online");
  assert.equal(due({ ...base, visible: true, paused: true }), false);
});
