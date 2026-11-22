// Break tests for the RFE desktop v5 mockup (desktop/mockups/v5): pages, dialogs, the search menu and the
// empty (first-run) mode. Run: node --test desktop/ui-tests/mockup-v5-break.test.mjs
// Written by qa-break; not run by it.
//
// Pages load in jsdom with their own scripts. jsdom does no layout, so "visible" means what R9 defines:
// text inside a closed <details> is hidden, and the search panel counts only while it has class "open".
// Each test's docstring gives its label, requirement ID and confidence.

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole, ResourceLoader } from "jsdom";
import { fileURLToPath, pathToFileURL } from "node:url";

const PAGE = fileURLToPath(new URL("../mockups/v5/01-rfe.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => {
  for (const w of live.splice(0)) {
    try { w.close(); } catch { /* already closed */ }
  }
});

/** Remote files (the Google Fonts stylesheet) get an empty body; the page's own scripts and CSS load. */
class LocalOnly extends ResourceLoader {
  fetch(url, options) {
    if (!/^file:/i.test(url)) return Promise.resolve(Buffer.from(""));
    return super.fetch(url, options);
  }
}

/** Opens the mockup. query: "v=full", "v=empty" or "" (the viewer). hash: e.g. "#go=files", "#sr", "#dlg=newconn&tab=2". */
async function open(query, hash = "") {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + String((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 2).join(" | ")));
  vc.on("error", (...a) => errs.push("console.error: " + a.join(" ").slice(0, 200)));
  const url = pathToFileURL(PAGE).href + (query ? "?" + query : "") + hash;
  const dom = await JSDOM.fromFile(PAGE, {
    url,
    runScripts: "dangerously",
    resources: new LocalOnly(),
    pretendToBeVisual: true,
    virtualConsole: vc,
    beforeParse(w) {
      w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} });
      w.HTMLElement.prototype.scrollIntoView = function () {};
      w.navigator.clipboard = { writeText: () => Promise.resolve() };
    },
  });
  const w = dom.window;
  live.push(w);
  await Promise.race([
    new Promise((resolve) => (w.document.readyState === "complete" ? resolve() : w.addEventListener("load", () => resolve()))),
    sleep(4000),
  ]);
  await sleep(40);
  return { w, d: w.document, K: w.K, errs };
}

const text = (el) => (el ? el.textContent.replace(/\s+/g, " ").trim() : "");
const need = (el, what) => {
  assert.ok(el, "no element: " + what);
  return el;
};
const clickIt = (el, what) => need(el, what).click();
const press = (w, target, key, extra = {}) =>
  target.dispatchEvent(new w.KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...extra }));
const isOpen = (d) => !!d.querySelector(".b-sr.open");

/** Text a person sees by default. A closed <details> shows only its summary; a closed search panel shows nothing;
 *  input values, placeholders and select options count as visible text. */
function visibleText(node) {
  const parts = [];
  const walk = (el) => {
    for (const ch of el.childNodes) {
      if (ch.nodeType === 3) { parts.push(ch.nodeValue); continue; }
      if (ch.nodeType !== 1) continue;
      const tag = ch.tagName;
      if (tag === "SCRIPT" || tag === "STYLE") continue;
      if (ch.classList.contains("b-sr") && !ch.classList.contains("open")) continue;
      if (tag === "DETAILS" && !ch.hasAttribute("open")) {
        for (const c of ch.children) if (c.tagName === "SUMMARY") walk(c);
        continue;
      }
      if (tag === "INPUT" || tag === "TEXTAREA") { parts.push(ch.value || "", ch.getAttribute("placeholder") || ""); continue; }
      if (tag === "OPTION") { parts.push(ch.textContent); continue; }
      walk(ch);
    }
  };
  walk(node);
  return parts.join(" ").replace(/\s+/g, " ");
}

/** The wording R9 names, plus the address, port, latency and MAC forms that R9 describes. */
const JARGON = [
  ["fingerprint", /fingerprint/i],
  ["certificate", /certificate/i],
  ["TLS", /\bTLS\b/],
  ["SHA-256", /SHA-?256/i],
  ["agent", /\bagent\b/i],
  ["rfe status", /rfe status/i],
  ["IP address", /\b\d{1,3}(?:\.\d{1,3}){3}\b/],
  ["port", /:\d{4,5}\b/],
  ["latency in ms", /\b\d+(?:\.\d+)?\s?ms\b/],
  ["MAC address", /\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b/i],
  ["key fingerprint groups", /\b(?=[0-9A-F]*[A-F])[0-9A-F]{4} [0-9A-F]{4}\b/],
];
const jargonIn = (s) => JARGON.flatMap(([label, re]) => { const m = re.exec(s); return m ? [`${label}: "${m[0]}"`] : []; });

