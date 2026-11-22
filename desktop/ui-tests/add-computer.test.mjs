// Adding a computer is three steps in one flow: 1 find or type it, 2 check it, 3 sign in or pair.
// A step indicator shows where you are; step 2 moves on to step 3 by itself when there is nothing
// to decide (the computer's key is already trusted) and stays when the person must trust a key;
// addresses, ports and key wording live in a "Technical details" fold that starts closed.
// The real window scripts (ui/*.js) run in jsdom on the stub core, like window.test.mjs.
//
//   node --test desktop/ui-tests/add-computer.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** A 1400 px window on the stub core, with the rail's Add a computer button ready to press. */
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

/* The v5 skin turns the dialog's class .dlg into .k-dlg; the button row keeps .df. */
const dialog = (d) => d.querySelector(".scrim .k-dlg, .scrim .dlg");
/* In the v5 window the box title follows the step ("Add a computer", then "Allow it on <PC>", then "Ready"), and so does the heading (h3) at the top of the pane. */
const boxTitle = (d) => (dialog(d)?.querySelector("h2")?.textContent || "").trim();
const title = (d) => (dialog(d)?.querySelector(".b-pane h3")?.textContent || "").trim();
const button = (d, label) => [...d.querySelectorAll(".scrim .df button")].find((b) => b.textContent.trim() === label);
/** The step indicator: its three labels and which one is the current step. */
const steps = (d) => {
  const col = dialog(d)?.querySelector(".b-sx");
  if (!col) return null;
  const items = [...col.children];
  return { labels: items.map((x) => x.querySelector("span").firstChild.textContent.trim()), current: items.findIndex((x) => x.classList.contains("on")) + 1, count: items.length };
};
/** Types an address in the first step's "Not in the list?" fold and presses Next. The computer is called by its host. */
function enter(d, host, port = "8765") {
  const f = d.querySelector("#v5addr");
  f.closest("details").open = true;
  f.value = port === "8765" ? host : host + ":" + port;
  f.dispatchEvent(new f.ownerDocument.defaultView.Event("input", { bubbles: true }));
  button(d, "Next").click();
}
/** Every step heading that ever appeared, so a screen that came and went still counts. The box title is "Add a computer" on every step, so the pane's first heading is what is recorded. */
function watchTitles(w, d) {
  const seen = [];
  const mo = new w.MutationObserver((list) => list.forEach((r) => r.addedNodes.forEach((n) => { if (n.nodeType === 1) { const h = n.querySelector?.(".b-pane h3"); if (h) seen.push(h.textContent.trim()); const wait = n.querySelector?.(".fxwait b"); if (wait) seen.push(wait.textContent.trim()); } })));
  mo.observe(d.querySelector("#layer"), { childList: true, subtree: true });
  return seen;
}
/** The dialog's visible words: everything except what sits in a closed "Technical details" fold. */
const plainText = (d) => { const c = dialog(d).cloneNode(true); c.querySelectorAll("details").forEach((x) => x.remove()); return c.textContent.replace(/\s+/g, " "); };
const TECH = /SHA-?256|certificate|\bagent\b|\bport\b|\d+\.\d+\.\d+\.\d+/i;

test("every first-step screen shows the step indicator 1, 2, 3 with step 1 current", async () => {
  const { d, errs } = await open();
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  const s = steps(d);
  assert.ok(s, "no step column on the first screen");
  assert.equal(s.count, 3, "three steps");
  assert.equal(s.current, 1, "step 1 is current");
  assert.deepEqual(s.labels.map((l) => l.replace(/^\d\s*/, "")), ["Find your PC", "Allow it on the PC", "Ready"]);
  assert.deepEqual([...dialog(d).querySelectorAll(".b-sx > div > i")].map((i) => i.textContent.trim()), ["1", "2", "3"]);
  assert.equal(dialog(d).querySelector(".b-sx").children.length, 3);
  assert.deepEqual(errs, []);
});

