// Text and control colours reach WCAG AA (4.5:1 for text) on the surfaces they are drawn on, in both
// themes. The tokens are read from ui/app.css, so a changed colour is checked without editing this.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { readAllCss } from "./css-lib.mjs";
const css = readAllCss();

function tokens(selector) {
  const start = css.indexOf(selector + "{");
  const body = css.slice(start + selector.length + 1, css.indexOf("\n}", start));
  const out = {};
  for (const m of body.matchAll(/--([a-z0-9-]+):\s*(#[0-9a-fA-F]{3,6})\b/g)) out[m[1]] = m[2];
  return out;
}

const light = tokens(":root");
const dark = { ...light, ...tokens(':root[data-theme="dark"]') };

function rgb(hex) {
  let h = hex.slice(1);
  if (h.length === 3) h = [...h].map((c) => c + c).join("");
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16) / 255);
}
function luminance(hex) {
  const [r, g, b] = rgb(hex).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
const ratio = (a, b) => {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
};

// [foreground, background, minimum]: text is 4.5, a control's outline or icon is 3.
const PAIRS = [
  ["on-surface", "surface", 4.5],
  ["on-surface", "sc", 4.5],
  ["on-surface", "sc-high", 4.5],
  ["on-var", "surface", 4.5],
  ["on-var", "sc", 4.5],
  ["on-var", "sc-high", 4.5],
  ["primary", "surface", 4.5],
  ["on-primary", "primary", 4.5],
  ["on-pc", "pc", 4.5],
  ["on-sec-c", "sec-c", 4.5],
  ["on-tert-c", "tert-c", 4.5],
  ["err", "surface", 4.5],
  ["on-err-c", "err-c", 4.5],
  ["warn", "surface", 4.5],
  ["on-warn-c", "warn-c", 4.5],
  ["ok", "surface", 4.5],
  ["snack-on", "snack", 4.5],
  ["outline", "surface", 3],
  // the accent as a filled control (white text on it) and as text on every surface
  ["on-accent", "accent", 4.5],
  ["primary", "sc", 4.5],
  ["primary", "sc-high", 4.5],
  ["primary", "sec-c", 4.5],
  ["primary", "pc", 4.5],
  // the one selected-row style: --sec-c fill, so its text and the status colours must read on it
  ["on-sec-c", "sec-c", 4.5],
  ["on-surface", "sec-c", 4.5],
  ["on-var", "sec-c", 4.5],
  ["err", "sec-c", 4.5],
  ["warn", "sec-c", 4.5],
  ["sel-bd", "surface", 1.5],
  // muted text and the status colours on cards and raised surfaces
  ["on-var", "sc-top", 4.5],
  ["outline", "sc", 3],
  ["outline", "sc-high", 3],
  ["err", "sc", 4.5],
  ["err", "sc-high", 4.5],
  ["warn", "sc", 4.5],
  ["warn", "sc-high", 4.5],
  ["ok", "sc", 4.5],
  ["ok", "sc-high", 4.5],
  // the destructive button, one style in both themes
  ["on-err", "err-fill", 4.5],
  // warning and error banners
  ["on-surface", "warn-bg", 4.5],
  ["warn", "warn-bg", 4.5],
  ["on-surface", "err-bg", 4.5],
  ["err", "err-bg", 4.5],
  // the amber "needs OK" dot and the counts that sit next to it
  ["amber-dot", "surface", 3],
  ["amber-dot", "sc-high", 3],
  ["on-var", "sc-high", 4.5],
  // toasts and the bulk bar
  ["snack-link", "snack", 4.5],
  ["snack-err", "snack", 4.5],
  ["snack-danger", "snack", 4.5],
];

// The dark block must restate every colour token the light block defines, so no light colour leaks into dark mode.
test("dark theme overrides every colour token of the light theme", () => {
  const darkOnly = tokens(':root[data-theme="dark"]');
  assert.deepEqual(Object.keys(light).filter((k) => !(k in darkOnly)), []);
});

// One accent, shared: the app icon's graphite (a white key on graphite). Neutral in both themes: dark ink on light, near-white on dark.
test("the accent is the icon's graphite: neutral, dark on light and light on dark", () => {
  for (const [n, tk] of [["light", light], ["dark", dark]]) {
    for (const key of ["accent", "primary"]) {
      const [r, g, b] = rgb(tk[key]);
      assert.ok(Math.max(r, g, b) - Math.min(r, g, b) <= 0.03, `${n} --${key} ${tk[key]} is not neutral`);
    }
  }
  assert.ok(rgb(light.accent)[0] < 0.3, "light accent is graphite");
  assert.ok(rgb(dark.accent)[0] > 0.8, "dark accent is near-white");
});

// Selected rows, destructive buttons and counts share one definition each: no coloured stripe, no glow, no gradient.
test("selected rows use the one neutral style (fill + thin border), counts are neutral", () => {
  const sel = [".li.sel", ".tile.sel", ".trr.sel", ".pal .it.hi", ".notif .ni.unread"];
  for (const s of sel) {
    const m = css.match(new RegExp(s.replace(/[.]/g, "\\.") + "\\{([^}]*)\\}"));
    assert.ok(m, `${s} rule missing`);
    assert.match(m[1], /background:var\(--sec-c\)/, s);
    assert.match(m[1], /box-shadow:inset 0 0 0 1px var\(--sel-bd\)/, s);
    assert.doesNotMatch(m[1], /border-left|var\(--primary\)|var\(--accent\)|gradient/, s);
  }
  for (const s of [".ib .bd", ".cnt"]) {
    const m = css.match(new RegExp(s.replace(/[.]/g, "\\.") + "\\{([^}]*)\\}"));
    assert.ok(m, `${s} rule missing`);
    assert.doesNotMatch(m[1], /var\(--(err|primary|accent)\)/, `${s} must be a neutral count`);
  }
  assert.match(css, /\.btn\.ed\{[^}]*background:var\(--err-fill\)/, "the destructive button uses --err-fill");
});

for (const [name, tk] of [["light", light], ["dark", dark]]) {
  test(`${name} theme: text and controls reach WCAG AA`, () => {
    const bad = [];
    for (const [fg, bg, min] of PAIRS) {
      assert.ok(tk[fg] && tk[bg], `token --${fg} or --${bg} is missing`);
      const r = ratio(tk[fg], tk[bg]);
      if (r < min) bad.push(`--${fg} on --${bg}: ${r.toFixed(2)}:1, needs ${min}:1`);
    }
    assert.deepEqual(bad, []);
  });
}
