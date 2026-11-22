// Screenshot gate for the Files screen (the one tree across the computers + the details panel), light and dark,
// full and empty. The v5 mockup and the real window (ui-dev/harness.html?seed=v5, wearing the v5 wrapper
// "rfe d5" on #app, as the new shell will) are rendered by the same chromium at 1280x800 and compared with the
// gate of visual-lib (pixelmatch, threshold 0.1, anti-aliased pixels not counted).
// Gated here: the STAGE region (x240-940, below the 54px top bar) and the RIGHT panel (340px, below the top bar):
// each must differ by at most 2% of its pixels (the owner's limit, not loosened). The sidebar and the top bar belong
// to the shell and are only reported. Every number is printed, pass or fail, and the three images go to OUT_DIR
// (RFE_VISUAL_OUT, default /tmp/claude-1000/visual-out): files-<theme>-<state>-{mockup,window,diff,before}.png.
// "before" is the same window without the wrapper (the old Files page).
// The window is driven like a person: the folders the mockup shows open are opened through A.v5Files.reveal.
//   node --test visual-files.test.mjs
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { skipReason, shot, compare, save, closeBrowser, GRAPHITE_ACCENT_CSS } from "./visual-lib.mjs";

const asked = (process.env.RFE_VISUAL_SCREENS || "").split(",").map((x) => x.trim());
const why = !asked.includes("files") ? "RFE_VISUAL_SCREENS does not name files (the port is not finished; the gate runs on request)" : skipReason();
after(() => closeBrowser());
const OPEN = `(async () => { const s = A.E.servers[0]; if (s) await A.v5Files.reveal(s.id, "/home/sam/Projects/garden-plan", "north-bed-after.jpg"); })()`;
const LIMIT = 2;
/* Test-only stand-in for the new shell (owned by another agent): the page area sits where the mockup's does, x240..1280 below a
   54px top bar, with no card margin or radius and no transfers dock. It touches only #main and #sheet; the sidebar and top bar stay as they are. */

if (why) test("visual files gate", { skip: why }, () => {});
else {
  for (const theme of ["light", "dark"]) for (const state of ["full", "empty"]) {
    test(`files / ${theme} / ${state}: stage and right panel match the mockup`, async () => {
      const tag = `files-${theme}-${state}`;
      const spec = { screen: "files", theme, state };
      const raw = compare(await shot({ kind: "mockup", ...spec }), await shot({ kind: "harness", ...spec, d5: true, after: state === "full" ? OPEN : "" }));
      /* the mockup wearing the window's graphite accent (the owner's palette choice); the raw numbers above are printed beside it */
      const a = await shot({ kind: "mockup", ...spec, css: GRAPHITE_ACCENT_CSS[theme] });
      const b = await shot({ kind: "harness", ...spec, d5: true, after: state === "full" ? OPEN : "" });
      save(`${tag}-before.png`, await shot({ kind: "harness", ...spec, onboarded: true }));
      const r = compare(a, b);
      const p = save(`${tag}-diff.png`, r.png); save(`${tag}-mockup.png`, a); save(`${tag}-window.png`, b);
      const reg = Object.entries(r.regions).map(([k, v]) => `${k} ${v.toFixed(2)}%`).join(", ");
      console.log(`# ${tag} (mockup in olive accent, as drawn): stage ${raw.regions.stage.toFixed(2)}%, right ${raw.regions.right.toFixed(2)}%`);
      console.log(`# ${tag}: frame ${r.pct.toFixed(2)}%; regions: ${reg}; diff ${p}`);
      assert.ok(r.regions.stage <= LIMIT, `${tag}: the stage region differs by ${r.regions.stage.toFixed(2)}% (limit ${LIMIT}%, over by ${(r.regions.stage - LIMIT).toFixed(2)}); ${reg}; diff ${p}`);
      assert.ok(r.regions.right <= LIMIT, `${tag}: the right panel differs by ${r.regions.right.toFixed(2)}% (limit ${LIMIT}%, over by ${(r.regions.right - LIMIT).toFixed(2)}); ${reg}; diff ${p}`);
    });
  }
}
