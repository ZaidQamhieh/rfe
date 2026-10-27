// The window's engine (ui/engine.js) against a fake core: paths, listings, server states and the
// transfer list, with `invoke` answered by the test. No DOM, no network.
//
//   node --test desktop/ui-tests/*.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const path = require.resolve("../ui/engine.js");
const fresh = () => {
  delete require.cache[path];
  return require(path);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (fn, what = "condition") => {
  for (let i = 0; i < 200; i++) {
    if (fn()) return;
    await sleep(5);
  }
  assert.fail("timed out waiting for " + what);
};

const HOST = "192.168.1.20:7443";
const HEALTH = { host: HOST, fingerprint: "ab".repeat(32), health: { status: "ok", name: "nas", version: "1.43.0", os: "Debian 12" }, status: null };
const entry = (name, o = {}) => ({ name, path: (o.dir || "/srv") + "/" + name, isDir: false, size: 10, mimeType: "", mode: "-rw-r--r--", modified: "2026-01-02T03:04:05Z", ...o });

/** An engine with one signed-in server and a core that serves `handlers`; `calls` records every command. */
async function boot(handlers = {}) {
  const E = fresh();
  const calls = [];
  E.setInvoke(async (cmd, args) => {
    calls.push([cmd, args]);
    const h = handlers[cmd];
    if (h) return typeof h === "function" ? h(args) : h;
    switch (cmd) {
      case "transfer_folder": return "/dl";
      case "local_places": return [{ label: "Home", path: "/home/u", kind: "home" }];
      case "list_hosts": return [{ key: HOST, name: "nas", host: HOST, fingerprint: "ab".repeat(32), username: "u", signedIn: true }];
      case "agent_health": return HEALTH;
      case "files_roots": return { locations: [{ path: "/srv", label: "srv", totalBytes: 0, freeBytes: 0, isOs: false }, { path: "/home/u", label: "u", totalBytes: 0, freeBytes: 0, isOs: false }], source: "roots" };
      default: throw "unknown command " + cmd;
    }
  });
  await E.init();
  await until(() => E.server(HOST).state === "online", "the server to come online");
  await until(() => E.server(HOST).roots.length, "the roots");
  return { E, calls };
}

test("paths: POSIX, drive and share roots, dot segments", () => {
  const { util: U } = fresh();
  assert.equal(U.norm("/a/./b/../c/"), "/a/c");
  assert.equal(U.parentOf("/a/b"), "/a");
  assert.equal(U.parentOf("/a"), "/");
  assert.equal(U.baseOf("/a/b.txt"), "b.txt");
  assert.equal(U.join("/a", "b/c"), "/a/b/c");
  assert.equal(U.join("/a", "x\\y"), "/a/x\\y", "a backslash in a POSIX name is part of the name (audit F-131)");
  assert.equal(U.norm("/a/x\\y/.."), "/a");
  assert.equal(U.norm("c:\\Users\\x\\.."), "C:\\Users");
  assert.equal(U.parentOf("C:\\Users"), "C:\\");
  assert.equal(U.isRoot("C:\\"), true);
  assert.equal(U.norm("\\\\nas\\share\\a"), "\\\\nas\\share\\a");
  assert.equal(U.isRoot("\\\\nas\\share\\"), true);
});

test("sizes and kinds", () => {
  const { util: U } = fresh();
  assert.equal(U.fmtBytes(999), "999 B");
  assert.equal(U.fmtBytes(1500), "1.50 KB");
  assert.equal(U.fmtBytes(-1), "—");
  assert.equal(U.kindOf({ n: "a.apk", t: "file", perm: "-rw-r--r--" }), "pkg");
  assert.equal(U.kindOf({ n: "a", t: "dir" }), "dir");
  assert.equal(U.permOctal("-rwxr-xr--"), "0754");
});

test("a failure from the core becomes an error with the agent's code and no prefix", async () => {
  const { E } = await boot({ files_list: () => Promise.reject("The agent says: not found (NOT_FOUND)") });
  await assert.rejects(E.fs.load(HOST, "/srv/missing"), (e) => e.code === "NOT_FOUND" && e.message === "not found");
  assert.equal(E.fs.state(HOST, "/srv/missing"), "error");
  assert.match(E.fs.error(HOST, "/srv/missing").message, /not found/);
});

test("the virtual root lists the allowed roots and is loaded as soon as load() settles", async () => {
  const { E, calls } = await boot();
  const items = await E.fs.load(HOST, "/");
  assert.deepEqual(items.map((n) => n.n), ["srv", "u"]);
  assert.equal(E.fs.state(HOST, "/"), "loaded", "a listing built without waiting must not be left in the loading state");
  assert.equal(calls.filter(([c]) => c === "files_list").length, 0, "the roots come from files_roots, not from a listing of /");
  assert.deepEqual(E.fs.list(HOST, "/").map((n) => n.n), ["srv", "u"]);
});

test("a listing is read page by page and merged", async () => {
  const pages = [{ entries: [entry("a"), entry("b")], nextCursor: "c1" }, { entries: [entry("c")], nextCursor: null }];
  const seen = [];
  const { E } = await boot({ files_list: (a) => { seen.push(a.cursor); return { path: a.path, ...pages[seen.length - 1] }; } });
  const items = await E.fs.load(HOST, "/srv");
  assert.deepEqual(items.map((n) => n.n), ["a", "b", "c"]);
  assert.deepEqual(seen, [null, "c1"]);
});

test("list() returns null while a folder loads, starts the load once, then returns the items", async () => {
  let n = 0;
  const { E } = await boot({ files_list: async () => { n++; await sleep(10); return { entries: [entry("x")], nextCursor: null }; } });
  assert.equal(E.fs.list(HOST, "/srv"), null);
  assert.equal(E.fs.list(HOST, "/srv"), null);
  await until(() => E.fs.list(HOST, "/srv"), "the listing");
  assert.equal(n, 1);
});

test("a folder's child count is only shown when the agent gave one", async () => {
  const { E } = await boot({ files_list: () => ({ entries: [entry("d", { isDir: true, childCount: 4 }), entry("e", { isDir: true })], nextCursor: null }) });
  const [d, e] = await E.fs.load(HOST, "/srv");
  assert.equal(d.cc, 4);
  assert.equal(e.cc, null);
});

test("a download is queued as a task, paused, resumed through retry, and finishes", async () => {
  const views = { r1: { id: "r1", direction: "download", state: "running", name: "big.bin", remotePath: "/srv/big.bin", localPath: "", done: 5, total: 10, error: "", verified: false, host: HOST } };
  const { E, calls } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => ["r1"],
    transfer_list: () => Object.values(views),
    transfer_pause: ({ id }) => { views[id].state = "paused"; },
    transfer_retry: ({ id }) => { views[id].state = "running"; },
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  assert.equal(tasks.length, 1);
  await until(() => tasks[0].rids.length, "the core to take the transfer");
  assert.deepEqual(calls.find(([c]) => c === "transfer_download_tree")[1], { host: HOST, remotePath: "/srv/big.bin", isDir: false, askWhere: false });
  await E.pollTransfers();
  assert.equal(tasks[0].state, "running");
  assert.equal(tasks[0].done, 5);

  await E.pause(tasks[0].id);
  assert.equal(tasks[0].state, "paused");
  assert.deepEqual(calls.filter(([c]) => c === "transfer_pause").map(([, a]) => a), [{ id: "r1" }]);
  await E.pollTransfers();
  assert.equal(tasks[0].state, "paused", "the core's own state keeps it paused");

  E.resumeTask(tasks[0].id);
  await until(() => calls.some(([c]) => c === "transfer_retry"), "the retry");
  views.r1.state = "done"; views.r1.done = 10; views.r1.localPath = "/dl/big.bin"; views.r1.verified = true;
  await E.pollTransfers();
  assert.equal(tasks[0].state, "done");
  assert.equal(tasks[0].savedPath, "/dl/big.bin");
  assert.equal(tasks[0].dstDir, "/dl");
});

test("pausing a transfer the core has not taken yet says so instead of doing nothing", async () => {
  const { E } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => new Promise(() => {}),
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await assert.rejects(E.pause(tasks[0].id), /Wait until the transfer has started/);
});

test("a transfer cancelled while the core is still setting it up is cancelled in the core too", async () => {
  let release;
  const { E, calls } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => new Promise((r) => { release = r; }),
    transfer_cancel: () => {},
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => release, "the core to be asked");
  await E.cancel(tasks[0].id);
  release(["r9"]);
  await until(() => calls.some(([c]) => c === "transfer_cancel"), "the core to be told to cancel");
  assert.deepEqual(calls.filter(([c]) => c === "transfer_cancel").map(([, a]) => a), [{ id: "r9" }]);
  assert.equal(E.tasks.length, 0);
});

