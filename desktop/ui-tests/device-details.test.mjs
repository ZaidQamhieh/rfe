// The device details dialog and the device list rows:
//  A. after a save the switches show what the agent answered; after a failure they show what the agent holds now;
//  B. a device signed in with the account has fixed "Allowed" rows for the five file grants, no switches for them;
//  C. address, version and device id sit in a "Technical details" fold that starts closed (dialog and list row);
//  D. a revoked device has no folder-limit field and shows one "No access. Revoked." line; E. the folder limit saves on Enter or Done only (blur and Escape discard), a refusal keeps the dialog open;
//  F. while a save runs only the clicked switch is disabled.
// Same jsdom harness as side-panel.test.mjs.
//
//   node --test desktop/ui-tests/device-details.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
import { readAllCss } from "./css-lib.mjs";
const css = readAllCss();
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const waitFor = async (fn, ms = 4000) => { for (let t = 0; t < ms; t += 25) { if (fn()) return; await sleep(25); } };
const NAS = "192.168.1.20:7443";
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
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}
const text = (el) => el.textContent.replace(/\s+/g, " ").trim();

const FILE = ["download", "upload", "modify", "delete", "share"];
const ALL = { download: true, upload: true, modify: true, delete: true, share: true, viewApps: false, launchApps: false, readOnly: false };

/** Opens the details of a fake device. `core.set` answers set_device_access (default: echo the patch merged into `grants`); `core.all` answers device_access_all. */
async function details(grants, over = {}, core = {}) {
  const ctx = await open(); const { w, A, E } = ctx;
  const srv = E.servers.find((s) => s.id === NAS) || E.servers[0];
  const calls = []; const orig = E.call.bind(E);
  const dev = { id: "dev-1", name: "Pixel 9", last: E.now(), paired: E.now() - 864e5, addr: "192.168.1.57", ver: "1.4.0", a: { ...grants }, ...over };
  E.call = (name, args) => {
    calls.push([name, JSON.parse(JSON.stringify(args || {}))]);
    if (name === "set_device_access") return core.set ? core.set(args) : Promise.resolve({ id: "dev-1", ...grants, ...args.patch });
    if (name === "device_access_all") return core.all ? core.all() : Promise.resolve([{ id: "dev-1", ...grants }]);
    if (name === "list_devices") return Promise.resolve([]);
    return orig(name, args);
  };
  A.pairServer = () => srv;
  A.X.devices = [dev]; A.X.devAt = E.now();
  A.deviceDetails("dev-1"); await sleep(60);
  ctx.calls = calls; ctx.dev = dev; ctx.dlg = () => w.document.querySelector(".dlg");
  ctx.sets = () => calls.filter((c) => c[0] === "set_device_access");
  return ctx;
}
const sw = (ctx, k) => ctx.dlg().querySelector('[data-c="' + k + '"]');
const on = (ctx, k) => sw(ctx, k).getAttribute("aria-checked");

test("A. after a save the switches show what the agent answered, not the local guess", async () => {
  // the agent turns Download off too while the person turned Upload on
  const ctx = await details({ ...ALL, upload: false }, {}, { set: () => Promise.resolve({ id: "dev-1", ...ALL, upload: true, download: false }) });
  sw(ctx, "upload").click(); await sleep(60);
  await waitFor(() => on(ctx, "upload") === "true");
  assert.equal(on(ctx, "upload"), "true");
  assert.equal(on(ctx, "download"), "false", "painted from the answer");
  assert.deepEqual(ctx.errs, []);
});

test("A. after a failed save the switches show what the agent holds now (a fresh read)", async () => {
  // the call fails, but the agent had in fact already applied Upload (a timeout after the write)
  const ctx = await details({ ...ALL, upload: false }, {}, { set: () => Promise.reject(new Error("timed out")), all: () => Promise.resolve([{ id: "dev-1", ...ALL, upload: true }]) });
  sw(ctx, "upload").click(); await sleep(60);
  await waitFor(() => ctx.calls.some((c) => c[0] === "device_access_all"));
  assert.ok(ctx.calls.some((c) => c[0] === "device_access_all"), "the access was read again");
  assert.equal(on(ctx, "upload"), "true", "what the agent holds, not the click's undo");
  assert.match(text(ctx.d.body), /timed out/, "the failure is still said");
  assert.ok(!sw(ctx, "upload").disabled);
});

