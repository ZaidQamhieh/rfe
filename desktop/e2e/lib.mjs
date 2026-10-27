// A small WebDriver client for the real app, and a throwaway agent to talk to.

import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import net from "node:net";

const DRIVER = "http://127.0.0.1:4444";
export const KEYS = { tab: "", enter: "", shiftTab: "", space: "", escape: "", down: "", up: "" };

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function call(method, path, body) {
  const r = await fetch(DRIVER + path, {
    method,
    headers: { "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const j = await r.json();
  if (j.value && j.value.error) throw new Error(`${method} ${path}: ${j.value.error}: ${j.value.message}`);
  return j.value;
}

export async function waitFor(what, fn, timeout = 15000) {
  const end = Date.now() + timeout;
  let last;
  while (Date.now() < end) {
    try {
      const v = await fn();
      if (v) return v;
    } catch (e) {
      last = e;
    }
    await sleep(100);
  }
  throw new Error(`timed out waiting for ${what}${last ? ` (${last.message})` : ""}`);
}

export class App {
  static async start() {
    const caps = { alwaysMatch: { "tauri:options": { application: process.env.RFE_DESKTOP_BIN } } };
    const v = await call("POST", "/session", { capabilities: caps });
    const app = new App(v.sessionId);
    await waitFor("the window", () => app.visible("#rail .dest"));
    return app;
  }

  constructor(id) {
    this.id = id;
  }

  req(method, path, body) {
    return call(method, `/session/${this.id}${path}`, body);
  }

  async find(css) {
    const el = await this.req("POST", "/element", { using: "css selector", value: css });
    return Object.values(el)[0];
  }

  async visible(css) {
    return this.script("const e = document.querySelector(arguments[0]); return !!e && !e.hidden && e.offsetParent !== null;", [css]);
  }

  script(code, args = []) {
    return this.req("POST", "/execute/sync", { script: code, args });
  }

  async text(css) {
    return this.req("GET", `/element/${await this.find(css)}/text`);
  }

  async value(css) {
    return this.script("return document.querySelector(arguments[0]).value;", [css]);
  }

  async click(css) {
    // Like a person: wait until it is there and enabled, scroll to it, and press it again if the
    // screen was still changing under the pointer.
    await waitFor(`${css} to be pressed`, async () => {
      const ready = await this.script(
        "const e = document.querySelector(arguments[0]); if (!e || e.disabled || e.hidden || e.offsetParent === null) return false; e.scrollIntoView({ block: 'center' }); return true;",
        [css]
      );
      if (!ready) return false;
      try {
        await this.req("POST", `/element/${await this.find(css)}/click`, {});
        return true;
      } catch (e) {
        if (/not interactable|stale element|intercepted/.test(e.message)) return false;
        throw e;
      }
    });
  }

  /** Presses the visible button whose label is exactly `label`, inside `scope` (default: the open dialog, else the page). */
  async clickLabel(label, scope) {
    await waitFor(`the "${label}" button`, async () => {
      const ok = await this.script(
        `const root = arguments[1] ? document.querySelector(arguments[1]) : (document.querySelector('.dlg') || document);
         if (!root) return false;
         const b = [...root.querySelectorAll('button')].find((x) => x.textContent.trim() === arguments[0] && !x.disabled && x.offsetParent !== null);
         if (!b) return false; b.setAttribute('data-e2e', '1'); return true;`,
        [label, scope || null]
      );
      if (!ok) return false;
      try {
        await this.req("POST", `/element/${await this.find('[data-e2e="1"]')}/click`, {});
        return true;
      } catch (e) {
        if (/not interactable|stale element|intercepted/.test(e.message)) return false;
        throw e;
      } finally {
        await this.script("document.querySelectorAll('[data-e2e]').forEach((x) => x.removeAttribute('data-e2e'));");
      }
    });
  }

  /** Presses a button of the Servers card of the server with this name. */
  async clickInCard(name, label) {
    // The card is found again on every try: a state change redraws it and would drop a mark set earlier.
    await waitFor(`the "${label}" button of ${name}`, async () => {
      const ok = await this.script(
        `const c = [...document.querySelectorAll('.sc')].find((x) => x.querySelector('b') && x.querySelector('b').textContent.trim() === arguments[0]);
         if (!c) return false;
         const b = [...c.querySelectorAll('button')].find((x) => x.textContent.trim() === arguments[1] && !x.disabled && x.offsetParent !== null);
         if (!b) return false; b.setAttribute('data-e2e', '1'); return true;`,
        [name, label]
      );
      if (!ok) return false;
      try {
        await this.req("POST", `/element/${await this.find('[data-e2e="1"]')}/click`, {});
        return true;
      } catch (e) {
        if (/not interactable|stale element|intercepted|no such element/.test(e.message)) return false;
        throw e;
      } finally {
        await this.script("document.querySelectorAll('[data-e2e]').forEach((x) => x.removeAttribute('data-e2e'));");
      }
    });
  }

  /** Presses the item of the open menu with this label. */
  async clickMenu(label) {
    await waitFor(`the "${label}" menu item`, async () => {
      const ok = await this.script(
        `const m = [...document.querySelectorAll('.menu .mi')].find((x) => x.innerText.trim().startsWith(arguments[0]) && !x.classList.contains('dis'));
         if (!m) return false; m.setAttribute('data-e2e', '1'); return true;`,
        [label]
      );
      if (!ok) return false;
      try {
        await this.req("POST", `/element/${await this.find('[data-e2e="1"]')}/click`, {});
        return true;
      } catch (e) {
        if (/not interactable|stale element|intercepted/.test(e.message)) return false;
        throw e;
      } finally {
        await this.script("document.querySelectorAll('[data-e2e]').forEach((x) => x.removeAttribute('data-e2e'));");
      }
    });
  }

  /** True when the visible page text contains `re` (a RegExp) in `css` (default: the whole body). */
  async has(re, css = "body") {
    const t = await this.script("const e = document.querySelector(arguments[0]); return e ? e.innerText : '';", [css]);
    return re.test(t);
  }

  /** Closes the first-run welcome if it is showing. */
  async skipWelcome() {
    await sleep(600);
    if (await this.visible(".onb")) {
      await this.click('.onb [data-ob="skip"]');
      await waitFor("the welcome to close", async () => !(await this.visible(".onb")));
    }
  }

  /** In the trust dialog: tick that the fingerprints were compared, then trust. */
  async confirmTrust() {
    await this.script("document.getElementById('tfm').click();");
    await this.clickLabel("They match: trust");
  }

  /** New connection -> Manual: types the address, compares nothing, stops at the trust dialog. */
  async addServer(agent, name) {
    await this.skipWelcome();
    await this.click("#rail [data-new]");
    await waitFor("the connection dialog", () => this.visible("#sh"));
    await this.fill("#sn", name);
    await this.fill("#sh", agent.host.split(":")[0]);
    await this.fill("#sp", agent.host.split(":")[1]);
    await this.clickLabel("Connect");
    await waitFor("the trust dialog", () => this.visible(".fpfull"));
  }

  /** The whole account sign-in, from the New connection dialog to the signed-in folder list. */
  async signInAccount(agent, name) {
    await this.addServer(agent, name);
    await this.confirmTrust();
    await waitFor("the sign-in dialog", () => this.visible("#siu"));
    await this.fill("#siu", agent.user);
    await this.fill("#sip", agent.password);
    await this.clickLabel("Sign in");
    await waitFor("the sign-in to finish", async () => !(await this.visible("#siu")));
  }

  /** The text of the snack bars on screen. */
  snacks() {
    return this.script("return [...document.querySelectorAll('.snack')].map((x) => x.innerText).join(' | ');");
  }

  /** Opens a rail destination by its label ("Servers", "Devices", ...). */
  async go(view) {
    // A click can land on a rail button the page is just replacing (after a sign-in the whole window
    // is drawn again), so it is repeated until the rail shows the view as the current one.
    for (let tries = 0; tries < 4; tries++) {
      await this.click(`#rail [data-go="${view}"]`);
      const end = Date.now() + 1500;
      while (Date.now() < end) {
        const on = await this.script(`const b = document.querySelector('#rail [data-go="${view}"]'); return !!b && b.classList.contains("on");`);
        if (on) return;
        await new Promise((r) => setTimeout(r, 50));
      }
    }
  }

  /** Replaces the contents of a field by typing, as a user does. */
  async fill(css, text) {
    const id = await this.find(css);
    await this.req("POST", `/element/${id}/clear`, {});
    await this.req("POST", `/element/${id}/value`, { text });
  }

  /** Sends keys to whatever has focus (Tab, Enter, text). */
  async press(keys) {
    const actions = [];
    for (const ch of keys) actions.push({ type: "keyDown", value: ch }, { type: "keyUp", value: ch });
    await this.req("POST", "/actions", { actions: [{ type: "key", id: "kbd", actions }] });
  }

  /** Presses on the middle of `css`, moves the pointer by (dx, dy) in steps and lets go: a real drag. */
  async drag(css, dx, dy) {
    const id = await this.find(css);
    const steps = 6;
    const moves = [];
    for (let i = 1; i <= steps; i++) moves.push({ type: "pointerMove", duration: 30, origin: "pointer", x: Math.round(dx / steps), y: Math.round(dy / steps) });
    try {
      await this.req("POST", "/actions", {
        actions: [{
          type: "pointer", id: "mouse", parameters: { pointerType: "mouse" },
          actions: [
            { type: "pointerMove", duration: 0, origin: { "element-6066-11e4-a52e-4f735466cecf": id }, x: 0, y: 0 },
            { type: "pointerDown", button: 0 },
            ...moves,
            { type: "pointerUp", button: 0 },
          ],
        }],
      });
    } finally {
      // A drag that failed half way must not leave the button held for the next test.
      await this.req("DELETE", "/actions").catch(() => {});
    }
  }

  async focused() {
    return this.script("const a = document.activeElement; return a ? (a.id || a.tagName) : null;");
  }

  async rows(css) {
    return this.script("return document.querySelectorAll(arguments[0]).length;", [css]);
  }

  async shot(file) {
    const b64 = await this.req("GET", "/screenshot");
    const png = Buffer.from(b64, "base64");
    if (file && process.env.E2E_SHOTS) writeFileSync(join(process.env.E2E_SHOTS, file), png);
    return png;
  }

  async rect() {
    return this.req("GET", "/window/rect");
  }

  async setRect(r) {
    return this.req("POST", "/window/rect", r);
  }

  async end() {
    try {
      await this.req("DELETE", "");
    } catch {
      /* the window may already be gone */
    }
  }
}

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
    s.on("error", reject);
  });
}