test("a key that needs trusting stays on step 2 and asks; nothing moves on until it is trusted", async () => {
  const { w, d, E, errs } = await open();
  const seen = watchTitles(w, d);
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  enter(d, "10.9.9.9"); await sleep(500);
  assert.equal(boxTitle(d), "Allow it on 10.9.9.9", "the box title names step 2");
  assert.match(title(d), /^Is this really 10\.9\.9\.9\?$/, "the question is asked: " + seen.join(" > "));
  assert.equal(steps(d).current, 2, "still step 2");
  assert.match(dialog(d).querySelector(".fxck").textContent, /It is the same on 10\.9\.9\.9/, "the tick-box says what the person confirms");
  assert.equal(d.querySelectorAll(".scrim").length, 1, "one dialog, not a stack");
  assert.ok(dialog(d).querySelector(".fpfull").textContent.replace(/[^0-9a-f]/gi, "").length >= 64, "the key is shown for the person to check");
  assert.equal(E.server("10.9.9.9:8765").state, "trust");
  await sleep(600);
  assert.match(title(d), /^Is this really 10\.9\.9\.9\?$/, "it did not move on by itself");
  assert.equal(button(d, "They match: trust"), undefined, "the old button text is gone");
  button(d, "Trust").click(); await sleep(300);
  assert.match(title(d), /^Is this really 10\.9\.9\.9\?$/, "an unticked box does not trust the key");
  assert.equal(E.server("10.9.9.9:8765").state, "trust");
  d.querySelector("#tfm").click(); button(d, "Trust").click(); await sleep(400);
  assert.match(title(d), /^Allow this computer on 10\.9\.9\.9$/, "after the person trusted it, step 3 follows");
  assert.deepEqual([...dialog(d).querySelectorAll(".fxtabs button")].map((b) => b.textContent.trim()), ["Password", "Pairing code", "On the PC"], "the three ways to be let in");
  assert.ok(button(d, "Ask") || dialog(d).querySelector("#siu, #sic"), "the way chosen has its own button or fields");
  assert.equal(steps(d).current, 2, "being let in is step 2 (\"Allow it on the PC\"); step 3 is Ready");
  assert.equal(d.querySelectorAll(".scrim").length, 1);
  assert.deepEqual(errs, []);
});

test("a computer whose key is already trusted goes from step 2 to step 3 by itself, with no Trust screen", async () => {
  const { w, d, E, errs } = await open();
  const addServer = E.addServer;
  E.addServer = (o) => { const s = addServer(o); s.pinned = true; s.fp = Array(32).fill("AB").join(":"); return s; }; /* the stub core answers an unknown address with this same key */
  const seen = watchTitles(w, d);
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  enter(d, "10.9.9.10"); await sleep(500);
  assert.ok(seen.some((t) => /^Finding 10\.9\.9\.10$/.test(t)), "step 2 was shown first: " + seen.join(" > "));
  assert.ok(!seen.some((t) => /^Is this really /.test(t)), "nothing to decide, so no Trust screen: " + seen.join(" > "));
  assert.match(title(d), /^Allow this computer on 10\.9\.9\.10$/);
  assert.equal(steps(d).current, 2, "being let in is still step 2");
  assert.equal(d.querySelectorAll(".scrim").length, 1, "one dialog");
  assert.equal(E.server("10.9.9.10:8765").state, "login");
  assert.deepEqual(errs, []);
});

test("Back on step 2 returns to step 1 with what was typed, and drops the half-added computer", async () => {
  const { d, E, errs } = await open();
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  enter(d, "10.9.9.9", "9001"); await sleep(500);
  assert.equal(steps(d).current, 2);
  button(d, "Back").click(); await sleep(200);
  assert.equal(steps(d).current, 1, "back on step 1");
  assert.equal(d.querySelector("#v5addr").value, "10.9.9.9:9001", "the typed address is back in the fold");
  assert.equal(E.server("10.9.9.9:9001"), undefined, "the unfinished computer is not left in the list");
  assert.equal(d.querySelectorAll(".scrim").length, 1);
  assert.deepEqual(errs, []);
});

