// The v5 screenshot-diff gate: the real window must match the v5 mockup, screen by screen.
// For each screen x theme x state: pixelmatch (threshold 0.1, anti-aliased pixels not counted) of the mockup against
// the window (ui-dev/harness.html?seed=v5) at 1280x800. It fails above 1% of the frame, or above 2% in any of four
// regions (sidebar x0-240, top bar 54px, stage, right panel 340px). Diff images go to /tmp/claude-1000/visual-out/.
// Computed-style checks: sidebar 240px wide, buttons 36px high with 8px radius, dialog radius 16px, body 14px Figtree.
//
// The port is not finished, so this file runs only the screens named in RFE_VISUAL_SCREENS (comma list of the keys
// of SCREENS in visual-lib.mjs, or "all"); with the variable unset it reports every test as skipped. Optional:
// RFE_VISUAL_THEMES=light,dark and RFE_VISUAL_STATES=full,empty narrow the matrix (default both of each).
//   RFE_VISUAL_SCREENS=files,servers node --test visual-gate.test.mjs
// The 1% / 2% limits are the owner's; the measured noise floor is in visual-selftest.test.mjs.
import { test, after } from "node:test";
import assert from "node:assert/strict";
import { skipReason, shot, compare, save, styleFacts, closeBrowser, SCREENS } from "./visual-lib.mjs";

const list = (v, dflt) => (v ? v.split(",").map((s) => s.trim()).filter(Boolean) : dflt);
const asked = list(process.env.RFE_VISUAL_SCREENS, []);
const screens = asked.includes("all") ? Object.keys(SCREENS) : asked;
const themes = list(process.env.RFE_VISUAL_THEMES, ["light", "dark"]);
const states = list(process.env.RFE_VISUAL_STATES, ["full", "empty"]);
const why = !screens.length ? "RFE_VISUAL_SCREENS is not set (the port is not finished; name the screens to gate)" : skipReason();
const skip = why || false;
after(() => closeBrowser());

if (skip) test("visual gate", { skip }, () => {});
else {
  for (const screen of screens) {
    assert.ok(SCREENS[screen], `RFE_VISUAL_SCREENS names an unknown screen "${screen}"; known: ${Object.keys(SCREENS).join(", ")}`);
    for (const theme of themes) for (const state of states) {
      test(`${screen} / ${theme} / ${state}: window matches the mockup`, async () => {
        const spec = { screen, theme, state };
        const [a, b] = [await shot({ kind: "mockup", ...spec }), await shot({ kind: "harness", ...spec })];
        const r = compare(a, b), tag = `${screen}-${theme}-${state}`;
        const p = save(`${tag}-diff.png`, r.png); save(`${tag}-mockup.png`, a); save(`${tag}-window.png`, b);
        const reg = Object.entries(r.regions).map(([k, v]) => `${k} ${v.toFixed(2)}%`).join(", ");
        assert.ok(r.pct <= 1, `${tag}: ${r.pct.toFixed(2)}% of the frame differs (limit 1%); regions: ${reg}; diff ${p}`);
        for (const [k, v] of Object.entries(r.regions)) assert.ok(v <= 2, `${tag}: the ${k} region differs by ${v.toFixed(2)}% (limit 2%); frame ${r.pct.toFixed(2)}%; diff ${p}`);
      });
    }
    test(`${screen}: computed styles of the window`, async () => {
      const f = await styleFacts({ kind: "harness", screen, theme: "light", state: "full" });
      assert.equal(f.sidebarWidth, 240, "sidebar width");
      /* a screen with no regular (non-small) button has nothing to measure */
      if (f.buttonHeight !== null) { assert.equal(f.buttonHeight, 36, "button height"); assert.equal(f.buttonRadius, 8, "button radius"); }
      if (/newconn|keychanged|srvset/.test(screen)) assert.equal(f.dialogRadius, 16, "dialog radius");
      assert.equal(f.bodyFontSize, 14, "body font size");
      assert.match(f.bodyFontFamily, /^"?Figtree/, "body font family");
    });
  }
}