test("A. if the fresh read fails too, the switch goes back to the value from before the click", async () => {
  const ctx = await details({ ...ALL, upload: false }, {}, { set: () => Promise.reject(new Error("timed out")), all: () => Promise.reject(new Error("gone")) });
  sw(ctx, "upload").click(); await sleep(60);
  await waitFor(() => /timed out/.test(text(ctx.d.body)));
  assert.equal(on(ctx, "upload"), "false");
  assert.match(text(ctx.d.body), /timed out/);
});

test("B. a device signed in with the account has fixed Allowed rows for the file grants and switches for the rest", async () => {
  const ctx = await details({ ...ALL, download: false, upload: false }, { viaLogin: true });
  const dlg = ctx.dlg();
  for (const k of FILE) assert.equal(sw(ctx, k), null, k + " has no switch");
  assert.equal(dlg.querySelectorAll(".sr.fixed").length, 6);
  for (const r of dlg.querySelectorAll(".sr.fixed")) assert.match(text(r.querySelector(".tag")), /^Allowed$/);
  assert.equal(dlg.querySelectorAll(".sr.fixed button, .sr.fixed [role=switch]").length, 0, "nothing in them can be pressed");
  for (const k of ["viewApps", "launchApps", "readOnly"]) assert.ok(sw(ctx, k), k + " stays a switch");
  sw(ctx, "readOnly").click(); await sleep(40);
  assert.deepEqual(ctx.sets()[0][1].patch, { readOnly: true });
});

test("B. a device paired with a code keeps all its switches", async () => {
  const ctx = await details(ALL);
  assert.equal(ctx.dlg().querySelectorAll(".sw[data-c]").length, 9);
  assert.equal(ctx.dlg().querySelectorAll(".sr.fixed").length, 0);
});

test("C. address, version and device id sit in a closed Technical details fold in the dialog", async () => {
  const ctx = await details(ALL);
  const dlg = ctx.dlg(); const fold = dlg.querySelector("details");
  assert.ok(fold, "a fold exists"); assert.equal(fold.open, false, "it starts closed");
  assert.match(text(fold.querySelector("summary")), /^Technical details$/);
  assert.match(text(fold), /192\.168\.1\.57/); assert.match(text(fold), /1\.4\.0/); assert.match(text(fold), /dev-1/);
  const clone = dlg.cloneNode(true); clone.querySelector("details").remove();
  assert.doesNotMatch(text(clone), /192\.168\.1\.57|1\.4\.0|Device id|dev-1/, "none of it outside the fold");
});

test("C. a device row in the list keeps address and version inside a closed fold", async () => {
  const ctx = await details(ALL); const { A, d } = ctx;
  for (const x of d.querySelectorAll(".scrim")) x.remove();
  A.go("devices"); await sleep(100); A.X.devices = [ctx.dev]; A.pageDevices(); await sleep(60);
  await waitFor(() => d.querySelector("#stage [data-v5pick]"));
  const row = d.querySelector("#stage [data-v5pick]"); assert.ok(row, "the device row is drawn");
  assert.doesNotMatch(text(row), /192\.168\.1\.57|1\.4\.0/, "the row line itself has neither");
  const rail = d.querySelector("#stage #v5rail"); assert.ok(rail, "the right panel shows the device");
  const fold = rail.querySelector("details"); assert.ok(fold && !fold.open, "closed fold in the right panel");
  assert.match(text(fold.querySelector("summary")), /^Technical details$/);
  assert.match(text(fold), /192\.168\.1\.57/); assert.match(text(fold), /1\.4\.0/);
  const clone = rail.cloneNode(true); clone.querySelector("details").remove();
  assert.doesNotMatch(text(clone), /192\.168\.1\.57|1\.4\.0/, "the panel outside the fold has neither");
  assert.deepEqual(ctx.errs, []);
});

test("D. a revoked device has no folder limit field and no way to save one", async () => {
  const ctx = await details({ ...ALL, jailRoot: "/srv" }, { revoked: true });
  const dlg = ctx.dlg();
  assert.equal(dlg.querySelector("#dvj"), null);
  assert.doesNotMatch(text(dlg), /Folder limit|Save folder limit/);
  assert.ok([...dlg.querySelectorAll(".btn")].some((b) => text(b) === "Done"));
});