function agentCli(args) {
  return execFileSync(process.env.RFE_AGENT_BIN, args, { encoding: "utf8" });
}

/** A throwaway agent on a random port with its own data, and one account. */
export class Agent {
  static async start(user = "owner", password = "e2e-password") {
    const port = await freePort();
    const data = mkdtempSync(join(tmpdir(), "rfe-e2e-agent-"));
    const roots = mkdtempSync(join(tmpdir(), "rfe-e2e-roots-"));
    const host = `127.0.0.1:${port}`;
    const child = spawn(
      process.env.RFE_AGENT_BIN,
      ["-addr", host, "-name", "rfe-e2e", "-data", data, "-roots", roots],
      // No notify-send and no session bus: pairing requests never reach a real desktop.
      { env: { PATH: "/nonexistent-rfe-test-path" }, stdio: "ignore" }
    );
    const agent = new Agent(child, host, data, roots, user, password);
    await waitFor("the agent", () => new Promise((ok) => net.connect(port, "127.0.0.1").on("connect", function () { this.end(); ok(true); }).on("error", () => ok(false))));
    agentCli(["adduser", "-password", password, "-data", data, user]);
    return agent;
  }

  constructor(child, host, data, roots, user, password) {
    Object.assign(this, { child, host, data, roots, user, password });
  }

