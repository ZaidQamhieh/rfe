// Devices > "Recent security events": an app launch entry from the agent is shown as a readable line (what, which app, from
// which device, and why it did not start), never with the long app_<hash> id, and the raw entry stays in a tooltip.
// Every new fixed phrase has a German, Spanish and Arabic row.
//
//   node --test desktop/ui-tests/devices-events-text.test.mjs

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

const HASH = "app_f790bca5d15b1efccfe057ad753b4f93dcdd58e19f1cc9b47d6f5b4b829aed1a";
/** The text the app builds from one agent audit entry (fpages.js loadAudit / fx4.js syncAudit). */
const entry = (action, target, detail, actor) => ({ at: Date.now() - 1000, text: [action, target, detail].filter(Boolean).join(" · ") + (actor ? " (" + actor + ")" : "") });

/** Draws the Devices page with these audit entries and returns the rendered event rows. */
async function events(ctx, list) {
  const { A, E, d } = ctx;
  A.loadDevices = () => {}; A.loadAudit = () => {};
  Object.assign(A.X, { devices: [], devAt: E.now(), devBusy: false, devErr: "", auditAt: E.now(), audit: list });
  A.go("devices"); A.pageDevices(); await sleep(150);
  return [...d.querySelectorAll("#stage .k-ev")];
}
const textOf = (el) => el.textContent.replace(/\s+/g, " ").trim();

test("a launch that found no desktop session reads as a sentence, without the app id, with the raw entry in the tooltip", async () => {
  const ctx = await open();
  const raw = entry("app_launch", HASH, 'name="Air" outcome=no_interactive_session', "Mobile App");
  const [row] = await events(ctx, [raw]);
  assert.ok(row, "the event row is drawn");
  const t = textOf(row);
  assert.match(t, /Launch of "Air"/);
  assert.match(t, /No desktop session/);
  assert.match(t, /from Mobile App/);
  assert.doesNotMatch(t, /app_[0-9a-f]{8}|no_interactive_session|outcome=/, "no raw id or outcome code");
  assert.ok(row.classList.contains("bad"), "a launch that did not start is flagged");
  assert.equal(row.getAttribute("title"), raw.text, "the raw entry is the tooltip");
  assert.deepEqual(ctx.errs, []);
});

test("every outcome the agent writes has its own phrase; an unknown one is still readable", async () => {
  const ctx = await open();
  const phrases = {
    started: "Started", permission_denied: "Not allowed for this device", invalid_id: "Invalid app", rate_limited: "Too many attempts",
    unsupported_platform: "This PC cannot launch apps", launcher_unavailable: "Launcher unavailable", catalog_error: "App list unavailable",
    not_found: "App not found", not_launchable: "App cannot be launched", busy: "Another launch is in progress",
    no_interactive_session: "No desktop session", launch_failed: "Could not start",
  };
  const rows = await events(ctx, [...Object.keys(phrases), "some_new_code"].map((o) => entry("app_launch", HASH, 'name="Air" outcome=' + o, "Mobile App")));
  assert.equal(rows.length, 4, "the page shows the newest four");
  for (const [code, phrase] of Object.entries(phrases)) {
    const [row] = await events(ctx, [entry("app_launch", HASH, 'name="Air" outcome=' + code, "Mobile App")]);
    const t = textOf(row);
    assert.ok(t.includes(phrase), code + " -> " + phrase + " (got: " + t + ")");
    assert.doesNotMatch(t, /outcome=|_/, code + " shows no raw code");
    assert.equal(row.classList.contains("bad"), code !== "started", code + " flag");
  }
  const [unk] = await events(ctx, [entry("app_launch", HASH, 'name="Air" outcome=some_new_code', "Mobile App")]);
  assert.match(textOf(unk), /Some new code/);
});

test("a launch refused before the app was known has no name and still reads well; the name's quotes survive", async () => {
  const ctx = await open();
  const [a] = await events(ctx, [entry("app_launch", HASH, "outcome=permission_denied", "Mobile App")]);
  assert.match(textOf(a), /Launch of an app/);
  assert.match(textOf(a), /Not allowed for this device/);
  assert.doesNotMatch(textOf(a), /app_[0-9a-f]{8}/);
  const [b] = await events(ctx, [entry("app_launch", HASH, 'name="Say \\"hi\\"" outcome=started', "")]);
  assert.match(textOf(b), /Launch of "Say "hi""/);
  assert.doesNotMatch(textOf(b), /\bfrom\b/, "no actor, no 'from'");
});