/* ---------------------------------------------------------------------------------------------------------------- */

/**
 * R4: Transfers is a list with an inspector; clicking a row selects it and the inspector shows that row's item.
 * Scenario: full data, Transfers open, the row "bed-layout.pdf" clicked.
 * Expected (R4): the row is selected and the inspector title is "bed-layout.pdf".
 * Code: INSP.transfers always draws walkthrough-full.mov, whatever pick.transfers holds (base.js:106).
 * Label: code failure. Basis: R4. Confidence: high.
 */
test("R4 transfers: the inspector shows the row that was clicked", async () => {
  const { d, errs } = await open("v=full", "#go=transfers");
  clickIt(d.querySelector('[data-k="pick:transfers:bed-layout.pdf"]'), "the bed-layout.pdf row");
  assert.ok(need(d.querySelector('[data-k="pick:transfers:bed-layout.pdf"]'), "the row after the click").classList.contains("sel"), "the row is selected");
  assert.equal(text(need(d.querySelector("#rail h2"), "the inspector title")), "bed-layout.pdf");
  assert.deepEqual(errs, []);
});

/**
 * R4: the inspector shows that row's item with its own details.
 * Scenario: full data, Devices open, "Galaxy Tab S9" clicked. The device's own rights (D.devices.may) say
 * "read-only", so the expected switches are: "Only look, never change" on, "Upload" off (formulas from the data).
 * Code: the permission switches are one fixed list for every device (base.js:105), so the switches match the
 * first device, not this one.
 * Label: code failure. Basis: R4 (the inspector shows that row's item). Confidence: medium (the requirement says
 * "that row's item"; the rights are part of the item, but the words do not name them).
 */
test("R4 devices: the inspector shows the selected device's own rights", async () => {
  const { d, K, errs } = await open("v=full", "#go=devices");
  const dev = K.D.devices.find((x) => x.n === "Galaxy Tab S9");
  clickIt(d.querySelector('[data-k="pick:devices:Galaxy Tab S9"]'), "the Galaxy Tab S9 row");
  const on = (label) => {
    const row = [...d.querySelectorAll("#st .rail.cdd .k-set")].find((r) => text(r).startsWith(label));
    return need(row, "the switch " + label).querySelector(".k-sw").classList.contains("on");
  };
  const readOnly = /read-only/.test(dev.may);
  assert.equal(on("Only look, never change"), readOnly, "read-only follows the device's own rights: " + dev.may);
  assert.equal(on("Upload"), !readOnly && /upload/i.test(dev.may), "upload follows the device's own rights");
  assert.deepEqual(errs, []);
});

/**
 * R10: a changed computer ("nas") opens one dialog about that computer with two actions; "reconnect" is for that
 * computer. Scenario: full data, the nas row in the sidebar clicked, then its "Reconnect".
 * Expected (R10): the next dialog is about nas (its name from D.servers) and does not name another computer.
 * Code: the reconnect action opens the Add-a-computer step 2, which is written for garage-pi
 * (base.js:150 and base.js:157).
 * Label: code failure. Basis: R10. Confidence: high.
 */
test("R10: reconnecting a changed computer opens the dialog for that computer, not another one", async () => {
  const { d, K, errs } = await open("v=full", "");
  clickIt(d.querySelector('#side [data-k="dlg:keychanged"]'), "the nas row in the sidebar");
  const dlg = need(d.querySelector(".k-layer .k-dlg"), "the changed-computer dialog");
  const reconnect = need([...dlg.querySelectorAll(".k-da button")].find((b) => /^Reconnect$/.test(b.textContent)), "the reconnect action");
  reconnect.click();
  const nasName = K.D.servers.find((s) => s.id === "nas").name;
  const next = text(need(d.querySelector(".k-layer .k-dlg"), "the dialog after reconnect"));
  assert.match(next, new RegExp("\\b" + nasName + "\\b"), "the dialog names " + nasName);
  assert.doesNotMatch(next, /garage-pi/, "the dialog names another computer");
  assert.deepEqual(errs, []);
});

/**
 * R8: "Next and Back move between steps"; the Next that is disabled is the one in empty mode.
 * Scenario: full data, step 2 open (#dlg=newconn&tab=2). Expected: an enabled control moves on to step 3.
 * Code: step 2 has Back and a disabled "Waiting for garage-pi…" button; nothing enabled leads to step 3
 * (base.js:154). Label: code failure. Basis: R8. Confidence: medium (the design may mean step 2 to advance by
 * itself, but the mockup has no such step).
 */
