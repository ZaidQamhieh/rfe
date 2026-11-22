// The runtime translator (ui/fx5.js) also translates data-tip tooltips and <option> labels. An option without a
// `value` attribute gets one equal to its English text, so code that reads select.value (the History filters)
// keeps seeing English whatever the language. Switching back to English restores the original text.
// The real window scripts (ui/*.js) run in jsdom on the stub core, like v5-wording.test.mjs.
//
//   node --test desktop/ui-tests/v5-i18n.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** A 1400 px window on the stub core, with at least one computer in the list. */
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
  return { w, d: w.document, A: w.A, errs };
}

test("data-tip and option labels are translated; select.value stays English; English restores", async () => {
  const { d, A, errs } = await open();
  const host = d.createElement("div");
  host.innerHTML = '<button type="button" id="i18t" data-tip="Close (Esc)" aria-label="Close">x</button>' +
    '<select id="i18s"><option>When</option><option>Today</option><option>Last 7 days</option></select>' +
    '<select id="i18v"><option value="all">Pictures</option></select>';
  d.body.appendChild(host);
  const sel = d.getElementById("i18s"), vsel = d.getElementById("i18v"), tip = d.getElementById("i18t");
  const opts = () => [...sel.options].map((o) => o.textContent);

  A.S.lang = "de"; A.applyLang(); await sleep(50);
  assert.equal(tip.getAttribute("data-tip"), "Schließen (Esc)");
  assert.deepEqual(opts(), ["Wann", "Heute", "Letzte 7 Tage"]);
  assert.deepEqual([...sel.options].map((o) => o.value), ["When", "Today", "Last 7 days"], "values stay English");
  sel.selectedIndex = 1;
  assert.equal(sel.value, "Today", "select.value is the English text");
  assert.equal(vsel.options[0].value, "all", "an explicit value is kept");
  assert.equal(vsel.options[0].textContent, "Bilder");

  A.S.lang = "en"; A.applyLang(); await sleep(50);
  assert.equal(tip.getAttribute("data-tip"), "Close (Esc)");
  assert.deepEqual(opts(), ["When", "Today", "Last 7 days"]);
  assert.deepEqual(errs, []);
});

test("a count suffix and the \"this one\" suffix on option labels are translated", async () => {
  const { A, errs } = await open();
  A.S.lang = "de"; A.applyLang();
  assert.equal(A.tr("Transfers · 4"), "Übertragungen · 4");
  assert.equal(A.tr("Zaid’s phone (this one)"), "Zaid’s phone (dieses Gerät)");
  assert.deepEqual(errs, []);
});
