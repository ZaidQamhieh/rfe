// The shell regions of the v5 screenshot-diff gate: the real window's sidebar (x 0-240) and top bar (54 px high,
// right of the sidebar) against the v5 mockup's, on the Files screen (the one screen both have), light and dark,
// full (the mockup's four sample computers) and empty (no computer). Stage and right panel are other steps' work.
//
// Each region is held to 1%, half the gate's 2% limit (visual-gate.test.mjs). Noise floor (visual-selftest.test.mjs): the
// mockup rendered twice differs by exactly 0 px, and the same frame shifted 1 px by 2.92% (sidebar) / 2.64% (top bar),
// so any placement error of 1 px is far over the limit. Measured today (both pages draw the shipped Figtree): sidebar
// 0.34-0.46%, top bar 0.26-0.30%; what remains is data the real window does not have (the sample counts 2 and 3, a computer
// reading "needs OK", the unread dot). Diff images go to /tmp/claude-1000/visual-out/shell-*.png.
//   node --test visual-shell.test.mjs
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { skipReason, shot, compare, save, closeBrowser } from "./visual-lib.mjs";

const skip = skipReason() || false;
after(() => closeBrowser());
const LIMIT = 1;
/* The owner moved Search out of the sidebar into a top bar button (2026-11-08), so the sidebar has one item fewer than the mockup's; its limit is the gate's 2%. */
const LIMITS = { sidebar: 2, top: LIMIT };

for (const theme of ["light", "dark"]) for (const state of ["full", "empty"]) {
  test(`shell / ${theme} / ${state}: sidebar and top bar match the mockup within ${LIMIT}%`, { skip }, async () => {
    const spec = { screen: "files", theme, state };
    const [a, b] = [await shot({ kind: "mockup", ...spec }), await shot({ kind: "harness", ...spec })];
    const r = compare(a, b), tag = `shell-${theme}-${state}`;
    const p = save(`${tag}-diff.png`, r.png); save(`${tag}-mockup.png`, a); save(`${tag}-window.png`, b);
    console.log(`# ${tag}: sidebar ${r.regions.sidebar.toFixed(3)}%, top bar ${r.regions.top.toFixed(3)}% (limit ${LIMIT}%) diff ${p}`);
    for (const k of ["sidebar", "top"]) assert.ok(r.regions[k] <= LIMITS[k], `${tag}: the ${k} region differs by ${r.regions[k].toFixed(3)}% (limit ${LIMITS[k]}%); diff ${p}`);
  });
}
