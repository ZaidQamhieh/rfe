// Fixes found by walking through the Settings page and the file list like a person would:
// Two clicks really opens, a bigger Interface size is never a smaller page, Sign out says what is true, the Settings
// strings and dialogs speak German/Spanish/Arabic, focus comes back to the control that opened a dialog, and the
// Test notification row does not repeat its name on its button.
//
//   node --test desktop/ui-tests/settings-journey.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

import { readAllCss } from "./css-lib.mjs";
const css = readAllCss();
const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const NAS = "192.168.1.20:7443";
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The window on the stub core, started on `view`, with every script error collected in `errs`. */
async function open(view = "settings") {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 300)));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1" + (view ? "&view=" + view : ""),
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
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}
const until = async (fn, what) => { for (let i = 0; i < 100; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();

// D1: the first click of "Two clicks" redraws the rows, so the browser never reports a double click on one element.
test("Two clicks: two quick clicks on the same folder open it, even though the first click redraws the row", async () => {
  const { w, d, A, E, errs } = await open("");
  /* the v5 tree: rows are .n[data-i]; a folder is "open" when its branch is expanded (A.v5Files.open) */
  await E.fs.load(NAS, "/srv"); A.v5Files.open.add(NAS + "\u0001/srv"); A.v5Files.paint(); await sleep(120);
  A.S.openMode = "double";
  const isOpen = () => A.v5Files.open.has(NAS + "\u0001/srv/projects");
  const row = (name) => [...d.querySelectorAll("#stage .tr .n[data-i]")].find((r) => r.querySelector(".l") && text(r.querySelector(".l")).startsWith(name));
  const click = (el) => el.dispatchEvent(new w.MouseEvent("click", { bubbles: true, cancelable: true }));
  const first = row("projects"); assert.ok(first);
  click(first);
  assert.equal(isOpen(), false, "one click only selects");
  assert.notEqual(row("projects"), first, "the first click redrew the row (why dblclick never arrived)");
  click(row("projects"));
  await until(isOpen, "two clicks to open the folder");
  assert.deepEqual(errs, []);
});

test("Two clicks: clicks on different items, or far apart, do not open; a browser dblclick after the opening click does not open a second time", async () => {
  const { w, d, A, E, errs } = await open("");
  await E.fs.load(NAS, "/srv"); A.v5Files.open.add(NAS + "\u0001/srv"); A.v5Files.paint(); await sleep(120);
  A.S.openMode = "double";
  const isOpen = (p) => A.v5Files.open.has(NAS + "\u0001" + p);
  const row = (name) => [...d.querySelectorAll("#stage .tr .n[data-i]")].find((r) => r.querySelector(".l") && text(r.querySelector(".l")).startsWith(name));
  const fire = (el, type) => el.dispatchEvent(new w.MouseEvent(type, { bubbles: true, cancelable: true }));
  const names = ["backups", "media"]; assert.ok(names.every((n) => row(n)), "two rows");
  fire(row(names[0]), "click"); fire(row(names[1]), "click");
  assert.ok(!isOpen("/srv/backups") && !isOpen("/srv/media"), "two different items are not a double click");
  fire(row("projects"), "click"); await sleep(520); fire(row("projects"), "click");
  assert.ok(!isOpen("/srv/projects"), "two slow clicks are two selections");
  fire(row("projects"), "click");
  await until(() => isOpen("/srv/projects"), "the folder to open");
  await sleep(150);
  const inside = [...d.querySelectorAll("#stage .tr .n[data-i]")].find((r) => r.getAttribute("aria-level") === "3"); assert.ok(inside, "rows inside the folder");
  const insidePath = inside.querySelector(".l").textContent.trim();
  fire(inside, "dblclick");
  await sleep(150);
  assert.ok(isOpen("/srv/projects") && ![...A.v5Files.open].some((k) => k.endsWith("/srv/projects/" + insidePath)), "the same gesture must not also open the first row of the new folder");
  assert.deepEqual(errs, []);
});

// D2: the root size is a function of the window and Interface size; a larger size must never give a smaller page.
function rootSize(u, W, H) {
  const m = css.match(/html\{--ui:1;font-size:([^}]*)\}/); assert.ok(m, "root rule");
  // the --root-* constants are unitless numbers (px is applied in the rule), so their values are substituted first
  const def = (n) => { const d = css.match(new RegExp(n + ":([^;}]*)")); assert.ok(d, n); return d[1]; };
  let s = m[1]; while (/var\(--root-/.test(s)) s = s.replace(/var\((--root-[\w-]+)\)/g, (_, n) => "(" + def(n) + ")");
  const expr = s.replace(/calc\(/g, "(").replace(/\bmin\(/g, "Math.min(").replace(/var\(--ui\)/g, "u")
    .replace(/(\d*\.?\d+)rem/g, "($1*16)").replace(/100vw/g, "W").replace(/100vh/g, "H").replace(/(\d*\.?\d+)px/g, "($1)");
  return new Function("u", "W", "H", "clamp", "return " + expr)(u, W, H, (lo, v, hi) => Math.max(lo, Math.min(v, hi)));
}

// The 0.01px slack covers the rounding of the CSS constants (13.088 vs 900 * .01455), far below one device pixel.
test("Interface size: 130% is never smaller than 115%, 100% or 90% in the same window", () => {
  const sizes = [0.9, 1, 1.15, 1.3];
  for (let W = 900; W <= 2560; W += 20) for (let H = 600; H <= 1440; H += 20) {
    for (let i = 1; i < sizes.length; i++) assert.ok(rootSize(sizes[i], W, H) >= rootSize(sizes[i - 1], W, H) - 0.01, `${sizes[i] * 100}% < ${sizes[i - 1] * 100}% at ${W}x${H}: ${rootSize(sizes[i], W, H)} < ${rootSize(sizes[i - 1], W, H)}`);
  }
  assert.ok(rootSize(1.3, 1280, 800) > rootSize(1.15, 1280, 800), "1280x800: 130% is larger than 115%");
});

test("Interface size: the 100% page is 16px up to 1280px wide, then grows with the window to 24px at 2560px", () => {
  for (const [W, H, px] of [[900, 600, 16], [1100, 700, 16], [1280, 800, 16], [1920, 1080, 20], [2560, 1440, 24], [3840, 2160, 24]]) assert.ok(Math.abs(rootSize(1, W, H) - px) < 0.01, `100% at ${W}x${H}: ${rootSize(1, W, H)}`);
  assert.ok(Math.abs(rootSize(1.3, 1280, 800) - 20.8) < 0.01, "130% is 130% of the window's size");
  assert.ok(Math.abs(rootSize(1.3, 2560, 1440) - 31.2) < 0.01, "130% of 24px in a large window");
});

// D3: Sign out says what is true.
test("Security > Sign out: no computer, a failed count and an empty list each get their own message", async () => {
  const { d, A, errs } = await open();
  const snack = () => { const s = d.querySelector(".snack"); return s ? text(s) : ""; };
  const hint = () => [...d.querySelectorAll(".k-sbody .k-set")].find((r) => r.firstElementChild.querySelector("b").textContent === "Sign out all phones").firstElementChild.querySelector("small").textContent;
  d.querySelector('[data-snav="Security"]').click(); await sleep(250);
  // a failed count
  A.X.devTried = true; A.X.devErr = "The list could not be refreshed: boom"; A.X.devAt = 1; A.X.devices = [];
  A.pageSettings(); assert.equal(hint(), "Could not count phones");
  d.querySelector('[data-sx="revokeall"]').click();
  assert.equal(snack(), "Could not check paired phones");
  // really none paired
  A.X.devErr = ""; A.pageSettings(); d.querySelector('[data-sx="revokeall"]').click();
  assert.equal(snack(), "No phones are paired");
  // no computer connected
  const was = A.pairServer; A.pairServer = () => null;
  A.pageSettings(); assert.equal(hint(), "No computer is connected");
  d.querySelector('[data-sx="revokeall"]').click();
  assert.equal(snack(), "No computer is connected");
  A.pairServer = was;
  A.S.lang = "de"; for (const k of ["No computer is connected", "Could not check paired phones", "Still checking paired phones. Try again in a moment."]) assert.notEqual(A.tr(k), k, k);
  assert.deepEqual(errs, []);
});

// D4: the Settings page and its dialogs in the other languages.
const KEYS = ["Troubleshooting", "Help", "Interface size", "Open files and folders", "One click", "Two clicks", "Change…", "Reset", "Send", "Log detail", "Detailed", "Check", "Back up…", "Restore…",
  "Keep items in Trash for", "Only used while Trash is on.", "Off means a delete cannot be undone.", "Try again by itself when a computer goes away.", "How many files move at the same time.", "Caps the total speed of all transfers.",
  "Keyboard shortcuts", "Press", "for everything", "No phones are paired", "The keystore works: a test secret was saved, read back and removed.", "Create a new device key?", "Create a new device key",
  "Connects to your computers over a private, checked connection.", "Welcome to RFE", "Make it yours", "Connect your first computer", "Later", "Update channel", "Check again", "Show in folder"];
for (const lang of ["de", "es", "ar"]) {
  test("Settings strings have a " + lang + " row (nav, hints, buttons, dialogs, tour)", async () => {
    const { A } = await open();
    A.S.lang = lang;
    const miss = KEYS.filter((k) => A.tr(k) === k);
    assert.deepEqual(miss, [], "untranslated in " + lang);
    assert.ok(A.tr("Downloads go to /home/me/Downloads").includes("/home/me/Downloads") && A.tr("Downloads go to /home/me/Downloads") !== "Downloads go to /home/me/Downloads", "the path stays, the words change");
    assert.notEqual(A.tr("Log of atlas-nas"), "Log of atlas-nas");
    assert.ok(A.tr("Log of atlas-nas").includes("atlas-nas"));
    assert.notEqual(A.tr("30 days"), "30 days"); assert.notEqual(A.tr("6 computers"), "6 computers"); assert.notEqual(A.tr("1.1.0 available"), "1.1.0 available");
    A.S.lang = "en";
  });
}

test("German Settings: every section and dialog is translated except names, key names, paths and sample data", async () => {
  const { w, d, A } = await open();
  const ok = /^(RFE|RFE Desktop|Remote File Explorer|System|Desktop|Details|Normal|Beta|Passphrase|English|Deutsch|Español|Faster folders|A fix|agent started|login ok|Ctrl|Esc|Enter|Del|Backspace|Alt\+|Ctrl\+|Shift\+|F\d|Shift$|Alt$|Updates$)/; /* "Updates" is also the German word (the Desktop notification kinds list it now that they start on); the Help list draws each key of a combination as its own key name */
  const sample = /^(atlas-nas|build-box|vps-fra1|studio-mac|media-pi|office-pc)$/;
  const seen = new Map();
  const isEn = (t) => { A.S.lang = "de"; const r = A.tr(t); A.S.lang = "en"; return r === t; };
  const collect = (root, where) => {
    const tw = d.createTreeWalker(root, 4); let n;
    while ((n = tw.nextNode())) { const t = n.nodeValue.trim(); if (!t || !/[A-Za-z]{3}/.test(t) || n.parentElement.closest("script,style,.mono,[data-nolocal],input,textarea") || ok.test(t) || sample.test(t)) continue; if (isEn(t)) seen.set(t, where); }
    root.querySelectorAll("[aria-label],[title],[placeholder]").forEach((e) => { for (const a of ["aria-label", "title", "placeholder"]) { const t = (e.getAttribute(a) || "").trim(); if (t && /[A-Za-z]{3}/.test(t) && !ok.test(t) && !sample.test(t) && isEn(t)) seen.set(t, where + "@" + a); } });
  };
  for (const nm of [...d.querySelectorAll("[data-snav]")].map((b) => b.dataset.snav)) {
    d.querySelector('[data-snav="' + nm + '"]').click(); await sleep(120);
    collect(d.querySelector("#stage"), nm);
    for (const sx of [...d.querySelectorAll("[data-sx]")].map((b) => b.dataset.sx)) {
      const b = d.querySelector('[data-sx="' + sx + '"]'); if (!b) continue;
      b.click(); await sleep(220);
      d.querySelectorAll(".dlg,.snack,.onb").forEach((x) => collect(x, nm + ":" + sx));
      d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true })); await sleep(80);
      d.querySelectorAll(".onb,.scrim").forEach((x) => x.remove());
    }
  }
  assert.deepEqual([...seen], [], "English left on the Settings page");
});

// D5: focus returns to the control that opened a dialog.
test("Escape on the Welcome tour gives focus back to the Show again button", async () => {
  const { w, d, errs } = await open();
  d.querySelector('[data-snav="About"]').click(); await sleep(120);
  const b = d.querySelector('[data-sx="welcome"]'); b.focus(); assert.equal(d.activeElement, b);
  b.click(); await sleep(120);
  assert.ok(d.querySelector(".onb"), "the tour is open");
  assert.notEqual(d.activeElement, b, "focus moved into the tour");
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true })); await sleep(80);
  assert.equal(d.querySelector(".onb"), null, "Escape closes the tour");
  assert.equal(d.activeElement && d.activeElement.dataset.sx, "welcome", "focus is back on Show again");
  assert.deepEqual(errs, []);
});

