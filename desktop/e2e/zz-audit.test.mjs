// Audit 2026-10-20: exploratory run of the real window against hostile folder content. Not part of the
// normal gate: it only runs when RFE_E2E_ONLY=zz-audit selects it (run.sh globs *.test.mjs, so it is
// skipped unless RFE_AUDIT=1). Findings go to docs/audit/2026-10-20-audit.md.

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync, symlinkSync } from "node:fs";
import { join } from "node:path";
import { App, Agent, waitFor, sleep, resetAppState, KEYS } from "./lib.mjs";

const on = process.env.RFE_AUDIT === "1";
let agent;
let app;

before(async () => {
  if (!on) return;
  agent = await Agent.start();
  const r = agent.roots;
  const names = [
    "<img src=x onerror=document.title='XSS1'>.txt",
    '"><svg onload=document.title="XSS2">.txt',
    "a&amp;b <b>bold</b>.txt",
    "x".repeat(250) + ".txt",
    "rtl‮txt.exe",
    "tab\tname.txt",
    "emoji-😀-ünï.txt",
    "-rf",
    ".hidden",
    "CON",
    "back\\slash.txt",
  ];
  for (const n of names) {
    try { writeFileSync(join(r, n), "hello"); } catch (e) { console.log("fixture skipped", JSON.stringify(n), e.code); }
  }
  execFileSync("mkfifo", [join(r, "pipe")]);
  symlinkSync("/etc", join(r, "etc-link"));
  symlinkSync("/etc/passwd", join(r, "passwd-link"));
  symlinkSync("loop", join(r, "loop"));
  mkdirSync(join(r, "many"));
  for (let i = 0; i < 5000; i++) writeFileSync(join(r, "many", `f${String(i).padStart(5, "0")}.txt`), "x");
  writeFileSync(join(r, "bomb.png"), Buffer.from("89504e470d0a1a0a0000000d4948445200ea60000000ea6008000000", "hex"));
});

after(async () => {
  if (app) await app.end();
  agent?.stop();
});

test("hostile names render as text, nothing executes", { skip: !on }, async () => {
  resetAppState();
  app = await App.start();
  await app.signInAccount(agent, "audit-pc");
  await waitFor("a listing", () => app.visible("[data-rows]"));
  console.log("ROWHTML", JSON.stringify(await app.script("return document.querySelector('[data-rows]').innerHTML.slice(0, 900);")));
  await app.click("[data-rows] [data-row], [data-rows] .row, [data-rows] > *");
  await app.script("const e=document.querySelector('[data-rows] [data-row], [data-rows] .row, [data-rows] > *'); e && e.dispatchEvent(new MouseEvent('dblclick',{bubbles:true}));");
  await sleep(1500);
  await sleep(1500);
  const title = await app.script("return document.title;");
  const injected = await app.script("return document.querySelectorAll('[data-rows] img[src=\"x\"], [data-rows] svg[onload], [data-rows] .nm b, [data-rows] [data-row] b').length;");
  const text = await app.script("return document.querySelector('[data-rows]').innerText;");
  const html = await app.script("return [...document.querySelectorAll('[data-rows] img[src=\"x\"], [data-rows] svg[onload], [data-rows] .nm b, [data-rows] [data-row] b')].map((e) => e.outerHTML.slice(0, 200));");
  console.log("INJECTED-HTML", JSON.stringify(html));
  console.log("TITLE", JSON.stringify(title), "INJECTED", injected);
  console.log("ROWS", JSON.stringify(text.slice(0, 1500)));
  await app.shot("audit-hostile.png");
  assert.equal(injected, 0);
  assert.doesNotMatch(title, /XSS/);
});

const dblReal = async (name) => {
  const tagged = await app.script(`document.querySelectorAll('[data-aud]').forEach((e)=>e.removeAttribute('data-aud')); const r=[...document.querySelectorAll('[data-rows] [role=row], [data-rows] .row, [data-rows] [data-i]')].find((e)=>(e.innerText||"").trim().startsWith(${JSON.stringify(name)})); if(!r) return false; r.setAttribute('data-aud','1'); return r.className;`);
  if (!tagged) return "no row";
  await app.click("[data-aud]");
  await sleep(300);
  await app.press(KEYS.enter);
  return "real:" + tagged;
};
const dbl0 = (name) => app.script(`const r=[...document.querySelectorAll('[data-rows] [data-row], [data-rows] .row, [data-rows] > div > *')].find((e)=>(e.innerText||"").trim().startsWith(${JSON.stringify(name)})); if(!r) return 'no row'; r.dispatchEvent(new MouseEvent('dblclick',{bubbles:true})); return 'ok';`);

test("downloading a FIFO from the real window", { skip: !on }, async () => {
  // Drives the app's own transfer engine (window.A.E.enqueue) instead of clicking rows: the row/dialog
  // navigation is flaky in WebDriver, the engine call is what the Download button runs.
  console.log("DIAG", JSON.stringify(await app.script("const E = window.A.E; return { servers: E.servers.map((s) => ({ id: s.id, name: s.name })), keys: Object.keys(E).slice(0, 60), rows: (document.querySelector('[data-rows]') || {}).innerText && document.querySelector('[data-rows]').innerText.slice(0, 200), crumb: (document.querySelector('[data-crumbs], .crumbs') || {}).innerText };")));
  const host = await app.script("return window.A.E.servers.map((s) => s.id)[0];");
  assert.ok(host, "a signed-in server");
  const q = await app.script(
    "const r = window.A.E.enqueue({ dir: 'down', host: arguments[0], srcDir: arguments[1], dstDir: '', names: ['pipe'] }); return JSON.stringify({ error: r.error || null, n: (r.tasks || []).length });",
    [host, agent.roots],
  );
  console.log("ENQUEUE", q);
  await sleep(8000);
  const st = await app.script("return window.A.E.tasks.map((t) => ({ name: t.name, state: t.state, msg: t.msg || '' }));");
  console.log("TASKS-AFTER-8s", JSON.stringify(st));
  await app.go("transfers");
  await sleep(500);
  await app.shot("audit-fifo-download.png");
  // F-01: the agent never answers for a FIFO, so the task stays active instead of failing promptly.
  const stuck = st.find((t) => t.name === "pipe");
  console.log("FIFO-TASK", JSON.stringify(stuck));
  assert.ok(stuck, "a pipe task exists");
});

test("5000-file folder: window stays responsive", { skip: !on }, async () => {
  await app.go("files");
  await waitFor("root listing", () => app.has(/many/, "[data-rows]"), 20000);
  const t0 = Date.now();
  console.log("MANY dbl", await dbl0("many"));
  await waitFor("many listing", () => app.has(/f00000|f00001/, "[data-rows]"), 60000);
  console.log("MANY first page in", Date.now() - t0, "ms");
  const n = await app.script("return document.querySelectorAll('[data-rows] *').length;");
  const t1 = Date.now();
  await app.script("const v=document.querySelector('[data-rows]'); v.scrollTop = v.scrollHeight; return 1;");
  await sleep(2000);
  console.log("MANY DOM nodes", n, "after scroll:", await app.script("return document.querySelectorAll('[data-rows] *').length;"), "roundtrip ms", Date.now() - t1 - 2000);
  console.log("MANY last visible:", JSON.stringify(await app.script("const t=document.querySelector('[data-rows]').innerText.split('\\n').filter((x)=>/^f\\d+/.test(x)); return t.slice(-2);")));
  await app.shot("audit-many.png");
});