  fingerprint() {
    const line = agentCli(["status", "-data", this.data]).split("\n").find((l) => l.startsWith("fingerprint:"));
    return line.replace("fingerprint:", "").replace(/[^0-9a-fA-F]/g, "").toLowerCase();
  }

  devices() {
    return agentCli(["devices", "-data", this.data]);
  }

  /** A fresh one-time pairing code, as `rfe pair` prints it on its first line. */
  pairCode() {
    const first = agentCli(["pair", "-data", this.data]).split("\n")[0];
    const m = first.match(/^Pairing code:\s*(\S+)/);
    if (!m) throw new Error(`unexpected pair output: ${first}`);
    return m[1];
  }

  /** `rfe pair requests`: the waiting approve-on-PC requests. */
  pairRequests() {
    return agentCli(["pair", "requests", "-data", this.data]);
  }

  /** What the owner does on the PC: "accept" or "reject" the waiting request with this id. */
  answerPairRequest(verb, id) {
    return agentCli(["pair", verb, "-data", this.data, id]);
  }

  /** The id of the waiting request that shows this match code. */
  pairRequestId(matchCode) {
    const line = this.pairRequests().split("\n").find((l) => l.includes(matchCode));
    if (!line) throw new Error(`no waiting request with match code ${matchCode}`);
    return line.trim().split(/\s+/)[0];
  }

  revoke(id) {
    return agentCli(["revoke", "-data", this.data, id]);
  }

  stop() {
    this.child.kill();
    rmSync(this.data, { recursive: true, force: true });
    rmSync(this.roots, { recursive: true, force: true });
  }
}

/**
 * Forgets everything the app saved (settings, saved hosts, the webview's storage, which holds the
 * "welcome was shown" flag), so the next start is a first run. Call it while no app is running.
 */
export function resetAppState() {
  const dir = process.env.E2E_APP_DATA;
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  if (process.env.XDG_CACHE_HOME) rmSync(join(process.env.XDG_CACHE_HOME, "app.rfe.desktop"), { recursive: true, force: true });
}