test("Confirming the device-key reset redraws the page but keeps focus on its Reset… button", async () => {
  const { d, errs } = await open();
  d.querySelector('[data-snav="Troubleshooting"]').click(); await sleep(120);
  const b = d.querySelector('[data-sx="devkey"]'); b.focus(); b.click(); await sleep(150);
  const go = d.querySelector('.dlg [data-id="go"]'); assert.ok(go, "the confirm dialog");
  go.click(); await sleep(500);
  assert.equal(d.querySelector(".dlg"), null, "dialog closed");
  assert.notEqual(d.querySelector('[data-sx="devkey"]'), b, "the page was redrawn");
  assert.equal(d.activeElement, d.querySelector('[data-sx="devkey"]'), "focus is on the new Reset… button");
  assert.deepEqual(errs, []);
});

// D7
test("Desktop: the Test notification row's button reads Test notification (the mockup's word), has no icon and no separate aria-label", async () => {
  const { d, errs } = await open();
  d.querySelector('[data-snav="Desktop"]').click(); await sleep(120);
  const row = [...d.querySelectorAll(".k-sbody .k-set")].find((r) => r.firstElementChild.querySelector("b").textContent === "Test notification");
  assert.ok(row, "row labelled Test notification");
  const btn = row.querySelector("button");
  assert.equal(text(btn), "Test notification");
  assert.equal(btn.getAttribute("aria-label"), null, "the visible word already names it");
  assert.equal(btn.querySelector("svg"), null, "buttons in Settings rows are words only");
  assert.deepEqual(errs, []);
});