test("E. the folder limit saves on Enter, only when changed, and there is no Save button", async () => {
  const ctx = await details({ ...ALL, jailRoot: "" });
  const dlg = ctx.dlg(); const j = dlg.querySelector("#dvj");
  const key = (k) => j.dispatchEvent(new ctx.w.KeyboardEvent("keydown", { key: k, bubbles: true }));
  assert.ok(![...dlg.querySelectorAll(".btn")].some((b) => /Save/.test(text(b))), "no Save button");
  key("Enter"); await sleep(30);
  assert.equal(ctx.sets().length, 0, "nothing changed, nothing sent");
  j.value = "/srv/photos"; key("Enter"); await sleep(40);
  assert.equal(ctx.sets().length, 1); assert.deepEqual(ctx.sets()[0][1].patch, { jailRoot: "/srv/photos" });
  key("Enter"); await sleep(30);
  assert.equal(ctx.sets().length, 1, "Enter again on the saved text does not send it twice");
  j.value = ""; key("Enter"); await sleep(40);
  assert.deepEqual(ctx.sets()[1][1].patch, { jailRoot: "" }, "empty clears the limit");
  assert.deepEqual(ctx.errs, []);
});

test("E. leaving the folder-limit field does not save it", async () => {
  const ctx = await details({ ...ALL, jailRoot: "" });
  const j = ctx.dlg().querySelector("#dvj");
  j.value = "/srv/unsaved"; j.dispatchEvent(new ctx.w.FocusEvent("blur")); j.dispatchEvent(new ctx.w.FocusEvent("focusout", { bubbles: true })); await sleep(50);
  assert.equal(ctx.sets().length, 0, "blur sent nothing");
});

test("E. Escape with an unsaved folder limit discards it: nothing is sent", async () => {
  const ctx = await details({ ...ALL, jailRoot: "" });
  const j = ctx.dlg().querySelector("#dvj");
  j.value = "/srv/unsaved";
  j.dispatchEvent(new ctx.w.KeyboardEvent("keydown", { key: "Escape", bubbles: true })); await sleep(50);
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 0, "Escape closes the dialog");
  j.dispatchEvent(new ctx.w.FocusEvent("blur")); await sleep(40); /* a browser may blur the field as the dialog goes away */
  assert.equal(ctx.sets().length, 0, "nothing was saved");
  assert.deepEqual(ctx.errs, []);
});

test("E. Done saves a changed folder limit and then closes; unchanged Done sends nothing", async () => {
  const ctx = await details({ ...ALL, jailRoot: "/srv" });
  const done = () => [...ctx.dlg().querySelectorAll(".df .btn")].find((b) => text(b) === "Done");
  const j = ctx.dlg().querySelector("#dvj"); j.value = "/srv/docs"; done().click(); await sleep(60);
  assert.equal(ctx.sets().length, 1); assert.deepEqual(ctx.sets()[0][1].patch, { jailRoot: "/srv/docs" });
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 0, "closed after the save went through");
  const ctx2 = await details({ ...ALL, jailRoot: "/srv" });
  [...ctx2.dlg().querySelectorAll(".df .btn")].find((b) => text(b) === "Done").click(); await sleep(40);
  assert.equal(ctx2.sets().length, 0, "unchanged: nothing sent"); assert.equal(ctx2.d.querySelectorAll(".dlg").length, 0);
});

/** An error as the engine builds it: the message plus the code that tells a refusal from a failure to reach the agent. */
const coded = (msg, code) => Object.assign(new Error(msg), { code });

