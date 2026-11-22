// Row density (Settings > Appearance): Compact and Comfortable set different row heights and paddings, through CSS variables on
// the root, and the lists of the Files tree, Transfers, History and Devices read them.
//
//   node --test desktop/ui-tests/row-density.test.mjs

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

import { readAllCss } from "./css-lib.mjs";
const css = readAllCss();

/** The declarations of every rule whose selector is exactly `sel`, merged, as { name: value }. */
function rule(sel) {
  const esc = sel.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const re = new RegExp("(^|[}\\n])\\s*" + esc + "\\s*\\{", "g");
  const found = {}; let m, n = 0;
  while ((m = re.exec(css))) {
    n++; const at = m.index + m[0].length;
    for (const x of css.slice(at, css.indexOf("}", at)).split(";").map((s) => s.trim()).filter(Boolean)) { const k = x.indexOf(":"); found[x.slice(0, k).trim()] = x.slice(k + 1).trim(); }
  }
  assert.ok(n, "no rule " + sel);
  return found;
}
const VARS = ["--row-h", "--ro-h", "--ev-h", "--ro-pad", "--ev-pad"];
const px = (v) => parseFloat(v);

test("Compact gives smaller values than Comfortable for every row variable", () => {
  const base = rule(":root"), compact = rule(':root[data-density="compact"]');
  for (const v of VARS) {
    assert.ok(base[v], v + " has a Comfortable value");
    assert.ok(compact[v], v + " has a Compact value");
    assert.ok(px(compact[v]) < px(base[v]), v + ": " + compact[v] + " < " + base[v]);
  }
});

test("the Files tree, Transfers, History and Devices rows read the variables", () => {
  assert.match(rule(".k-frow").height, /var\(--row-h\)/, "Files tree");
  assert.match(rule(".stage.v5x .li .ro")["min-height"], /var\(--ro-h\)/, "Transfers");
  assert.match(rule(".stage.v5x .li .ro").padding, /var\(--ro-pad\)/, "Transfers padding");
  assert.match(rule(":where(.d5) .cpg .ro")["min-height"], /var\(--ro-h\)/, "Devices");
  assert.match(rule(":where(.d5) .cpg .ro").padding, /var\(--ro-pad\)/, "Devices padding");
  assert.match(rule(":where(.d5).rfe-pages .b-ev")["min-height"], /var\(--ev-h\)/, "History");
  assert.match(rule(":where(.d5).rfe-pages .b-ev").padding, /var\(--ev-pad\)/, "History padding");
});

test("choosing Compact in Settings puts data-density=compact on the root, Comfortable puts it back", async () => {
  const { A, d, errs } = await open();
  const seg = (v) => d.querySelector('[data-set="density"] button[data-v="' + v + '"]');
  A.go("settings"); await sleep(80);
  assert.equal(d.documentElement.dataset.density, "comfortable");
  seg("compact").click(); await sleep(60);
  assert.equal(d.documentElement.dataset.density, "compact");
  seg("comfortable").click(); await sleep(60);
  assert.equal(d.documentElement.dataset.density, "comfortable");
  assert.deepEqual(errs, []);
});
