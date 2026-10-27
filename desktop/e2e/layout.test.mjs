// The shape of the window: panels dragged to a size, one click to open, the menu button, the interface
// size, and the same screens at the sizes a screen really has. Pictures go to E2E_SHOTS when it is set.
//
//   desktop/e2e/run.sh

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { App, Agent, waitFor, sleep, resetAppState, KEYS } from "./lib.mjs";

let agent;
let app;

before(async () => {
  agent = await Agent.start();
  // Something to look at: folders and files of different kinds.
  for (const d of ["android", "checksums", "desktop", "symbols"]) mkdirSync(join(agent.roots, d));
  mkdirSync(join(agent.roots, "desktop", "inner"));
  writeFileSync(join(agent.roots, "notes.txt"), "hello\n");
  writeFileSync(join(agent.roots, "release.apk"), Buffer.alloc(4096));
  writeFileSync(join(agent.roots, "backup.tar.zst"), Buffer.alloc(8192));
  resetAppState();
  app = await App.start();
  await app.signInAccount(agent, "layout-pc");
  await app.go("files");
  await waitFor("the listing", () => app.visible("[data-rows] .li"));
});

after(async () => {
  if (app) await app.end();
  agent?.stop();
});

const path = () => app.script("return A.mainPane.path;");
const cssVar = (n) => app.script("return document.getElementById('main').style.getPropertyValue(arguments[0]);", [n]);
const rem = async (n) => parseFloat(await cssVar(n));
const remPx = () => app.script("return parseFloat(getComputedStyle(document.documentElement).fontSize);");

test("one click on a folder opens it", async () => {
  await app.click('[data-rows] .li[data-i="0"]');
  await waitFor("the folder to open", async () => (await path()) !== "/");
  const inside = await path();
  assert.match(inside, /rfe-e2e-roots-/);
  await waitFor("its folders", () => app.has(/android/, "[data-rows]"));
  await app.click('[data-rows] .li[data-i="0"]');
  await waitFor("android to open", async () => /android$/.test(await path()));
  await app.script("A.mainPane.go(arguments[0]);", [inside]);
  await waitFor("the folders again", () => app.has(/symbols/, "[data-rows]"));
});

test("the round icon selects without opening", async () => {
  const here = await path();
  await app.click('[data-rows] .li[data-i="1"] .lead');
  await waitFor("the row to be selected", async () => (await app.script("return A.mainPane.sel.size;")) === 1);
  assert.equal(await path(), here, "it did not open");
  await app.script("A.mainPane.sel.clear(); A.mainPane.selChanged();");
});

test("the side panel is dragged wider and narrower, within limits, and folds away", async () => {
  const w0 = await rem("--sidew");
  const u = await remPx();
  assert.equal(w0, 21);
  await app.drag("#rszSide", -120, 0);
  const w1 = await rem("--sidew");
  assert.ok(Math.abs((w1 - w0) * u - 120) < 12, "wider by about 120 px: " + w1);
  const real = await app.script("return document.getElementById('side').getBoundingClientRect().width;");
  assert.ok(Math.abs(real - w1 * u) < 2, "the panel really is that wide: " + real);
  await app.drag("#rszSide", -500, 0);
  const wMax = await rem("--sidew");
  assert.ok(wMax > w1 + 2, "wider still: " + wMax);
  const rows = await app.script("return document.querySelector('[data-rows]').clientWidth / parseFloat(getComputedStyle(document.documentElement).fontSize);");
  assert.ok(rows >= 33, "the list keeps its room: " + rows + " rem");
  await app.drag("#rszSide", 700, 0);
  assert.equal(await app.script("return document.getElementById('main').classList.contains('sidehid');"), true, "folds away when dragged narrow");
  await app.shot("9-side-hidden.png");
  await app.click("#rszSide");
  await waitFor("the panel to come back", async () => !(await app.script("return document.getElementById('main').classList.contains('sidehid');")));
  await app.click("#btnSide");
  assert.equal(await app.script("return document.getElementById('main').classList.contains('sidehid');"), true, "the button hides it");
  await app.click("#btnSide");
  await app.script("A.S.sideR = 21; window.dispatchEvent(new Event('resize'));");
});

test("transfers float in a pill and open and fold from the pill or the top bar", async () => {
  await app.script("A.state.sheetOpen = false; A.renderAll();");
  const open = () => app.script("return !document.getElementById('sheet').classList.contains('min') && document.querySelector('#sheet .spop').offsetHeight > 0;");
  assert.equal(await open(), false, "folded to the pill");
  const r = await app.script("const p = document.getElementById('spill').getBoundingClientRect(), m = document.getElementById('main').getBoundingClientRect(); return [p.bottom <= m.bottom, p.right <= m.right];");
  assert.deepEqual(r, [true, true], "the pill sits inside the window");
  await app.click("#spill");
  await waitFor("the pill opens the list", open);
  await app.click("#btnXfer");
  await waitFor("the top bar button folds it", async () => !(await open()));
  await app.click("#btnXfer");
  await waitFor("and opens it again", open);
  await app.click("#spill");
});

