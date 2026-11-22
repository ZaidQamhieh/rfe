// Settings > Security > Account server (rfe-7np phase 3): a person can see the account-server status of a computer, trust another
// computer's account server after comparing its key fingerprint, stop trusting it, and turn this computer's own account-server
// role on or off. Every sentence it shows has a German, Spanish and Arabic row, and a refusal (403) is said in plain words.
//
//   node --test desktop/ui-tests/account-server.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

const FP = "ab".repeat(32);
const OTHER_FP = "0f".repeat(32);
const OWNER_PW = "owner-pw-for-the-test";
const FORBIDDEN = "This sign-in cannot change the account server. Sign in on this computer with its own account password, not a pairing code or an account server.";

/** The window on the stub core, opened on Settings; the five account_server_* commands are answered by a fake agent kept in `fake`. */
async function open(over = {}) {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  const dom = await JSDOM.fromFile(page, {
    runScripts: "dangerously", resources: "usable", pretendToBeVisual: true, virtualConsole: vc,
    url: "file://" + page + "?fast=1&view=settings",
    beforeParse(w) {
      const mem = new Map();
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (mem.has(k) ? mem.get(k) : null), setItem: (k, v) => mem.set(k, String(v)), removeItem: (k) => mem.delete(k), get length() { return mem.size; }, key: (i) => [...mem.keys()][i] ?? null }, configurable: true });
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
  await sleep(200);
  const A = w.A, E = w.A.E;
  const fake = { enabled: false, name: "atlas-nas", keyFingerprint: null, trusted: null, offered: null, calls: [], refuse: null, keyError: null,
    remote: { hostPort: "10.0.0.9:8765", name: "Office PC", publicKey: "Zm9v", keyFingerprint: FP }, ...over };
  const orig = E.call.bind(E);
  E.call = (name, args) => {
    if (!name.startsWith("account_server_")) return orig(name, args);
    fake.calls.push([name, args]);
    const status = () => ({ enabled: fake.enabled, name: fake.name, keyFingerprint: fake.enabled ? OTHER_FP : null, trustedServer: fake.trusted, offered: fake.offered });
    if (name === "account_server_status") return Promise.resolve(status());
    if (name === "account_server_key") return fake.keyError ? Promise.reject(new Error(fake.keyError)) : Promise.resolve({ ...fake.remote, hostPort: args.hostPort });
    if (fake.refuse) return Promise.reject(new Error(fake.refuse));
    if (name === "account_server_enable") { fake.enabled = args.enabled; return Promise.resolve(status()); }
    if (name === "account_server_trust") { fake.trusted = { name: args.name, publicKey: args.publicKey, keyFingerprint: args.keyFingerprint }; return Promise.resolve(status()); }
    if (name === "account_server_untrust") { fake.trusted = null; return Promise.resolve(status()); }
    return Promise.reject(new Error("unknown command " + name));
  };
  return { w, d: w.document, A, E, errs, fake };
}
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();
const until = async (fn, what) => { for (let i = 0; i < 100; i++) { if (fn()) return; await sleep(30); } assert.fail("timed out waiting for " + what); };
const dlg = (d) => d.querySelector(".scrim .dlg, .k-dlg, .scrim [role=dialog]");
const openDialog = async (o) => {
  o.d.querySelector('[data-snav="Security"]').click(); await sleep(250);
  const btn = o.d.querySelector('[data-sx="acctserver"]');
  assert.ok(btn, "Security has an Account server button");
  btn.click();
  await until(() => dlg(o.d) && dlg(o.d).querySelector("[data-acs]"), "the account server dialog");
  await sleep(50);
  return dlg(o.d);
};
const click = (el) => el.dispatchEvent(new el.ownerDocument.defaultView.MouseEvent("click", { bubbles: true, cancelable: true }));
/** The jsdom window builds its own objects; compare them as plain data. */
const plain = (v) => JSON.parse(JSON.stringify(v));
/** The confirm button of the password prompt (the topmost dialog). */
const promptGo = (d) => [...[...d.querySelectorAll(".scrim")].pop().querySelectorAll("button")].find((b) => b.dataset.id === "go");
const calls = (o, name) => o.fake.calls.filter((c) => c[0] === name);

