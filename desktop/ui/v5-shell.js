/* RFE v5 shell glue: small behaviours of the new sidebar and slim top bar that do not belong to any page.
   The sidebar and top bar themselves are drawn by renderRail / buildTop in pages.js; this file only adds
   the Ctrl+\ shortcut they rely on (the Overview page is drawn by v5-transfers.js). */
rfeModule('v5-shell', ['ui'], function () {
  'use strict';
  const A = window.A; if (!A) return;

  /* Helpers the v5 pages share. v5on: true while the v5 wrapper (class "d5" on #app) is on the window.
     v5em: one of the mockup's colour pictures, n pixels wide, from the emoji folder beside the scripts. */
  const EMOJI = new URL('emoji/', (document.currentScript && document.currentScript.src) || location.href).href;
  A.v5on = () => { const a = document.getElementById('app'); return !!a && a.classList.contains('d5'); };
  /* The closed "Technical details" fold of the v5 pages: rows are [label, value] (a third item `true` sets the value in the normal font); skipEmpty leaves out rows with no value; cls adds a class to the fold. */
  A.v5tech = (rows, extra, skipEmpty, cls) => '<details class="b-tech' + (cls ? ' ' + cls : '') + '"><summary>Technical details</summary><dl class="k-kv">' + (skipEmpty ? rows.filter((r) => r[1]) : rows).map((r) => '<dt>' + A.esc(r[0]) + '</dt><dd' + (r[2] ? '' : ' class="mono"') + '>' + A.esc(r[1]) + '</dd>').join('') + '</dl>' + (extra || '') + '</details>';
  A.v5em = (name, n) => '<img class="k-e" src="' + EMOJI + name + '.svg" width="' + n + '" height="' + n + '" alt="" draggable="false">';

  /* Ctrl+\ (Cmd+\ on a Mac) collapses or expands the sidebar, the same as the button in its top row and Ctrl+. */
  document.addEventListener('keydown', (e) => {
    if (!(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey || e.key !== '\\') return;
    if (document.querySelector('.scrim')) return; /* a dialog keeps the keys to itself */
    e.preventDefault(); A.toggleRail();
  });

  /* Hover-to-peek: the icons-only sidebar opens over the page while the pointer rests on it, and closes when it leaves.
     The page does not move (the column stays narrow); a short delay keeps a pass of the pointer from opening it. */
  const app = () => document.getElementById('app');
  let timer = 0;
  const close = () => { clearTimeout(timer); const a = app(); if (a && a.classList.contains('sbpeek')) { a.classList.remove('sbpeek'); a.classList.add('sbmin'); } };
  document.addEventListener('mouseover', (e) => {
    const a = app(); if (!a || !e.target.closest || !e.target.closest('#rail') || !a.classList.contains('sbmin') || a.classList.contains('sbpeek')) return;
    clearTimeout(timer);
    timer = setTimeout(() => { if (a.classList.contains('sbmin') && !document.querySelector('.scrim')) { a.classList.remove('sbmin'); a.classList.add('sbpeek'); } }, A.T.linger);
  });
  document.addEventListener('mouseout', (e) => {
    if (!e.target.closest || !e.target.closest('#rail')) return;
    const to = e.relatedTarget; if (to && to.closest && to.closest('#rail')) return;
    close();
  });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); });
  A.railPeekClose = close;
});
