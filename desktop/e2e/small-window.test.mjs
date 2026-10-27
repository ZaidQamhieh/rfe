// Layout at the smallest size the window may have (900x600), in every screen: nothing may need a
// sideways scroll, in the page or in a table, and the window refuses to go smaller. The compositor's
// scale option does not reach the WebKit window here (window.devicePixelRatio stays 1), so a 200%
// display is checked as what it comes to in CSS pixels: 1280x800 screen -> a 640x400 request, which
// the minimum size turns into 900x600.
//
//   desktop/e2e/run.sh

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { App, Agent, waitFor, sleep, resetAppState } from "./lib.mjs";

let agent;
let app;

before(async () => {
  agent = await Agent.start();
  resetAppState();
  app = await App.start();
  await app.signInAccount(agent, "small-pc");
});

after(async () => {
  if (app) await app.end();
  agent?.stop();
});

// What is wider than its box, if anything: the page itself, or something that would scroll sideways.
const overflow = () =>
  app.script(`
    const bad = [];
    const de = document.documentElement;
    if (de.scrollWidth > de.clientWidth) bad.push("page " + de.scrollWidth + ">" + de.clientWidth);
    for (const w of document.querySelectorAll("#stage, #stage > *, [data-rows], .main, #rail")) {
      if (w.offsetParent !== null && w.scrollWidth > w.clientWidth + 1 && getComputedStyle(w).overflowX !== "hidden") bad.push((w.id || w.className || w.tagName) + " " + w.scrollWidth + ">" + w.clientWidth);
    }
    return bad.join("; ");
  `);

const VIEWS = [
  ["files", /./],
  ["servers", /small-pc/],
  ["devices", /Paired devices/],
  ["transfers", /Transfers/],
  ["search", /Search/],
  ["tools", /Tools/],
  ["history", /History/],
  ["settings", /Theme/],
];

test("the window cannot be made smaller than 900x600", async () => {
  const r = await app.rect();
  await app.setRect({ x: r.x, y: r.y, width: 640, height: 400 });
  await sleep(600);
  const size = await app.script("return [window.innerWidth, window.innerHeight];");
  assert.ok(size[0] >= 890 && size[1] >= 590, `the window shrank to ${size.join("x")}`);
});

test("every screen fits without a sideways scroll at the smallest size", async () => {
  const problems = [];
  for (const [view, ready] of VIEWS) {
    await app.go(view);
    await waitFor(`${view} to show`, () => app.has(ready, "#stage, .main, body"));
    await sleep(900);
    const bad = await overflow();
    if (bad) problems.push(`${view}: ${bad}`);
    await app.shot(`8-900x600-${view}.png`);
  }
  assert.deepEqual(problems, [], problems.join("\n"));
});