test("disconnecting a server pauses its transfers in the core and keeps them waiting until it is back", async () => {
  const views = { r1: { id: "r1", direction: "upload", state: "running", name: "big.bin", remotePath: "/srv/big.bin", localPath: "", done: 5, total: 10, error: "", verified: false, host: HOST } };
  const { E, calls } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => ["r1"],
    transfer_list: () => Object.values(views),
    transfer_pause: ({ id }) => { views[id].state = "paused"; },
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => tasks[0].rids.length, "the core to take the transfer");
  await E.pollTransfers();
  E.disconnect(HOST);
  assert.equal(tasks[0].state, "waiting");
  await until(() => calls.some(([c]) => c === "transfer_pause"), "the core to be told to pause");
  views.r1.state = "running"; /* even a transfer the core has not stopped yet is not shown as running */
  await E.pollTransfers();
  assert.equal(tasks[0].state, "waiting");
  views.r1.state = "paused";
  await E.pollTransfers();
  assert.equal(tasks[0].state, "waiting");
});

test("a health answer that arrives after Disconnect does not bring the server back or restart its transfers", async () => {
  let release;
  const { E, calls } = await boot();
  E.setInvoke(async (cmd, args) => {
    calls.push([cmd, args]);
    if (cmd === "agent_health") return new Promise((r) => { release = () => r(HEALTH); });
    if (cmd === "transfer_list") return [];
    return null;
  });
  const s = E.server(HOST);
  const pending = E.check(s);
  await until(() => release, "the health read to start");
  E.disconnect(HOST);
  release();
  await pending;
  await sleep(20);
  assert.equal(s.state, "disconnected");
  assert.equal(calls.filter(([c]) => c === "transfer_retry").length, 0);
});