test("the menu button puts the labels beside the icons and back", async () => {
  await app.click("#rail [data-menu]");
  assert.equal(await app.script("return document.getElementById('app').classList.contains('wide');"), true);
  const w = await app.script("return document.getElementById('rail').getBoundingClientRect().width;");
  assert.ok(w > 200, "a wide rail: " + w);
  await app.shot("9-menu-open.png");
  await app.click("#rail [data-menu]");
  assert.equal(await app.script("return document.getElementById('app').classList.contains('wide');"), false);
});

test("a bigger interface size scales everything and keeps the layout whole", async () => {
  await app.setRect({ x: 0, y: 0, width: 1500, height: 960 });
  await sleep(600);
  const u0 = await remPx();
  await app.script("A.S.uiSize = 130; A.applyZoom();");
  await waitFor("the scale", async () => (await remPx()) > u0 * 1.1);
  const sideways = await app.script("const de = document.documentElement; return de.scrollWidth > de.clientWidth + 1 || de.scrollHeight > de.clientHeight + 1;");
  assert.equal(sideways, false, "no scroll");
  await app.shot("9-size-130.png");
  await app.script("A.S.uiSize = 100; A.applyZoom();");
  await waitFor("back to normal", async () => Math.abs((await remPx()) - u0) < 0.2);
  await app.setRect({ x: 0, y: 0, width: 1440, height: 900 });
});

test("the page scales with the window: 13 px at the smallest, bigger as the window grows", async () => {
  const want = { "900x600": 13.1, "1100x700": 16, "1440x900": 17.6, "1920x1080": 19, "2560x1440": 21.8 };
  for (const [size, px] of Object.entries(want)) {
    const [width, height] = size.split("x").map(Number);
    await app.setRect({ x: 0, y: 0, width, height });
    await sleep(700);
    const u = await remPx();
    assert.ok(Math.abs(u - px) < 0.6, `${size}: one rem is ${u} px, expected about ${px}`);
  }
  await app.setRect({ x: 0, y: 0, width: 1440, height: 900 });
});

test("both panels dragged to their limits at the smallest window still leave a usable list", async () => {
  await app.go("files");
  await app.setRect({ x: 0, y: 0, width: 1100, height: 700 });
  await sleep(700);
  await app.script("A.state.sheetOpen = true; A.renderAll();");
  await app.drag("#rszSide", -300, 0);
  const box = await app.script("const r = document.querySelector('[data-rows]'); return [r.clientWidth, r.clientHeight];");
  assert.ok(box[1] >= 150, "the list is tall enough for rows: " + box);
  assert.ok(box[0] >= 400, "the list is wide enough for names: " + box);
  const nameW = await app.script("const n = document.querySelector('[data-rows] .li'); return n ? n.children[1].getBoundingClientRect().width : 0;");
  assert.ok(nameW >= 120, "the names column keeps room: " + nameW);
  await app.shot("9-limits-1100x700.png");
  await app.script("A.S.sideR = 21; A.state.sheetOpen = false; window.dispatchEvent(new Event('resize'));");
  await app.setRect({ x: 0, y: 0, width: 1440, height: 900 });
});

test("a narrow window gets the drawer: the labels over the page, closed by Escape", async () => {
  await app.setRect({ x: 0, y: 0, width: 1100, height: 700 });
  await sleep(700);
  await app.click("#rail [data-menu]");
  assert.equal(await app.script("return document.getElementById('app').classList.contains('drawer');"), true);
  await app.shot("9-drawer-1100x700.png");
  await app.press(KEYS.escape);
  await waitFor("Escape to close it", async () => !(await app.script("return document.getElementById('app').classList.contains('drawer');")));
  await app.setRect({ x: 0, y: 0, width: 1440, height: 900 });
});

const SIZES = [[900, 600], [1100, 700], [1440, 900], [1920, 1080], [2560, 1440]];
for (const view of ["files", "tools"]) {
  test(`${view} at every size`, async () => {
    await app.go(view);
    await waitFor(`${view} to draw`, () => app.has(view === "tools" ? /Favorites/ : /Name/, "#stage"));
    for (const [width, height] of SIZES) {
      await app.setRect({ x: 0, y: 0, width, height });
      await sleep(900);
      await app.shot(`9-${view}-${width}x${height}.png`);
      const sideways = await app.script("const de = document.documentElement; return de.scrollWidth > de.clientWidth + 1 || de.scrollHeight > de.clientHeight + 1;");
      assert.equal(sideways, false, `${view} ${width}x${height}: the page scrolls`);
    }
    await app.setRect({ x: 0, y: 0, width: 1440, height: 900 });
  });
}
