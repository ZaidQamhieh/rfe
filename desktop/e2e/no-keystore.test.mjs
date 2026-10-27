// A session with no Secret Service at all (no gnome-keyring, KeePassXC or KWallet on the bus): the
// window still opens, and signing in stops with a message that says what to install, before the
// agent is asked for anything. Runs only in a session started without a keyring:
//
//   RFE_E2E_NO_KEYRING=1 RFE_E2E_ONLY=no-keystore desktop/e2e/run.sh

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { App, Agent, waitFor, resetAppState } from "./lib.mjs";

const without = process.env.RFE_E2E_NO_KEYRING === "1";
let agent;
let app;

before(async () => {
  if (without) agent = await Agent.start();
});

after(async () => {
  if (app) await app.end();
  agent?.stop();
});

test("no Secret Service: the window opens and sign-in says what is missing", { skip: !without }, async () => {
  resetAppState();
  app = await App.start();
  await app.addServer(agent, "no-keystore-pc");
  await app.confirmTrust();
  await waitFor("the sign-in dialog", () => app.visible("#siu"));
  await app.fill("#siu", agent.user);
  await app.fill("#sip", agent.password);
  await app.clickLabel("Sign in");

  // The answer is shown in the dialog, which stays open for another try.
  await waitFor("the answer", () => app.has(/keystore|Secret Service|keyring/i, ".dlg"));
  const said = await app.script("return document.querySelector('.dlg').innerText;");
  console.log(`message shown: ${said.replace(/\n+/g, " | ")}`);
  assert.ok(await app.visible("#siu"), "still on the sign-in dialog");
  assert.doesNotMatch(agent.devices(), /RFE Desktop/, "nothing was enrolled on the agent");
  await app.shot("7-no-keystore.png");
  await app.clickLabel("Not now");

  // Settings: the keystore check says it is not working, and says why.
  await app.go("settings");
  await waitFor("settings", () => app.has(/Appearance/, "#stage"));
  await app.clickLabel("Troubleshooting", ".setn");
  await waitFor("the troubleshooting section", () => app.has(/Check the keystore/, "#stage"));
  await app.clickLabel("Check", "#stage");
  await waitFor("the answer", async () => /keystore is not working/.test(await app.snacks()));
  assert.match(await app.snacks(), /Secret Service/i);
});
