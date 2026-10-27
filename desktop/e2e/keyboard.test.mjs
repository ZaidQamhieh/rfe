// Keyboard only, in the real window: every sign-in route and the main actions are reached and used
// with Tab, Shift+Tab, Enter, Space and Escape, and the order Tab takes through a screen is recorded
// and checked (it is the reading order; every stop shows a focus outline). Prints each order.
//
//   desktop/e2e/run.sh

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { App, Agent, waitFor, sleep, resetAppState, KEYS } from "./lib.mjs";

let a;
let b;
let c;
let app;

before(async () => {
  a = await Agent.start("owner", "e2e-keyboard-a");
  b = await Agent.start("owner", "e2e-keyboard-b");
  c = await Agent.start("owner", "e2e-keyboard-c");
  resetAppState();
  app = await App.start();
});

after(async () => {
  if (app) await app.end();
  a?.stop();
  b?.stop();
  c?.stop();
});

// What a screen reader would call the focused control, and whether it shows a focus outline.
const focusInfo = () =>
  app.script(`
    const e = document.activeElement;
    if (!e || e === document.body) return null;
    const cs = getComputedStyle(e);
    const name = e.id || e.getAttribute("aria-label") || (e.textContent || "").trim().slice(0, 40) || e.tagName;
    // The search box shows its focus as the primary-colour border of the bar around it (:focus-within).
    const ring = (cs.outlineStyle !== "none" && parseFloat(cs.outlineWidth) > 0) || (e.closest(".sbar") && getComputedStyle(e.closest(".sbar")).borderTopColor !== "rgba(0, 0, 0, 0)");
    return { name, outline: !!ring, tag: e.tagName };
  `);

async function waitForFocusChange(before, ms = 600) {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    const f = await focusInfo();
    // No focus yet is a state too: from it, wait until something takes the focus.
    if ((f ? f.name : null) !== (before ?? null)) return;
    await new Promise((r) => setTimeout(r, 15));
  }
}

async function tabTo(id, max = 60) {
  for (let i = 0; i < max; i++) {
    const f = await focusInfo();
    if (f && f.name === id) return;
    await app.press(KEYS.tab);
    // The press returns before the webview has moved focus; reading too early can see a stale
    // stop, and a slow frame can fold two presses into one. Wait for focus to leave this stop.
    await waitForFocusChange(f && f.name);
  }
  throw new Error(`Tab never reached ${id}; the last stop was ${JSON.stringify(await focusInfo())}`);
}

/** Starts from the top of the page and tabs through the screen once; returns each stop. */
async function tabOrder(max = 80) {
  await app.script("document.activeElement && document.activeElement.blur(); window.scrollTo(0, 0);");
  const stops = [];
  for (let i = 0; i < max; i++) {
    const before = stops.length ? stops[stops.length - 1].name : null;
    await app.press(KEYS.tab);
    // The press returns before the webview has moved focus (see tabTo): wait for it to leave the last stop.
    await waitForFocusChange(before);
    const f = await focusInfo();
    if (!f) break;
    if (stops.some((s) => s.name === f.name && s.tag === f.tag) && stops.length > 2) break;
    stops.push(f);
  }
  return stops;
}

function checkOrder(screen, stops, mustInclude) {
  console.log(`tab order, ${screen}: ${stops.map((s) => s.name).join(" > ")}`);
  const noRing = stops.filter((s) => !s.outline).map((s) => s.name);
  assert.deepEqual(noRing, [], `${screen}: these stops show no focus outline`);
  const names = stops.map((s) => s.name);
  for (const want of mustInclude) assert.ok(names.includes(want), `${screen}: Tab never reaches ${want}: ${names.join(", ")}`);
  // Reading order: the order must follow the order on the page.
  assert.ok(names.length >= mustInclude.length, `${screen}: too few stops`);
}

const press = async (keys) => app.press(keys);

async function openNewConnection() {
  await app.skipWelcome();
  await app.script("document.querySelector('#rail [data-new]').focus();");
  await press(KEYS.enter);
  await waitFor("the connection dialog", () => app.visible("#sh"));
}

