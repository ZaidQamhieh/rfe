// Text and control colours reach WCAG AA (4.5:1 for text) on the surfaces they are drawn on, in both
// themes. The tokens are read from ui/app.css, so a changed colour is checked without editing this.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", "ui", "app.css"), "utf8");

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
];

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
