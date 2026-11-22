// The scripts register themselves with the module registry (ui/registry.js) and say what they need, so the order of the
// <script> tags does not matter and a missing need is named when the page loads.
//
//   node --test desktop/ui-tests/registry.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync, readdirSync, writeFileSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";

const uiDir = fileURLToPath(new URL("../ui/", import.meta.url));
const harness = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** What each script file declares: { name, deps } for every rfeModule(...) call. */
function declared() {
  const out = [];
  for (const f of readdirSync(uiDir).filter((x) => x.endsWith(".js"))) {
    for (const m of readFileSync(uiDir + f, "utf8").matchAll(/^rfeModule\('([^']+)', \[([^\]]*)\]/gm)) {
      out.push({ file: f, name: m[1], deps: [...m[2].matchAll(/'([^']+)'/g)].map((d) => d[1]) });
    }
  }
  return out;
}

test("every dependency names a module or one of the three plain scripts, and nothing waits on itself in a circle", () => {
  const mods = declared(), names = new Set(mods.map((m) => m.name)), plain = new Set(["engine", "icons", "qr"]);
  assert.ok(mods.length >= 15, "the app's scripts are registered");
  for (const m of mods) for (const d of m.deps) assert.ok(names.has(d) || plain.has(d), m.name + " needs unknown '" + d + "'");
  const state = new Map();
  const visit = (n, path) => {
    if (state.get(n) === 2 || plain.has(n)) return;
    assert.notEqual(state.get(n), 1, "circle: " + [...path, n].join(" -> "));
    state.set(n, 1); mods.find((m) => m.name === n).deps.forEach((d) => visit(d, [...path, n])); state.set(n, 2);
  };
  mods.forEach((m) => visit(m.name, []));
});

/** The harness page with the app's scripts in the given order, loaded in jsdom. */
async function boot(order) {
  const html = readFileSync(harness, "utf8");
  const tag = (f) => '<script src="../ui/' + f + '"></script>';
  const mods = declared().map((m) => m.file).filter((f, i, a) => a.indexOf(f) === i && f !== "registry.js");
  let first = true;
  const swapped = html.replace(/<script src="\.\.\/ui\/(ui|pages|features|fpages|fx\d|v5-[a-z]+)\.js"><\/script>\n?/g, () => { if (!first) return ""; first = false; return order(mods).map(tag).join("\n") + "\n"; });
  const page = fileURLToPath(new URL("../ui-dev/.registry-order.html", import.meta.url));
  writeFileSync(page, swapped);
  const errs = [], vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push(((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  try {
    const dom = await JSDOM.fromFile(page, {
      runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc, url: "file://" + page + "?fast=1",
      beforeParse(w) {
        w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
        w.TextEncoder = TextEncoder; w.TextDecoder = TextDecoder;
        w.ResizeObserver = class { observe() {} disconnect() {} };
        w.HTMLElement.prototype.scrollIntoView = function () {};
        Object.defineProperty(w.crypto, "subtle", { value: webcrypto.subtle, configurable: true });
        w.navigator.clipboard = { writeText: () => Promise.resolve() };
      },
    });
    const w = dom.window; live.push(w);
    for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
    await sleep(150);
    return { w, errs };
  } finally { rmSync(page, { force: true }); }
}

test("the window starts with its scripts in reverse order, with every module run", async () => {
  const { w, errs } = await boot((m) => [...m].reverse());
  assert.deepEqual(JSON.parse(JSON.stringify(w.rfeModule.pending())), []);
  assert.equal(w.rfeModule.done().length, declared().length);
  assert.ok(w.A && w.A.ready && typeof w.A.pageTools === "function", "the app is up");
  assert.deepEqual(errs.filter((e) => !/Not implemented/.test(e)), []);
});

test("a module whose need never appears is named, with the need, once the page has loaded", async () => {
  const reports = [], vc = new VirtualConsole();
  vc.on("jsdomError", (e) => reports.push(e.detail ? String(e.detail.message) : e.message));
  const dom = new JSDOM("<!doctype html><body></body>", { runScripts: "outside-only", virtualConsole: vc });
  const w = dom.window; live.push(w);
  w.eval(readFileSync(uiDir + "registry.js", "utf8"));
  let ran = false;
  w.rfeModule("late", ["nothing"], () => { ran = true; });
  assert.deepEqual(JSON.parse(JSON.stringify(w.rfeModule.pending())), [{ name: "late", missing: ["nothing"] }]);
  assert.equal(ran, false);
  w.dispatchEvent(new w.Event("load"));
  assert.ok(reports.some((r) => /late \(needs nothing\)/.test(r)), "the page names it: " + reports.join(" / "));
  w.rfeModule("nothing", [], () => {});
  assert.equal(ran, true, "it runs as soon as the need is there");
  assert.throws(() => w.rfeModule("late", [], () => {}), /defined twice/);
});
