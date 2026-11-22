/* Module registry: each script says what it is and what it needs, and runs once its needs are there, so the order of the
   <script> tags no longer matters. A script that never runs (a need that never appears, or a typo in a name) is reported by
   name when the page has loaded, instead of failing later with "undefined is not a function".
     rfeModule('pages', ['ui'], function () { ... });
   The three plain scripts that come before the modules count as ready when their global exists. */
(function () {
  const GLOBALS = { engine: 'Engine', icons: 'RFE', qr: 'QR' };
  const done = new Set(), waiting = [];
  const ready = (d) => done.has(d) || (GLOBALS[d] !== undefined && window[GLOBALS[d]] !== undefined);
  function flush() {
    let ran;
    do {
      ran = false;
      for (let i = 0; i < waiting.length; i++) {
        const m = waiting[i];
        if (!m.deps.every(ready)) continue;
        waiting.splice(i, 1); done.add(m.name); ran = true; m.fn(); break;
      }
    } while (ran);
  }
  const rfeModule = (name, deps, fn) => {
    if (typeof name !== 'string' || !Array.isArray(deps) || typeof fn !== 'function') throw new TypeError('rfeModule(name, deps, fn)');
    if (done.has(name) || waiting.some((m) => m.name === name)) throw new Error('module "' + name + '" is defined twice');
    waiting.push({ name, deps, fn }); flush();
  };
  /* The modules that have not run, each with the needs that are missing. Empty when everything started. */
  rfeModule.pending = () => waiting.map((m) => ({ name: m.name, missing: m.deps.filter((d) => !ready(d)) }));
  rfeModule.done = () => [...done];
  const report = () => {
    flush();
    const p = rfeModule.pending();
    if (p.length) throw new Error('modules that never started: ' + p.map((m) => m.name + ' (needs ' + m.missing.join(', ') + ')').join('; '));
  };
  window.rfeModule = rfeModule;
  document.addEventListener('DOMContentLoaded', flush);
  window.addEventListener('load', report);
})();
