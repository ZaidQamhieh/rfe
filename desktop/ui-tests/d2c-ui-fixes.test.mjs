// Window fixes in the v5 pages: Files tree clicks only act on the Files page; a Shift range is the rows as drawn; "show in
// folder" matches a start folder at a path boundary; Back opens the first level too; the recent-file thumbnail goes to its own
// tile; the search panel's step timer stops when it closes; the size filter uses the megabyte the sizes are shown in; a size
// near a unit boundary gets the next unit; "Moved today" survives Clear; the Overview's timer stops on other pages.
//
//   node --test desktop/ui-tests/d2c-ui-fixes.test.mjs

import { test, afterEach } from "node:test";
import assert from "node:assert/strict";
import { JSDOM, VirtualConsole } from "jsdom";
import { webcrypto } from "node:crypto";
import { fileURLToPath } from "node:url";

const page = fileURLToPath(new URL("../ui-dev/harness.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const live = [];
afterEach(() => { for (const w of live.splice(0)) { try { w.close(); } catch { /* already closed */ } } });

/** The real window scripts on the stub core; timers set with setInterval are recorded so a test can see them stop. */
async function open() {
  const errs = [], ivs = [], cleared = new Set();
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errs.push("script: " + ((e.detail && e.detail.stack) || e.message).split("\n").slice(0, 3).join(" | ")));
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
      const si = w.setInterval.bind(w), ci = w.clearInterval.bind(w);
      w.setInterval = (fn, ms, ...a) => { const id = si(fn, ms, ...a); ivs.push({ id, fn }); return id; };
      w.clearInterval = (id) => { cleared.add(id); return ci(id); };
    },
  });
  const w = dom.window;
  live.push(w);
  for (let i = 0; i < 100 && !(w.A && w.A.ready && w.A.E.servers.length); i++) await sleep(30);
  await sleep(250);
  return { w, d: w.document, A: w.A, E: w.A.E, errs, ivs, cleared };
}
const click = (w, el, o = {}) => el.dispatchEvent(new w.MouseEvent("click", { bubbles: true, cancelable: true, ...o }));
const key = (h, p) => h + "\u0001" + p;
const result = (name, b, extra = {}) => ({ host: "local", dir: "/a", path: "/a/" + name, node: { n: name, t: "file", b, mod: Date.now(), perm: "-rw-r--r--" }, ...extra });
const searchRun = (results) => ({ q: "f", scope: "all", filter: "all", results, scanned: results.length, done: true, tok: 1, hosts: ["local"], skipped: [] });

test("1. a click on a row of another page (Overview recent files) does not reach the Files tree", async () => {
  const { w, d, A } = await open();
  const T = A.v5Files, p = A.mainPane;
  const idx = T.rows.findIndex((r) => (r.k === "dir" || r.k === "file") && !(p.host === r.h && p.path === r.dir && p.sel.has(r.n.n)));
  assert.ok(idx >= 0, "the tree draws at least one folder or file row");
  const r = T.rows[idx];
  A.go("home"); await sleep(150);
  const before = JSON.stringify([p.host, p.path, [...p.sel]]);
  const fake = d.createElement("div"); fake.setAttribute("data-x", "rec"); fake.setAttribute("data-i", String(idx)); d.getElementById("stage").appendChild(fake);
  click(w, fake); await sleep(50);
  assert.equal(JSON.stringify([p.host, p.path, [...p.sel]]), before, "the Files selection is untouched by a click on " + r.n.n);
});