test("A. Security has an Account server row whose dialog shows this computer's status and the trusted server", async () => {
  const o = await open({ enabled: true, trusted: { name: "Home PC", publicKey: "Zm9v", keyFingerprint: OTHER_FP } });
  o.d.querySelector('[data-snav="Security"]').click(); await sleep(250);
  const rows = [...o.d.querySelectorAll(".k-set")].filter((r) => text(r).startsWith("Account server"));
  assert.equal(rows.length, 1, "one Account server row in Security");
  const el = await openDialog(o);
  assert.equal(calls(o, "account_server_status").length >= 1, true, "status is read from the agent");
  assert.equal(calls(o, "account_server_status")[0][1].host, o.A.pairServer() ? o.A.pairServer().id : o.E.servers[0].id);
  const t = text(el);
  assert.match(t, /Account server on this computer/);
  assert.match(t, /On/);
  assert.ok(t.includes(OTHER_FP), "its own key fingerprint is shown");
  assert.match(t, /Home PC/);
  assert.ok(t.includes(OTHER_FP), "the trusted server's fingerprint is shown");
  assert.ok(el.querySelector('[data-acs="untrust"]'), "Stop trusting is offered while one is trusted");
  assert.equal(o.errs.length, 0, o.errs.join("\n"));
});

test("B. trusting: look up the key by host:port, see its fingerprint, tick that it matches, then pin it", async () => {
  const o = await open();
  const el = await openDialog(o);
  assert.equal(el.querySelector('[data-acs="trust"]'), null, "nothing to trust before a key is looked up");
  const addr = el.querySelector("#acs-addr");
  addr.value = " 10.0.0.9:8765 "; addr.dispatchEvent(new o.w.Event("input", { bubbles: true }));
  click(el.querySelector('[data-acs="lookup"]'));
  await until(() => el.querySelector('[data-acs="trust"]'), "the key to be shown");
  assert.deepEqual(plain(calls(o, "account_server_key")[0][1]), { hostPort: "10.0.0.9:8765" }, "the typed address, trimmed, is what is fetched");
  assert.ok(text(el).includes(FP), "the fingerprint is shown for the owner to compare");
  assert.match(text(el), /Office PC/);
  const trust = el.querySelector('[data-acs="trust"]');
  assert.equal(trust.disabled, true, "trusting waits for the owner's confirmation");
  click(trust); await sleep(80);
  assert.equal(calls(o, "account_server_trust").length, 0, "a disabled button pins nothing");
  const box = el.querySelector("#acs-ok"); box.checked = true; box.dispatchEvent(new o.w.Event("change", { bubbles: true }));
  assert.equal(el.querySelector('[data-acs="trust"]').disabled, false);
  click(el.querySelector('[data-acs="trust"]'));
  await until(() => o.d.querySelector("#acs-pw"), "the password prompt");
  assert.equal(calls(o, "account_server_trust").length, 0, "nothing is pinned before the password is given");
  o.d.querySelector("#acs-pw").value = OWNER_PW;
  click(promptGo(o.d));
  await until(() => calls(o, "account_server_trust").length === 1, "the trust call");
  const a = calls(o, "account_server_trust")[0][1];
  assert.equal(a.password, OWNER_PW, "the password the owner typed travels with the pin");
  assert.equal(a.name, "Office PC"); assert.equal(a.publicKey, "Zm9v"); assert.equal(a.keyFingerprint, FP, "the fingerprint the owner confirmed travels with the key");
  await until(() => text(el).includes("Stop trusting"), "the trusted server to show");
  assert.ok(o.fake.trusted && o.fake.trusted.keyFingerprint === FP);
  assert.equal(el.querySelector('[data-acs="trust"]'), null, "the confirmed key is cleared after pinning");
  assert.equal(o.errs.length, 0, o.errs.join("\n"));
});

test("B2. choosing a computer you already know fills the address; a failed look-up says why and trusts nothing", async () => {
  const o = await open({ keyError: "That computer's account server is turned off. Turn it on there first." });
  const el = await openDialog(o);
  const pick = el.querySelector("#acs-pick");
  const other = o.E.servers.find((s) => s.addr && s.id !== (o.A.pairServer() || {}).id);
  assert.ok(pick && other, "a list of the other computers");
  pick.value = other.id; pick.dispatchEvent(new o.w.Event("change", { bubbles: true }));
  assert.equal(el.querySelector("#acs-addr").value, other.addr);
  click(el.querySelector('[data-acs="lookup"]'));
  await until(() => /turned off/.test(text(el)), "the look-up failure");
  assert.equal(el.querySelector('[data-acs="trust"]'), null);
  assert.equal(calls(o, "account_server_trust").length, 0);
  const bad = el.querySelector("#acs-addr"); bad.value = "   "; click(el.querySelector('[data-acs="lookup"]')); await sleep(60);
  assert.equal(calls(o, "account_server_key").length, 1, "an empty address is not sent");
  assert.match(text(el), /Enter the address/);
});

