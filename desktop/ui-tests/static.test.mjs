// Things about the window's files that no behaviour test would notice: the page must run under the
// app's content security policy (no inline script, no network), every script is loaded, both
// themes define the same colours, and the window cannot be sized below what the layout needs.
//
//   node --test desktop/ui-tests/*.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";

const ui = new URL("../ui/", import.meta.url);
const read = (f) => readFileSync(new URL(f, ui), "utf8");
const conf = JSON.parse(readFileSync(new URL("../src-tauri/tauri.conf.json", import.meta.url), "utf8"));
const html = read("index.html");
const scripts = readdirSync(ui).filter((f) => f.endsWith(".js"));

test("the content security policy allows no inline code, no eval and no network", () => {
  const csp = conf.app.security.csp;
  assert.doesNotMatch(csp, /unsafe-inline|unsafe-eval|https?:\/\/(?!ipc\.localhost)/);
  assert.match(csp, /default-src 'self'/);
  assert.match(csp, /connect-src ipc: http:\/\/ipc\.localhost(;|$)/);
  assert.match(csp, /style-src 'self'(;|$)/);
});

test("index.html has no inline script, style or event handler and no outside URL", () => {
  assert.doesNotMatch(html, /<script(?![^>]*\ssrc=)/i);
  assert.doesNotMatch(html, /<style/i);
  assert.doesNotMatch(html, /\son[a-z]+\s*=/i);
  assert.doesNotMatch(html, /https?:\/\//i);
});

test("every script file is loaded by index.html, in an order where each one's dependencies come first", () => {
  const loaded = [...html.matchAll(/<script src="([^"]+)"/g)].map((m) => m[1]);
  for (const f of loaded) assert.ok(existsSync(new URL(f, ui)), f + " exists");
  assert.deepEqual([...loaded].sort(), [...scripts].sort());
  const order = (f) => loaded.indexOf(f);
  for (const [a, b] of [["csp-shim.js", "ui.js"], ["engine.js", "ui.js"], ["ui.js", "pages.js"], ["pages.js", "features.js"], ["features.js", "fpages.js"], ["fpages.js", "fx2.js"], ["fx4.js", "fx5.js"], ["fx5.js", "fx6.js"]]) assert.ok(order(a) < order(b), a + " before " + b);
});

test("no script reaches the network, evaluates strings or writes a script tag", () => {
  const bad = /\b(fetch|XMLHttpRequest|WebSocket|EventSource|importScripts)\s*\(|\beval\s*\(|new Function\s*\(|document\.write|<script/;
  for (const f of scripts) assert.doesNotMatch(read(f).replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1"), bad, f);
});

test("scripts do not store secrets in the page", () => {
  for (const f of scripts) assert.doesNotMatch(read(f), /localStorage\.setItem\([^)]*(password|token|secret)/i, f);
});

test("the dark theme redefines every colour the light theme defines", () => {
  const css = read("app.css");
  const block = (start) => { let d = 0, i = css.indexOf("{", start); const from = i; for (; i < css.length; i++) { if (css[i] === "{") d++; if (css[i] === "}" && --d === 0) break; } return css.slice(from + 1, i); };
  const vars = (b) => new Map([...b.matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]));
  const light = vars(block(css.indexOf(":root{")));
  const dark = new Map();
  for (const m of css.matchAll(/:root\[data-theme="dark"\]\s*\{/g)) for (const [k, v] of vars(block(m.index))) dark.set(k, v);
  const colour = (v) => /^(#|rgb|hsl)/.test(v);
  const missing = [...light].filter(([k, v]) => colour(v) && !dark.has(k)).map(([k]) => k);
  assert.deepEqual(missing, []);
  assert.ok(light.size > 40);
});

test("the window cannot be made smaller than the layout needs", () => {
  const w = conf.app.windows[0];
  assert.ok(w.minWidth >= 900 && w.minHeight >= 600);
  assert.ok(w.width >= w.minWidth && w.height >= w.minHeight);
});

test("a computer's name is escaped wherever it goes into a dialog body (it can come from the network)", () => {
  for (const f of scripts) {
    read(f).split("\n").forEach((line, i) => {
      if (!/\bbody:/.test(line)) return;
      for (const m of line.matchAll(/(?:hostLabel|hostName)\(/g)) {
        const before = line.slice(0, m.index);
        const open = (before.match(/esc\(/g) || []).length;
        assert.ok(open > 0, `${f}:${i + 1} puts a host name in a dialog body without esc()`);
      }
    });
  }
});

test("the data-fx dispatcher splits at the first | only, so a search text with a | survives", () => {
  assert.doesNotMatch(read("features.js"), /dataset\.fx\.split\('\|'\)/);
});