test("R8: step 2 in full mode moves on to step 3 by itself (owner chose auto-advance)", async () => {
  const { d } = await open("v=full", "#dlg=newconn&tab=2");
  assert.ok(d.querySelector(".k-layer .b-auto"), "step 2 says it moves on by itself");
  await new Promise((r) => setTimeout(r, 5400));
  assert.equal(d.querySelector(".k-layer .b-sx > div.on > span").firstChild.textContent, "Ready");
});

/**
 * R8: "Next and Back move between steps". Scenario: full data, step 3 open (#dlg=newconn&tab=3). Expected: an
 * enabled Back that returns to step 2. Code: step 3 offers "Add another" (back to step 1) and "Open its files"
 * only (base.js:154). Label: code failure. Basis: R8. Confidence: medium.
 */
test("R8: step 3 offers Back to step 2", async () => {
  const { d } = await open("v=full", "#dlg=newconn&tab=3");
  const back = [...d.querySelectorAll(".k-layer .k-da button")].find((b) => b.getAttribute("data-k") === "dlg:newconn:2");
  assert.ok(back && !back.disabled, "an enabled Back to step 2");
});

/**
 * R12: every page and dialog renders in empty mode with no script error. Scenario: empty mode opened directly on
 * the Servers page with the dialog "srvset" (the viewer's "PC settings" button does this in the empty frame).
 * Code: K.S returns undefined when there are no servers, and DLG.srvset reads s.name (base.js:167); the error stops
 * the mount before the search menu's listeners are attached.
 * Label: code failure. Basis: R12. Confidence: high.
 */
test("R12 empty mode: the Servers settings dialog renders without a script error", async () => {
  const { errs } = await open("v=empty", "#go=servers&dlg=srvset");
  assert.deepEqual(errs, []);
});

/**
 * R12: every dialog renders in empty mode with no script error. Scenario: empty mode, the "status" dialog.
 * Code: DLG.status reads S('studio').ver, which is undefined with no servers (kit.js:322).
 * Label: code failure. Basis: R12. Confidence: high.
 */
test("R12 empty mode: the status dialog renders without a script error", async () => {
  const { errs } = await open("v=empty", "#dlg=status");
  assert.deepEqual(errs, []);
});

/**
 * R12: every page and dialog renders in full and empty mode with no script error. Scenario: each name in K.DLG
 * opened in a fresh layer, in both modes; errors are collected from the call and from the console.
 * Expected: no failures. Code (empty mode): DLG.status and DLG.srvset throw (kit.js:322, base.js:167).
 * Label: code failure. Basis: R12. Confidence: high.
 */
test("R12 every dialog in K.DLG renders in full and empty mode without a script error", async () => {
  const bad = [];
  for (const mode of ["v=full", "v=empty"]) {
    const { d, K, errs } = await open(mode, "#go=home");
    for (const name of Object.keys(K.DLG)) {
      d.querySelectorAll(".k-layer").forEach((l) => l.remove());
      const before = errs.length;
      try { K.openDlg(name); } catch (e) { bad.push(`${mode} ${name}: ${e.message}`); }
      if (errs.length > before) bad.push(`${mode} ${name}: ${errs.slice(before).join(" // ")}`);
    }
  }
  assert.deepEqual(bad, []);
});

/**
 * R12: every page, tool tab, settings tab and the search menu renders in both modes with no script error.
 * Scenario: each view opened, then every tool tab and every settings tab clicked, in full and empty mode.
 * Expected: no failures. Label: code failure. Basis: R12. Confidence: high (a guard;
 * it guards the views the dialog failures above could be mistaken for).
 */
test("R12 every page, tool tab, settings tab and the search menu renders in full and empty mode", async () => {
  const bad = [];
  for (const mode of ["v=full", "v=empty"]) {
    const { d, K, errs } = await open(mode, "#go=home");
    const note = (what, before) => { if (errs.length > before) bad.push(`${mode} ${what}: ${errs.slice(before).join(" // ")}`); };
    for (const v of ["home", "files", "servers", "devices", "transfers", "history", "results", "search", "tools", "settings"]) {
      const before = errs.length;
      try { K.go(v); } catch (e) { bad.push(`${mode} ${v}: ${e.message}`); }
      note(v, before);
    }
    K.go("tools"); await sleep(10);
    for (const b of [...d.querySelectorAll(".k-ttabs button")]) { const before = errs.length; b.click(); note("tool " + b.dataset.k, before); }
    K.go("settings"); await sleep(10);
    for (const b of [...d.querySelectorAll(".k-snav button")]) { const before = errs.length; b.click(); note("settings " + b.dataset.k, before); }
    K.go("files"); await sleep(10);
  }
  assert.deepEqual(bad, []);
});