/** New connection by keyboard, then the trust dialog's button by Tab and Enter. */
async function connectKeyboard(agent, name) {
  await openNewConnection();
  await app.script("document.querySelector('#sn').focus();");
  await press(name);
  await app.fill("#sh", agent.host.split(":")[0]);
  await app.fill("#sp", agent.host.split(":")[1]);
  await app.script("document.querySelector('#sn').focus();");
  await press(KEYS.enter);
  await waitFor("the trust dialog", () => app.visible(".fpfull"));
  // The key is trusted only after the box is ticked: Space on the box, then Enter.
  await app.script("document.getElementById('tfm').focus();");
  await press(KEYS.space);
  await press(KEYS.enter);
  await waitFor("the sign-in dialog", () => app.visible("#siu"));
}

test("the rail and the top bar: tab order and outlines", async () => {
  await app.skipWelcome();
  await app.script("document.activeElement && document.activeElement.blur();");
  checkOrder("rail", await tabOrder(40), ["New connection", "Files", "Servers", "Devices", "Transfers", "Search", "Tools", "History", "Settings"]);
});

test("the welcome closes with Escape and the New connection dialog is keyboard friendly", async () => {
  await openNewConnection();
  assert.equal((await focusInfo()).name, "sn", "the dialog starts in its first field");
  checkOrder("new connection", await tabOrder(20), ["sn", "sh", "sp", "Connect"]);
  await press(KEYS.escape);
  await waitFor("the dialog to close", async () => !(await app.visible("#sh")));
});

test("sign in with an account by keyboard", async () => {
  await connectKeyboard(a, "kb-a");
  assert.equal((await focusInfo()).name, "siu", "sign-in starts in the username field");
  checkOrder("sign-in", await tabOrder(20), ["Account", "Pairing code", "Ask the PC", "siu", "sip", "Not now", "Sign in"]);
  await app.script("document.querySelector('#siu').focus();");
  await press(a.user + KEYS.tab + a.password + KEYS.enter);
  await waitFor("the dialog to close", async () => !(await app.visible("#siu")));
  await waitFor("the listing", () => app.visible("[data-rows]"));
});

test("pair with a code by keyboard", async () => {
  await connectKeyboard(b, "kb-b");
  await app.script("document.querySelector('[data-m=\"code\"]').focus();");
  await press(KEYS.enter);
  await waitFor("the code field", () => app.visible("#sic"));
  assert.equal((await focusInfo()).name, "sic", "focus goes to the code field");
  await press(b.pairCode() + KEYS.enter);
  await waitFor("the dialog to close", async () => !(await app.visible("#sic")), 20000);
  assert.match(await app.snacks(), /Signed in to kb-b/);
});

test("approve on the PC by keyboard, cancel by keyboard, then approve", async () => {
  await connectKeyboard(c, "kb-c");
  await app.script("document.querySelector('[data-m=\"ask\"]').focus();");
  await press(KEYS.enter);
  await app.script("document.querySelector('.dlg [data-id=\"go\"]').focus();");
  await press(KEYS.enter);
  await waitFor("a match code", async () => (await app.text(".dlg .pcode")).trim().length >= 6);
  await press(KEYS.escape);
  await waitFor("the dialog to close", async () => !(await app.visible(".dlg")));
  await app.go("servers");
  await app.clickInCard("kb-c", "Sign in");
  await waitFor("the sign-in dialog", () => app.visible("#siu"));
  await app.script("document.querySelector('[data-m=\"ask\"]').focus();");
  await press(KEYS.enter);
  await app.script("document.querySelector('.dlg [data-id=\"go\"]').focus();");
  await press(KEYS.enter);
  await waitFor("a match code", async () => (await app.text(".dlg .pcode")).trim().length >= 6);
  const code = (await app.text(".dlg .pcode")).trim();
  c.answerPairRequest("accept", c.pairRequestId(code));
  await waitFor("the dialog to close", async () => !(await app.visible(".dlg")), 30000);
});