test("C. Stop trusting asks first, then sends the delete", async () => {
  const o = await open({ trusted: { name: "Home PC", publicKey: "Zm9v", keyFingerprint: OTHER_FP } });
  const el = await openDialog(o);
  click(el.querySelector('[data-acs="untrust"]')); await sleep(100);
  assert.equal(calls(o, "account_server_untrust").length, 0, "asked first");
  const ask = [...o.d.querySelectorAll(".scrim")].pop();
  assert.match(text(ask), /signed out of this computer/);
  click([...ask.querySelectorAll("button")].find((b) => text(b) === "Stop trusting"));
  await until(() => calls(o, "account_server_untrust").length === 1, "the untrust call");
  await until(() => !el.querySelector('[data-acs="untrust"]'), "the dialog to show nothing trusted");
  assert.equal(o.fake.trusted, null);
});

test("D. the toggle turns this computer's account-server role on and off", async () => {
  const o = await open();
  const el = await openDialog(o);
  assert.match(text(el), /Off/);
  click(el.querySelector('[data-acs="toggle"]'));
  await until(() => calls(o, "account_server_enable").length === 1, "enable call");
  assert.deepEqual(plain(calls(o, "account_server_enable")[0][1]), { host: calls(o, "account_server_status")[0][1].host, enabled: true });
  await until(() => text(el.querySelector('[data-acs="toggle"]')) === "Turn off", "the button to flip");
  assert.ok(text(el).includes(OTHER_FP), "its key fingerprint appears once it is on");
  click(el.querySelector('[data-acs="toggle"]'));
  await until(() => calls(o, "account_server_enable").length === 2, "disable call");
  assert.equal(calls(o, "account_server_enable")[1][1].enabled, false);
});

test("E. a session that came from an account server is refused in plain words, and nothing changes", async () => {
  const o = await open({ refuse: FORBIDDEN });
  const el = await openDialog(o);
  click(el.querySelector('[data-acs="toggle"]'));
  await until(() => text(el).includes(FORBIDDEN), "the refusal to be shown in the dialog");
  assert.equal(o.fake.enabled, false);
  assert.ok(el.querySelector('[role="alert"], .v5err'), "shown as an error");
});

/** Looks a key up and ticks the box, so the next click on Trust opens the password prompt. */
async function readyToTrust(o) {
  const el = await openDialog(o);
  el.querySelector("#acs-addr").value = "10.0.0.9:8765";
  click(el.querySelector('[data-acs="lookup"]'));
  await until(() => el.querySelector('[data-acs="trust"]'), "the key to be shown");
  const box = el.querySelector("#acs-ok"); box.checked = true; box.dispatchEvent(new o.w.Event("change", { bubbles: true }));
  return el;
}

test("G. trusting asks for the owner password first: cancel or an empty box sends nothing, the typed one is sent once and cleared", async () => {
  const o = await open();
  const el = await readyToTrust(o);
  click(el.querySelector('[data-acs="trust"]'));
  await until(() => o.d.querySelector("#acs-pw"), "the password prompt");
  const field = o.d.querySelector("#acs-pw");
  assert.equal(field.type, "password", "typed characters are hidden");
  assert.match(text([...o.d.querySelectorAll(".scrim")].pop()), /password/i);
  click(promptGo(o.d)); await sleep(80);
  assert.equal(calls(o, "account_server_trust").length, 0, "an empty password is not sent");
  assert.ok(o.d.querySelector("#acs-pw"), "the prompt stays open to try again");
  assert.match(text([...o.d.querySelectorAll(".scrim")].pop()), /Enter your password/);
  const cancel = [...[...o.d.querySelectorAll(".scrim")].pop().querySelectorAll("button")].find((b) => text(b) === "Cancel");
  click(cancel); await sleep(100);
  assert.equal(o.d.querySelector("#acs-pw"), null, "Cancel closes the prompt");
  assert.equal(calls(o, "account_server_trust").length, 0, "cancelling pins nothing");
  click(el.querySelector('[data-acs="trust"]'));
  await until(() => o.d.querySelector("#acs-pw"), "the prompt again");
  assert.equal(o.d.querySelector("#acs-pw").value, "", "the earlier password is not kept");
  o.d.querySelector("#acs-pw").value = OWNER_PW;
  click(promptGo(o.d));
  await until(() => calls(o, "account_server_trust").length === 1, "the trust call");
  assert.equal(calls(o, "account_server_trust")[0][1].password, OWNER_PW);
  await until(() => o.d.querySelector("#acs-pw") === null, "the prompt to close");
  assert.ok(!o.d.body.innerHTML.includes(OWNER_PW), "the password is not left anywhere on the page");
  assert.ok(!JSON.stringify(o.fake.calls.filter((c) => c[0] !== "account_server_trust")).includes(OWNER_PW), "no other call carries it");
  assert.equal(o.errs.length, 0, o.errs.join("\n"));
});

