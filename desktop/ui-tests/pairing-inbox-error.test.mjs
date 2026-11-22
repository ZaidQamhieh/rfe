// A transient failure while looking for pairing requests keeps the last good list on the Devices page and says,
// inline, that the list could not be refreshed, with a Try again button; a good answer clears the message.
//
//   node --test desktop/ui-tests/pairing-inbox-error.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, started on the Devices page; list_pair_requests answers from ctx.requests (per host id) or fails for the hosts in ctx.fail (host id -> message). */
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
  const ctx = { w, d: w.document, A: w.A, errs, requests: {}, fail: {} };
  const E = w.A.E; const orig = E.call.bind(E);
  E.call = (name, args) => {
    if (name !== "list_pair_requests") return orig(name, args);
    if (ctx.fail[args.host]) return Promise.reject(new Error(ctx.fail[args.host]));
    return Promise.resolve({ requests: (ctx.requests[args.host] || []).map((q) => ({ ...q })) });
  };
  return ctx;
}

const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const REQ = { id: "r1", label: "Pixel 9", address: "192.168.1.77", matchCode: "4829 1307", ageSeconds: 18, expiresInSeconds: 102 };
/** The ids of the requests in the inbox, as an array of this realm (the window's arrays are not reference-equal to it). */
const ids = (A) => Array.from(A.X.inbox, (q) => q.id).sort();
/** The ids of the online computers the inbox looks at. */
const hosts = (A) => A.E.servers.filter((x) => x.state === "online").map((x) => x.id);
/** Every host fails with the same text. */
const failAll = (ctx, why) => { for (const h of hosts(ctx.A)) ctx.fail[h] = why; };

test("a failed refresh keeps the last list, says so inline, and Try again recovers", async () => {
  const ctx = await open(); const { d, A } = ctx; const [h1] = hosts(A);
  ctx.requests[h1] = [REQ];
  await A.loadInbox();
  assert.equal(A.X.inbox.length, 1, "the request is listed");

  failAll(ctx, "the agent did not answer");
  await A.loadInbox();
  assert.equal(A.X.inbox.length, 1, "the last good list is kept after a failed refresh");
  assert.equal(A.X.pending && A.X.pending.id, "r1", "the pending request is kept too");
  A.pageDevices();
  const stage = text(d.querySelector("#stage"));
  assert.match(stage, /Pixel 9/, "the request is still shown");
  assert.match(stage, /Pairing requests could not be refreshed/);
  assert.match(stage, /the agent did not answer/);
  assert.doesNotMatch(stage, /Nobody is asking to join|No phones are asking to pair/);
  const retry = d.querySelector('#stage [data-fx="inbox.retry"]');
  assert.ok(retry, "a Try again button is offered");

  ctx.fail = {}; ctx.requests[h1] = [];
  retry.click();
  await sleep(100);
  assert.equal(A.X.inbox.length, 0, "a good answer replaces the list");
  A.pageDevices();
  assert.doesNotMatch(text(d.querySelector("#stage")), /could not be refreshed/, "the message is gone after a good answer");
  assert.deepEqual(ctx.errs, []);
});

test("a failed refresh with nothing listed shows the error, not the empty line", async () => {
  const ctx = await open(); const { d, A } = ctx;
  failAll(ctx, "connection refused");
  await A.loadInbox();
  assert.equal(A.X.inbox.length, 0);
  A.pageDevices();
  const stage = text(d.querySelector("#stage"));
  assert.match(stage, /Pairing requests could not be refreshed/);
  assert.doesNotMatch(stage, /Nobody is asking to join|No phones are asking to pair/, "an unknown list is not shown as an empty one");
  assert.ok(d.querySelector('#stage [data-fx="inbox.retry"]'));
});

test("one computer failing keeps only its own last requests; the others refresh normally", async () => {
  const ctx = await open(); const { A } = ctx; const [h1, h2] = hosts(A);
  ctx.requests[h1] = [{ ...REQ, id: "a1", label: "Phone A" }]; ctx.requests[h2] = [{ ...REQ, id: "b1", label: "Phone B" }];
  await A.loadInbox();
  assert.deepEqual(ids(A), ["a1", "b1"]);
  ctx.requests[h1] = [{ ...REQ, id: "a2", label: "Phone A2" }]; ctx.fail[h2] = "timed out";
  await A.loadInbox();
  assert.deepEqual(ids(A), ["a2", "b1"], "host 1 refreshed, host 2 kept its last list");
});
