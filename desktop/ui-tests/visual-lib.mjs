// Screenshot-diff helpers: the v5 mockup and the real window (ui-dev/harness.html on the stub backend, seeded
// with the mockup's own data by ui-dev/v5-seed.js) are rendered by the same chromium at 1280x800, scale 1, and compared.
//
// Needs playwright-core, pixelmatch and pngjs (devDependencies) and the chromium binary
//   ~/.cache/ms-playwright/chromium-1248/chrome-linux64/chrome   (override: RFE_CHROMIUM).
// Without them skipReason() returns a sentence and the tests skip; nothing here throws at import time.
// If the packages live outside ui-tests/node_modules, point RFE_VISUAL_MODULES at the node_modules folder that holds them.
//
// Fonts: both pages ask Google Fonts for Figtree and JetBrains Mono. The network is never used: the CSS request is
// answered with the faces the window ships (ui/css/v5-fonts.css, as data: URLs; see shippedFontCss below), so both pages
// draw Figtree and JetBrains Mono the same way. The font-family *declaration* is still checked by the computed-style test.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const OUT_DIR = process.env.RFE_VISUAL_OUT || "/tmp/claude-1000/visual-out";
export const CHROME = process.env.RFE_CHROMIUM || path.join(os.homedir(), ".cache/ms-playwright/chromium-1248/chrome-linux64/chrome");
export const SIZE = { width: 1280, height: 800 };
const MOCKUP_URL = pathToFileURL(path.join(HERE, "../mockups/v5/01-rfe.html")).href;
const HARNESS_URL = pathToFileURL(path.join(HERE, "../ui-dev/harness.html")).href;

/* The screens the mockup offers (its own view strip, mockups/v5/_nav.js) and how the harness reaches the same one.
   `hash` is what the mockup reads at start (K.mount: go=<view>, dlg=<dialog>&tab=<n>, sr = search panel);
   `harness` is run in the harness page once the window is up: `go` -> A.go(view), `run` -> any expression. */
export const SCREENS = {
  overview: { hash: "", harness: { go: "home" } },
  search: { hash: "sr", harness: { go: "home", run: "A.v5Pages.panel.open(A.E.servers.length ? 'bed' : '')" } },
  results: { hash: "go=results", harness: { go: "home", run: "(async () => { const P = A.v5Pages.panel; P.open(A.E.servers.length ? 'bed' : ''); await new Promise((r) => setTimeout(r, 400)); const sc = document.querySelector('#srp [data-x=\"pscope\"][data-v=\"all\"]'); if (sc) sc.click(); await new Promise((r) => setTimeout(r, 700)); const all = document.querySelector('#srp [data-x=\"pall\"]'); if (all) all.click(); else { P.close(true); A.go('search'); } })()" } },
  files: { hash: "go=files", harness: { go: "files", run: "(async () => { const s = A.E.servers.find((x) => x.name === 'studio-pc'); if (s) { await new Promise((r) => setTimeout(r, 600)); await A.v5Files.reveal(s.id, '/home/sam/Projects/garden-plan', 'north-bed-after.jpg'); } })()" } },
  servers: { hash: "go=servers", harness: { go: "servers" } },
  devices: { hash: "go=devices", harness: { go: "devices", run: "new Promise((r) => setTimeout(r, 900)).then(() => A.loadInbox()).then(() => { if (!A.E.servers.length) return; /* first run: the shipped Devices page is the empty Computers page, there is no this-PC fallback to open */ const d = (A.X.devices || []).find((x) => x.name === 'Pixel 8'); if (d) A.v5Devices.sel = d.id; A.v5Devices.devices(); })" } },
  history: { hash: "go=history", harness: { go: "history", run: "new Promise((r) => setTimeout(r, 900)).then(() => { const H = A.E.history, keep = H.filter((h) => h.v5); H.splice(0, H.length, ...keep); H.unshift = () => H.length; A.v5Devices.hsel = 'v5h4'; A.v5Devices.history(); })" } },
  "newconn-1": { hash: "dlg=newconn&tab=1", harness: { go: "home", run: "A.serverDialog()" } },
  "newconn-2": { hash: "dlg=newconn&tab=2", harness: { go: "home", run: "A.serverDialog()" } },
  "newconn-3": { hash: "dlg=newconn&tab=3", harness: { go: "home", run: "A.serverDialog()" } },
  keychanged: { hash: "dlg=keychanged", harness: { go: "home", run: "(() => { const s = A.E.servers.find((x) => x.name === 'nas'); if (s) A.reverifyDialog(s.id); else A.v5Computers.noPc('A computer changed'); })()" } },
  srvset: { hash: "go=servers&dlg=srvset", harness: { go: "servers", run: "(() => { const s = A.E.servers.find((x) => x.name === 'studio-pc'); A.v5Computers.settings(s); })()" } },
};

