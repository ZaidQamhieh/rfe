// Small engine fixes: folder listings under a renamed or removed folder are forgotten at a path boundary; an unchanged
// re-read of a folder is silent and keeps showing the old listing; forgetting a computer twice, or calling the same
// "stop listening" twice, removes nothing else; a cancelled transfer is not marked failed by its late setup error;
// the reconnect countdown shown to the person is the real wait.
//
//   node --test desktop/ui-tests/d2c-engine-fixes.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const path = require.resolve("../ui/engine.js");
const fresh = () => { delete require.cache[path]; return require(path); };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (fn, what = "condition") => { for (let i = 0; i < 200; i++) { if (fn()) return; await sleep(5); } assert.fail("timed out waiting for " + what); };

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
      case "files_roots": return { locations: [{ path: "/srv", label: "srv", totalBytes: 0, freeBytes: 0, isOs: false }], source: "roots" };
      default: throw "unknown command " + cmd;
    }
  });
  await E.init();
  await until(() => E.server(HOST).state === "online", "the server to come online");
  await until(() => E.server(HOST).roots.length, "the roots");
  return { E, calls };
}

test("1. renaming a folder forgets the listings below it, and only those (not a sibling that shares the name's start)", async () => {
  const { E } = await boot({
    files_list: ({ path: p }) => ({ entries: p === "/srv" ? [entry("a", { isDir: true }), entry("ab", { isDir: true })] : [entry("x.txt", { dir: p })], nextCursor: null }),
    files_rename: ({ newName }) => entry(newName, { isDir: true }),
  });
  for (const p of ["/srv", "/srv/a", "/srv/a/b", "/srv/ab"]) await E.fs.load(HOST, p);
  await E.fs.rename(HOST, "/srv", "a", "c");
  assert.equal(E.fs.state(HOST, "/srv/a"), "none", "the renamed folder");
  assert.equal(E.fs.state(HOST, "/srv/a/b"), "none", "a folder inside it is not served from the old path");
  assert.equal(E.fs.state(HOST, "/srv/ab"), "loaded", "a sibling whose name starts the same stays");
});

test("2. removing a folder forgets the listings below it", async () => {
  const { E } = await boot({
    files_list: ({ path: p }) => ({ entries: p === "/srv" ? [entry("a", { isDir: true })] : [entry("x.txt", { dir: p })], nextCursor: null }),
    files_trash: () => null,
  });
  for (const p of ["/srv", "/srv/a", "/srv/a/b"]) await E.fs.load(HOST, p);
  await E.fs.remove(HOST, "/srv", ["a"]);
  assert.equal(E.fs.state(HOST, "/srv/a/b"), "none");
});

test("3. reading a folder again with the same answer says nothing and keeps the listing on show; a different answer is announced", async () => {
  let names = ["one", "two"];
  const { E } = await boot({ files_list: () => ({ entries: names.map((n) => entry(n)), nextCursor: null }) });
  await E.fs.load(HOST, "/srv");
  const seen = []; E.on((t, d) => { if (t === "fs") seen.push(d.dir); });
  const p = E.fs.refresh(HOST, "/srv");
  assert.ok(E.fs.list(HOST, "/srv"), "the old listing is still served while it is read again");
  await p;
  assert.deepEqual(seen, [], "nothing changed, nothing is announced");
  assert.equal(E.fs.list(HOST, "/srv").length, 2);
  names = ["one", "two", "three"];
  await E.fs.refresh(HOST, "/srv");
  assert.ok(seen.length >= 1, "a changed listing is announced");
  assert.equal(E.fs.list(HOST, "/srv").length, 3);
});

test("4. forgetting a computer twice does not remove another computer", async () => {
  const { E } = await boot({ remove_host: () => null });
  const other = E.addServer({ name: "other", host: "10.0.0.9:7443" });
  const a = E.forget(HOST), b = E.forget(HOST);
  await Promise.all([a, b]);
  assert.deepEqual(E.servers.map((s) => s.id), [other.id]);
});

test("5. the function that stops listening can be called twice without stopping someone else", async () => {
  const E = fresh();
  const got = [];
  const offA = E.on(() => got.push("a")); E.on(() => got.push("b"));
  offA(); offA();
  E.emit("x");
  assert.deepEqual(got, ["b"]);
});

test("6. a transfer cancelled while it was being set up is not marked failed when the setup then errors", async () => {
  let fail;
  const { E } = await boot({
    files_list: () => ({ entries: [entry("big.bin", { size: 10 })], nextCursor: null }),
    transfer_download_tree: () => new Promise((_, rej) => { fail = rej; }),
  });
  await E.fs.load(HOST, "/srv");
  const { tasks } = E.enqueue({ dir: "down", host: HOST, srcDir: "/srv", dstDir: "", names: ["big.bin"] });
  await until(() => fail, "the core to be asked");
  await E.cancel(tasks[0].id);
  const notes = E.notes.length;
  fail("The agent says: no space (NO_SPACE)");
  await sleep(30);
  assert.equal(tasks[0].state, "cancelled");
  assert.equal(E.notes.length, notes, "no error note for a transfer the person cancelled");
  assert.ok(!E.history.some((h) => /failed/.test(h.text)), "no 'failed' line in the history");
});

test("7. the reconnect countdown is the real wait: the next try comes RETRY seconds after the loss, not at the next 6 s mark", async () => {
  const { E, calls } = await boot();
  E.setInvoke(async (cmd) => { calls.push([cmd]); if (cmd === "agent_health") throw "no answer"; if (cmd === "transfer_list") return []; return null; });
  const s = E.server(HOST);
  await E.check(s);
  assert.equal(s.state, "lost");
  const tries = () => calls.filter(([c]) => c === "agent_health").length;
  const base = tries();
  for (let i = 0; i < 3; i++) E.tick(1000);
  assert.equal(Math.ceil(s.retryIn), 5, "the page's \"Retrying in N s\" counts down with the clock");
  for (let i = 0; i < 4; i++) E.tick(1000);
  await sleep(10);
  assert.equal(tries(), base, "not yet after 7 s");
  E.tick(1000);
  await sleep(10);
  assert.equal(tries(), base + 1, "tried again after 8 s");
});
