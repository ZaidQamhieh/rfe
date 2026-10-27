// Every screen of the signed-in window, opened in the real window against a real agent: it must
// appear and show no error. Screenshots go to E2E_SHOTS when it is set.
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
  await app.signInAccount(agent, "screens-pc");
});

after(async () => {
  if (app) await app.end();
  agent?.stop();
});

const errorShown = () => app.script("return [...document.querySelectorAll('.snack.err, .es.warn')].map((x) => x.innerText).join(' | ');");

// Opens a rail destination, waits for it to settle, checks it shows no error, and takes the picture.
async function visit(view, ready, shot) {
  await app.go(view);
  await waitFor(`${view} to show`, () => app.has(ready, "#stage, .main, body"));
  await sleep(1200);
  await app.shot(`6-${shot || view}.png`);
  assert.equal(await errorShown(), "", `${view}: the screen reports an error`);
}

test("files: the agent's folder lists its contents", async () => {
  await app.go("files");
  await waitFor("the listing", () => app.visible("[data-rows]"));
  await sleep(800);
  await app.shot("6-files.png");
  assert.equal(await errorShown(), "");
});

test("servers", () => visit("servers", /screens-pc/));
test("devices", () => visit("devices", /Paired devices/));
test("transfers", () => visit("transfers", /Transfers/));
test("search", () => visit("search", /Search/));
test("tools", () => visit("tools", /Tools/));
test("history", () => visit("history", /History/));
test("settings", () => visit("settings", /Theme/));

test("pair a phone: a code is generated and shown on Devices", async () => {
  await app.go("devices");
  await waitFor("a code", () => app.visible("#pairCode"));
  assert.match((await app.text("#pairCode")).replace(/\s/g, ""), /^[2-9A-HJ-NP-Z]{8}$/, "the code has the agent's format");
  assert.equal(await errorShown(), "");
});