test("a folder that failed before the server's places were known is read again once they are", async () => {
  const { E } = await boot({ files_list: () => Promise.reject("path is outside allowed root: /") });
  await E.fs.load(HOST, "/srv/x").catch(() => {});
  assert.equal(E.fs.state(HOST, "/srv/x"), "error");
  await E.loadRoots(E.server(HOST));
  assert.equal(E.fs.state(HOST, "/srv/x"), "none");
});

test("a login the agent no longer accepts sends the server back to sign-in", async () => {
  const { E } = await boot();
  E.setInvoke(async (cmd) => {
    if (cmd === "agent_health") return Promise.reject("The agent no longer accepts this login. Sign in again.");
    return null;
  });
  const s = E.server(HOST);
  await E.check(s);
  assert.equal(s.state, "login");
  assert.equal(s.signedIn, false);
});

test("a server disconnected while a transfer is being set up has it paused when the core answers", async () => {
  let release;
  const { E, calls } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => new Promise((r) => { release = r; }),
    transfer_pause: () => {},
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => release, "the core to be asked");
  E.disconnect(HOST);
  assert.equal(tasks[0].state, "waiting");
  release(["r5"]);
  await until(() => calls.some(([c]) => c === "transfer_pause"), "the pause");
  assert.deepEqual(calls.filter(([c]) => c === "transfer_pause").map(([, a]) => a), [{ id: "r5" }]);
});

test("a conflict is not answered while its server is disconnected", async () => {
  const views = { r1: { id: "r1", direction: "upload", state: "conflict", name: "big.bin", remotePath: "/srv/big.bin", localPath: "", done: 0, total: 10, error: "", verified: false, host: HOST, conflict: { size: 5, modifiedMs: 0, isDir: false } } };
  const { E, calls } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => ["r1"],
    transfer_list: () => Object.values(views),
    transfer_pause: () => {},
    transfer_resolve: () => {},
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => tasks[0].rids.length, "the core to take the transfer");
  await E.pollTransfers();
  assert.equal(tasks[0].state, "conflict");
  E.disconnect(HOST);
  await E.resolve(tasks[0].id, "replace");
  assert.equal(calls.filter(([c]) => c === "transfer_resolve").length, 0, "nothing is sent to a server that is away");
  assert.equal(tasks[0].state, "conflict");
});

test("a setup that failed while the server was away starts again when it is back", async () => {
  let n = 0; let fail;
  const { E } = await boot({
    files_list: () => ({ entries: [entry("dir", { isDir: true })], nextCursor: null }),
    transfer_download_tree: () => { n++; if (n === 1) return new Promise((_, rej) => { fail = rej; }); return ["r1"]; },
    transfer_pause: () => {},
    transfer_list: () => [],
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["dir"] });
  await until(() => fail, "the first setup");
  E.disconnect(HOST);
  fail(new Error("Could not reach the computer"));
  await until(() => !tasks[0].starting, "the setup to end");
  assert.equal(tasks[0].state, "waiting", "not failed: the server was away");
  await E.connect(HOST);
  await until(() => n === 2, "the setup to run again");
  await until(() => tasks[0].rids.length === 1, "the second setup to finish");
});

test("a folder with nothing in it is done at once", async () => {
  const { E } = await boot({
    files_list: () => ({ entries: [entry("empty", { isDir: true })], nextCursor: null }),
    transfer_download_tree: () => [],
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["empty"] });
  await until(() => tasks[0].state === "done", "the folder to count as done");
});