test("2. a Shift range selects the rows between the anchor and the click as they are drawn", async () => {
  const { w, d, A, E } = await open();
  const T = A.v5Files, p = A.mainPane;
  const h = E.servers[0].id;
  for (const q of ["/srv", "/srv/projects"]) T.open.add(key(h, q));
  T.paint(); await sleep(400);
  const group = new Map();
  for (const r of T.rows) if ((r.k === "dir" || r.k === "file") && !r.root) { const k = r.h + "|" + r.dir; group.set(k, (group.get(k) || []).concat(r)); }
  const rows = [...group.values()].find((g) => g.length >= 3);
  assert.ok(rows, "an open folder with three rows is drawn: " + [...group.keys()].join(" ; "));
  const { dir } = rows[0];
  const idx = (r) => T.rows.findIndex((x) => x.h === r.h && x.dir === r.dir && x.n && x.n.n === r.n.n && x.k === r.k);
  const tile = (r) => d.querySelector('.tr .n[data-i="' + idx(r) + '"]');
  click(w, tile(rows[0])); await sleep(50);
  /* the pane's own order (here by name, last first) is not the drawn one; the range must follow what is drawn */
  const byName = [...rows].sort((a, b) => (a.n.n < b.n.n ? 1 : -1)).map((r) => r.n.n);
  assert.notDeepEqual(byName.slice(0, 3), rows.slice(0, 3).map((r) => r.n.n), "the test needs a pane order that differs from the drawn one");
  p.sort = { key: "name", dir: -1 }; p.refreshItems();
  click(w, tile(rows[2]), { shiftKey: true }); await sleep(50);
  assert.equal(p.host, h); assert.equal(p.path, dir);
  assert.deepEqual([...p.sel].sort(), rows.slice(0, 3).map((r) => r.n.n).sort(), "the three rows drawn from the anchor to the click");
});

test("3. \"show in folder\" does not take a start folder whose name only begins the same (/home/u vs /home/user2)", async () => {
  const { A, E } = await open();
  const T = A.v5Files, h = E.servers[0].id, s = E.server(h);
  s.virtualRoot = null; const real = E.start; E.start = (x) => (x === h ? "/home/u" : real(x));
  await T.reveal(h, "/home/user2/docs", null);
  const keys = [...T.open];
  assert.ok(!keys.some((k) => k.includes("/home/u/er2")), "no folder named er2 under /home/u: " + keys.join(" | "));
  assert.ok(keys.includes(key(h, "/home/user2")), "the real folder is opened");
});

test("4. Back to a place opens the first level of the path too (a computer whose row is the root)", async () => {
  const { w, d, A, E } = await open();
  const T = A.v5Files, h = E.servers[0].id;
  A.mainPane.host = h; T.open.delete(key(h, "/"));
  T.hist.push({ h, dir: "/data/x", names: [] }); T.paintTop();
  const b = d.querySelector('#ftop [data-top="back"]'); assert.ok(b && !b.disabled, "the Back button is on");
  click(w, b); await sleep(50);
  assert.ok(T.open.has(key(h, "/")), "the root level is open");
  assert.ok(T.open.has(key(h, "/data")) && T.open.has(key(h, "/data/x")));
});

test("5. a recent-file thumbnail goes to the tile that shows its file even when the list was reordered meanwhile", async () => {
  const { A, E, d } = await open();
  const V = A.v5Pages, h = E.servers[0].id;
  A.go("home"); await sleep(200);
  const pending = {}; const real = E.call;
  E.call = (cmd, a) => (cmd === "files_thumb" ? new Promise((res) => { pending[a.path] = res; }) : real.call(E, cmd, a));
  const A1 = { host: h, name: "a.png", path: "/p/a.png", modified: "2026-01-01T00:00:00Z" }, B1 = { host: h, name: "b.png", path: "/p/b.png", modified: "2026-01-02T00:00:00Z" };
  V.recent.list = [A1, B1]; E.emit("servers"); await sleep(150);
  assert.ok(pending["/p/a.png"], "the thumbnail of a.png was asked for");
  V.recent.list = [B1, A1]; /* the list changed order; the tiles are not redrawn yet */
  pending["/p/a.png"]("data:image/png;base64,QUFB");
  await sleep(30);
  const tiles = [...d.querySelectorAll(".rcf")];
  assert.equal(tiles.length, 2);
  assert.ok(!tiles[0].querySelector("img"), "the tile that now shows b.png got no picture");
  assert.ok(tiles[1].querySelector("img"), "the tile that shows a.png got its own");
  E.call = real;
});

