// Dark mode of the v5 design: the Theme control switches the whole window, a dark start works, and the dark tokens keep readable contrast.
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { JSDOM, ResourceLoader } from "jsdom";
import { fileURLToPath, pathToFileURL } from "node:url";

const PAGE = fileURLToPath(new URL("../mockups/v5/01-rfe.html", import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
class Offline extends ResourceLoader { fetch(u, o) { return String(u).startsWith("file:") ? super.fetch(u, o) : Promise.resolve(Buffer.from("")); } }
async function open(query, hash = "") {
  const dom = await JSDOM.fromFile(PAGE, { url: pathToFileURL(PAGE).href + query + hash, runScripts: "dangerously", resources: new Offline(), pretendToBeVisual: true,
    beforeParse(w) { w.matchMedia = () => ({ matches: false, addEventListener() {}, addListener() {} }); } });
  const w = dom.window;
  if (w.document.readyState !== "complete") await Promise.race([new Promise((r) => w.addEventListener("load", r, { once: true })), sleep(8000)]);
  await sleep(200);
  return w;
}
const themeBtn = (w, label) => [...w.document.querySelectorAll(".k-seg button")].find((b) => b.textContent === label);
const goSettings = (w) => { w.document.querySelector('#side [data-k="go:settings"]').click(); w.document.querySelector('[data-k="set:Appearance"]').click(); };

test("the window starts light when the system is light and no theme is chosen", async () => {
  const w = await open("?v=full");
  assert.equal(w.document.querySelector(".win").dataset.theme, "light");
});

test("?theme=dark starts dark, in full and in empty mode", async () => {
  for (const v of ["full", "empty"]) {
    const w = await open(`?v=${v}&theme=dark`);
    assert.equal(w.document.querySelector(".win").dataset.theme, "dark", v);
  }
});

test("Settings > Appearance > Theme: Dark and Light switch the window and mark the choice", async () => {
  const w = await open("?v=full");
  goSettings(w);
  themeBtn(w, "Dark").click();
  await sleep(50);
  assert.equal(w.document.querySelector(".win").dataset.theme, "dark");
  assert.ok(themeBtn(w, "Dark").classList.contains("on") && !themeBtn(w, "System").classList.contains("on"), "Dark is marked");
  themeBtn(w, "Light").click();
  await sleep(50);
  assert.equal(w.document.querySelector(".win").dataset.theme, "light");
  assert.ok(themeBtn(w, "Light").classList.contains("on"), "Light is marked");
});

test("the account menu Theme control also switches the window, and the choice survives opening a page", async () => {
  const w = await open("?v=full");
  w.K.menu("account", 300, 200);
  themeBtn(w, "Dark").click();
  await sleep(50);
  assert.equal(w.document.querySelector(".win").dataset.theme, "dark");
  w.document.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  w.document.querySelector('#side [data-k="go:servers"]').click();
  await sleep(50);
  assert.equal(w.document.querySelector(".win").dataset.theme, "dark");
});

/* contrast of the dark tokens (WCAG 2): body text and controls need 4.5:1 */
const lum = (h) => { const [r, g, b] = [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)); return 0.2126 * r + 0.7152 * g + 0.0722 * b; };
const ratio = (a, b) => { const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p); return (x + 0.05) / (y + 0.05); };
test("dark tokens keep at least 4.5:1 for text, accent text, status colours and filled buttons", () => {
  const css = fs.readFileSync(new URL("../mockups/v5/kit.css", import.meta.url), "utf8");
  const block = css.match(/\.win\[data-theme="dark"\] \{([^}]*)\}/)[1];
  const d5 = css.match(/\.win\.d5\[data-theme="dark"\] \{([^}]*)\}/)[1];
  const t = Object.fromEntries([...(block + d5).matchAll(/--([a-z0-9-]+):\s*(#[0-9a-f]{3,6})\b/gi)].map((m) => [m[1], m[2].length === 4 ? "#" + [...m[2].slice(1)].map((c) => c + c).join("") : m[2]]));
  const fails = [];
  for (const fg of ["t1", "t2", "t3"]) for (const bg of ["bg", "sf", "rz"]) if (ratio(t[fg], t[bg]) < 4.5) fails.push(`${fg} on ${bg}`);
  for (const fg of ["ac-ink", "pos", "neg", "warn"]) for (const bg of ["bg", "sf"]) if (ratio(t[fg], t[bg]) < 4.5) fails.push(`${fg} on ${bg}`);
  if (ratio(t["on-ac"], t.ac) < 4.5) fails.push("on-ac on ac");
  const danger = css.match(/\.win\[data-theme="dark"\] \.k-dlg \.k-btn\.warn[^{]*\{\s*background:\s*(#[0-9a-f]{6})/i)[1];
  if (ratio("#ffffff", danger) < 4.5) fails.push("white on the danger button");
  assert.deepEqual(fails, []);
});