test("a reconnect that came while the core was still stopping a transfer starts it once the core has stopped", async () => {
  const views = { r1: { id: "r1", direction: "download", state: "running", name: "big.bin", remotePath: "/srv/big.bin", localPath: "", done: 5, total: 10, error: "", verified: false, host: HOST } };
  let retries = 0;
  const { E } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => ["r1"],
    transfer_list: () => Object.values(views),
    transfer_pause: () => {},
    transfer_retry: ({ id }) => { retries++; if (views[id].state === "paused") views[id].state = "running"; },
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => tasks[0].rids.length, "the core to take it");
  await E.pollTransfers();
  tasks[0].state = "queued"; /* the window has asked to start it again; the core is still stopping */
  tasks[0].resumedAt = Date.now(); /* by a reconnect, just now */
  views.r1.state = "paused"; /* ...and now it has stopped */
  await E.pollTransfers();
  await until(() => retries > 0, "the automatic retry");
  assert.notEqual(tasks[0].state, "paused");
});

test("a Pause all from the tray stays paused: the window does not start it again", async () => {
  const views = { r1: { id: "r1", direction: "download", state: "running", name: "big.bin", remotePath: "/srv/big.bin", localPath: "", done: 5, total: 10, error: "", verified: false, host: HOST } };
  let retries = 0;
  const { E } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => ["r1"],
    transfer_list: () => Object.values(views),
    transfer_retry: () => { retries++; },
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => tasks[0].rids.length, "the core to take it");
  await E.pollTransfers();
  views.r1.state = "paused"; /* the tray paused it in the core; the window did not ask */
  await E.pollTransfers();
  await new Promise((r) => setTimeout(r, 300));
  await E.pollTransfers();
  assert.equal(retries, 0);
  assert.equal(tasks[0].state, "paused");
});

test("searching this computer asks the core and returns what it found", async () => {
  const { E, calls } = await boot({
    local_search: () => [{ name: "report.txt", path: "/home/u/report.txt", isDir: false, size: 3, mimeType: "", mode: "-rw-r--r--", modified: "2026-01-02T03:04:05Z" }],
  });
  const s = E.makeSearch(["local"], "report", {});
  await until(() => s.step(), "the search");
  assert.equal(s.results.length, 1);
  assert.equal(s.results[0].path, "/home/u/report.txt");
  assert.equal(calls.filter(([c]) => c === "local_search").length, 1);
});

test("a transfer the core refuses fails with the core's words", async () => {
  const { E } = await boot({
    files_list: () => ({ entries: [entry("big.bin")], nextCursor: null }),
    transfer_download_tree: () => Promise.reject("Could not create the file: disk full"),
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => tasks[0].state === "failed", "the failure");
  assert.equal(tasks[0].msg, "Could not create the file: disk full");
});

test("a download is not queued for a server that is not connected", async () => {
  const { E } = await boot();
  E.server(HOST).state = "offline";
  const r = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["x"] });
  assert.match(r.error, /is not connected/);
});

test("a certificate that changed puts the server in the refused state", async () => {
  const E = fresh();
  E.setInvoke(async (cmd) => {
    switch (cmd) {
      case "transfer_folder": return "/dl";
      case "local_places": return [];
      case "list_hosts": return [{ key: HOST, name: "nas", host: HOST, fingerprint: "ab".repeat(32), username: "u", signedIn: true }];
      case "agent_health": throw "the agent's certificate does not match the pinned fingerprint";
      case "probe_agent": return { fingerprint: "cd".repeat(32), previous: "ab".repeat(32), changed: true };
      default: throw "unknown command " + cmd;
    }
  });
  await E.init();
  await until(() => E.server(HOST).state === "refused", "the refusal");
  assert.equal(E.server(HOST).newFp.startsWith("CD:CD"), true);
});

test("a folder the agent counted only up to its cap says 1000+, a listed one says its real count", () => {
  const { fs } = fresh();
  assert.equal(fs.itemsLabel({ t: "dir", cc: 1000 }), "1000+ items");
  assert.equal(fs.itemsLabel({ t: "dir", cc: 1 }), "1 item");
  assert.equal(fs.itemsLabel({ t: "dir", cc: 7 }), "7 items");
  assert.equal(fs.itemsLabel({ t: "dir", cc: 1000, kids: new Array(1200).fill({}) }), "1000 items", "a listed folder shows its cc, not a cap");
});

test("with hidden files off, a loaded folder's count leaves out its dotfiles (F-36)", () => {
  const fs = fresh().fs;
  const kids = [{ n: "a" }, { n: "b" }, { n: ".hidden" }];
  assert.equal(fs.itemCount({ t: "dir", cc: 3, kids }), 3);
  fs.setDotsHidden(() => true);
  assert.equal(fs.itemCount({ t: "dir", cc: 3, kids }), 2);
  assert.equal(fs.itemCount({ t: "dir", cc: 3 }), 3, "an unlisted folder keeps the agent's count");
});
