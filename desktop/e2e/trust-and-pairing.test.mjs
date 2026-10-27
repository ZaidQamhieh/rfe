// The sign-in side of the app in the real window: two servers pinned and kept, Forget, pairing with a
// code, approving on the PC (the match code the window shows is the one the agent shows its owner), a
// login the agent revoked, and the two-step "Create a new device key". Screenshots go to E2E_SHOTS
// when it is set.
//
//   desktop/e2e/run.sh            SCALE=2 desktop/e2e/run.sh

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { App, Agent, waitFor, sleep, resetAppState } from "./lib.mjs";

let a;
let b;
let c; // a third agent: the first two are used by the sign-in tests
let app;

before(async () => {
  a = await Agent.start("owner", "e2e-password-a");
  b = await Agent.start("owner", "e2e-password-b");
  c = await Agent.start("owner", "e2e-password-c");
  resetAppState();
  app = await App.start();
});

after(async () => {
  if (app) await app.end();
  a?.stop();
  b?.stop();
  c?.stop();
});

// A failure says what the window showed, which is most of the diagnosis.
const step = (name, fn) =>
  test(name, async () => {
    try {
      await fn();
    } catch (e) {
      const seen = await app.script("return document.body.innerText.slice(0, 600);").catch(() => "?");
      e.message += `\n[window] ${seen.replace(/\n+/g, " | ")}\n[snacks] ${await app.snacks().catch(() => "?")}`;
      throw e;
    }
  });

const cards = () => app.rows("#sgrid .scard, #sgrid [data-sa='more']");

step("two servers are kept, each with its own pin and login", async () => {
  await app.signInAccount(a, "server-a");
  await app.signInAccount(b, "server-b");
  await app.go("servers");
  await waitFor("two cards", async () => (await app.rows("#sgrid [data-sa='more']")) === 2);
  const text = await app.text("#sgrid");
  assert.ok(text.includes("server-a") && text.includes("server-b"), text);
  await app.shot("7-servers.png");
  assert.match(a.devices(), /RFE Desktop/);
  assert.match(b.devices(), /RFE Desktop/);
});

step("Forget asks first, and only the second step removes the server", async () => {
  await app.click("#sgrid [data-sa='more']");
  await app.clickMenu("Forget server");
  await waitFor("the confirmation", () => app.visible(".dlg.danger"));
  assert.match(await app.text(".dlg h2"), /^Forget /);
  await app.shot("7-forget-asked.png");
  await app.clickLabel("Cancel");
  assert.equal(await app.rows("#sgrid [data-sa='more']"), 2, "cancelling forgets nothing");
  await app.click("#sgrid [data-sa='more']");
  await app.clickMenu("Forget server");
  await waitFor("the confirmation", () => app.visible(".dlg.danger"));
  await app.clickLabel("Forget");
  await waitFor("one server left", async () => (await app.rows("#sgrid [data-sa='more']")) === 1);
});

step("pair with a one-time code", async () => {
  await app.click("#rail [data-new]");
  await waitFor("the connection dialog", () => app.visible("#sh"));
  await app.click('.dlg [data-fx="conn.tab|code"]');
  await waitFor("the code form", () => app.visible("#pcc"));
  await app.fill("#pcn", "paired-a");
  await app.fill("#pch", a.host);
  const code = a.pairCode();
  await app.fill("#pcc", code);
  assert.equal((await app.value("#pcc")).replace(/\s/g, ""), code, "the field takes the agent's own code format");
  await app.shot("7-pairing-code-form.png");
  await app.clickLabel("Continue");
  await waitFor("the trust dialog", () => app.visible(".fpfull"));
  await app.confirmTrust();
  await waitFor("signed in with the code", async () => /Signed in to paired-a|Paired/.test(await app.snacks()), 20000);
  await app.go("devices");
  await waitFor("this app on Devices", () => app.has(/RFE Desktop\s+This app/, "#stage"));
  await app.shot("7-paired-with-code.png");
});

step("approve on the PC: the match code is the one the agent shows its owner", async () => {
  await app.addServer(c, "approved-c");
  await app.confirmTrust();
  await waitFor("the sign-in dialog", () => app.visible("#siu"));
  await app.click('.dlg [data-m="ask"]');
  await app.clickLabel("Ask for approval");
  await waitFor("the match code", async () => (await app.text(".dlg .pcode")).trim().length >= 6);
  const shown = (await app.text(".dlg .pcode")).trim();
  const onPc = c.pairRequests();
  assert.ok(onPc.includes(shown), `window: ${shown}; agent: ${onPc}`);
  await app.shot("7-approve-on-pc.png");

  // Not now only stops waiting; then ask again and let the owner accept the new request.
  await app.clickLabel("Not now");
  await waitFor("the dialog to close", async () => !(await app.visible(".dlg")));
  await app.go("servers");
  await app.clickInCard("approved-c", "Sign in");
  await waitFor("the sign-in dialog", () => app.visible("#siu"));
  await app.click('.dlg [data-m="ask"]');
  await app.clickLabel("Ask for approval");
  await waitFor("a new match code", async () => {
    const code = (await app.text(".dlg .pcode")).trim();
    return code.length >= 6 && code !== shown;
  });
  const second = (await app.text(".dlg .pcode")).trim();
  c.answerPairRequest("accept", c.pairRequestId(second));
  await waitFor("the sign-in to finish", async () => !(await app.visible(".dlg")), 30000);
  assert.match(await app.snacks(), /Signed in to approved-c/);
});

step("a login the agent revoked sends the window back to sign-in", async () => {
  const rows = c.devices().split("\n").filter((l) => /RFE Desktop/.test(l));
  assert.ok(rows.length >= 1, c.devices());
  for (const line of rows) c.revoke(line.trim().split(/\s+/)[0]);
  // The window finds out by itself (its periodic check gets a 401) and asks for a sign-in again.
  await app.go("servers");
  await waitFor("the revoked server asks for a sign-in", () => app.script("const c = [...document.querySelectorAll('.sc')].find((x) => x.querySelector('b').textContent.trim() === 'approved-c'); return !!c && /not signed in|no longer accepts this login|Signed out on this server/.test(c.innerText);"), 40000);
  await app.shot("7-login-revoked.png");
});

step("Create a new device key needs a confirmation", async () => {
  await app.go("settings");
  await waitFor("settings", () => app.has(/Appearance/, "#stage"));
  await app.clickLabel("Troubleshooting", ".setn");
  await waitFor("the troubleshooting section", () => app.has(/Device key/, "#stage"));
  await app.clickLabel("New key…", "#stage");
  await waitFor("the confirmation", () => app.visible(".dlg.danger"));
  assert.match(await app.text(".dlg h2"), /Create a new device key/);
  await app.shot("7-reset-key-asked.png");
  await app.clickLabel("Cancel");
  assert.doesNotMatch(await app.snacks(), /new device key will be created/);
  await app.clickLabel("New key…", "#stage");
  await waitFor("the confirmation", () => app.visible(".dlg.danger"));
  await app.clickLabel("Create a new device key");
  await waitFor("the note", async () => /A new device key will be created/.test(await app.snacks()));
  await sleep(200);
});