/**
 * R6: Escape closes the search menu. Scenario: empty mode, the servers settings dialog error (see the R12 test
 * above) has already happened on mount; "/" opens the menu, then Escape.
 * Expected (R6): the menu closes. Code: the error stops base.js before the Escape and click-outside listeners are
 * attached (base.js:200-207 never run); kit.js only closes dialog layers on Escape.
 * Label: code failure. Basis: R6 (Escape closes it), with R12 as the cause. Confidence: high.
 */
test("R6: after a dialog error on mount in empty mode, Escape still closes the search menu", async () => {
  const { w, d } = await open("v=empty", "#go=servers&dlg=srvset");
  press(w, d.body, "/");
  assert.ok(isOpen(d), "precondition: / opens the search menu");
  press(w, d.body, "Escape");
  assert.equal(isOpen(d), false, "Escape closes the search menu");
});

/**
 * R6: a click outside the panel closes it. Scenario: as above, empty mode after the mount error; "/" opens the
 * menu; a click on the page (#st) outside the panel.
 * Expected (R6): the menu closes. Code: the click-outside listener is never attached (base.js:200).
 * Label: code failure. Basis: R6. Confidence: high.
 */
test("R6: after a dialog error on mount in empty mode, a click outside closes the search menu", async () => {
  const { w, d } = await open("v=empty", "#go=servers&dlg=srvset");
  press(w, d.body, "/");
  assert.ok(isOpen(d), "precondition: / opens the search menu");
  clickIt(d.querySelector("#st"), "the page outside the panel");
  assert.equal(isOpen(d), false, "a click outside closes the search menu");
});

/**
 * R6 (with R7's "Filters menu"): choosing an item in the search panel's Filters menu must not close the panel.
 * Scenario: full data, search menu open ("#sr"), Filters opened, its first item ("Any kind") chosen.
 * Expected (R6: only Escape and a click outside the panel close it): the panel stays open and only the menu closes.
 * Code: the menu item carries data-k="close"; kit.js closes the menu first, then the panel's own click handler sees
 * a detached target and closes the panel (base.js:200-204).
 * Label: code failure. Basis: R6. Confidence: high.
 */
test("R6: choosing an item in the Filters menu leaves the search panel open", async () => {
  const { d } = await open("v=full", "#sr");
  assert.ok(isOpen(d), "precondition: the search menu is open");
  clickIt(d.querySelector('.b-sr [data-k="menu:sfilt"]'), "the Filters button");
  clickIt(d.querySelector('.k-menu [data-k="close"]'), "the first Filters item");
  assert.ok(isOpen(d), "the panel stays open after a choice inside it");
});

/**
 * R6: Ctrl K and "/" open the search menu. Scenario: full data, History open, focus in the History Filter field,
 * Ctrl K pressed. Expected (R6): the search menu opens; R6 names no exception for a focused field.
 * Code: kit.js ignores every key in an input except Escape (kit.js:412), so Ctrl K does nothing there.
 * Label: code failure. Basis: R6. Confidence: medium (the "/" case must still type in the field: see the guard
 * test below, which expects no open).
 */
test("R6: Ctrl K opens the search menu even when the focus is in a field", async () => {
  const { w, d } = await open("v=full", "#go=history");
  const field = need(d.querySelector(".b-filt input"), "the History Filter field");
  field.focus();
  press(w, field, "k", { ctrlKey: true });
  assert.ok(isOpen(d), "Ctrl K opens the search menu");
});

/**
 * R6 guard: "/" typed into a field types the slash; it does not open the search menu. Scenario: History Filter
 * field focused and "/" pressed. Expected: no search menu. Label: code failure. Basis: R6 with the field's own
 * typing (plain input). Confidence: high (a guard against a regression of the rule in kit.js:412).
 */
test("R6 guard: a slash typed into a field does not open the search menu", async () => {
  const { w, d } = await open("v=full", "#go=history");
  const field = need(d.querySelector(".b-filt input"), "the History Filter field");
  field.focus();
  press(w, field, "/");
  assert.equal(isOpen(d), false);
});

/**
 * R7: the line naming how many computers are searched follows the switches. Scenario: full data, search menu
 * open, the work-laptop switch turned off. Expected (formula: enabled computers less one, of all computers):
 * "Searching 1 of 4 computers". Code: the line is fixed text (01-rfe.html:50); a switch only toggles a class
 * (base.js:196).
 * Label: code failure. Basis: R7. Confidence: high.
 */
test("R7: turning a computer's switch off updates the line that names how many are searched", async () => {
  const { d, K } = await open("v=full", "#sr");
  const pill = [...d.querySelectorAll('.b-sr .s-pl button[data-k="sr:pick"]')].find((b) => /work-laptop/.test(b.textContent));
  clickIt(pill, "the work-laptop switch");
  const total = K.D.servers.length;
  const on = K.D.servers.filter((s) => s.dot === "ok").length - 1;
  assert.equal(text(need(d.querySelector(".b-sr .s-sc > p"), "the scope line")), `Searching ${on} of ${total} computers`);
});

