// Memory of the running app: its own process and the WebKit helper processes it starts, measured as
// PSS (shared pages split between the processes that map them) after sign-in, so the numbers add up.
// Prints a table; fails only past a generous budget, so a leak or a doubled webview is caught.

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { App, Agent, waitFor, resetAppState } from "./lib.mjs";

const BUDGET_MB = Number(process.env.E2E_MEMORY_BUDGET_MB || 450);
let agent;
let app;

before(async () => {
  agent = await Agent.start();
});
after(async () => {
  if (app) await app.end();
  agent?.stop();
});

function pss(pid) {
  try {
    const m = readFileSync(`/proc/${pid}/smaps_rollup`, "utf8").match(/^Pss:\s+(\d+) kB/m);
    return m ? Number(m[1]) / 1024 : 0;
  } catch {
    return 0;
  }
}

function threads(pid) {
  try {
    return readdirSync(`/proc/${pid}/task`).length;
  } catch {
    return 0;
  }
}

function appProcesses() {
  const out = [];
  for (const pid of readdirSync("/proc").filter((p) => /^\d+$/.test(p))) {
    let comm;
    try {
      comm = readFileSync(`/proc/${pid}/comm`, "utf8").trim();
    } catch {
      continue;
    }
    if (/^(rfe-desktop|WebKit|tauri-driver)/.test(comm)) out.push({ pid, comm, mb: pss(pid) });
  }
  return out;
}

test("memory after sign-in stays in budget", async () => {
  resetAppState();
  app = await App.start();
  await app.signInAccount(agent, "mem-pc");
  await new Promise((r) => setTimeout(r, 2000));
  const procs = appProcesses();
  let total = 0;
  for (const p of procs.sort((a, b) => b.mb - a.mb)) {
    console.log(`  ${p.comm.padEnd(18)} pid ${String(p.pid).padEnd(7)} ${p.mb.toFixed(1)} MB  ${threads(p.pid)} threads`);
    if (!p.comm.startsWith("tauri-driver")) total += p.mb;
  }
  if (process.env.E2E_SMAPS) {
    for (const p of procs.filter((q) => /rfe-desktop|WebKitWeb/.test(q.comm))) {
      const text = readFileSync(`/proc/${p.pid}/smaps`, "utf8");
      const by = new Map();
      let name = "";
      for (const line of text.split("\n")) {
        if (/^[0-9a-f]+-[0-9a-f]+ /.test(line)) name = line.split(/\s+/).slice(5).join(" ") || "[anon]";
        const m = line.match(/^Pss:\s+(\d+) kB/);
        if (m) by.set(name, (by.get(name) || 0) + Number(m[1]));
      }
      console.log(`  -- ${p.comm} by mapping`);
      for (const [k, v] of [...by].sort((a, b) => b[1] - a[1]).slice(0, 12)) console.log(`     ${(v / 1024).toFixed(1).padStart(6)} MB  ${k}`);
    }
  }
  console.log(`  app total (without the test driver): ${total.toFixed(1)} MB`);
  assert.ok(total < BUDGET_MB, `app uses ${total.toFixed(0)} MB, budget ${BUDGET_MB} MB`);

  // Use it: refresh, open and close Settings, make the report, over and over. WebKit collects its
  // garbage lazily, so the figure moves up and down by a few MB (measured: 3000 bare IPC calls stay
  // within about 10 MB); the check is a ceiling, which a real leak (a listener or node per round)
  // would pass within a few hundred rounds.
  const sum = () => appProcesses().filter((p) => !/^(tauri-driver|WebKitWebDriver)/.test(p.comm)).reduce((a, p) => a + p.mb, 0);
  const pass = async () => {
    await app.go("servers");
    await waitFor("the server card", () => app.has(/mem-pc/, "#stage"));
    await app.go("devices");
    await waitFor("the device list", () => app.has(/RFE Desktop/, "#stage"));
    await app.go("settings");
    await waitFor("settings", () => app.has(/Theme/, "#stage"));
    await app.go("files");
    await waitFor("the listing", () => app.visible("[data-rows]"));
  };
  // WebKit keeps decoded images, layout and script caches of the screens it has shown. Measured over
  // 400 rounds (E2E_TRACE=1 prints it): +40 MB after 40 rounds, +65 after 200, +78 after 400, so it
  // levels off; a leak per round would keep climbing. The ceiling leaves room for that plateau.
  await pass();
  await new Promise((r) => setTimeout(r, 2000));
  const idle = sum();
  const rounds = Number(process.env.E2E_ROUNDS || 120);
  for (let i = 0; i < rounds; i++) {
    await pass();
    if (process.env.E2E_TRACE && i % 40 === 39) console.log(`  round ${i + 1}: ${sum().toFixed(1)} MB`);
  }
  await new Promise((r) => setTimeout(r, 2000));
  const used = sum();
  console.log(`  after ${rounds} rounds of Servers, Devices, Settings and Files: ${idle.toFixed(1)} -> ${used.toFixed(1)} MB`);
  assert.ok(used < idle + 100, `grew by ${(used - idle).toFixed(1)} MB over ${rounds} rounds`);
});
