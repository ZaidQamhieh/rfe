// Recent files and the Tools page follow the selected computer:
//  - "recent files" are built from transfers with a computer, keyed by that computer, never from a scan of its disk, so a
//    file moved with computer 1 shows under computer 1 only and a screenshot made on the PC shows nowhere;
//  - the Tools page (Trash, Share links, Apps) shows the selected computer's items, with a Show all button for every computer's.
// Pure helpers (Engine.util) in node, then the real window scripts in jsdom on the stub core.
//
//   node --test desktop/ui-tests/recent-tools-scope.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const Engine = require("../ui/engine.js");
const { pickToolHost, scopeToHost, isNoisePath, recentFromTasks } = Engine.util;

const done = (host, dir, name, extra = {}) => ({ id: "t-" + host + name, state: "done", dir, host, name, isDir: false, dstDir: "/srv/in", srcPath: "/srv/out/" + name, finishedAt: 1000, ...extra });

test("recent files are the files moved with that computer and no other", () => {
  const tasks = [done("h1", "up", "from-phone.jpg", { finishedAt: 3000 }), done("h2", "up", "other.txt", { finishedAt: 4000 }), done("h1", "down", "report.pdf", { finishedAt: 5000 })];
  const r = recentFromTasks(tasks, "h1");
  assert.deepEqual(r.map((e) => e.name), ["report.pdf", "from-phone.jpg"], "newest first, only host h1");
  assert.equal(r[1].path, "/srv/in/from-phone.jpg", "an upload lives in the folder it was sent to");
  assert.equal(r[0].path, "/srv/out/report.pdf", "a download is opened where it is on the computer");
  assert.ok(r.every((e) => e.host === "h1"));
  assert.deepEqual(recentFromTasks(tasks, "h3"), [], "a computer with no transfers has no recent files");
});

test("only finished transfers count, small files are kept, and the limit applies", () => {
  const tasks = [done("h1", "up", "tiny.txt", { bytes: 1, finishedAt: 10 }), done("h1", "up", "running.bin", { state: "running", finishedAt: 0 }), done("h1", "up", "failed.bin", { state: "failed" }), done("h1", "up", "b.txt", { finishedAt: 20 }), done("h1", "up", "c.txt", { finishedAt: 30 })];
  assert.deepEqual(recentFromTasks(tasks, "h1").map((e) => e.name), ["c.txt", "b.txt", "tiny.txt"]);
  assert.deepEqual(recentFromTasks(tasks, "h1", { limit: 2 }).map((e) => e.name), ["c.txt", "b.txt"]);
});

test("hidden dotfiles and operating-system paths never appear; a repeated transfer of one file is listed once", () => {
  const tasks = [done("h1", "up", ".bashrc"), done("h1", "up", "x.txt", { dstDir: "/home/u/.cache/app" }), done("h1", "up", "k.dll", { dstDir: "C:\\Windows\\System32" }), done("h1", "up", "n.js", { dstDir: "/p/node_modules/x" }),
    done("h1", "up", "keep.txt", { finishedAt: 5 }), done("h1", "up", "keep.txt", { finishedAt: 9, id: "again" })];
  const r = recentFromTasks(tasks, "h1");
  assert.deepEqual(r.map((e) => e.name), ["keep.txt"]);
  assert.equal(r[0].modified, new Date(9).toISOString(), "the later transfer wins");
});

test("isNoisePath flags dotfiles and system folders, not ordinary paths", () => {
  for (const p of ["/proc/1/status", "/sys/x", "/dev/null", "C:\\Windows\\a.dll", "C:\\Users\\a\\AppData\\Local\\x", "/a/node_modules/b", "/a/.git/config", "/a/.cache/b", "/home/u/.profile"]) assert.ok(isNoisePath(p), p);
  for (const p of ["/home/u/Documents/a.txt", "D:\\Photos\\a.jpg", "/srv/dev-notes/a.txt", "/srv/gitlab/a.txt", "/data/tiny.txt"]) assert.ok(!isNoisePath(p), p);
});

test("the selected computer: the remembered one while it is online, else the Files computer, else the first", () => {
  assert.equal(pickToolHost(["a", "b"], "b", "a"), "b");
  assert.equal(pickToolHost(["a", "b"], "gone", "b"), "b");
  assert.equal(pickToolHost(["a", "b"], "", "local"), "a");
  assert.equal(pickToolHost([], "a", "b"), "");
});

test("scopeToHost keeps the selected computer's items, or all of them on Show all", () => {
  const items = [{ host: "a", n: 1 }, { host: "b", n: 2 }, { host: "a", n: 3 }];
  assert.deepEqual(scopeToHost(items, (x) => x.host, "a", false).map((x) => x.n), [1, 3]);
  assert.deepEqual(scopeToHost(items, (x) => x.host, "a", true).map((x) => x.n), [1, 2, 3]);
  assert.deepEqual(scopeToHost(items, (x) => x.host, "", false).length, 3, "no computer selected: nothing to narrow by");
  assert.equal(scopeToHost(items, (x) => x.host, "a", false) !== items, true, "a new array");
});