test("6. closing the search panel stops its search steps; opening it picks them up again", async () => {
  const { A, E } = await open();
  const V = A.v5Pages, P = V.panel; let steps = 0;
  const real = E.makeSearch;
  E.makeSearch = (...a) => { const j = real(...a); const st = j.step; j.step = (...b) => { steps++; return st(...b); }; return j; };
  A.T.step = 80; A.T.stepGap = 20;
  P.open("zzz"); P.close();
  await sleep(250);
  assert.equal(steps, 0, "no step ran after the panel was closed");
  const sr = A.searchRun; assert.equal(sr.done, false);
  P.open(); await sleep(400);
  assert.ok(steps > 0 && sr.done, "opening the panel again finished the search");
});

test("7. the size filter's megabyte is the decimal one the sizes show; 999.96 KB reads 1 MB, not 1,000 KB", async () => {
  const { w, d, A } = await open();
  A.searchRun = searchRun([result("f.bin", 1040000), result("g.bin", 999960)]);
  A.go("search"); await sleep(150);
  const rows = () => [...d.querySelectorAll('#stage [data-x="srow"]')].filter((c, i, a) => a.findIndex((x) => x.dataset.i === c.dataset.i) === i).map((c) => c.dataset.i);
  assert.equal(rows().length, 2);
  const text = d.getElementById("stage").textContent;
  assert.ok(text.includes("1 MB"), "999.96 KB is shown as 1 MB: " + text.slice(0, 300));
  assert.ok(!text.includes("1,000 KB"));
  const sel = d.querySelector('#stage [data-rf="size"]'); assert.ok(sel, "the size filter is there");
  sel.value = "Over 1 MB"; sel.dispatchEvent(new w.Event("change", { bubbles: true })); await sleep(50);
  assert.deepEqual(rows(), ["0"], "1.04 MB passes Over 1 MB; 999.96 KB (shown as 1 MB) does not");
});

test("8. Moved today keeps what finished transfers moved after the list is cleared", async () => {
  const { A, E, d } = await open();
  A.go("home"); await sleep(200);
  const moved = () => { const s = [...d.querySelectorAll(".stat")].find((x) => x.textContent.includes("Moved today")); return s.querySelector("b").textContent; };
  const t = { id: "tx-1", state: "done", bytes: 5e6, done: 5e6, rids: [], host: E.servers[0].id, dir: "down", name: "x.bin", speed: 0 };
  E.tasks.push(t); await sleep(400); /* the clock ticks and sees it */
  E.tasks.splice(E.tasks.indexOf(t), 1); E.emit("transfers"); await sleep(150);
  A.go("transfers"); A.go("home"); await sleep(200);
  assert.match(moved(), /^5(\.0+)? MB$/);
});

test("9. the Overview's timer stops on another page, starts again on return, and skips a patch the engine's tick just made", async () => {
  const { A, E, d, ivs, cleared } = await open();
  A.go("home"); await sleep(200);
  const home = () => ivs.filter((i) => /patchHome/.test(i.fn.toString()) && !cleared.has(i.id));
  assert.equal(home().length, 1, "one timer while the Overview is open");
  const chart = d.querySelector('#stage [data-f="chart"]'); assert.ok(chart);
  const mo = new d.defaultView.MutationObserver(() => {}); mo.observe(chart, { childList: true });
  E.emit("tick"); const afterTick = mo.takeRecords().length; assert.ok(afterTick > 0, "the tick patches the chart");
  home()[0].fn(); assert.equal(mo.takeRecords().length, 0, "the timer does not patch it again right after");
  mo.disconnect();
  const old = home()[0];
  A.go("transfers"); await sleep(50);
  old.fn();
  assert.ok(cleared.has(old.id), "the timer cleared itself on another page");
  A.go("home"); await sleep(200);
  assert.equal(home().length, 1, "and runs again on return");
});