test("a long app id in any other entry is cut short, and the full text stays in the tooltip", async () => {
  const ctx = await open();
  const raw = entry("share_created", HASH, "", "Mobile App");
  const [row] = await events(ctx, [raw]);
  assert.doesNotMatch(textOf(row), /app_[0-9a-f]{12}/);
  assert.equal(row.getAttribute("title"), raw.text);
});

test("the phrases of a launch line have German, Spanish and Arabic rows", async () => {
  const ctx = await open(); const { A, d } = ctx;
  const outs = ["started", "permission_denied", "invalid_id", "rate_limited", "unsupported_platform", "launcher_unavailable", "catalog_error", "not_found", "not_launchable", "busy", "no_interactive_session", "launch_failed"];
  const pieces = new Set();
  for (const o of outs) for (const nm of ['name="Air" ', ""]) {
    const [row] = await events(ctx, [entry("app_launch", HASH, nm + "outcome=" + o, "Mobile App")]);
    const it = d.createTreeWalker(row, 4);
    for (let n; (n = it.nextNode());) { const e = n.parentElement; if (e && e.closest("time,.mono")) continue; for (const p of n.nodeValue.split(" · ")) if (p.trim()) pieces.add(p.trim()); }
  }
  const gaps = [];
  for (const p of pieces) for (const lang of ["de", "es", "ar"]) { A.S.lang = lang; if (A.tr(p) === p) gaps.push(lang + ": " + p); }
  A.S.lang = "en";
  assert.ok(pieces.size >= 12, "the phrases were collected: " + pieces.size);
  assert.deepEqual(gaps, []);
});


const DEVICE_ID = "d_3f9a1c52e07b4d68a1b2c3d4e5f60718";
const UPDATED = 'jailRoot="" readOnly=false viewApps=true launchApps=false browse=true download=true upload=true modify=true delete=true share=true wake=true';

test("a device access change reads as a sentence: no key=value pairs, no raw action name, the raw entry in the tooltip", async () => {
  const ctx = await open();
  const raw = entry("device_updated", DEVICE_ID, UPDATED, "host owner (CLI)");
  const [row] = await events(ctx, [raw]);
  assert.ok(row, "the event row is drawn");
  const t = textOf(row);
  assert.match(t, /Device access changed/);
  assert.match(t, /Every folder/);
  assert.match(t, /from host owner \(CLI\)/);
  assert.doesNotMatch(t, /device_updated|readOnly|=false|=true|jailRoot|d_3f9a1c52/, "no raw action, key or id: " + t);
  assert.equal(row.getAttribute("title"), raw.text, "the raw entry is the tooltip");
  assert.ok(!row.classList.contains("bad"), "a permission change is not an error");
});

test("a device access change says when the device is read-only, limited to a folder, or has actions turned off", async () => {
  const ctx = await open();
  const lim = UPDATED.replace('jailRoot=""', 'jailRoot="C:\\\\Program Files (x86)\\\\Docs"').replace("readOnly=false", "readOnly=true").replace("upload=true", "upload=false");
  const [row] = await events(ctx, [entry("device_updated", DEVICE_ID, lim, "host owner (CLI)")]);
  const t = textOf(row);
  assert.match(t, /Device access changed/);
  assert.match(t, /Limited to a folder/);
  assert.match(t, /Read-only/);
  assert.match(t, /Some actions turned off/);
  assert.match(t, /from host owner \(CLI\)/, "an actor with its own brackets is read whole");
  assert.doesNotMatch(t, /Every folder|Program Files|=true|=false/);
  const [none] = await events(ctx, [entry("device_updated", DEVICE_ID, UPDATED, "")]);
  assert.doesNotMatch(textOf(none), /\bfrom\b/, "no actor, no 'from'");
});

test("the phrases of a device access change have German, Spanish and Arabic rows", async () => {
  const { A } = await open();
  const gaps = [];
  for (const p of ["Device access changed", "Every folder", "Limited to a folder", "Read-only", "Some actions turned off"]) for (const lang of ["de", "es", "ar"]) { A.S.lang = lang; if (A.tr(p) === p) gaps.push(lang + ": " + p); }
  A.S.lang = "en";
  assert.deepEqual(gaps, []);
});
