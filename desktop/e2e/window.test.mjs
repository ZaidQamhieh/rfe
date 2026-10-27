// Window behaviour in the real session: a second launch hands over to the running window instead of
// opening another. (The remembered window size is not driven here: WebDriver's "close window" ends the
// app without the close event the size is saved on, so a real window-manager close is needed.)
//
//   desktop/e2e/run.sh            SCALE=2 desktop/e2e/run.sh

import { test, after } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import { App, resetAppState } from "./lib.mjs";

let app;
after(async () => {
  if (app) await app.end();
});

// Processes whose executable is the app under test (the WebKit helpers have other names).
function appPids() {
  const bin = process.env.RFE_DESKTOP_BIN;
  return readdirSync("/proc")
    .filter((n) => /^\d+$/.test(n))
    .filter((n) => {
      try {
        return readFileSync(`/proc/${n}/cmdline`, "utf8").split("\0")[0] === bin;
      } catch {
        return false;
      }
    });
}

test("a second launch is handed over to the running window and exits", async () => {
  resetAppState();
  app = await App.start();
  assert.equal(appPids().length, 1, "one app process before the second launch");

  const second = spawn(process.env.RFE_DESKTOP_BIN, [], { stdio: "ignore", env: process.env });
  const code = await new Promise((resolve) => {
    const t = setTimeout(() => {
      second.kill();
      resolve("did not exit");
    }, 15000);
    second.on("exit", (c) => (clearTimeout(t), resolve(c)));
  });
  assert.equal(code, 0, "the second process exits by itself");
  assert.equal(appPids().length, 1, "still one app window process");

  // The first window noticed: its own log (in the diagnostics report) says so, and it still answers.
  const report = await app.script("return window.__TAURI__.core.invoke('diagnostics');");
  assert.match(report, /second launch was handed over/);
  assert.ok(await app.visible("#rail .dest"), "the window still answers");
});