let deps = null, depsReason = null, depsTried = false;
/* Loads playwright-core, pixelmatch and pngjs once; on failure remembers why. */
function loadDeps() {
  if (depsTried) return deps;
  depsTried = true;
  const bases = [import.meta.url];
  if (process.env.RFE_VISUAL_MODULES) bases.push(pathToFileURL(path.join(process.env.RFE_VISUAL_MODULES, "x.js")).href);
  let lastErr;
  for (const b of bases) {
    try {
      const req = createRequire(b);
      const pm = req("pixelmatch");
      deps = { chromium: req("playwright-core").chromium, PNG: req("pngjs").PNG, pixelmatch: pm.default || pm };
      return deps;
    } catch (e) { lastErr = e; }
  }
  depsReason = "playwright-core, pixelmatch or pngjs is not installed (" + String(lastErr && lastErr.message).split("\n")[0] + ")";
  return null;
}

/* null when the visual tests can run, else the sentence to give as the skip reason. */
export function skipReason() {
  if (!fs.existsSync(CHROME)) return "chromium binary not found at " + CHROME;
  if (!loadDeps()) return depsReason;
  return null;
}

let browser = null;
/* One shared chromium for the whole test file. */
async function getBrowser() {
  if (!browser) browser = await loadDeps().chromium.launch({ executablePath: CHROME, args: ["--force-device-scale-factor=1", "--font-render-hinting=none", "--disable-lcd-text"] });
  return browser;
}
export async function closeBrowser() { if (browser) { const b = browser; browser = null; await b.close(); } }

const FREEZE_CSS = "html{font-size:16px!important}*{animation:none!important;transition:none!important;caret-color:transparent}";
/* UTC on purpose: the string is read in the machine's zone otherwise, and the window (ui-dev/v5-seed.js NOW, 10:46 in the page's UTC zone) would sit hours away from its own seed. */
const FIXED_TIME = "2026-11-01T10:46:00Z";

/* The mockup asks Google Fonts for Figtree and JetBrains Mono; the window ships both (ui/css/v5-fonts.css). So the mockup is
   answered with the same faces from those shipped files (as data: URLs), and both pages draw text in the same fonts. */
let fontCss = null;
function shippedFontCss() {
  if (fontCss !== null) return fontCss;
  const dir = path.join(HERE, "../ui/css");
  fontCss = fs.readFileSync(path.join(dir, "v5-fonts.css"), "utf8").replace(/url\("([^"]+)"\)/g, (m, rel) => `url("data:font/woff2;base64,${fs.readFileSync(path.join(dir, rel)).toString("base64")}")`);
  return fontCss;
}

/* A fresh page: 1280x800, scale 1, fixed clock, no network (fonts answered locally), animation and caret frozen. */
async function newPage(theme) {
  const ctx = await (await getBrowser()).newContext({ viewport: SIZE, deviceScaleFactor: 1, colorScheme: theme, reducedMotion: "reduce", locale: "en-GB", timezoneId: "UTC" });
  const page = await ctx.newPage();
  await page.clock.setFixedTime(FIXED_TIME);
  await page.route(/fonts\.googleapis\.com/, (r) => r.fulfill({ status: 200, contentType: "text/css", body: shippedFontCss() }));
  await page.route(/fonts\.gstatic\.com/, (r) => r.abort());
  return { ctx, page };
}

const settle = async (page) => { await page.addStyleTag({ content: FREEZE_CSS }); await page.evaluate(() => document.fonts.ready); await page.waitForTimeout(350); };

/* Screenshot of one mockup screen: opened with ?v=full|empty&theme=..#<hash>, the .win element is captured.
   spec.css: extra test-only CSS for the mockup page (the mockup files themselves are never touched). */
async function shotMockup({ screen, theme, state, css }) {
  const sc = SCREENS[screen]; if (!sc) throw new Error("unknown screen " + screen);
  const { ctx, page } = await newPage(theme);
  try {
    await page.goto(`${MOCKUP_URL}?v=${state}&theme=${theme}${sc.hash ? "#" + sc.hash : ""}`);
    await page.waitForSelector(".win .main .stage", { state: "attached" });
    if (css) await page.addStyleTag({ content: css });
    await settle(page);
    return await page.locator(".win").screenshot({ animations: "disabled" });
  } finally { await ctx.close(); }
}