test("E. a refused folder limit says why in view, keeps the dialog open, and neither Enter nor Done resends the same text", async () => {
  let refuse = true;
  const ctx = await details({ ...ALL }, {}, { set: () => (refuse ? Promise.reject(coded("not a folder", "BAD_REQUEST")) : Promise.resolve({ id: "dev-1", ...ALL })) });
  const j = ctx.dlg().querySelector("#dvj"); const key = (k) => j.dispatchEvent(new ctx.w.KeyboardEvent("keydown", { key: k, bubbles: true }));
  const done = () => [...ctx.dlg().querySelectorAll(".df .btn")].find((b) => text(b) === "Done");
  let seen = 0; ctx.dlg().querySelector("#dvje").scrollIntoView = function () { seen++; };
  j.value = "/nope"; done().click(); await sleep(50);
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 1, "the dialog stays open");
  assert.match(text(ctx.dlg().querySelector("#dvje")), /not a folder/);
  assert.ok(seen >= 1, "the message is scrolled into view");
  assert.equal(ctx.dlg().querySelector("#dvje").getAttribute("role"), "alert");
  done().click(); await sleep(40);
  assert.equal(ctx.sets().length, 1, "Done does not send the refused text again");
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 1, "and the dialog stays open with the message");
  key("Enter"); await sleep(40);
  assert.equal(ctx.sets().length, 1, "Enter does not send the refused text again either");
  assert.match(text(ctx.dlg().querySelector("#dvje")), /not a folder/, "the reason is still there");
  refuse = false; j.value = "/other"; key("Enter"); await sleep(40);
  assert.equal(ctx.sets().length, 2, "changed text is sent");
  done().click(); await sleep(40);
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 0, "once saved, Done closes");
});

test("E. after a failure to reach the agent, Done and Enter send the same text again", async () => {
  let n = 0;
  const ctx = await details({ ...ALL }, {}, { set: () => (++n <= 2 ? Promise.reject(coded("cannot reach the agent securely: timed out", "NETWORK")) : Promise.resolve({ id: "dev-1", ...ALL })) });
  const j = ctx.dlg().querySelector("#dvj"); const key = (k) => j.dispatchEvent(new ctx.w.KeyboardEvent("keydown", { key: k, bubbles: true }));
  const done = () => [...ctx.dlg().querySelectorAll(".df .btn")].find((b) => text(b) === "Done");
  j.value = "/srv/x"; done().click(); await sleep(50);
  assert.equal(ctx.sets().length, 1); assert.match(text(ctx.dlg().querySelector("#dvje")), /cannot reach/);
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 1, "the dialog stays open");
  done().click(); await sleep(50);
  assert.equal(ctx.sets().length, 2, "Done tries the same text again after a transport failure");
  key("Enter"); await sleep(50);
  assert.equal(ctx.sets().length, 3, "Enter does too");
  assert.equal(text(ctx.dlg().querySelector("#dvje")), "", "the message is gone once the save went through");
  done().click(); await sleep(40);
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 0, "saved text: Done closes");
});

test("E. the refusal message goes when the field is edited, and returns only for the refused text", async () => {
  const ctx = await details({ ...ALL, jailRoot: "/srv" }, {}, { set: () => Promise.reject(coded("not a folder", "BAD_REQUEST")) });
  const j = ctx.dlg().querySelector("#dvj"); const m = ctx.dlg().querySelector("#dvje");
  const key = (k) => j.dispatchEvent(new ctx.w.KeyboardEvent("keydown", { key: k, bubbles: true }));
  const type = (v) => { j.value = v; j.dispatchEvent(new ctx.w.Event("input", { bubbles: true })); };
  j.value = "/nope"; key("Enter"); await sleep(50);
  assert.match(text(m), /not a folder/);
  type("/srv"); assert.equal(text(m), "", "edited back to the saved text: no stale error");
  type("/nope"); assert.match(text(m), /not a folder/, "the refused text shows its reason again");
  type("/elsewhere"); assert.equal(text(m), "", "other text: no error");
  // without an input event (value set, then Enter): the saved text clears the message on the early return
  type("/nope"); j.value = "/srv"; key("Enter"); await sleep(30);
  assert.equal(text(m), "", "Enter on the saved text clears the message");
  assert.equal(ctx.sets().length, 1, "and sends nothing");
  j.value = "/nope"; key("Enter"); await sleep(50); assert.match(text(m), /not a folder/);
  j.value = "/srv"; [...ctx.dlg().querySelectorAll(".df .btn")].find((b) => text(b) === "Done").click(); await sleep(30);
  assert.equal(text(m), "", "Done on the saved text clears it too"); assert.equal(ctx.d.querySelectorAll(".dlg").length, 0, "and closes");
});