test("H. a missing, wrong or rate-limited password is shown in plain words and pins nothing; disabling and unpinning ask for no password", async () => {
  const PASSWORD_NEEDED = "Trusting an account server needs this computer's account password. Enter the password of the account you are signed in with.";
  const WRONG = "That is not the password of the account this app is signed in with, so nothing was trusted. Check it and try again.";
  const MANY = "Too many wrong passwords for this account in a short time. Wait a few minutes, then try again.";
  for (const refuse of [PASSWORD_NEEDED, WRONG, MANY]) {
    const o = await open({ refuse });
    const el = await readyToTrust(o);
    click(el.querySelector('[data-acs="trust"]'));
    await until(() => o.d.querySelector("#acs-pw"), "the password prompt");
    o.d.querySelector("#acs-pw").value = "typed";
    click(promptGo(o.d));
    await until(() => text(el).includes(refuse), "the refusal to be shown: " + refuse);
    assert.equal(o.fake.trusted, null);
    assert.ok(el.querySelector('[role="alert"], .v5err'), "shown as an error");
    assert.ok(!o.d.body.innerHTML.includes("typed"), "the password does not stay on the page");
  }
  const o = await open({ trusted: { name: "Home PC", publicKey: "Zm9v", keyFingerprint: OTHER_FP } });
  const el = await openDialog(o);
  click(el.querySelector('[data-acs="toggle"]'));
  await until(() => calls(o, "account_server_enable").length === 1, "enable call");
  await until(() => !el.querySelector('[data-acs="untrust"]').disabled, "the dialog to settle");
  assert.ok(!("password" in calls(o, "account_server_enable")[0][1]), "turning the role on asks for no password");
  click(el.querySelector('[data-acs="untrust"]')); await sleep(100);
  click([...[...o.d.querySelectorAll(".scrim")].pop().querySelectorAll("button")].find((b) => text(b) === "Stop trusting"));
  await until(() => calls(o, "account_server_untrust").length === 1, "the untrust call");
  assert.ok(!("password" in calls(o, "account_server_untrust")[0][1]), "stopping trust asks for no password");
});

test("F. every sentence of the dialog has a German, Spanish and Arabic row", async () => {
  const o = await open({ enabled: true, trusted: { name: "Home PC", publicKey: "Zm9v", keyFingerprint: OTHER_FP } });
  const el = await openDialog(o);
  const addr = el.querySelector("#acs-addr"); addr.value = "10.0.0.9:8765"; click(el.querySelector('[data-acs="lookup"]'));
  await until(() => el.querySelector('[data-acs="trust"]'), "key");
  const seen = new Set();
  const walker = o.d.createTreeWalker(el, 4);
  for (let n; (n = walker.nextNode());) { const p = n.parentElement; if (p.closest(".mono,[data-nolocal],script,style,option")) continue; const t = n.nodeValue.replace(/\s+/g, " ").trim(); if (/[A-Za-z]{3}/.test(t)) seen.add(t); }
  for (const e of el.querySelectorAll("[aria-label],[placeholder],[data-tip]")) for (const a of ["aria-label", "placeholder", "data-tip"]) { const t = e.getAttribute(a); if (t && /[A-Za-z]{3}/.test(t)) seen.add(t); }
  for (const t of ["Account server", "Manage…", "Stop trusting this account server?", "Account server turned on", "Account server turned off", "Now trusting that account server", "No longer trusting an account server", FORBIDDEN,
    "Trusting an account server lets its accounts sign in here with full owner access. Enter the password of the account you are signed in with to confirm.",
    "Account password", "The password goes only to this computer and is not saved.", "Enter your password",
    "Trusting an account server needs this computer's account password. Enter the password of the account you are signed in with.",
    "That is not the password of the account this app is signed in with, so nothing was trusted. Check it and try again.",
    "Too many wrong passwords for this account in a short time. Wait a few minutes, then try again."]) seen.add(t);
  const SAME_IN_DE_ES = new Set(["Name"]);
  const gaps = [];
  for (const t of seen) for (const lang of ["de", "es", "ar"]) { o.A.S.lang = lang; if (o.A.tr(t) === t && !(lang !== "ar" && SAME_IN_DE_ES.has(t))) gaps.push(lang + ": " + t); }
  o.A.S.lang = "en";
  assert.ok(seen.size > 12, "the dialog really has text (" + seen.size + ")");
  assert.deepEqual(gaps, []);
});
