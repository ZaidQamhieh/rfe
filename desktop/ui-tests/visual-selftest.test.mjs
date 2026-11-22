// Proves the screenshot-diff gate can tell things apart: identical frames give 0%, a 1 px shift gives a measured
// noise level, and today's real window against the v5 mockup is far above the 1% limit (so the gate can fail).
// Always on; skips cleanly when chromium or the npm packages are missing (see visual-lib.mjs).
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { skipReason, shot, compare, shifted, save, closeBrowser } from "./visual-lib.mjs";

const skip = skipReason() || false;
after(() => closeBrowser());
const SPEC = { screen: "overview", theme: "light", state: "full" };
const mock = (o = {}) => ({ kind: "mockup", ...SPEC, ...o });
const real = (o = {}) => ({ kind: "harness", ...SPEC, ...o });

test("mockup against the same mockup rendered again: 0% different, in every region", { skip }, async () => {
  const a = await shot(mock()), b = await shot(mock());
  const r = compare(a, b);
  save("selftest-mockup-vs-mockup.png", r.png);
  console.log(`# same-vs-same: ${r.diff} px = ${r.pct.toFixed(4)}% regions ${JSON.stringify(r.regions)}`);
  assert.equal(r.width, 1280); assert.equal(r.height, 800);
  assert.equal(r.diff, 0);
  for (const v of Object.values(r.regions)) assert.equal(v, 0);
});

test("mockup against itself shifted 1 px: the noise a sub-pixel layout difference makes is measured", { skip }, async () => {
  const a = await shot(mock());
  const r = compare(a, shifted(a, 1, 1));
  save("selftest-mockup-vs-shifted.png", r.png);
  console.log(`# 1px-shift noise: ${r.diff} px = ${r.pct.toFixed(4)}% regions ${JSON.stringify(Object.fromEntries(Object.entries(r.regions).map(([k, v]) => [k, +v.toFixed(3)])))}`);
  /* Measured noise (chromium 1248, overview/light/full, 1280x800): a 1 px offset of the whole frame makes 28086 px =
     2.74% of the frame differ (sidebar 2.92%, top bar 2.64%, stage 1.84%, right panel 4.50%). So a layout that is
     off by even 1 px anywhere is already over the 1% frame and 2% region limits: the limits demand pixel-exact
     placement, and the 1% / 2% thresholds are NOT loosened for font noise. Identical renders differ by exactly 0. */
  assert.ok(r.pct > 0, "a 1 px shift must register as a difference");
  assert.ok(r.pct < 100);
});

test("mockup against today's real window: far above the 1% limit, so the gate can fail", { skip }, async () => {
  /* "files" is the one screen both have today: the window has no Overview or Show-all page yet. */
  const a = await shot(mock({ screen: "files" })), b = await shot(real({ screen: "files" }));
  const r = compare(a, b);
  save("selftest-mockup-vs-harness.png", r.png); save("selftest-harness.png", b); save("selftest-mockup.png", a);
  console.log(`# mockup-vs-harness today: ${r.diff} px = ${r.pct.toFixed(2)}% regions ${JSON.stringify(Object.fromEntries(Object.entries(r.regions).map(([k, v]) => [k, +v.toFixed(2)])))}`);
  assert.ok(r.pct > 1, `expected more than 1% difference today, got ${r.pct.toFixed(2)}%`);
});