test("9. a revoked device row's No access tag has German, Spanish and Arabic rows", async () => {
  const ctx = await details({ ...ALL }, { revoked: true }); const { A, d } = ctx; const { MAP } = A.fx5;
  for (const l of ["de", "es", "ar"]) for (const w of ["No access", "Removed"]) assert.ok(MAP[l][w] && MAP[l][w] !== w, l + " has a row for " + w);
  for (const x of d.querySelectorAll(".scrim")) x.remove();
  A.S.lang = "de"; A.applyLang();
  A.go("devices"); await sleep(100); A.X.devices = [ctx.dev]; A.pageDevices(); await sleep(80);
  await waitFor(() => d.querySelector("#stage [data-v5pick] .st .bad")); /* a slow CI runner renders later than a fixed sleep */
  assert.equal(text(d.querySelector("#stage [data-v5pick] .st .bad")), "Entfernt"); /* the v5 row's revoked mark */
  A.S.lang = "en"; A.applyLang();
});

test("9. a device named like a menu word keeps its name when the language is not English", async () => {
  for (const name of ["Remove", "Allowed"]) {
    const ctx = await details({ ...ALL }, { name }); const { A, d } = ctx;
    A.S.lang = "de"; A.applyLang(); await sleep(60);
    assert.equal(ctx.dlg().querySelector("h2").textContent, name, "dialog title: " + name);
    for (const x of d.querySelectorAll(".scrim")) x.remove();
    A.go("devices"); await sleep(100); A.X.devices = [ctx.dev]; A.pageDevices(); await sleep(80);
    assert.equal(text(d.querySelector("#stage [data-v5pick] .dnt")), name, "list row name: " + name);
    assert.equal(text(d.querySelector("#stage #v5rail h2 .dnt")), name, "right panel name: " + name);
    A.S.lang = "en"; A.applyLang();
  }
});

test("D. a revoked device shows one line, No access. Revoked., instead of a permissions table", async () => {
  const ctx = await details({ ...ALL }, { revoked: true });
  const dlg = ctx.dlg();
  assert.match(text(dlg), /No access\. Revoked\./);
  assert.equal(dlg.querySelector(".capm"), null, "no table");
  assert.doesNotMatch(text(dlg), /Download files|Not allowed|Make share links/);
});

