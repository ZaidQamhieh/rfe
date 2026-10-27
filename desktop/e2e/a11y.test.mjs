// What a screen reader is given: the app's AT-SPI tree, read from the accessibility bus of the
// private session. Not Orca itself (it is not installed here): this is the data Orca speaks from.
// Skips when the accessibility bus is not available.
//
//   desktop/e2e/run.sh

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { App, Agent, waitFor, resetAppState, KEYS } from "./lib.mjs";

const dump = join(dirname(fileURLToPath(import.meta.url)), "atspi-dump.py");
const BUTTON = "button";
let app;
let agent;
after(async () => {
  if (app) await app.end();
  agent?.stop();
});

function tree() {
  const out = execFileSync("python3", [dump, "rfe"], { encoding: "utf8", timeout: 30000 });
  return out
    .split("\n")
    .filter(Boolean)
    .map((l) => {
      const [depth, role, showing, ...name] = l.split("\t");
      return { depth: Number(depth), role, showing: showing === "1", name: name.join("\t") };
    });
}

test("the screens expose headings, labelled fields, buttons and tables to assistive technology", async (t) => {
  agent = await Agent.start();
  resetAppState();
  app = await App.start();
  let nodes;
  try {
    nodes = await (async () => {
      let last;
      for (let i = 0; i < 20; i++) {
        try {
          const n = tree();
          if (n.length > 5) return n;
        } catch (e) {
          last = e;
        }
        await new Promise((r) => setTimeout(r, 500));
      }
      throw last || new Error("empty tree");
    })();
  } catch (e) {
    t.skip(`no accessibility bus: ${String(e.message).split("\n")[0]}`);
    return;
  }
  const shown = nodes.filter((n) => n.showing);
  const dumpOf = (list) => list.map((n) => `${"  ".repeat(n.depth)}${n.role}: ${n.name}`).join("\n");
  const named = (list, role, re) => list.some((n) => n.role === role && re.test(n.name));
  writeFileSync(join(process.env.E2E_APP_DATA, "..", "atspi-start.txt"), dumpOf(nodes));
  // The window's frame: the rail's destinations are buttons with names, and the page has headings.
  for (const want of [/New connection/, /^Files$/, /^Servers$/, /^Devices$/, /^Transfers$/, /^Settings$/]) {
    assert.ok(named(shown, BUTTON, want), `a button named ${want}\n` + dumpOf(shown));
  }
  assert.ok(named(shown, "heading", /Servers/), "the Servers page's heading is a heading\n" + dumpOf(shown));

  // A dialog is announced as a dialog, with its fields labelled.
  await app.skipWelcome();
  await app.click("#rail [data-new]");
  await waitFor("the connection dialog", () => app.visible("#sh"));
  await new Promise((r) => setTimeout(r, 400));
  const dlg = tree().filter((n) => n.showing);
  assert.ok(dlg.some((n) => n.role === "dialog" || n.role === "alert"), "the dialog has a dialog role\n" + dumpOf(dlg));
  assert.ok(named(dlg, "heading", /New connection/), "the dialog's title is a heading\n" + dumpOf(dlg));
  for (const label of [/Name/, /Host or address/, /Port/]) {
    assert.ok(dlg.some((n) => (n.role === "label" || n.role === "entry") && label.test(n.name)), `a field labelled ${label}\n` + dumpOf(dlg));
  }
  assert.ok(named(dlg, BUTTON, /Connect/), "the Connect button has a name");
  await app.press(KEYS.escape);

  // Sign in, then read the Devices page.
  await app.signInAccount(agent, "a11y-pc");
  await app.go("devices");
  await waitFor("this app on Devices", () => app.has(/RFE Desktop\s+This app/, "#stage"));
  await new Promise((r) => setTimeout(r, 400));
  const devices = tree().filter((n) => n.showing);
  writeFileSync(join(process.env.E2E_APP_DATA, "..", "atspi-devices.txt"), dumpOf(devices));
  assert.ok(named(devices, "heading", /Devices/), "devices heading\n" + dumpOf(devices));
  assert.ok(named(devices, BUTTON, /More for RFE Desktop/), "the row's menu button names its device\n" + dumpOf(devices));
  assert.ok(named(devices, BUTTON, /New code|Copy code/), "the pairing code's buttons have names");
});