/* Screenshot of the same screen in the real window: harness.html?fast=1&seed=v5[-empty], theme set on <html>, A.go(view). */
async function shotHarness(spec) {
  const { screen, theme, state } = spec;
  const sc = SCREENS[screen]; if (!sc) throw new Error("unknown screen " + screen);
  const { ctx, page } = await newPage(theme);
  try {
    /* The mockup opens with its sidebar expanded; the window keeps the choice in its settings, so ask for expanded unless a setting exists. */
    await ctx.addInitScript(() => { try { if (!localStorage.getItem("rfe-tonal-settings-v1")) localStorage.setItem("rfe-tonal-settings-v1", JSON.stringify({ railWide: true, onboarded: true })); } catch (e) { /* no storage */ } });
    const seed = state === "empty" ? "v5-empty" : "v5";
    /* spec.onboarded: the first-run welcome is already dismissed (otherwise it covers the empty screens). */
    if (spec.onboarded) await page.addInitScript(() => { try { localStorage.setItem("rfe-tonal-settings-v1", JSON.stringify({ onboarded: true })); } catch (e) { /* no storage */ } });
    await page.goto(`${HARNESS_URL}?fast=1&seed=${seed}`);
    await page.waitForFunction(() => typeof A !== "undefined" && !!document.querySelector("#stage") && document.querySelector("#stage").children.length > 0, null, { timeout: 15000 });
    await page.evaluate((t) => { document.documentElement.dataset.theme = t; }, theme);
    await settle(page);
    const h = sc.harness;
    /* spec.d5: the window wears the v5 wrapper ("rfe d5" on #app) before the screen opens, as the new shell will; spec.css: extra test-only CSS;
       spec.after: an async expression run once the screen is open (e.g. open the folders the mockup shows). */
    if (spec.d5) await page.evaluate(() => document.getElementById("app").classList.add("rfe", "d5"));
    if (spec.css) await page.addStyleTag({ content: spec.css });
    if (h.go) {
      try { await page.evaluate((v) => A.go(v), h.go); }
      catch (e) { throw new Error(`the window cannot show "${h.go}" yet: ${String(e.message).split("\n")[0]}`); }
    }
    if (h.run) await page.evaluate((c) => (0, eval)(c), h.run);
    if (spec.after) await page.evaluate((c) => (0, eval)(c), spec.after);
    await settle(page);
    return await page.screenshot({ clip: { x: 0, y: 0, ...SIZE }, animations: "disabled" });
  } finally { await ctx.close(); }
}

/* The window's accent is the owner's graphite (ui/app.css --accent, --primary, --on-accent; commit cc3bc658, 2026-11-09), the mockup's is olive.
   Used as spec.css on the mockup page, these give the mockup the window's accent so a gate measures layout and type, not that one palette
   decision. The mockup's --ac-soft is kept (the window uses the same value). Keep in step with app.css. */
export const GRAPHITE_ACCENT_CSS = {
  light: ".win.d5{--ac:#262626;--on-ac:#fff;--ac-ink:#1a1a1a}",
  dark: '.win.d5[data-theme="dark"]{--ac:#ececea;--on-ac:#0e0e0e;--ac-ink:#f2f2f0}',
};

/* spec: { kind: "mockup" | "harness", screen, theme: "light"|"dark", state: "full"|"empty" } -> PNG buffer. */
export function shot(spec) { return spec.kind === "mockup" ? shotMockup(spec) : shotHarness(spec); }