/**
 * R7: a computer switched off leaves the results. Scenario: full data, search menu open, work-laptop switched off.
 * Expected: its group is gone from the results list. Code: the results list is drawn once and never filtered by
 * the switches (01-rfe.html:50, base.js:196).
 * Label: code failure. Basis: R7 (one switch per computer, which scopes the search). Confidence: medium.
 */
test("R7: a computer switched off drops its results from the list", async () => {
  const { d } = await open("v=full", "#sr");
  const list = () => need(d.querySelector(".b-sr .s-ls"), "the results list");
  assert.match(text(list()), /work-laptop/, "precondition: work-laptop has results");
  const pill = [...d.querySelectorAll('.b-sr .s-pl button[data-k="sr:pick"]')].find((b) => /work-laptop/.test(b.textContent));
  clickIt(pill, "the work-laptop switch");
  assert.doesNotMatch(text(list()), /work-laptop/);
});

/**
 * R7: "a results page whose table columns each filter". Scenario: full data, Show all results open, the Computer
 * filter set to studio-pc. Expected (formula: results on studio-pc in D.search.res): that many rows.
 * Code: the filter controls have no change handler (01-rfe.html:57), so every row stays.
 * Label: code failure. Basis: R7. Confidence: medium (the mockup might be a static picture; R7 says the columns
 * filter, so a control that does nothing is a gap).
 */
test("R7: a column filter on the results page narrows the rows", async () => {
  const { w, d, K } = await open("v=full", "#go=results");
  const sel = [...d.querySelectorAll(".rt .hc select")].find((s) => [...s.options].some((o) => o.textContent === "studio-pc"));
  need(sel, "the Computer filter").value = "studio-pc";
  sel.dispatchEvent(new w.Event("change", { bubbles: true }));
  const expected = K.D.search.res.filter((r) => r[3] === "studio-pc").length;
  assert.equal(d.querySelectorAll(".rt .c").length / 6, expected);
});

/**
 * R7: the number on the results page agrees with the rows under it. Scenario: full data, Show all results open.
 * Expected: the number the page states ("14 for …") equals the rows it lists (six cells per row).
 * Code: the header number is typed in (01-rfe.html:57) and the table lists D.search.res, which has six rows.
 * Label: code failure. Basis: R7 (Show all opens the results of the search the panel counts; the panel says 14).
 * Confidence: medium.
 */
test("R7: the count on the Show all results page matches the rows it lists", async () => {
  const { d } = await open("v=full", "#go=results");
  const stated = Number(/^(\d+) for/.exec(text(need(d.querySelector(".k-ph .k-sub"), "the results line")))?.[1]);
  assert.ok(Number.isFinite(stated), "the results line states a count");
  assert.equal(d.querySelectorAll(".rt .c").length / 6, stated);
});

/**
 * R7: "a results page whose table columns each filter". Scenario: full data, Show all results open; every column
 * header checked for a filter control. Expected: all six columns have one. Code: the first column (the file-type
 * icon) has no control (01-rfe.html:57).
 * Label: code failure. Basis: R7. Confidence: medium (whether an icon column is a "column" is a reading).
 */
test("R7: every results column has a filter", async () => {
  const { d } = await open("v=full", "#go=results");
  const heads = [...d.querySelectorAll(".rt .hc")];
  assert.equal(heads.length, 6, "six columns");
  assert.deepEqual(heads.map((h) => !!h.querySelector("input,select")), [true, true, true, true, true, true]);
});

/**
 * R5: History's filters work, not only sit on one line. Scenario: full data, History open, the Kind filter set to
 * "Transfers" (the first option that starts with it). Expected (formula: events in B.HIST with cat Transfers): that
 * many rows. Code: the filter selects have no change handler (base.js:122-129), so every event stays.
 * Label: code failure. Basis: R5 ("History has filters"). Confidence: medium.
 */
test("R5: the Kind filter narrows the History events", async () => {
  const { w, d, K } = await open("v=full", "#go=history");
  const kind = need([...d.querySelectorAll(".b-filt select")].find((s) => s.getAttribute("aria-label") === "Kind"), "the Kind filter");
  const opt = [...kind.options].find((o) => o.textContent.startsWith("Transfers"));
  kind.value = opt.value;
  kind.dispatchEvent(new w.Event("change", { bubbles: true }));
  const expected = w.B.HIST.filter((h) => h.cat === "Transfers").length;
  assert.equal(d.querySelectorAll(".b-ev").length, expected);
});

