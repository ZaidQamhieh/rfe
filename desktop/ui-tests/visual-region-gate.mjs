// Shared body of the per-screen region gates (visual-devices.test.mjs, visual-computers.test.mjs); the pattern of visual-files.test.mjs.
// The mockup and the real window (ui-dev/harness.html?seed=v5) are rendered by the same chromium at 1280x800 and compared with the gate of
// visual-lib (pixelmatch, threshold 0.1, anti-aliased pixels not counted). Gated: the STAGE and the RIGHT panel, each at most LIMIT percent
// of its pixels (the owner's 2%, not loosened). The sidebar and top bar belong to the shell gate (visual-shell.test.mjs) and are only reported.
// The mockup is drawn with the window's graphite accent (GRAPHITE_ACCENT_CSS: the owner's palette choice, see visual-lib); the numbers for the
// mockup as drawn (olive) are printed beside. Every number is printed, pass or fail; the images go to OUT_DIR as <tag>-{mockup,window,diff}.png.
// The gate runs only when RFE_VISUAL_SCREENS names the screen (one of `names`), like the other port gates; otherwise it reports one skipped test.
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { skipReason, shot, compare, save, closeBrowser, GRAPHITE_ACCENT_CSS } from "./visual-lib.mjs";

export const LIMIT = 2;

/* Defines the four tests (light/dark x full/empty) of one screen. key: the SCREENS entry of visual-lib; names: the RFE_VISUAL_SCREENS words that ask for it. */
export function defineRegionGate({ key, names, label }) {
  const asked = (process.env.RFE_VISUAL_SCREENS || "").split(",").map((x) => x.trim());
  const why = !names.some((n) => asked.includes(n)) ? `RFE_VISUAL_SCREENS does not name ${names.join(" or ")} (the port is not finished; the gate runs on request)` : skipReason();
  after(() => closeBrowser());
  if (why) { test(`visual ${label} gate`, { skip: why }, () => {}); return; }
  for (const theme of ["light", "dark"]) for (const state of ["full", "empty"]) {
    test(`${label} / ${theme} / ${state}: stage and right panel match the mockup`, async () => {
      const tag = `${label}-${theme}-${state}`, spec = { screen: key, theme, state };
      const b = await shot({ kind: "harness", ...spec });
      const raw = compare(await shot({ kind: "mockup", ...spec }), b);
      const a = await shot({ kind: "mockup", ...spec, css: GRAPHITE_ACCENT_CSS[theme] });
      const r = compare(a, b);
      const p = save(`${tag}-diff.png`, r.png); save(`${tag}-mockup.png`, a); save(`${tag}-window.png`, b);
      const reg = Object.entries(r.regions).map(([k, v]) => `${k} ${v.toFixed(2)}%`).join(", ");
      console.log(`# ${tag} (mockup in olive accent, as drawn): stage ${raw.regions.stage.toFixed(2)}%, right ${raw.regions.right.toFixed(2)}%`);
      console.log(`# ${tag}: frame ${r.pct.toFixed(2)}%; regions: ${reg}; diff ${p}`);
      for (const [k, what] of [["stage", "the stage region"], ["right", "the right panel"]]) {
        assert.ok(r.regions[k] <= LIMIT, `${tag}: ${what} differs by ${r.regions[k].toFixed(2)}% (limit ${LIMIT}%, over by ${(r.regions[k] - LIMIT).toFixed(2)}); ${reg}; diff ${p}`);
      }
    });
  }
}
