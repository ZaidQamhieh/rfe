// The window talks to the Rust core only through Tauri commands. A misspelled command or argument
// name is not caught by any compiler and fails only when the button is pressed, so this reads both
// sides: every `call('name', {args})` in ui/*.js must name a registered command and pass exactly
// its parameters (Tauri turns snake_case parameters into camelCase keys).
//
//   node --test desktop/ui-tests/*.test.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";

const src = new URL("../src-tauri/src/", import.meta.url);
const ui = new URL("../ui/", import.meta.url);

/** name -> [{name, optional}] for every #[tauri::command] fn. */
function commands() {
  const out = {};
  for (const f of readdirSync(src).filter((x) => x.endsWith(".rs"))) {
    const t = readFileSync(new URL(f, src), "utf8");
    const re = /#\[tauri::command[^\]]*\]\s*(?:#\[[^\n]*\n\s*)*(?:pub(?:\([a-z]+\))? )?(?:async )?fn (\w+)\s*(?:<[^>]*>)?\(/g;
    for (let m; (m = re.exec(t)); ) {
      let depth = 1, j = re.lastIndex;
      while (depth) { const c = t[j++]; depth += (c === "(") - (c === ")"); }
      const params = [];
      let cur = "", d = 0;
      for (const c of t.slice(re.lastIndex, j - 1)) {
        if ("<([".includes(c)) d++;
        if (">)]".includes(c)) d--;
        if (c === "," && d === 0) { params.push(cur); cur = ""; } else cur += c;
      }
      if (cur.trim()) params.push(cur);
      out[m[1]] = params
        .map((p) => p.trim()).filter(Boolean)
        .map((p) => { const i = p.indexOf(":"); return [p.slice(0, i).trim().replace(/^mut\s+/, ""), p.slice(i + 1)]; })
        .filter(([, ty]) => !/State<|AppHandle|Window/.test(ty))
        .map(([n, ty]) => ({ name: n.replace(/_(\w)/g, (_, c) => c.toUpperCase()), optional: ty.includes("Option<") }));
    }
  }
  return out;
}

/** The top-level keys of the object literal that starts at `s[i]` (`{`). */
function topKeys(s, i) {
  const keys = []; let d = 0;
  for (let k = i; k < s.length; k++) {
    const c = s[k];
    if ("{([".includes(c)) d++;
    if ("})]".includes(c)) { d--; if (d === 0) break; }
    if (d === 1 && (c === "{" || c === ",")) {
      const m = /^\s*(\w+)\s*(?=[:,}])/.exec(s.slice(k + 1));
      if (m) keys.push(m[1]);
    }
  }
  return new Set(keys);
}

const scripts = readdirSync(ui).filter((f) => f.endsWith(".js"));

test("every command the window calls is registered, with exactly its parameters", () => {
  const cmds = commands();
  assert.ok(Object.keys(cmds).length > 60, "found the commands");
  let calls = 0;
  const problems = [];
  for (const f of scripts) {
    const t = readFileSync(new URL(f, ui), "utf8");
    const re = /(?:call|invoke)\(\s*['"]([a-z_0-9]+)['"]\s*(?:,\s*(\{))?/g;
    for (let m; (m = re.exec(t)); ) {
      calls++;
      const [, name, brace] = m;
      const def = cmds[name];
      if (!def) { problems.push(`${f}: no command ${name}`); continue; }
      const keys = brace ? topKeys(t, m.index + m[0].length - 1) : new Set();
      const known = new Set(def.map((p) => p.name));
      const missing = def.filter((p) => !p.optional && !keys.has(p.name)).map((p) => p.name);
      const extra = [...keys].filter((k) => !known.has(k));
      if (missing.length || extra.length) problems.push(`${f}: ${name} missing [${missing}] unexpected [${extra}]`);
    }
  }
  assert.ok(calls > 80, "found the calls");
  assert.deepEqual(problems, []);
});

test("every command is registered with the app", () => {
  const lib = readFileSync(new URL("lib.rs", src), "utf8");
  const handler = lib.slice(lib.indexOf("generate_handler"));
  const missing = Object.keys(commands()).filter((n) => !new RegExp(`\\b${n}\\b`).test(handler));
  assert.deepEqual(missing, []);
});

test("the dev stand-in for the core answers every command the window calls", () => {
  const stub = readFileSync(new URL("../ui-dev/stub-backend.js", import.meta.url), "utf8");
  const missing = new Set();
  for (const f of scripts) {
    const t = readFileSync(new URL(f, ui), "utf8");
    for (const m of t.matchAll(/(?:call|invoke)\(\s*['"]([a-z_0-9]+)['"]/g)) if (!new RegExp(`\\b${m[1]}\\s*\\(`).test(stub)) missing.add(m[1]);
  }
  assert.deepEqual([...missing], []);
});