/**
 * R9: no technical wording is visible by default. Scenario: empty mode, Files (first run). The set-up steps say
 * to compare the key with "rfe status" on the PC; that text is visible, outside any disclosure.
 * Expected (R9): no "rfe status" or key wording visible.
 * Code: base.js:82 (the Files empty state, step 4).
 * Label: code failure. Basis: R9. Confidence: high.
 */
test("R9 empty mode, Files: the set-up steps show no 'rfe status' or key wording", async () => {
  const { d } = await open("v=empty", "#go=files");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".win"), "the window"))), []);
});

/**
 * R9: the share link dialog shows no address and port. Scenario: full data, Files, the selected file's "Share
 * link…" button. Expected (R9): no IP address or port visible. Code: DLG.share prints
 * https://192.168.1.20:8765/... in a read-only field (kit.js:333), and the button is in the details panel
 * (base.js:80).
 * Label: code failure. Basis: R9. Confidence: high.
 */
test("R9 share dialog: the share address and port are not visible", async () => {
  const { d } = await open("v=full", "#go=files");
  clickIt(d.querySelector('.olside [data-k="dlg:share"]'), "Share link in the details panel");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".k-layer"), "the share dialog"))), []);
});

/**
 * R9: Tools > Share links shows no addresses. Scenario: full data, Tools, the Share links tab. Expected (R9): no
 * IP address or port visible. Code: V.tool.share prints each link's https://192.168.1.20:8765/... (kit.js:270).
 * Label: code failure. Basis: R9. Confidence: high.
 */
test("R9 Tools > Share links: the links are not shown with an address and port", async () => {
  const { d } = await open("v=full", "#go=tools");
  clickIt(d.querySelector('[data-k="tool:share"]'), "the Share links tab");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector("#st"), "the page"))), []);
});

/**
 * R9: Settings > Transfers shows no "SHA-256". Scenario: full data, Settings, the Transfers section. Expected (R9):
 * no "SHA-256" visible. Code: the "Verify checksums" row says "…the server's SHA-256" (kit.js:284), outside any
 * disclosure.
 * Label: code failure. Basis: R9 (names SHA-256). Confidence: high.
 */
test("R9 Settings > Transfers: 'SHA-256' is not visible", async () => {
  const { d } = await open("v=full", "#go=settings");
  clickIt(d.querySelector('[data-k="set:Transfers"]'), "the Transfers section");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector("#st"), "the page"))), []);
});

/**
 * R9: Settings > Security shows no "certificate". Scenario: full data, Settings, the Security section. Expected (R9):
 * no "certificate" visible. Code: the row title "Pinned certificates" (kit.js:287).
 * Label: code failure. Basis: R9 (names certificate). Confidence: high.
 */
test("R9 Settings > Security: 'Pinned certificates' is not visible", async () => {
  const { d } = await open("v=full", "#go=settings");
  clickIt(d.querySelector('[data-k="set:Security"]'), "the Security section");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector("#st"), "the page"))), []);
});

/**
 * R9: Settings > Troubleshooting shows no "agent" wording. Scenario: full data, Settings, the Troubleshooting
 * section. Expected (R9): no "agent" visible. Code: the row "Agent log" (kit.js:290).
 * Label: code failure. Basis: R9 (names agent). Confidence: high.
 */
test("R9 Settings > Troubleshooting: the 'Agent log' wording is not visible", async () => {
  const { d } = await open("v=full", "#go=settings");
  clickIt(d.querySelector('[data-k="set:Troubleshooting"]'), "the Troubleshooting section");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector("#st"), "the page"))), []);
});

/**
 * R9: the Status dialog (Files > studio-pc > More > Status) shows no address, port, MAC or agent wording.
 * Scenario: full data, Files, the studio-pc row's More menu, Status. Expected (R9): none visible.
 * Code: DLG.status prints the LAN and Tailscale addresses with ports, a MAC address and "Agent" (kit.js:322), all
 * outside any disclosure; the menu entry is MENU.server (kit.js:359).
 * Label: code failure. Basis: R9. Confidence: high.
 */
test("R9 Status dialog (Files > studio-pc > More > Status): no address, port, MAC or agent wording", async () => {
  const { d } = await open("v=full", "#go=files");
  clickIt(d.querySelector('[data-k="menu:server"]'), "More on the studio-pc row");
  clickIt(d.querySelector('.k-menu [data-k="dlg:status"]'), "Status in the menu");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".k-layer"), "the status dialog"))), []);
});

/**
 * R9: the Pinned certificates dialog shows no address, port or certificate wording. Scenario: full data, Settings,
 * Security, "Show…" on Pinned certificates. Expected (R9): none visible.
 * Code: DLG.pins prints each server's address with its port and the key tag (kit.js:349).
 * Label: code failure. Basis: R9. Confidence: high.
 */
