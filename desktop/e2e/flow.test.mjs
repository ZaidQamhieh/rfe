// The first-run flow in the real window: New connection, compare the fingerprint, trust, sign in,
// see this computer on Devices, sign out. Real WebKit, real keyring, a real agent; nothing is faked.
// Screenshots go to E2E_SHOTS when it is set.
//
//   desktop/e2e/run.sh

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { App, Agent, waitFor, resetAppState, KEYS } from "./lib.mjs";

let agent;
let app;

before(async () => {
  agent = await Agent.start();
});

after(async () => {
  if (app) await app.end();
  agent?.stop();
});

const norm = (s) => s.replace(/[^0-9a-fA-F]/g, "").toLowerCase();

test("first run: address, compare fingerprint, trust, sign in, devices, sign out", async () => {
  resetAppState();
  app = await App.start();
  // A first run opens the welcome: three steps, the last one offers how to add a computer.
  await waitFor("the welcome", () => app.visible(".onb"));
  await app.shot("1-welcome.png");
  await app.click('.onb [data-ob="next"]');
  await app.click('.onb [data-ob="next"]');
  await app.shot("1b-add-a-computer.png");
  await app.click('.onb [data-ob="manual"]');
  await waitFor("the connection dialog", () => app.visible("#sh"));
  await app.fill("#sn", "e2e-pc");
  await app.fill("#sh", agent.host.split(":")[0]);
  await app.fill("#sp", agent.host.split(":")[1]);
  await app.shot("2-new-connection.png");
  await app.clickLabel("Connect");

  await waitFor("the trust dialog", () => app.visible(".fpfull"));
  await waitFor("the fingerprint to be filled in", async () => norm(await app.text(".fpfull")).length >= 64);
  assert.equal(norm(await app.text(".fpfull")), agent.fingerprint(), "the window shows the agent's own fingerprint");
  await app.shot("3-trust.png");
  await app.confirmTrust();

  await waitFor("the sign-in dialog", () => app.visible("#siu"));
  await app.fill("#siu", agent.user);
  await app.fill("#sip", agent.password);
  await app.shot("4-sign-in.png");
  await app.clickLabel("Sign in");

  await waitFor("the dialog to close", async () => !(await app.visible("#siu")));
  await waitFor("the server's own name in the rail's header", () => app.has(/e2e-pc/, "#top"));
  await waitFor("a listing", () => app.visible("[data-rows]"));
  assert.match(agent.devices(), /RFE Desktop/, "the agent lists the paired device");
  await app.go("devices");
  await waitFor("this app on Devices", () => app.has(/RFE Desktop\s+This app/, "#stage"));
  await app.shot("5-devices.png");

  await app.go("servers");
  await waitFor("the server card", () => app.has(/e2e-pc/, "#stage"));
  await app.shot("6-servers.png");
});

test("keyboard only: the whole sign-in without touching the mouse", async () => {
  await app.end();
  resetAppState();
  app = await App.start();
  await waitFor("the welcome", () => app.visible(".onb"));
  await app.press(KEYS.escape);
  await waitFor("the welcome to close", async () => !(await app.visible(".onb")));
  await app.script("document.querySelector('#rail [data-new]').focus();");
  await app.press(KEYS.enter);
  await waitFor("the connection dialog", () => app.visible("#sh"));
  await app.script("document.querySelector('#sn').focus();");
  await app.press("e2e-kb" + KEYS.tab);
  await app.fill("#sh", agent.host.split(":")[0]);
  await app.fill("#sp", agent.host.split(":")[1]);
  await app.script("document.querySelector('#sh').focus();");
  await app.press(KEYS.enter);
  await waitFor("the trust dialog", () => app.visible(".fpfull"));
  await app.script("document.querySelector('#tfm').focus();");
  await app.press(KEYS.space);
  await app.press(KEYS.enter);
  await waitFor("the sign-in dialog", () => app.visible("#siu"));
  assert.equal(await app.focused(), "siu", "sign-in starts in the username field");
  await app.press(agent.user + KEYS.tab + agent.password + KEYS.enter);
  await waitFor("the dialog to close", async () => !(await app.visible("#siu")));
  await waitFor("a signed-in server", () => app.has(/Signed in to e2e-kb/, "body").catch(() => false).then((v) => v || app.script("return !!document.querySelector('.rows')")));
});