test("technical details start closed; the words for them stay out of the plain text", async () => {
  const { d, errs } = await open();
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  assert.doesNotMatch(plainText(d), /\bagent\b|SHA-?256|certificate|fingerprint/i, "step 1 plain text");
  enter(d, "10.9.9.9", "8765"); await sleep(500);
  const det = dialog(d).querySelector("details.b-tech");
  assert.ok(det, "the trust step has a Technical details fold");
  assert.equal(det.open, false, "closed to start with");
  assert.equal(det.querySelector("summary").textContent.trim(), "Technical details");
  assert.match(det.textContent, /10\.9\.9\.9/, "the address is inside it");
  /* the computer is called by the address that was typed, so its name (in the heading and sentences) is not a technical leak */
  assert.doesNotMatch(plainText(d).split("10.9.9.9").join("it"), TECH, "address, port, SHA-256, certificate and agent stay inside the fold");
  assert.deepEqual([...dialog(d).querySelectorAll(".df button")].map((b) => b.textContent.trim()), ["Back", "Cancel", "Trust"], "Back, Cancel and the trust button");
  assert.deepEqual(errs, []);
});

test("buttons are one short word and the screens say computer, not server or agent", async () => {
  const { d, errs } = await open();
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  for (const screen of ["find", "check"]) {
    if (screen === "check") { enter(d, "10.9.9.9"); await sleep(500); }
    for (const b of dialog(d).querySelectorAll(".df button")) assert.ok(b.textContent.trim().split(/\s+/).length <= 2, "button text too long: " + b.textContent.trim());
    assert.doesNotMatch(plainText(d), /\bservers?\b|\bagents?\b/i, "the " + screen + " screen uses plain words");
  }
  assert.deepEqual(errs, []);
});

test("a pairing code goes from step 2 to step 3 in the same dialog and falls back to sign-in with the error", async () => {
  const { w, d, E, errs } = await open();
  const addServer = E.addServer;
  E.addServer = (o) => { const s = addServer(o); s.pinned = true; s.fp = Array(32).fill("AB").join(":"); return s; };
  const seen = watchTitles(w, d);
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  /* The v5 first step has no code tab; "Nobody at that PC?" under its list (not hidden in the closed fold) offers "Use a code", which opens the code form in the same box. */
  assert.equal(dialog(d).querySelector("details.b-more [data-code]"), null, "the code button is not inside the closed fold");
  assert.match(dialog(d).querySelector(".b-cd").textContent, /^Nobody at that PC\?/, "the first step says what to do when nobody is at the PC");
  const use = dialog(d).querySelector("[data-code]");
  assert.equal(use.textContent.trim(), "Use a code");
  use.click(); await sleep(100);
  assert.equal(d.querySelectorAll(".scrim").length, 1, "the code form takes over the same dialog");
  assert.equal(title(d), "Add with a pairing code");
  assert.equal(steps(d).current, 1, "the code form is still step 1");
  d.querySelector("#pcn").value = "code-pc"; d.querySelector("#pch").value = "10.9.9.11:8765"; d.querySelector("#pcc").value = "ABCD2345";
  button(d, "Next").click(); await sleep(500);
  assert.ok(seen.includes("Finding code-pc"), "step 2 was shown: " + seen.join(" > "));
  assert.ok(seen.includes("Pairing with code-pc…"),"step 3 shows the pairing at work: " + seen.join(" > "));
  assert.ok(!seen.some((t) => /^Is this really /.test(t)), "no Trust screen for a key that is already trusted");
  assert.match(title(d), /^Allow this computer on code-pc$/, "the stub core refuses the code, so the sign-in screen shows why");
  assert.equal(steps(d).current, 2);
  assert.match(dialog(d).textContent, /code is not valid/i);
  assert.equal(d.querySelectorAll(".scrim").length, 1, "one dialog throughout");
  assert.deepEqual(errs, []);
});

test("the pairing-code form has Back, which returns to the list of PCs in the same dialog", async () => {
  const { d, errs } = await open();
  d.querySelector("#rail [data-new]").click(); await sleep(150);
  dialog(d).querySelector("[data-code]").click(); await sleep(100);
  assert.equal(title(d), "Add with a pairing code");
  assert.ok(!dialog(d).querySelector(".fxtabs"), "the old tab bar is gone from the code form");
  assert.deepEqual([...dialog(d).querySelectorAll(".df button")].map((b) => b.textContent.trim()), ["Back", "Cancel", "Next"]);
  button(d, "Back").click(); await sleep(150);
  assert.equal(title(d), "Which PC do you want to reach?");
  assert.ok(d.querySelector("#v5addr"), "step 1 is back");
  assert.equal(d.querySelectorAll(".scrim").length, 1);
  assert.deepEqual(errs, []);
});
