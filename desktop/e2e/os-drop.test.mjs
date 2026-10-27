// A real file drop: another program (a GTK window standing in for a file manager) starts a drag with a
// file in it, the compositor carries it over the app's window, and the app uploads it to the folder
// under the pointer. WebDriver cannot produce this, so the pointer is moved through KWin's fake-input
// protocol (e2e/os-drop/fakeinput.c; the session runs KWin with KWIN_WAYLAND_NO_PERMISSION_CHECKS=1).
//
//   desktop/e2e/run.sh            RFE_E2E_ONLY=os-drop desktop/e2e/run.sh

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, existsSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { App, Agent, waitFor, sleep, resetAppState } from "./lib.mjs";

const here = dirname(fileURLToPath(import.meta.url));
let agent, app, work, fake, src, log;

before(async () => {
  work = mkdtempSync(join(tmpdir(), "rfe-e2e-drop-"));
  try {
    execFileSync("wayland-scanner", ["client-header", join(here, "os-drop/fake_input.xml"), join(work, "fake_input-client-protocol.h")]);
    execFileSync("wayland-scanner", ["private-code", join(here, "os-drop/fake_input.xml"), join(work, "fake_input-protocol.c")]);
    const flags = execFileSync("pkg-config", ["--cflags", "--libs", "wayland-client"]).toString().trim().split(/\s+/);
    fake = join(work, "fakeinput");
    execFileSync("gcc", ["-o", fake, join(here, "os-drop/fakeinput.c"), join(work, "fake_input-protocol.c"), `-I${work}`, ...flags]);
  } catch (e) {
    fake = null;
    return;
  }
  agent = await Agent.start();
  resetAppState();
  app = await App.start();
  await app.signInAccount(agent, "drop-pc");
});

after(async () => {
  src?.kill();
  if (app) await app.end();
  agent?.stop();
  if (work) rmSync(work, { recursive: true, force: true });
});

const move = (...cmds) => execFileSync(fake, cmds);

test("a file dragged in from another program is uploaded to the folder under the pointer", async (t) => {
  if (!fake) return t.skip("needs wayland-scanner, gcc and the wayland-client headers");
  mkdirSync(join(agent.roots, "drop-here"));
  await app.go("files");
  await app.script(`const A = window.A, E = A.E; A.setMainHost(E.servers[0].id); return 1;`);
  await sleep(1000);
  await app.script(`window.A.mainPane.go(${JSON.stringify(agent.roots)}); return 1;`);
  await waitFor("the folder row", () => app.has(/drop-here/, "[data-rows]"));
  await app.setRect({ x: 0, y: 0, width: 1000, height: 640 });
  await sleep(600);

  // Where the app's page is on the screen: the pointer reports its place in the page.
  await app.script(`window.__pt = null; document.addEventListener("mousemove", (e) => { window.__pt = [e.clientX, e.clientY]; }, true); return 1;`);
  let origin = null;
  for (const [x, y] of [[500, 300], [300, 200], [700, 400], [200, 500]]) {
    move(`m:${x},${y}`, "s:80", `m:${x + 3},${y + 3}`, "s:150");
    const p = await app.script("return window.__pt;");
    if (p) { origin = [x + 3 - p[0], y + 3 - p[1]]; break; }
  }
  assert.ok(origin, "the pointer never reached the page");

  const file = join(work, "dropped.txt");
  writeFileSync(file, "from another program\n");
  log = join(work, "src.log");
  src = spawn("sh", ["-c", `exec python3 "${join(here, "os-drop/dragsrc.py")}" "${file}" >"${log}" 2>&1`], { env: process.env });
  await waitFor("the drag source window", () => existsSync(log) && /ready/.test(readFileSync(log, "utf8")), 15000);
  await sleep(500);

  // Where the source window is: sweep the screen until the pointer is reported inside it.
  let sorigin = null;
  for (let y = 40; y < 800 && !sorigin; y += 60) {
    for (let x = 40; x < 1280 && !sorigin; x += 60) {
      const before = readFileSync(log, "utf8").length;
      move(`m:${x},${y}`, "s:40", `m:${x + 2},${y + 2}`, "s:60");
      const add = readFileSync(log, "utf8").slice(before).match(/motion (\d+) (\d+)/);
      if (add) sorigin = [x + 2 - +add[1], y + 2 - +add[2]];
    }
  }
  assert.ok(sorigin, "the drag source window was not found on the screen");

  const row = await app.script(`const r = [...document.querySelectorAll("[data-rows] .li, [data-rows] .tile")].find((x) => /drop-here/.test(x.innerText)).getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2];`);
  const from = [sorigin[0] + 150, sorigin[1] + 100];
  const to = [origin[0] + row[0], origin[1] + row[1]];
  await app.shot("os-drop-before.png");
  move(`m:${from[0]},${from[1]}`, "s:150", "d", "s:150", `l:${from[0]},${from[1]},${from[0] + 12},${from[1] + 12},4`, `l:${from[0] + 12},${from[1] + 12},${to[0]},${to[1]},40`, "s:400", `m:${to[0] + 1},${to[1]}`, "s:300");
  await app.shot("os-drop-over.png");
  move("u", "s:500");
  const landed = join(agent.roots, "drop-here", "dropped.txt");
  await waitFor("the file to arrive in the folder", () => existsSync(landed), 20000);
  assert.equal(readFileSync(landed, "utf8"), "from another program\n");
});
