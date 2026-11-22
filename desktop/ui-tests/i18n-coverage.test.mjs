// Every English word on the main pages must have a German, Spanish and Arabic row in ui/fx5.js: the translator matches
// the English text, so a reworded sentence silently stays English. This renders each page on the stub core, lists
// the visible text, and fails for any piece with no translation. Names, paths, numbers and file data are not text to
// translate and are skipped (DATA below).
//
//   node --test desktop/ui-tests/i18n-coverage.test.mjs

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
      Object.defineProperty(w, "innerWidth", { value: 1400, configurable: true, writable: true });
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


const PAGES = ["home", "servers", "files", "transfers", "devices", "tools", "history", "settings", "search"];
/* Words that are the same in German or Spanish as in English, so an unchanged result is correct there (Arabic is still checked). */
const SAME_IN_DE_ES = new Set(["Computer", "Name", "Text", "Desktop", "System", "Offline", "Online", "Status", "Videos", "Audio", "Apps", "Download", "Upload", "Error", "Total", "Admin", "Link", "Links", "Cache", "Backup", "Hotspot", "Tailscale", "Linux", "Windows", "macOS", "LAN", "OK", "Details", "offline", "Online"]);
/* Product and system names: never translated. */
const PROPER = new Set(["RFE", "Debian", "Ubuntu", "Tailscale", "Windows", "Linux", "macOS", "Android"]);
/* Pieces that are data from the stub or the operating system, not interface text. */
const DATA = new Set(["login", "device pair", "password", "Download", "Upload", "Pixel 8", "Debian 12"]);

test("every English piece of text on the main pages has a German, Spanish and Arabic row", async () => {
  const { d, A, E, errs } = await open();
  assert.deepEqual(errs, [], "page scripts load"); assert.equal(typeof A.tr, "function");
  /* Account names are data the agent reports (the audit log's target), not interface text, so they are skipped like computer names. */
  const names = new Set([...E.servers.map((s) => s.name), ...E.servers.map((s) => s.user).filter(Boolean)]);
  const seen = new Map();
  for (const p of PAGES) {
    A.go(p); await sleep(400);
    for (const root of [d.getElementById("stage"), d.getElementById("rail")].filter(Boolean)) {
      const add = (t) => { for (const part of t.replace(/\s+/g, " ").trim().split(" · ")) { const x = part.trim(); if (x && !seen.has(x)) seen.set(x, p); } };
      const it = d.createTreeWalker(root, 4);
      for (let n; (n = it.nextNode());) { const e = n.parentElement; if (e && e.closest("script,style,.mono,code,pre,[data-nolocal],.crumb,.dnt")) continue; add(n.nodeValue); }
      for (const e of root.querySelectorAll("[data-tip],[aria-label],[placeholder]")) for (const a of ["data-tip", "aria-label", "placeholder"]) if (e.getAttribute(a)) add(e.getAttribute(a));
    }
  }
  const gaps = [];
  for (const [t, page] of seen) {
    if (!/[A-Za-z]{3}/.test(t) || /\d|^[\/~.]/.test(t) || names.has(t) || PROPER.has(t) || DATA.has(t) || /^Download ·|^zaid, |^[\w-]+ off$/.test(t) || /^[a-z0-9._-]+$/.test(t) && /[.-]/.test(t)) continue;
    for (const lang of ["de", "es", "ar"]) {
      A.S.lang = lang;
      if (A.tr(t) === t && !(lang !== "ar" && SAME_IN_DE_ES.has(t))) gaps.push(lang + " " + page + ": " + t);
    }
  }
  A.S.lang = "en";
  assert.deepEqual(gaps, [], "English text with no translation row in fx5.js");
  assert.deepEqual(errs, []);
});