// ---------- the real window ----------
const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

async function open() {
  const errs = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
  const store = new Map();
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
      Object.defineProperty(w, "localStorage", { value: { getItem: (k) => (store.has(k) ? store.get(k) : null), setItem: (k, v) => store.set(k, String(v)), removeItem: (k) => store.delete(k) }, configurable: true });
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length > 1); i++) await sleep(30);
  await sleep(150);
  const servers = w.A.E.servers;
  servers[0].state = "online"; servers[1].state = "online";
  return { w, d: w.document, A: w.A, errs, h1: servers[0].id, h2: servers[1].id };
}

async function tools(o, tab) {
  o.A.X.tool = tab;
  o.A.go("tools");
  await sleep(100);
  o.A.pageTools();
  await sleep(30);
}

test("Trash lists the selected computer's deleted items; Show all lists every computer's", async () => {
  const o = await open();
  const X = o.A.X, t = Date.now();
  X.trash = [{ host: o.h1, id: "1", name: "one.txt", path: "/a/one.txt", isDir: false, b: 5, at: t }, { host: o.h2, id: "2", name: "two.txt", path: "/b/two.txt", isDir: false, b: 5, at: t }];
  X.trashAt = t;
  X.toolHost = o.h1;
  await tools(o, "trash");
  const names = () => Array.from(o.d.querySelectorAll(".k-trash .tr b")).map((b) => b.textContent);
  assert.deepEqual(names(), ["one.txt"], "only the selected computer");
  const all = o.d.querySelector('[data-fx="tool.all"]');
  assert.ok(all, "a Show all button");
  assert.match(all.textContent, /Show all/);
  all.click(); await sleep(50);
  assert.deepEqual(names().sort(), ["one.txt", "two.txt"], "every computer's items");
  assert.match(o.d.querySelector('[data-fx="tool.all"]').textContent, /selected computer|Show/i);
  o.d.querySelector('[data-fx="tool.all"]').click(); await sleep(50);
  assert.deepEqual(names(), ["one.txt"], "pressing it again returns to the selected computer");
  o.d.querySelector('[data-fx="tool.host|' + o.h2 + '"]').click(); await sleep(50);
  assert.deepEqual(names(), ["two.txt"], "choosing another computer switches the list");
  assert.deepEqual(o.errs, []);
});

test("Share links follow the same selection and Show all, and Revoke still acts on the right link", async () => {
  const o = await open();
  const X = o.A.X, t = Date.now();
  X.shares = [{ host: o.h2, hash: "hh2", name: "two.pdf", path: "/b/two.pdf", exp: t + 3.6e6 }, { host: o.h1, hash: "hh1", name: "one.pdf", path: "/a/one.pdf", exp: t + 3.6e6 }];
  X.sharesAt = t;
  X.toolHost = o.h1;
  await tools(o, "share");
  const rows = () => Array.from(o.d.querySelectorAll('[data-row^="share|"] b')).map((b) => b.textContent);
  assert.deepEqual(rows(), ["one.pdf"]);
  assert.ok(o.d.querySelector('[data-fx="share.revoke|1"]'), "keeps the index of the link in X.shares");
  assert.equal(o.d.querySelector('[data-fx="share.revoke|0"]'), null);
  o.d.querySelector('[data-fx="tool.all"]').click(); await sleep(50);
  assert.deepEqual(rows().sort(), ["one.pdf", "two.pdf"]);
  assert.deepEqual(o.errs, []);
});

test("Changed lately lists the transfers with that computer, not a scan of its disk", async () => {
  const o = await open();
  let scans = 0;
  const call0 = o.A.E.call;
  o.A.E.call = function (cmd) { if (cmd === "files_recent") scans++; return call0.apply(this, arguments); };
  const E = o.A.E;
  E.tasks.push(done(o.h1, "up", "phone-photo.jpg", { finishedAt: Date.now() - 60e3 }), done(o.h2, "up", "for-two.txt", { finishedAt: Date.now() - 30e3 }));
  o.A.recentOnServer(o.h1);
  await sleep(150);
  const text = o.d.body.textContent;
  assert.match(text, /phone-photo\.jpg/);
  assert.doesNotMatch(text, /for-two\.txt/, "another computer's file");
  assert.doesNotMatch(text, /notes\.txt/, "the stub's disk scan result");
  assert.equal(scans, 0, "no files_recent scan is made");
  assert.deepEqual(o.errs, []);
});
