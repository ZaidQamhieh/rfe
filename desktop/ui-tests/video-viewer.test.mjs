// The video viewer: Open on a video plays it in the preview dialog.
//  1. hasViewer is true for the video types the webview plays (mp4, webm, mov, m4v, ogg) and false for the rest;
//  2. a video over the size cap gets the short "too big" reason and is never read;
//  3. the bytes become a Blob URL in a <video controls>, and the URL is revoked when the dialog closes;
//  4. the CSP gains media-src blob: and nothing else.
//
//   node --test desktop/ui-tests/video-viewer.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const here = (p) => fileURLToPath(new URL(p, import.meta.url));
const page = here("../ui-dev/harness.html");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const NAS = "192.168.1.20:7443";
const BIG = "This video is too big to play here. Download it instead.";
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
  const w = dom.window; live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(150);
  return { w, d: w.document, A: w.A, E: w.A.E, errs };
}

/** A copy made of this realm's own objects, so deepEqual does not trip on the window's Object prototype. */
const plain = (x) => JSON.parse(JSON.stringify(x));
const file = (n, b) => ({ n, t: "file", b, perm: "-rw-r--r--", own: "me", mod: 1 });

/** Answers the video commands with `size` bytes, records every call, and records the Blob URLs made and revoked. */
function stub(ctx, size = 16) {
  const { w, E } = ctx;
  const calls = [], made = [], revoked = [];
  const orig = E.call;
  E.call = (cmd, args) => {
    if (cmd === "files_video" || cmd === "local_read_video") { calls.push({ cmd, args }); return Promise.resolve(new w.Uint8Array(size).buffer); }
    return orig(cmd, args);
  };
  w.URL.createObjectURL = (b) => { const u = "blob:null/" + made.length; made.push({ u, type: b.type, size: b.size }); return u; };
  w.URL.revokeObjectURL = (u) => { revoked.push(u); };
  return { calls, made, revoked };
}

test("1. hasViewer is true for the video types the webview plays, on a computer and on this one", async () => {
  const { A, errs } = await open();
  for (const name of ["a.mp4", "a.webm", "a.mov", "a.m4v", "a.ogg", "A.MP4"]) {
    assert.equal(A.hasViewer(NAS, file(name, 1e6)), true, name + " remote");
    assert.equal(A.hasViewer("local", file(name, 1e6)), true, name + " local");
  }
  for (const name of ["a.mkv", "a.avi", "a.wmv", "a.flv"]) assert.equal(A.hasViewer(NAS, file(name, 1e6)), false, name);
  assert.equal(A.hasViewer(NAS, { ...file("a.mp4", 1e6), perm: "--w--w--w-" }), false, "an unreadable video has no viewer");
  assert.deepEqual(errs, []);
});

test("2. a video over 256 MiB says it is too big; one that cannot play still says so; a small one has no block", async () => {
  const ctx = await open(); const { A, errs } = ctx;
  assert.equal(A.openBlock(NAS, file("clip.mp4", 1e6)), "");
  assert.equal(A.openBlock(NAS, file("clip.mp4", 256 * 1024 * 1024)), "");
  assert.equal(A.openBlock(NAS, file("clip.mp4", 256 * 1024 * 1024 + 1)), BIG);
  assert.equal(A.openBlock("local", file("clip.webm", 412e6)), BIG);
  assert.equal(A.openBlock(NAS, file("clip.mkv", 1e6)), "The app does not play video.");
  const s = stub(ctx);
  A.previewDialog(NAS, "/srv/v", file("huge.mp4", 412e6)); await sleep(120);
  const body = ctx.d.querySelector(".pvbody");
  assert.ok(body.textContent.includes(BIG), "the dialog shows the reason: " + body.textContent);
  assert.equal(body.querySelector("video"), null);
  assert.deepEqual(s.calls, [], "a video over the cap is never read");
  assert.deepEqual(errs, []);
});

test("3. a remote video plays from a Blob URL in <video controls>, and the URL is revoked on close", async () => {
  const ctx = await open(); const { A, d, errs } = ctx;
  const s = stub(ctx, 32);
  const dlg = A.previewDialog(NAS, "/srv/v", file("walk.mp4", 32)); await sleep(120);
  assert.deepEqual(plain(s.calls), [{ cmd: "files_video", args: { host: NAS, path: "/srv/v/walk.mp4" } }]);
  const v = d.querySelector(".pvbody video");
  assert.ok(v, "a video element is in the viewer");
  assert.ok(v.hasAttribute("controls"));
  assert.equal(v.getAttribute("src"), "blob:null/0");
  assert.deepEqual(s.made, [{ u: "blob:null/0", type: "video/mp4", size: 32 }]);
  assert.deepEqual(s.revoked, [], "kept while the viewer is open");
  dlg.close(); await sleep(30);
  assert.deepEqual(s.revoked, ["blob:null/0"], "revoked once, on close");
  assert.deepEqual(errs, []);
});

test("3. a video on this computer is read with local_read_video and gets its type from the ending", async () => {
  const ctx = await open(); const { A, d, errs } = ctx;
  const s = stub(ctx, 8);
  const dlg = A.previewDialog("local", "/home/me", file("loop.webm", 8)); await sleep(120);
  assert.deepEqual(plain(s.calls), [{ cmd: "local_read_video", args: { path: "/home/me/loop.webm" } }]);
  assert.equal(s.made[0].type, "video/webm");
  assert.ok(d.querySelector(".pvbody video[controls]"));
  dlg.close(); await sleep(30);
  assert.deepEqual(s.revoked, [s.made[0].u]);
  assert.deepEqual(errs, []);
});

test("3. closing before the bytes arrive makes no URL, so none is left behind", async () => {
  const ctx = await open(); const { A, E, w, errs } = ctx;
  const s = stub(ctx);
  const orig = E.call; let release;
  E.call = (cmd, args) => (cmd === "files_video" ? new Promise((r) => { release = () => r(new w.Uint8Array(4).buffer); }) : orig(cmd, args));
  const dlg = A.previewDialog(NAS, "/srv/v", file("slow.mp4", 4)); await sleep(60);
  dlg.close(); release(); await sleep(60);
  assert.deepEqual(s.made, []);
  assert.deepEqual(s.revoked, []);
  assert.deepEqual(errs, []);
});

test("3. a failed read shows the reason in the viewer", async () => {
  const ctx = await open(); const { A, d, E } = ctx;
  const orig = E.call;
  E.call = (cmd, args) => (cmd === "files_video" ? Promise.reject(new Error(BIG)) : orig(cmd, args));
  A.previewDialog(NAS, "/srv/v", file("a.mp4", 10)); await sleep(120);
  assert.ok(d.querySelector(".pvbody").textContent.includes(BIG));
});

test("4. the CSP gains media-src blob: and nothing else", () => {
  const conf = JSON.parse(readFileSync(here("../src-tauri/tauri.conf.json"), "utf8"));
  const csp = conf.app.security.csp;
  const dirs = Object.fromEntries(csp.split(";").map((x) => x.trim()).filter(Boolean).map((x) => { const [k, ...v] = x.split(/\s+/); return [k, v.join(" ")]; }));
  assert.equal(dirs["media-src"], "blob:");
  assert.deepEqual(Object.keys(dirs), ["default-src", "base-uri", "form-action", "connect-src", "img-src", "style-src", "media-src"]);
  assert.equal(dirs["default-src"], "'self'");
  assert.equal(dirs["connect-src"], "ipc: http://ipc.localhost");
  assert.equal(dirs["img-src"], "'self' data:");
  assert.equal(dirs["style-src"], "'self'");
});