/* Opens a page on a screen and returns the measured style facts used by the computed-style checks (see visual-gate). */
export async function styleFacts(spec) {
  const { ctx, page } = await newPage(spec.theme);
  try {
    const sc = SCREENS[spec.screen];
    const mock = spec.kind === "mockup";
    if (!(spec.kind === "mockup")) await ctx.addInitScript(() => { try { if (!localStorage.getItem("rfe-tonal-settings-v1")) localStorage.setItem("rfe-tonal-settings-v1", JSON.stringify({ railWide: true, onboarded: true })); } catch (e) { /* no storage */ } });
    if (mock) { await page.goto(`${MOCKUP_URL}?v=${spec.state}&theme=${spec.theme}${sc.hash ? "#" + sc.hash : ""}`); await page.waitForSelector(".win .main .stage", { state: "attached" }); }
    else {
      await page.goto(`${HARNESS_URL}?fast=1&seed=${spec.state === "empty" ? "v5-empty" : "v5"}`);
      await page.waitForFunction(() => typeof A !== "undefined" && document.querySelector("#stage").children.length > 0, null, { timeout: 15000 });
      if (sc.harness.go) await page.evaluate((v) => A.go(v), sc.harness.go);
      if (sc.harness.run) await page.evaluate((c) => (0, eval)(c), sc.harness.run);
    }
    await settle(page);
    return await page.evaluate((m) => {
      const cs = (el) => (el ? getComputedStyle(el) : null);
      const pick = (sel) => [...document.querySelectorAll(sel)].find((e) => e.getBoundingClientRect().width > 0);
      const side = pick(m ? ".win > .side" : "#rail"), btn = pick(m ? ".win .k-btn" : ".k-btn:not(.sm), button.btn"), dlg = pick('.k-dlg, [role="dialog"]'), b = cs(document.body);
      const r = (e) => (e ? e.getBoundingClientRect() : null);
      return { sidebarWidth: side ? r(side).width : null, buttonHeight: btn ? r(btn).height : null, buttonRadius: btn ? parseFloat(cs(btn).borderTopLeftRadius) : null,
        dialogRadius: dlg ? parseFloat(cs(dlg).borderTopLeftRadius) : null, bodyFontSize: parseFloat(b.fontSize), bodyFontFamily: b.fontFamily };
    }, mock);
  } finally { await ctx.close(); }
}

function decode(buf) { return loadDeps().PNG.sync.read(buf); }
/* Pads (with opaque magenta, so a size mismatch counts as a difference) or crops a PNG to w x h. */
function fit(png, w, h) {
  const { PNG } = loadDeps(); const out = new PNG({ width: w, height: h });
  for (let i = 0; i < out.data.length; i += 4) { out.data[i] = 255; out.data[i + 1] = 0; out.data[i + 2] = 255; out.data[i + 3] = 255; }
  PNG.bitblt(png, out, 0, 0, Math.min(w, png.width), Math.min(h, png.height), 0, 0);
  return out;
}

/* The gate's four regions of a 1280x800 frame: sidebar x0-240, top bar 54px high (right of the sidebar),
   right panel 340px wide (below the top bar), and the stage in between. */
export const REGIONS = {
  sidebar: (w, h) => [0, 0, 240, h],
  top: (w, h) => [240, 0, w - 240, 54],
  right: (w, h) => [w - 340, 54, 340, h - 54],
  stage: (w, h) => [240, 54, w - 240 - 340, h - 54],
};

/* pixelmatch (threshold 0.1, anti-aliased pixels not counted) of two PNG buffers.
   Returns { width, height, diff, pct, regions: { name: pct }, png } where png is the diff image
   (red = different, yellow = anti-aliasing ignored). */
export function compare(bufA, bufB) {
  const { PNG, pixelmatch } = loadDeps();
  const a0 = decode(bufA), b0 = decode(bufB);
  const w = Math.max(a0.width, b0.width), h = Math.max(a0.height, b0.height);
  const a = fit(a0, w, h), b = fit(b0, w, h), out = new PNG({ width: w, height: h });
  const diff = pixelmatch(a.data, b.data, out.data, w, h, { threshold: 0.1, includeAA: false, diffColor: [255, 0, 0], aaColor: [255, 255, 0] });
  const regions = {};
  for (const [name, box] of Object.entries(REGIONS)) {
    const [x0, y0, rw, rh] = box(w, h); let n = 0;
    for (let y = y0; y < y0 + rh; y++) for (let x = x0; x < x0 + rw; x++) { const i = (y * w + x) * 4; if (out.data[i] === 255 && out.data[i + 1] === 0 && out.data[i + 2] === 0) n++; }
    regions[name] = (100 * n) / (rw * rh);
  }
  return { width: w, height: h, diff, pct: (100 * diff) / (w * h), regions, png: PNG.sync.write(out) };
}

/* Mockup shifted 1 px right and 1 px down (edge filled with magenta): the self-test's noise probe. */
export function shifted(buf, dx = 1, dy = 1) {
  const { PNG } = loadDeps(); const src = decode(buf), out = fit(new PNG({ width: 1, height: 1 }), src.width, src.height);
  PNG.bitblt(src, out, 0, 0, src.width - dx, src.height - dy, dx, dy);
  return PNG.sync.write(out);
}

/* Writes a PNG into OUT_DIR and returns its path. */
export function save(name, buf) { fs.mkdirSync(OUT_DIR, { recursive: true }); const p = path.join(OUT_DIR, name); fs.writeFileSync(p, buf); return p; }