test("R9 Pinned certificates dialog: no address, port or certificate wording", async () => {
  const { d } = await open("v=full", "#go=settings");
  clickIt(d.querySelector('[data-k="set:Security"]'), "the Security section");
  clickIt(d.querySelector('[data-k="dlg:pins"]'), "Show on Pinned certificates");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".k-layer"), "the pinned dialog"))), []);
});

/**
 * R9: the update dialog shows no "SHA-256". Scenario: full data, Settings, About, "Check now". Expected (R9): none.
 * Code: DLG.updates says the package is kept only if its SHA-256 matches (kit.js:351), outside any disclosure.
 * Label: code failure. Basis: R9 (names SHA-256). Confidence: high.
 */
test("R9 Check for updates dialog: no SHA-256 wording", async () => {
  const { d } = await open("v=full", "#go=settings");
  clickIt(d.querySelector('[data-k="set:About"]'), "the About section");
  clickIt(d.querySelector('[data-k="dlg:updates"]'), "Check now");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".k-layer"), "the updates dialog"))), []);
});

/**
 * R9: the welcome dialog shows no certificate wording. Scenario: full data, Settings, About, "Show the welcome
 * screen". Expected (R9): none. Code: DLG.welcome says "trusted once, by its certificate key" (kit.js:352).
 * Label: code failure. Basis: R9 (names certificate). Confidence: high.
 */
test("R9 Welcome dialog: no certificate wording", async () => {
  const { d } = await open("v=full", "#go=settings");
  clickIt(d.querySelector('[data-k="set:About"]'), "the About section");
  clickIt(d.querySelector('[data-k="dlg:welcome"]'), "Show the welcome screen");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".k-layer"), "the welcome dialog"))), []);
});

/**
 * R9: the launch dialog shows no agent wording. Scenario: full data, Tools, Apps, GIMP's card. Expected (R9): none.
 * Code: DLG.launch says "recorded in the agent's security log" (kit.js:346).
 * Label: code failure. Basis: R9 (names agent). Confidence: high.
 */
test("R9 Launch dialog (Tools > Apps): no agent wording", async () => {
  const { d } = await open("v=full", "#go=tools");
  clickIt(d.querySelector('[data-k="tool:apps"]'), "the Apps tab");
  clickIt(d.querySelector('[data-k="dlg:launch"]'), "the first launchable app");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".k-layer"), "the launch dialog"))), []);
});

/**
 * R9 (technical wording in general): the Back up dialog shows no cipher names. Scenario: full data, Settings,
 * Backup, "Back up…". Expected: no AES, PBKDF2 or GCM visible. Code: the hint "Encrypts the file (AES-GCM, key
 * from PBKDF2)" (kit.js:348), outside any disclosure.
 * Label: code failure. Basis: R9 ("no technical wording is visible by default anywhere"). Confidence: medium (R9's
 * named list does not include ciphers; the general sentence does).
 */
test("R9 Back up dialog: no AES or PBKDF2 wording", async () => {
  const { d } = await open("v=full", "#go=settings");
  clickIt(d.querySelector('[data-k="set:Backup"]'), "the Backup section");
  clickIt(d.querySelector('[data-k="dlg:backup"]'), "Back up…");
  assert.doesNotMatch(visibleText(need(d.querySelector(".k-layer"), "the backup dialog")), /\bAES\b|PBKDF2|\bGCM\b/);
});

/**
 * R9 (technical wording in general): the Properties dialog shows no "sha256" line. Scenario: full data, Files, the
 * first file's More menu, Properties. Expected: no SHA-256 or sha256 visible. Code: the Checksum row prints
 * "sha256 · 6c1f…" in the open panel (kit.js:334).
 * Label: code failure. Basis: R9 (names SHA-256; "sha256" is the same wording). Confidence: medium.
 */
test("R9 Properties dialog: no sha256 line", async () => {
  const { d } = await open("v=full", "#go=files");
  clickIt(d.querySelector('[data-k="menu:file"]'), "More on the first file");
  clickIt(d.querySelector('.k-menu [data-k="dlg:props"]'), "Properties in the menu");
  assert.deepEqual(jargonIn(visibleText(need(d.querySelector(".k-layer"), "the properties dialog"))), []);
});

/**
 * R9 guard: the default pages, the search menu and the add-a-computer steps show no technical wording and no
 * open details. Scenario: full data, each page opened. Expected: none. Label: code failure. Basis: R9. Confidence:
 * high (a guard for the pages that are clean now).
 */