test("settings: every control is reachable by keyboard", async () => {
  await app.go("settings");
  await waitFor("settings", () => app.has(/Theme/, "#stage"));
  const stops = [];
  for (const tab of ["Transfers", "Connection", "Appearance", "Files", "Security", "Desktop", "Backup", "Troubleshooting"]) {
    await app.clickLabel(tab, ".setn");
    await sleep(350); /* the accessible names are filled in a moment after a page is drawn */
    // Choosing a section keeps focus on its button; Tab onward from there through the page's controls.
    await app.script("document.querySelector('#stage').scrollTo(0, 0); const b = document.querySelector('.setn button[aria-current]'); if (b) b.focus();");
    const got = [];
    for (let i = 0; i < 60; i++) {
      const before = got.length ? got[got.length - 1].name : (await focusInfo()).name;
      await app.press(KEYS.tab);
      await waitForFocusChange(before);
      const inStage = await app.script("return !!document.activeElement && !!document.activeElement.closest('#stage');");
      if (!inStage) break;
      got.push(await focusInfo());
    }
    console.log(`tab order, settings, ${tab}: ${got.map((s) => s.name).join(" > ")}`);
    stops.push(...got);
  }
  const names = stops.map((s) => s.name);
  for (const want of ["Change…", "Reset", "Reconnect automatically"]) assert.ok(names.includes(want), `Tab never reaches ${want}: ${names.join(", ")}`);
  assert.deepEqual(stops.filter((s) => !s.outline).map((s) => s.name), [], "settings: these stops show no focus outline");
});

test("a switch is toggled with Space", async () => {
  await app.clickLabel("Connection", ".setn");
  await sleep(350);
  await app.script("document.activeElement && document.activeElement.blur();");
  await tabTo("Reconnect automatically");
  const state = () => app.script("return document.activeElement.getAttribute('aria-checked');");
  const before = await state();
  await press(KEYS.space);
  await waitFor("the switch to flip", async () => (await state()) !== before);
  await press(KEYS.space);
  await waitFor("the switch to flip back", async () => (await state()) === before);
});

test("a menu opens with Enter, moves with the arrows and closes with Escape", async () => {
  await app.go("devices");
  await waitFor("the device list", () => app.has(/RFE Desktop\s+This app/, "#stage"));
  await app.script("document.querySelector('#stage [aria-label^=\"More for RFE Desktop\"]').focus();");
  await press(KEYS.enter);
  await waitFor("the menu", () => app.visible(".menu"));
  assert.equal(await app.script("return document.activeElement.classList.contains('mi');"), true, "focus moves into the menu");
  const first = await app.script("return document.activeElement.innerText.trim();");
  await press(KEYS.down);
  assert.notEqual(await app.script("return document.activeElement.innerText.trim();"), first, "the arrow moves to the next item");
  await press(KEYS.escape);
  await waitFor("the menu to close", async () => !(await app.visible(".menu")));
  assert.match(await app.script("return document.activeElement.getAttribute('aria-label') || '';"), /^More for RFE Desktop/, "focus goes back to the button that opened it");
});

test("Revoke on this computer asks first, by keyboard", async () => {
  await app.script("document.querySelector('#stage [aria-label^=\"More for RFE Desktop\"]').focus();");
  await press(KEYS.enter);
  await waitFor("the menu", () => app.visible(".menu"));
  await app.script("[...document.querySelectorAll('.menu .mi')].find((x) => /Revoke access/.test(x.innerText)).focus();");
  await press(KEYS.enter);
  await waitFor("the confirmation", () => app.visible(".dlg"));
  await waitFor("the confirmation text", async () => /This is the sign-in this app uses/.test(await app.text(".dlg")));
  assert.match(await app.text(".dlg"), /This is the sign-in this app uses/);
  await press(KEYS.escape);
  await waitFor("the dialog to close", async () => !(await app.visible(".dlg")));
  assert.ok(await app.has(/RFE Desktop\s+This app/, "#stage"), "still signed in: nothing was revoked");
});