test("B. a device signed in with the account shows one note above the fixed rows and a short Allowed on each", async () => {
  const ctx = await details({ ...ALL }, { viaLogin: true });
  const dlg = ctx.dlg(); const note = dlg.querySelector("#dvacct");
  assert.ok(note, "the note is there"); assert.equal(text(note), "Signed in with the account: file actions are always allowed");
  assert.equal(dlg.querySelectorAll("#dvacct").length, 1);
  const fixed = dlg.querySelectorAll(".sr.fixed"); assert.equal(fixed.length, 6);
  for (const r of fixed) assert.equal(text(r.querySelector(".tag")), "Allowed");
  assert.doesNotMatch(text(dlg), /Allowed \(signed in/);
  assert.ok(note.compareDocumentPosition(fixed[0]) & ctx.w.Node.DOCUMENT_POSITION_FOLLOWING, "the note sits above the first fixed row");
  const plain = await details(ALL); assert.equal(plain.dlg().querySelector("#dvacct"), null, "a code-paired device has no note");
});

test("4. a device row for an account sign-in shows the file access, not only Browse", async () => {
  const ctx = await details({ ...ALL }, { viaLogin: true }); const { A, d } = ctx;
  for (const x of d.querySelectorAll(".scrim")) x.remove();
  A.go("devices"); await sleep(100); A.X.devices = [ctx.dev]; A.pageDevices(); await sleep(60);
  const sub = () => text(d.querySelector("#stage [data-v5pick] small"));
  const swit = (label) => [...d.querySelectorAll("#stage [data-v5perms] .k-sw")].find((b) => b.getAttribute("aria-label") === label);
  assert.match(sub(), /^Account ·/, "the row says the device signed in with the account");
  for (const l of ["Download", "Upload", "Rename and move", "Delete", "Make share links"]) {
    assert.equal(swit(l).getAttribute("aria-checked"), "true", l + " is on for an account sign-in");
    assert.equal(swit(l).disabled, true, l + " is fixed for an account sign-in");
  }
  A.X.devices = [{ ...ctx.dev, viaLogin: false }]; A.pageDevices(); await sleep(40);
  assert.match(sub(), /^Pairing code ·/, "a code-paired device says so");
  assert.equal(swit("Download").disabled, false, "a code-paired device keeps its own switches");
  assert.ok(swit("Download").dataset.c === "download");
});

test("9. the Wake computers grant is a switch for a code-paired device and fixed on for an account sign-in", async () => {
  const ctx = await details({ ...ALL, wake: false }); const { A, d } = ctx;
  for (const x of d.querySelectorAll(".scrim")) x.remove();
  A.go("devices"); await sleep(100); A.X.devices = [ctx.dev]; A.pageDevices(); await sleep(60);
  const swit = () => [...d.querySelectorAll("#stage [data-v5perms] .k-sw")].find((b) => b.getAttribute("aria-label") === "Wake computers");
  assert.ok(swit(), "the Wake computers switch is listed");
  assert.equal(swit().dataset.c, "wake");
  assert.equal(swit().getAttribute("aria-checked"), "false");
  assert.equal(swit().disabled, false);
  A.X.devices = [{ ...ctx.dev, viaLogin: true }]; A.pageDevices(); await sleep(40);
  assert.equal(swit().getAttribute("aria-checked"), "true", "an account sign-in may always wake");
  assert.equal(swit().disabled, true);
});

test("8. the This app tag is outside the truncated name, so a long name cannot hide it", async () => {
  const ctx = await details({ ...ALL }, { current: true, name: "A very long device name ".repeat(6) }); const { A, d } = ctx;
  for (const x of d.querySelectorAll(".scrim")) x.remove();
  A.go("devices"); await sleep(100); A.X.devices = [ctx.dev]; A.pageDevices(); await sleep(60);
  const b = d.querySelector("#stage [data-v5pick] b"); const tag = b.querySelector(".k-pill");
  assert.ok(tag && text(tag) === "This app", "tag present");
  const name = b.querySelector(".dnt"); assert.ok(name && !name.contains(tag), "the tag is not inside the name element");
  assert.equal(tag.parentNode, b, "the tag sits beside the name");
});

test("E. a short Done button closes the dialog", async () => {
  const ctx = await details(ALL);
  const done = [...ctx.dlg().querySelectorAll(".df .btn")].find((b) => text(b) === "Done");
  assert.ok(done, "Done is there"); done.click(); await sleep(30);
  assert.equal(ctx.d.querySelectorAll(".dlg").length, 0);
});

test("F. while a save runs only the clicked switch is disabled", async () => {
  let release; const gate = new Promise((r) => { release = r; });
  const ctx = await details({ ...ALL, upload: false }, {}, { set: () => gate.then(() => ({ id: "dev-1", ...ALL, upload: true })) });
  sw(ctx, "upload").click(); await sleep(30);
  assert.equal(sw(ctx, "upload").disabled, true);
  const others = [...ctx.dlg().querySelectorAll(".sw[data-c]")].filter((b) => b.dataset.c !== "upload");
  assert.ok(others.length >= 6 && others.every((b) => !b.disabled), "the other switches can still be pressed");
  release(); await sleep(50);
  assert.equal(sw(ctx, "upload").disabled, false);
  assert.equal(on(ctx, "upload"), "true");
});

test("S3. changing the access of the device in use asks first; Cancel saves nothing, Change saves", async () => {
  const ctx = await details({ ...ALL, upload: false }, { current: true });
  sw(ctx, "upload").click(); await sleep(60);
  assert.equal(ctx.sets().length, 0, "nothing is saved before the answer");
  const ask = [...ctx.w.document.querySelectorAll(".dlg")].find((x) => /own access/.test(x.textContent));
  assert.ok(ask, "a confirmation names the change");
  [...ask.querySelectorAll("button")].find((b) => /Cancel/.test(b.textContent)).click(); await sleep(60);
  assert.equal(ctx.sets().length, 0, "Cancel saves nothing");
  sw(ctx, "upload").click(); await sleep(60);
  const ask2 = [...ctx.w.document.querySelectorAll(".dlg")].find((x) => /own access/.test(x.textContent));
  [...ask2.querySelectorAll("button")].find((b) => /Change/.test(b.textContent)).click(); await sleep(80);
  assert.equal(ctx.sets().length, 1, "Change saves once");
  assert.equal(ctx.sets()[0][1].confirmSelf, true);
  assert.deepEqual(ctx.errs, []);
});