test("R9 guard: the default pages, search menu and add-a-computer steps show no technical wording and no open details", async () => {
  const views = [
    ["home", "#go=home"], ["files", "#go=files"], ["servers", "#go=servers"], ["devices", "#go=devices"],
    ["transfers", "#go=transfers"], ["history", "#go=history"], ["search menu", "#sr"],
    ["add step 1", "#dlg=newconn&tab=1"], ["add step 2", "#dlg=newconn&tab=2"], ["add step 3", "#dlg=newconn&tab=3"],
    ["PC changed", "#dlg=keychanged"],
  ];
  const bad = [];
  for (const [name, hash] of views) {
    const { d } = await open("v=full", hash);
    const win = need(d.querySelector(".win"), "the window");
    for (const j of jargonIn(visibleText(win))) bad.push(`${name}: ${j}`);
    /* The merged Devices page opens its own "Devices on <computer>" section (details.cdv), as the shipped window does; every other disclosure stays closed. */
    const opened = d.querySelectorAll(".win details[open]:not(.cdv)").length + (name === "devices" ? 0 : d.querySelectorAll(".win details.cdv[open]").length);
    if (opened) bad.push(`${name}: ${opened} disclosure(s) open by default`);
  }
  assert.deepEqual(bad, []);
});

/**
 * R2 guard: the Files tree has no pairing-request rows. Scenario: full data, Files. Expected: none of "asks to join",
 * "wants to take over", "wants to replace", "wants to see files". Label: code failure. Basis: R2. Confidence: high
 * (a guard).
 */
test("R2 guard: the Files tree has no pairing-request rows", async () => {
  const { d } = await open("v=full", "#go=files");
  assert.doesNotMatch(text(need(d.querySelector(".ol .tr"), "the Files tree")), /asks to join|wants to take over|wants to replace|wants to see files/i);
});

/**
 * R3 guard: empty mode's Overview shows no sample data. Scenario: empty mode, Overview. Expected: no computer,
 * phone or file name from the sample data. Label: code failure. Basis: R3. Confidence: high (a guard).
 */
test("R3 guard: empty mode Overview shows no sample computers, phones or files", async () => {
  const { d } = await open("v=empty", "#go=home");
  assert.doesNotMatch(text(need(d.querySelector("#st"), "the page")), /studio-pc|work-laptop|media-mini|Pixel|north-bed|\bnas\b|walkthrough/i);
});

/**
 * R11 guard: the inspector's "Settings for <name>…" opens that computer's settings, after another row is picked.
 * Scenario: full data, Servers, media-mini picked, its settings button. Expected: the dialog is titled for
 * media-mini (formula: the name in D.servers). Label: code failure. Basis: R11. Confidence: high (a guard;
 * it guards the same stale-selection pattern as the Transfers test).
 */
test("R11 guard: the settings dialog is for the computer picked in the list", async () => {
  const { d, K } = await open("v=full", "#go=servers");
  clickIt(d.querySelector('[data-k="pick:servers:mini"]'), "the media-mini row");
  clickIt(d.querySelector('#rail [data-k="dlg:srvset"]'), "Settings for media-mini");
  const name = K.D.servers.find((s) => s.id === "mini").name;
  assert.equal(text(need(d.querySelector(".k-layer h2"), "the dialog title")), "Settings for " + name);
});

/**
 * R8 guard: in empty mode the Next of step 1 is disabled. Scenario: empty mode, step 1 open. Expected: the Next
 * button exists and is disabled. Label: code failure. Basis: R8. Confidence: high (a guard).
 */
test("R8 guard: empty mode step 1 has a disabled Next", async () => {
  const { d } = await open("v=empty", "#dlg=newconn&tab=1");
  const next = need([...d.querySelectorAll(".k-layer .k-da button")].find((b) => /Next/.test(b.textContent)), "Next");
  assert.equal(next.disabled, true);
});

/**
 * R15 frame check: the viewer shows two frames, full then empty, and its strip switches both frames together.
 * Scenario: the viewer (no ?v) opened; "Add 2" then "Overview" clicked in the strip. Expected: both frames show the
 * same state after each click, full first then empty. Label: code failure. Basis: R15. Confidence: high (expected
 * to pass; it guards the strip's shared state).
 */
test("R15 viewer: the strip switches both frames to the same state", async () => {
  const { d } = await open("", "");
  const frames = [...d.querySelectorAll(".mk-pair iframe")];
  assert.equal(frames.length, 2, "two frames");
  assert.match(frames[0].getAttribute("src"), /\?v=full/);
  assert.match(frames[1].getAttribute("src"), /\?v=empty/);
  const strip = (name) => need([...d.querySelectorAll(".mk-views button")].find((b) => text(b) === name), "the strip button " + name);
  strip("Add 2").click();
  for (const f of frames) assert.match(f.getAttribute("src"), /#dlg=newconn&tab=2$/);
  strip("Overview").click();
  assert.match(frames[0].getAttribute("src"), /\?v=full#$/);
  assert.match(frames[1].getAttribute("src"), /\?v=empty#$/);
});
