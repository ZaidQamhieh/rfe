/* RFE Tonal - shell, pages, dialogs, palette, keyboard, main loop. */
rfeModule('pages', ['ui'], function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, fic = A.fic, st = A.state, Pane = A.Pane;
  const KEY = 'rfe-tonal-settings-v1';
  Object.assign(S, A.store.get(KEY, {}));
  st.sheetOpen = false; /* the strip always starts folded; it opens when a transfer is queued */
  const save = () => { if (/[?&]thumb=1/.test(location.search)) return; A.store.set(KEY, S); };
  /* The page scales with the window by itself (the root size in app.css: 16px at 1280px wide, up to 24px at 2560px wide). Interface size multiplies that, and the smallest window grows with it. */
  let scaleNow = 1;
  const UI_SIZE_BASE = 100, UI_SCALE_MIN = 0.9, UI_SCALE_MAX = 1.3; /* the interface size in percent (100 is normal) and the scale range it maps to */
  A.applyZoom = () => {
    const m = Math.min(UI_SCALE_MAX, Math.max(UI_SCALE_MIN, (+S.uiSize || UI_SIZE_BASE) / UI_SIZE_BASE));
    document.documentElement.style.setProperty('--ui', String(m)); A.uReset();
    if (m !== scaleNow) { scaleNow = m; E.call('desktop_set_scale', { scale: m }).catch(() => { scaleNow = 0; }); }
  };
  const applyTheme = () => {
    const dark = S.theme === 'dark' || (S.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'; document.documentElement.dataset.density = S.density === 'compact' ? 'compact' : 'comfortable';
    syncThemeBtn();
  };

  function syncThemeBtn() {
    const b = $('#btnTheme'); if (!b) return; const dark = document.documentElement.dataset.theme === 'dark';
    b.innerHTML = ic(dark ? 'moon' : 'sun'); b.title = 'Theme: ' + (S.theme === 'system' ? 'system (' + (dark ? 'dark' : 'light') + ')' : S.theme);
  }
  function setTheme(v) { S.theme = v; save(); applyTheme(); syncThemeBtn(); if (st.view === 'settings') pageSettings(); }
  function themeMenu(btn) {
    const r = btn.getBoundingClientRect(); const mk = (v, icn, l) => ({ icon: S.theme === v ? 'check' : icn, label: l, onClick: () => setTheme(v) });
    A.menu(r.right, r.bottom + 6, [{ head: 'Theme' }, mk('light', 'sun', 'Light'), mk('dark', 'moon', 'Dark'), mk('system', 'monitor', 'Match system')], { right: true });
  }
  A.setTheme = setTheme; A.save = save;
  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => { if (S.theme === 'system') { applyTheme(); syncThemeBtn(); } });

  /* ---------- panes ---------- */
  A.mainPane = new Pane('main', null, { onNav: () => { renderDetail(); renderChip(); }, onSelect: () => renderDetail(), onSpin: () => { const b = $('#btnRefresh'); if (b) { b.classList.add('spin'); setTimeout(() => b.classList.remove('spin'), A.T.spin); } } });
  A.localPane = new Pane('local', 'local', { compact: true, rh: 40, onSelect: () => renderLocalFooter(), onNav: () => renderLocalFooter() });
  A.activePane = A.mainPane;
  A.setActive = (p) => { A.activePane = p; };
  A.mainHost = () => A.mainPane.host;
  A.setMainHost = (h) => { A.mainPane.setHost(h); renderChip(); renderDetail(); renderRail(); };

  /* ---------- routing / layout ---------- */
  /* Opens a page. In the v5 window one tab, Devices, holds the computers and the phones paired with each: 'devices' and 'computers' both open it ('devices' with the phones section shown). */
  A.go = (v) => {
    if (v === 'computers') v = 'servers'; /* alias: the page is called Devices now, and its view id stays 'servers' */
    if (v === 'devices' && A.v5on && A.v5on() && A.v5Devices) { A.v5Devices.reveal(); v = 'servers'; }
    if (st.view === v && v !== 'files') { renderStage(); return; }
    st.view = v; closePop(); renderAll(); if (v === 'tools' && A.toolOpened) A.toolOpened();
  };
  function layout() {
    const tree = st.view === 'files' && !!A.v5Files && A.v5Files.on(); /* the v5 Files page keeps its details inside the page, so the side column is not used */
    const v5r = !!A.v5Devices && A.v5Devices.rail(); /* the v5 Devices and History pages carry their own right panel (v5-devices.js) */
    const m = $('#main'); m.classList.toggle('v5r', v5r); m.classList.toggle('nosid', (st.view !== 'files' || tree) && !v5r); m.classList.toggle('nosheet', st.view === 'transfers' || tree); /* the v5 Files page has no floating Transfers pill (the sidebar's Transfers page shows them) */
    m.classList.toggle('sidehid', st.view === 'files' && !!S.sideHide);
    m.style.setProperty('--sidew', sideNow().toFixed(2) + 'rem');
    const gp = $('#rszSide'); if (gp) { gp.setAttribute('aria-valuemin', '0'); gp.setAttribute('aria-valuemax', String(Math.round(sideMax()))); gp.setAttribute('aria-valuenow', S.sideHide ? '0' : String(Math.round(sideNow()))); }
    $('#side').style.display = (st.view === 'files' && !S.sideHide && !tree) || v5r ? '' : 'none'; $('#sheet').classList.toggle('min', !st.sheetOpen);
    const bx = $('#btnXfer'); if (bx) { bx.hidden = st.view === 'transfers'; bx.setAttribute('aria-pressed', String(!!st.sheetOpen)); bx.title = st.sheetOpen ? 'Hide transfers' : 'Show transfers'; }
    const railWideNow = !!S.railWide && remW() >= WIDE_MIN, railDrawerNow = !!st.drawer && remW() < WIDE_MIN;
    $('#app').classList.toggle('wide', railWideNow);
    $('#app').classList.toggle('drawer', railDrawerNow);
    $('#app').classList.toggle('collapsed', !railWideNow && !railDrawerNow); /* the navigation shows icons only */
    $('#app').classList.toggle('sbmin', !railWideNow && !railDrawerNow && !$('#app').classList.contains('sbpeek')); /* the v5 sidebar's own name for the icons-only column */
    const sbar = $('.sbar'); if (sbar) sbar.hidden = st.view !== 'search'; /* the search field shows only on the Search page, until the search panel replaces it */
    const b = $('#btnSide'); if (b) { b.hidden = st.view !== 'files'; b.setAttribute('aria-pressed', String(!S.sideHide)); b.title = S.sideHide ? 'Show details' : 'Hide details'; }
  }
  /* ---------- sizes, in rem so they scale with the window: the side panel and the transfers strip are dragged to the size wanted, or folded away ---------- */
  const SIDE_MIN = 18, SIDE_HIDE = 12, SIDE_DEF = 21, WIDE_MIN = 75;
  /* The list always keeps room: about 34 rem across and 26 rem high (its header, four rows and its footer). */
  const LIST_W = 34;
  const SIDE_STEP_REM = 1.5; /* in rem: the gap kept beside the list, the slide-in distance of the side panel and one keyboard step of its grip */
  function clamp(v, a, b) { return Math.max(a, Math.min(b, v)); }
  const remPx = () => A.rem(1);
  const remW = () => innerWidth / remPx();
  const sideMax = () => { const m = $('#main'); const w = m ? m.clientWidth / remPx() : 0; return w ? Math.max(SIDE_MIN, Math.min(w * 0.6, w - SIDE_STEP_REM - LIST_W)) : 40; };
  const sideNow = () => clamp(S.sideR || SIDE_DEF, SIDE_MIN, sideMax());
  /* Runs `move(dx, dy)` (in px) while the pointer is down on `el`, then `end(moved)`. */
  function dragging(el, e, move, end) {
    if (e.button !== undefined && e.button !== 0) return;
    e.preventDefault(); const x0 = e.clientX, y0 = e.clientY; let moved = false;
    try { el.setPointerCapture(e.pointerId); } catch (err) { /* not capturable */ }
    const onMove = (ev) => { const dx = ev.clientX - x0, dy = ev.clientY - y0; if (!moved && Math.abs(dx) + Math.abs(dy) > 3) moved = true; if (moved) move(dx, dy); };
    const onUp = () => { el.removeEventListener('pointermove', onMove); el.removeEventListener('pointerup', onUp); el.removeEventListener('pointercancel', onUp); document.body.classList.remove('resizing'); document.body.style.cursor = ''; end(moved); };
    el.addEventListener('pointermove', onMove); el.addEventListener('pointerup', onUp); el.addEventListener('pointercancel', onUp); document.body.classList.add('resizing'); document.body.style.cursor = getComputedStyle(el).cursor;
  }
  const refit = () => { [A.mainPane, A.localPane].forEach((p) => p && p.fit && p.fit()); A.mainPane.render && A.mainPane.render(); };
  /* The columns cannot interpolate between one track and two, so the panel itself carries the motion: it slides in from the side. It is never faded and no copy of it lingers, so there is no frame where it is half gone. */
  A.toggleSide = () => {
    const side = $('#side'); const calm = matchMedia && matchMedia('(prefers-reduced-motion:reduce)').matches;
    S.sideHide = !S.sideHide; save(); layout(); refit();
    if (!S.sideHide && !calm && side.animate) side.animate([{ transform: 'translateX(' + SIDE_STEP_REM + 'rem)' }, { transform: 'none' }], { duration: 220, easing: 'cubic-bezier(.2,.8,.2,1)' });
  };
  const grip = $('#rszSide');
  grip.addEventListener('pointerdown', (e) => {
    const w0 = S.sideHide ? 0 : sideNow(); const dir = document.documentElement.dir === 'rtl' ? -1 : 1;
    dragging(grip, e, (dx) => { const want = w0 - dir * dx / remPx(); S.sideHide = want < SIDE_HIDE; if (!S.sideHide) S.sideR = clamp(want, SIDE_MIN, sideMax()); layout(); }, (moved) => { if (!moved) S.sideHide = !S.sideHide; save(); layout(); refit(); });
  });
  grip.addEventListener('keydown', (e) => {
    const rt = document.documentElement.dir === 'rtl' ? -1 : 1; const k = { ArrowLeft: SIDE_STEP_REM * rt, ArrowRight: -SIDE_STEP_REM * rt }[e.key]; if (!k && e.key !== 'Home' && e.key !== 'Enter' && e.key !== ' ') return; e.preventDefault();
    if (e.key === 'Enter' || e.key === ' ') S.sideHide = !S.sideHide; else if (e.key === 'Home') { S.sideR = SIDE_DEF; S.sideHide = false; } else { const was = S.sideHide ? 0 : sideNow(); const want = was + k; S.sideHide = want < SIDE_HIDE; if (!S.sideHide) S.sideR = clamp(want, SIDE_MIN, sideMax()); }
    save(); layout(); refit();
  });
  grip.addEventListener('dblclick', () => { S.sideR = SIDE_DEF; S.sideHide = false; save(); layout(); refit(); });
  let lastUnit = 0;
  addEventListener('resize', () => { layout(); renderRail(); const u = A.u(); if (u !== lastUnit) { lastUnit = u; refit(); } else [A.mainPane, A.localPane].forEach((p) => p && p.fit && p.fit()); });
  function renderAll() { layout(); renderRail(); renderChip(); renderStage(); renderSide(); renderSheet(true); const nm = { home: 'Overview', files: 'Files', servers: A.v5on && A.v5on() ? 'Devices' : 'Computers', devices: 'Devices', transfers: 'Transfers', search: 'Search', tools: 'Tools', history: 'History', settings: 'Settings' }[st.view]; document.title = (nm ? nm + ' - ' : '') + 'RFE'; }
  A.renderAll = renderAll;
  A.renderRail = () => renderRail(); /* A control that redraws the page keeps the keyboard focus: the same switch or segment button gets it back. */
  A.pageSettings = () => {
    const a = document.activeElement; const inSeg = a && a.closest && a.closest('[data-set]');
    const sel = a && a.dataset && $('#stage').contains(a) ? (a.dataset.sw ? '[data-sw="' + a.dataset.sw + '"]' : a.dataset.sx ? '[data-sx="' + a.dataset.sx + '"]' : a.dataset.snav ? '[data-snav="' + a.dataset.snav + '"]' : inSeg && a.dataset.v !== undefined ? '[data-set="' + inSeg.dataset.set + '"] [data-v="' + a.dataset.v + '"]' : '') : '';
    pageSettings();
    document.querySelectorAll('#stage .seg[data-set]').forEach((g) => { const b = g.closest('.sr') && g.closest('.sr').querySelector('.l b'); g.setAttribute('role', 'group'); if (b) g.setAttribute('aria-label', b.textContent.trim()); });
    if (sel) { const n = $(sel, $('#stage')); if (n) n.focus(); }
  }; A.renderDetail = () => renderDetail();

  /* ---------- rail ---------- */
  function counts() {
    const T = E.tasks; const g = { active: 0, attn: 0, queued: 0, done: 0 }; for (const t of T) g[A.groupOf(t)]++; return g;
  }
  /* Draws the v5 sidebar: the logo row with its collapse button, the nine destinations (Overview has no page yet and opens Files), the computers saved in the app each with its real state, and "Add a computer". Counts and states come from the engine; with no computer saved the list is empty. */
  /* A computer's connection state: the dot's tone and the short word beside it (nothing for a computer that is up). */
const PCS = { online: ['ok', ''], refused: ['bad', 'needs OK'], trust: ['warn', 'needs OK'], login: ['warn', 'sign in'], connecting: ['warn', 'connecting'], lost: ['bad', 'lost'], offline: ['off', 'offline'], disconnected: ['off', 'not connected'] };
  A.PC_STATE = PCS;
  function renderRail() {
    const c = counts();
    const wide = remW() >= WIDE_MIN ? !!S.railWide : !!st.drawer;
    const N = (v, i, l, extra) => '<button class="k-navi' + (st.view === v ? ' on' : '') + '" data-go="' + v + '" aria-label="' + l + '" title="' + l + '"' + (st.view === v ? ' aria-current="page"' : '') + '>' + ic(i, { size: 18 }) + '<span>' + l + '</span>' + (extra || '') + '</button>';
    const badge = (n, bad) => '<em class="k-badge' + (bad ? ' bad' : '') + '">' + n + '</em>';
    /* A computer's dot and the short word beside it: nothing for a computer that is up, plain words for the rest. */
    const pc = (s) => { const p = PCS[s.state] || ['off', STL[s.state] || s.state]; return '<button class="pc" data-pc="' + esc(s.id) + '" title="' + esc(s.name) + '"><i class="k-dot ' + p[0] + '"></i><span class="pn">' + esc(s.name) + '</span>' + (p[1] ? '<small>' + (s.state === 'offline' && s.incompatible ? 'too old' : p[1]) + '</small>' : '') + '</button>'; };
    const lab = wide ? 'Collapse navigation' : 'Expand navigation';
    /* Redrawing the rail must not drop the keyboard focus from the button that was just used. */
    const fa = document.activeElement; const fsel = fa && $('#rail').contains(fa) ? (fa.dataset.go ? '[data-go="' + fa.dataset.go + '"]' : fa.dataset.new ? '[data-new]' : fa.dataset.menu ? '[data-menu]' : fa.dataset.pc ? '[data-pc="' + fa.dataset.pc + '"]' : '') : '';
    $('#rail').innerHTML = '<div class="lg"><svg class="lg-key" viewBox="100 100 294 294" width="22" height="22" fill="none" stroke="currentColor" stroke-width="48" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="196" cy="196" r="66"/><path d="M246 246L364 364M330 330L362 298M296 296L318 274"/><circle cx="196" cy="196" r="24" fill="currentColor" stroke="none"/></svg><span>RFE</span><button class="sb-t" data-menu="1" title="' + lab + ' (Ctrl+. or Ctrl+\\)" aria-label="' + lab + '" aria-keyshortcuts="Control+Period Control+Backslash" aria-expanded="' + wide + '">' + ic('panel', { size: 16 }) + '</button></div>' +
      N('home', 'grid', 'Overview') + N('files', 'folder', 'Files') + N('servers', 'server', A.v5on && A.v5on() ? 'Devices' : 'Computers', A.v5on && A.v5on() && A.X && A.X.inbox && A.X.inbox.length ? badge(A.X.inbox.length, true) : '') + (A.v5on && A.v5on() ? '' : N('devices', 'phone', 'Devices', A.X && A.X.inbox && A.X.inbox.length ? badge(A.X.inbox.length, true) : '')) +
      N('transfers', 'swap', 'Transfers', c.attn ? badge(c.attn) : c.active + c.queued ? badge(c.active + c.queued) : '') +
      N('tools', 'layers', 'Tools') + N('history', 'history', 'History') + N('settings', 'sliders', 'Settings') +
      '<h4>Computers</h4>' + E.servers.map(pc).join('') + '<button class="pc" data-new="1" title="Add a computer" aria-label="Add a computer">' + ic('plus', { size: 14 }) + 'Add a computer</button><span class="gap"></span>';
    if (fsel) { const n = $(fsel, $('#rail')); if (n) n.focus(); }
  }
  $('#rail').addEventListener('click', (e) => {
    e.railHandled = true; /* the page-wide click that closes the drawer must not see this one */
    const b = e.target.closest('[data-go]'), p = e.target.closest('[data-pc]');
    if (b) { st.drawer = false; layout(); A.go(b.dataset.go); if (b.dataset.go === 'search') { const q = $('#q'); if (q) q.focus(); } } else if (e.target.closest('[data-new]')) { st.drawer = false; layout(); A.serverDialog(); }
    else if (p) { /* A computer that is up (or can be connected) opens in Files; any other state is handled on the Computers page. */
      st.drawer = false; layout(); const s = E.server(p.dataset.pc);
      if (s && (s.state === 'online' || s.state === 'disconnected')) { if (st.view !== 'files') A.go('files'); A.setMainHost(s.id); if (s.state === 'disconnected') E.connect(s.id); } else A.go('servers');
    } else if (e.target.closest('[data-menu]')) { A.toggleRail(); const m = $('#rail [data-menu]'); if (m) m.focus(); }
  });
  /* Collapses the navigation to icons, or expands it with its labels (button in the rail's top row, Ctrl+.). In a wide window the choice is saved in the settings; in a narrow one the labels open as a drawer for the moment only. */
  A.toggleRail = () => {
    if (remW() >= WIDE_MIN) { S.railWide = !S.railWide; save(); } else st.drawer = !st.drawer;
    layout(); renderRail(); A.mainPane.render && A.mainPane.render();
  };
  /* The drawer (a window under 75 rem (WIDE_MIN)) closes with Escape or a click anywhere else. */
  const closeDrawer = () => { if (!st.drawer) return; st.drawer = false; layout(); renderRail(); };
  document.addEventListener('click', (e) => { if (st.drawer && !e.railHandled && !e.target.closest('#rail')) closeDrawer(); });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && st.drawer) { closeDrawer(); const m = $('#rail [data-menu]'); if (m) m.focus(); } });

  /* ---------- top bar ---------- */
  function buildTop() {
    /* The slim v5 top bar: Pair a phone, the notifications bell and the account button. The search field is there too but hidden except on the Search page (layout() shows it), until the search panel replaces it. */
    $('#top').innerHTML = '<label class="sbar" hidden>' + ic('search') + '<input id="q" placeholder="Search files on this computer" aria-label="Search" autocomplete="off"><button type="button" class="k-ib" id="qclr" aria-label="Clear search" hidden>' + ic('x', { size: 16 }) + '</button><kbd>/</kbd></label><button class="k-btn sm ghost" id="btnSearch" title="Search (Ctrl K)">' + ic('search', { size: 16 }) + '<span>Search</span></button><button class="k-btn sm ghost" id="btnQR" title="Pair a phone (QR)">' + ic('qr', { size: 16 }) + '<span>Pair a phone</span></button><button class="k-ib" id="btnBell" title="Notifications" aria-label="Notifications">' + ic('bell', { size: 16 }) + '<i class="k-nb" id="bd" hidden></i></button><button class="k-av" id="btnAv" title="Account" aria-label="Account">Z</button>';
    const q = $('#q'); let tm; const qc = $('#qclr'); const syncClr = () => { qc.hidden = !q.value; const k = $('.sbar kbd'); if (k) k.style.display = q.value ? 'none' : ''; }; q.addEventListener('input', syncClr); qc.addEventListener('click', () => { q.value = ''; syncClr(); q.focus(); });
    q.addEventListener('input', () => { clearTimeout(tm); tm = setTimeout(() => runQuery(q.value, false), A.T.search); });
    q.addEventListener('keydown', (e) => { if (e.key === 'Enter') { clearTimeout(tm); runQuery(q.value, true); } if (e.key === 'Escape') { clearTimeout(tm); q.value = ''; q.blur(); } });
    $('#side').insertAdjacentHTML('afterend', '<button class="ib" id="btnSide" aria-label="Details panel" aria-pressed="true">' + ic('panel') + '</button>');
    $('#btnSide').addEventListener('click', () => A.toggleSide());
    $('#btnQR').addEventListener('click', () => A.pairDialog && A.pairDialog());
    $('#btnBell').addEventListener('click', (e) => bellMenu(e.currentTarget));
    /* The account menu also holds the theme choice, which no longer has a button of its own. */
    $('#btnAv').addEventListener('click', (e) => { const btn = e.currentTarget, r = btn.getBoundingClientRect(); A.menu(r.right - 224 * A.u(), r.bottom + 6, [{ head: (() => { const sv = E.server(A.mainPane.host); return sv && sv.signedIn && sv.user ? sv.user + '@' + sv.name : 'Not signed in'; })() }, { icon: 'phone', label: 'Pair a phone', onClick: () => A.pairDialog && A.pairDialog() }, { icon: 'user', label: 'Accounts…', onClick: () => A.accountsDialog && A.accountsDialog() }, { icon: 'sun', label: 'Theme', onClick: () => themeMenu(btn) }, { icon: 'gear', label: 'Settings', onClick: () => A.go('settings') }, { icon: 'keyboard', label: 'Keyboard shortcuts', onClick: shortcutsDialog }]); });
  }
  function runQuery(v, commit) {
    st.q = v.trim(); /* Only a search the user confirmed goes to Recent, not each word typed on the way. */ if (st.q && commit !== false && A.recordQuery) A.recordQuery(st.q);
    if (!st.q) { if (st.view === 'search') { A.searchRun = null; renderStage(); } return; }
    if (st.view !== 'search') { st.view = 'search'; layout(); renderRail(); renderSide(); renderSheet(true); }
    startSearch();
  }
  A.runQuery = (v) => { const q = $('#q'); if (q) q.value = v; runQuery(v); };
  function renderChip() {
    const h = A.mainPane.host; const s = E.server(h) || { name: 'No computer', state: 'disconnected', latency: 0, via: '' }; const chip = $('#chip');
    const state = h === 'local' ? 'online' : s.state; const lat = h === 'local' ? 'local files' : state === 'online' ? s.latency + ' ms · ' + s.via : state === 'lost' ? 'connection lost' : state === 'offline' ? (s.incompatible ? 'agent too old' : 'offline') : state === 'connecting' ? 'connecting…' : state === 'trust' ? 'verify key' : state === 'refused' ? 'key changed' : state === 'login' ? 'sign in needed' : 'not connected';
    const L = +s.latency || 0, bars = state !== 'online' ? 0 : h === 'local' ? 4 : L < 30 ? 4 : L < 80 ? 3 : L < 200 ? 2 : 1;
    if (chip) chip.innerHTML = '<i class="dot ' + state + '"></i>' + esc(A.hostName(h)) + (bars ? '<span class="sig" aria-hidden="true">' + [1, 2, 3, 4].map((n) => '<i' + (n > bars ? ' class="off"' : '') + '></i>').join('') + '</span>' : '') + '<small>' + esc(lat) + '</small>' + ic('chevron-down');
    const av = $('#btnAv'); if (av) { const u = h !== 'local' && s.signedIn && s.user; av.innerHTML = u ? esc(String(u)[0].toUpperCase()) : ic('user'); } /* the account button shows who is signed in */
    const q = $('#q'); if (q) q.placeholder = 'Search in ' + (h === 'local' ? 'this computer' : s.name);
  }
  function closePop() { A.closeMenus(); }
  function bellMenu(btn) {
    const r = btn.getBoundingClientRect(); const ns = E.notes.slice(0, 12);
    const ni = (n) => '<div role="listitem" class="ni ' + n.kind + (n.read ? '' : ' unread') + '" title="' + esc(new Date(n.at).toLocaleString(U.lang())) + '">' + ic(n.kind === 'error' ? 'alert-circle' : n.kind === 'ok' ? 'check-circle' : 'info') + '<div><span>' + esc(n.text) + '</span><small>' + A.ago(n.at) + '</small></div></div>';
    const day0 = new Date().setHours(0, 0, 0, 0); const today = ns.filter((n) => n.at >= day0), earlier = ns.filter((n) => n.at < day0);
    const html = ns.length ? (today.length ? '<div class="mh">Today</div>' + today.map(ni).join('') : '') + (earlier.length ? '<div class="mh">Earlier</div>' + earlier.map(ni).join('') : '') : '<div role="listitem" class="ni">No notifications</div>';
    const unread = ns.some((n) => !n.read);
    const m = A.menu(r.right, r.bottom + 6, [], { cls: 'notif', right: true }); m.setAttribute('role', 'dialog'); m.setAttribute('aria-label', 'Notifications'); m.innerHTML = '<div class="mh">Notifications</div><div role="list">' + html + '</div>' + (unread ? '<hr><div class="mi" data-clear="1" role="button" tabindex="0">' + ic('check') + '<span>Mark all as read</span></div>' : '');
    const clr = $('[data-clear]', m); m.tabIndex = -1; (clr || m).focus();
    if (clr) clr.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); clr.click(); } });
    m.addEventListener('click', (e) => { if (e.target.closest('[data-clear]')) { E.notes.forEach((n) => (n.read = true)); updateBell(); A.closeMenus(); } });
    /* They count as read once the list is closed, not while it is still being read. */
    const lay = $('#layer'); const mo = new MutationObserver(() => { if (!m.isConnected) { mo.disconnect(); E.notes.forEach((n) => (n.read = true)); updateBell(); } }); mo.observe(lay, { childList: true });
  }
  function updateBell() { const n = E.notes.filter((x) => !x.read).length; const b = $('#bd'); if (b) { b.hidden = !n; b.textContent = n; } const bb = $('#btnBell'); if (bb) bb.setAttribute('aria-label', n ? 'Notifications, ' + n + ' unread' : 'Notifications'); }

  /* ---------- stage ---------- */
  /* Draws the page of the current view into the stage. Settings goes through A.pageSettings so a full redraw gives the focused control its focus back. */
  function renderStage() {
    const s = $('#stage');
    if (st.view === 'files' && A.v5Files && A.v5Files.on()) { s.className = 'stage'; A.v5Files.mount(s); A.v5Files.warm(); return; } /* the v5 window draws Files as one tree over the same main Pane (v5-files.js) */
    if (st.view === 'files') { s.className = 'stage'; s.innerHTML = '<div id="paneMain" style="display:flex;flex-direction:column;flex:1;min-height:0"></div>'; A.mainPane.mount($('#paneMain')); return; }
    if (A.v5Pages && A.v5Pages.draws(st.view)) { s.scrollTop = 0; A.v5Pages.mount(s); if (A.afterPage) A.afterPage(st.view); return; } /* the v5 window draws Overview, Transfers and Search itself (v5-transfers.js) */
    s.className = 'stage page'; s.scrollTop = 0;
    ({ servers: serversPage, transfers: pageTransfers, search: pageSearch, history: pageHistory, settings: () => A.pageSettings(), devices: () => A.pageDevices(), tools: () => A.pageTools() })[st.view](); if (A.afterPage) A.afterPage(st.view);
  }
  /* ---------- side (details + local) ---------- */
  function renderSide() {
    const side = $('#side'); if (st.view !== 'files') return;
    if (A.v5Files && A.v5Files.on()) { side.innerHTML = ''; return; } /* the tree page carries its own details panel */
    side.innerHTML = '<div class="stabs2" role="tablist"><button role="tab" aria-selected="' + (st.sideTab === 'details') + '" data-tab="details" class="' + (st.sideTab === 'details' ? 'on' : '') + '">Details</button><button role="tab" aria-selected="' + (st.sideTab === 'local') + '" data-tab="local" class="' + (st.sideTab === 'local' ? 'on' : '') + '">Local files</button></div><div class="sbody"><div class="detail" id="detail"' + (st.sideTab === 'details' ? '' : ' hidden') + '></div><div id="localWrap" style="display:' + (st.sideTab === 'local' ? 'flex' : 'none') + ';flex-direction:column;flex:1;min-height:0"><div id="paneLocal" style="display:flex;flex-direction:column;flex:1;min-height:0"></div><div class="da" id="localFoot" style="border-top:1px solid var(--outline-var);padding-top:.75rem"></div></div></div>';
    A.localPane.mount($('#paneLocal')); renderDetail(); renderLocalFooter();
  }
  /* A click on the details panel: a tab, or one of its action buttons (data-do). The v5 tree page calls it for its own panel too. */
  A.sideClick = (e) => {
    const t = e.target.closest('[data-tab]'); if (t) { st.sideTab = t.dataset.tab; renderSide(); return; }
    const a = e.target.closest('[data-do]'); if (!a) return; const p = A.mainPane; const sel = p.selected();
    switch (a.dataset.do) {
      case 'download': p.transfer(); break; case 'rename': p.startRename(sel[0]); break; case 'delete': p.del(); break;
      case 'preview': p.preview(sel[0]); break; case 'open': if (sel[0]) p.open(sel[0]); break; case 'newfolder': p.startNew(); break;
      case 'upload': uploadFab(); break;
      case 'share': if (sel[0] && A.shareDialog) A.shareDialog(p.host, p.path, sel[0]); break;
      case 'more': A.openDetailMenu(a, p.ctxItems(true)); break;
      case 'uploadsel': A.localPane.transfer(); break;
      case 'settings': A.chooseDownloadFolder().then(() => renderDetail()); break; /* the folder picker opens here and the file stays selected */
    }
  };
  $('#side').addEventListener('click', A.sideClick);
  function renderLocalFooter() {
    const f = $('#localFoot'); if (!f) return; const sel = A.localPane.selected(); const m = A.mainPane; const ok = m.host !== 'local' && E.connected(m.host);
    f.innerHTML = sel.length ? '<button class="btn f" data-do="uploadsel"' + (ok ? '' : ' disabled') + ' title="Upload ' + sel.length + ' to ' + esc(A.hostName(m.host)) + '">' + ic('upload') + 'Upload</button><span style="color:var(--on-var);font-size:.7812rem;align-self:center">' + (ok ? 'into ' + esc(m.path) : m.host === 'local' || !E.server(m.host) ? 'Open a computer in the file list first' : esc(A.hostName(m.host)) + ' is not connected') + '</span>' : '<div style="color:var(--on-var);font-size:.8125rem;display:flex;gap:.5rem;align-items:center">' + ic('info') + 'Select files here, then upload. Or drag them onto the file list.</div>';
  }
  /* ---------- details panel (the right column of Files) ----------
     One layout for a file, a folder, several items and "nothing selected": a preview tile, the name with a small type icon,
     one short line of facts, where it is, then the actions (Download, Open, Share link and a "…" menu for the rest).
     Technical facts (full path, permissions, owner, download folder) sit in a closed "Technical details" block. */
  const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  /* A date for the short facts line, "1 Nov 09:12" (the year only when it is not this year); other languages use the shared date format. */
  function shortWhen(ms) {
    if (!ms) return '';
    if (!/^en/.test(U.lang())) return U.fmtDate(ms);
    const d = new Date(ms); const two = (x) => String(x).padStart(2, '0');
    return d.getDate() + ' ' + MONTHS[d.getMonth()] + (d.getFullYear() !== new Date(E.now()).getFullYear() ? ' ' + d.getFullYear() : '') + ' ' + two(d.getHours()) + ':' + two(d.getMinutes());
  }
  /* The last two folder names of a path ("Projects › garden-plan"), or "/" for the very top. */
  function crumb(path) { const seg = String(path || '').split(/[\\/]+/).filter(Boolean); return seg.length ? seg.slice(-2).join(' › ') : '/'; }
  /* One bordered action button of the panel: an icon and one short word. o: pri (main action), red (destructive), off (disabled), tip (hover hint), desc (id of the line that says why it is off), kc (keyboard hint). */
  function KB(act, icon, label, o) {
    o = o || {};
    return '<button type="button" class="k-btn' + (o.pri ? ' pri' : '') + (o.red ? ' rm' : '') + '" data-do="' + act + '"' + (o.off ? ' disabled' : '') + (o.desc ? ' aria-describedby="' + o.desc + '"' : '') + ' title="' + esc(o.tip || label) + '">' + ic(icon, { size: 16 }) + '<span class="bl">' + label + '</span>' + (o.kc ? '<kbd>' + o.kc + '</kbd>' : '') + '</button>';
  }
  /* The square "…" button that opens the menu with everything the panel has no room for. */
  const KMORE = '<button type="button" class="k-ib" data-do="more" aria-haspopup="menu" aria-label="More" title="Rename, save to, delete and more">' + ic('more', { size: 16 }) + '</button>';
  const kvRows = (rows) => '<dl class="k-kv">' + rows.map((r) => '<dt>' + r[0] + '</dt><dd' + (r[2] ? ' class="' + r[2] + '"' : '') + '>' + r[1] + '</dd>').join('') + '</dl>';
  const techBlock = (rows) => '<details class="tech"><summary>Technical details</summary>' + kvRows(rows) + '</details>';

  /* Thumbnails: fetched once per file version through the existing preview calls and kept for a while; the tile shows the type icon until one arrives. */
  const THUMBS = new Map(); const THUMB_BUSY = new Set(); let thumbWant = '';
  const wantsThumb = (host, n) => n.t !== 'dir' && U.kindOf(n) === 'image' && E.fs.canRead(n) && (host !== 'local' || n.b <= E.limits.maxImageBytes);
  const thumbKey = (host, full, n) => JSON.stringify([host, full, n.mod || 0, n.b || 0]);
  /* Asks for the thumbnail of picture `n` and, when it arrives and that file is still the one shown, puts it in the tile. Returns the cached picture ('' if none yet). */
  function loadThumb(host, dir, n) {
    const full = U.join(dir, n.n); const key = thumbKey(host, full, n); thumbWant = key;
    if (THUMBS.has(key)) return THUMBS.get(key);
    if (THUMB_BUSY.has(key)) return '';
    THUMB_BUSY.add(key);
    let got; try { got = host === 'local' ? E.call('local_read_image', { path: full }) : E.call('files_thumb', { host, path: full, size: 512 }); } catch (e) { got = Promise.reject(e); }
    Promise.resolve(got).then((src) => (typeof src === 'string' && /^data:image\//.test(src) ? src : ''), () => '').then((src) => {
      THUMB_BUSY.delete(key); THUMBS.set(key, src); if (THUMBS.size > 60) THUMBS.delete(THUMBS.keys().next().value);
      const box = src && thumbWant === key && $('#detail .dprev'); if (box) box.innerHTML = '<img alt="" src="' + esc(src) + '">';
    });
    return '';
  }
  /* The preview tile of one item: its picture when it has one, otherwise a flat tile with the type icon. A file the app can show opens its preview on click. */
  function tileHTML(host, dir, n) {
    const open = n.t !== 'dir' && A.hasViewer && A.hasViewer(host, n);
    const attrs = open ? ' data-do="preview" title="Open the preview (Space)"' : '';
    if (n.t === 'dir') return '<div class="dprev">' + bigPic('dir', '', ic('folder', { size: 56 })) + '</div>';
    if (!E.fs.canRead(n)) return '<div class="dprev warn">' + ic('lock', { size: 56 }) + '</div>';
    if (wantsThumb(host, n)) { const src = loadThumb(host, dir, n); return '<div class="dprev zoom"' + attrs + '>' + (src ? '<img alt="" src="' + esc(src) + '">' : bigPic('image', n.n, ic('image', { size: 56 }))) + '</div>'; }
    const k = U.kindOf(n);
    if (k === 'video') return '<div class="dprev">' + bigPic('video', n.n, ic('play', { size: 56 })) + '</div>';
    if (n.content) return '<div class="dprev' + (open ? ' zoom' : '') + '"' + attrs + '><pre>' + esc(n.content.split('\n').slice(0, 9).join('\n')) + '</pre></div>';
    return '<div class="dprev' + (open ? ' zoom' : '') + '"' + attrs + '>' + bigPic(window.RFE.kindOf(n), n.n, fic(n)) + '</div>';
  }
  /* The big picture of the details panel's preview tile: with the v5 skin on, the colour picture the tree rows use for this kind of item
     (`kind` is the app's kind word, `name` the file name); otherwise `line`, the plain line icon. */
  function bigPic(kind, name, line) {
    return A.v5Files && A.v5Files.on() && A.v5Files.emKind ? A.v5Files.emKind(kind, name, 56) : line;
  }
  /* The small picture before an item's name in the details panel: the colour picture of the tree row with the v5 skin on, otherwise the line icon
     (`inList` picks the plain icon size the many-item list uses). */
  function smallPic(n, inList) {
    if (A.v5Files && A.v5Files.on() && A.v5Files.emKind) return A.v5Files.emKind(window.RFE.kindOf(n), n.n || '', 16);
    return inList ? fic(n) : window.RFE.fileIcon(n, { size: 16 });
  }
  /* The "…" menu of the panel: the file menu of the main list plus "Save to…" (the folder downloads go to). `items` is that list. */
  A.detailMenuItems = (items, host) => {
    if (host === 'local') return items;
    const out = items.slice(); const at = out.findIndex((m) => m && m.label === 'Copy path');
    out.splice(at >= 0 ? at : out.length, 0, { icon: 'folder', label: 'Save to…', onClick: () => { A.chooseDownloadFolder().then(() => renderDetail()); } });
    return out;
  };
  /* Opens the "…" menu under its button. */
  A.openDetailMenu = (btn, items) => { const b = btn.getBoundingClientRect(); A.menu(b.right, b.bottom + 4, A.detailMenuItems(items, A.mainPane.host), { right: true }); };

  function renderDetail() {
    const d = $('#detail'); if (!d) return; const p = A.mainPane;
    const special = A.v5Files && A.v5Files.spDetail && A.v5Files.spDetail(); /* a Trash / Share links / Apps row of the Files tree is selected */
    if (special) { d.innerHTML = special; return; }
    if (!p.online()) { d.innerHTML = '<div class="dd dnc">' + (A.v5Files && A.v5Files.on() && A.v5Files.em ? A.v5Files.em('desktop_computer', 40) : ic('plug', { size: 40 })) + '<h3>' + esc(A.hostName(p.host)) + '</h3><p>Not connected. Details appear here once you connect.</p></div>'; return; }
    const sel = p.selected(); const dir = E.fs.get(p.host, p.path); const remote = p.host !== 'local'; const host = esc(A.hostName(p.host));
    if (sel.length === 0 && !dir) {
      d.innerHTML = '<div class="dprev">' + bigPic('dir', '', ic('folder', { size: 56 })) + '</div><h3>' + host + '</h3><p class="k-sub">Details appear here once the folder is open.</p>';
      return;
    }
    if (sel.length === 0) {
      const root = U.isRoot && U.isRoot(p.path); const cnt = E.fs.itemCount(dir); const blocked = !!(A.readOnlyNote(p.host) || A.atVirtualRoot(p.host, p.path));
      d.innerHTML = '<div class="dprev">' + bigPic('dir', '', ic('folder', { size: 56 })) + '</div><h3 data-nolocal>' + smallPic({ n: '', t: 'dir' }) + '<span>' + esc(root ? A.hostName(p.host) : U.baseOf(p.path) || A.hostName(p.host)) + '</span></h3>' +
        '<p class="k-sub">' + [cnt.toLocaleString(U.lang()) + (cnt === 1 ? ' item' : ' items'), shortWhen(dir.mod)].filter(Boolean).join(' · ') + '</p>' +
        '<dl class="k-kv"><dt>On</dt><dd>' + host + '</dd><dt>Folder</dt><dd>' + esc(crumb(p.path)) + '</dd></dl>' +
        '<div class="k-dact"><div class="k-row">' + (blocked ? KB('newfolder', 'folder-plus', 'New', { off: true, tip: 'Open one of the folders first' }) : KB('newfolder', 'folder-plus', 'New', { tip: 'New folder' })) + (remote ? KB('upload', 'upload', 'Upload', { tip: 'Upload files into this folder' }) : '') + '</div></div>' +
        techBlock([['Location', esc(p.path), 'mono'], ['Permissions', esc(dir.perm) + ' ' + esc(dir.own), 'mono'], ['Modified', U.fmtDate(dir.mod)], ['Computer', host + (remote ? ' · ' + E.server(p.host).latency + ' ms' : '')]]);
      return;
    }
    if (sel.length === 1) {
      const n = sel[0]; const isDir = n.t === 'dir'; const full = U.join(p.path, n.n); const t = isDir ? E.fs.total(n) : null; const block = A.openBlock(p.host, n);
      const facts = [isDir ? (n.cc == null && !n.kids ? '' : E.fs.itemsLabel(n, (c) => c.toLocaleString(U.lang()))) : U.fmtBytes(n.b), shortWhen(n.mod)].filter(Boolean).join(' · ');
      const share = remote && !isDir ? KB('share', 'link', 'Share link…', { tip: 'Make a link anyone can use for a while' }) : '';
      d.innerHTML = tileHTML(p.host, p.path, n) + '<h3 data-nolocal>' + smallPic(n) + '<span>' + esc(n.n) + '</span></h3><p class="k-sub">' + esc(facts) + '</p>' +
        (!E.fs.canRead(n) ? '<div class="dwarn">' + ic('lock', { size: 16 }) + '<span>You can’t read this file, so downloading it will fail.</span></div>' : '') +
        '<dl class="k-kv"><dt>On</dt><dd>' + host + '</dd><dt>Folder</dt><dd>' + esc(crumb(p.path)) + '</dd></dl>' +
        '<div class="k-dact">' + KB('download', remote ? 'download' : 'upload', remote ? 'Download' : 'Upload', { pri: true, kc: remote ? 'Ctrl D' : '' }) +
        KB('open', isDir ? 'folder-open' : 'play', 'Open', { off: !!block, desc: block ? 'dwhy' : '', tip: isDir ? 'Open this folder' : block || (remote ? 'Show it from the computer without downloading' : 'Open it') }) +
        (block ? '<div class="dwhy" id="dwhy">' + esc(block) + '</div>' : '') + '<div class="k-row">' + share + KMORE + '</div></div>' +
        techBlock([['Full path', esc(full), 'mono'], ['Permissions', esc(n.perm) + ' ' + esc(n.own) + (E.fs.canRead(n) ? '' : ', mode ' + U.permOctal(n.perm)), 'mono'], ['Modified', U.fmtDate(n.mod)]].concat(t ? [['Contents', t.files.toLocaleString(U.lang()) + ' files, ' + t.dirs + ' folders · ' + U.fmtBytes(t.bytes)]] : []).concat(remote ? [['Saves to', esc(S.downloadDir)]] : []));
      return;
    }
    const tot = sel.reduce((a, n) => a + E.fs.total(n).bytes, 0);
    d.innerHTML = '<div class="dprev">' + (A.v5Files && A.v5Files.on() && A.v5Files.em ? A.v5Files.em('card_file_box', 56) : ic('copy', { size: 56 })) + '</div><h3>' + sel.length + ' items selected</h3><p class="k-sub">' + U.fmtBytes(tot) + ' total</p><ul class="names">' +
      sel.slice(0, 8).map((n) => '<li>' + smallPic(n, true) + '<span class="trunc">' + esc(n.n) + '</span></li>').join('') + (sel.length > 8 ? '<li class="more">and ' + (sel.length - 8) + ' more</li>' : '') + '</ul>' +
      '<div class="k-dact">' + KB('download', remote ? 'download' : 'upload', remote ? 'Download' : 'Upload', { pri: true, kc: remote ? 'Ctrl D' : '' }) + KB('delete', 'trash', 'Delete', { red: true }) + '</div>';
  }

  /* ---------- sheet ---------- */
  let sheetSig = '';
  A.renderSheet = renderSheet;
  function renderSheet(force) {
    const sh = $('#sheet'); if (st.view === 'transfers') return;
    const c = counts(); const sig = E.tasks.map((t) => t.id + t.state + (t.msg || '') + (t.conflict ? 'c' : '')).join('|') + '/' + st.sheetTab + st.sheetOpen + st.view + (E.tasks.length ? '' : 'e');
    if (!force && sig === sheetSig) { A.patchCards(sh); updateTotals(); return; }
    sheetSig = sig; layout();
    const TI = { active: 'swap', attn: 'alert', queued: 'clock', done: 'check-circle' }; /* every tab shows its icon, its name and its count */
    const tab = (k, l, n) => '<button class="stab' + (st.sheetTab === k ? ' on' : '') + '" data-stab="' + k + '" role="tab" title="' + l + '" aria-label="' + l + '" aria-selected="' + (st.sheetTab === k) + '">' + ic(TI[k], { size: 18 }) + '<span>' + l + '</span>' + (n ? ' <b>' + n + '</b>' : '') + '</button>';
    const list = E.tasks.filter((t) => A.groupOf(t) === st.sheetTab).sort((a, b) => a.queuedAt - b.queuedAt);
    const anyRun = E.tasks.some((t) => t.state === 'running' || t.state === 'queued'), anyPaused = E.tasks.some((t) => t.state === 'paused');
    sh.style.display = st.view === 'home' && !E.tasks.length ? 'none' : ''; /* nothing to show on Overview until a transfer exists */
    sh.classList.toggle('min', !st.sheetOpen); $('#main').classList.toggle('noconn', !(A.mainPane.host !== 'local' && E.connected(A.mainPane.host)));
    const up = A.mainPane.host === 'local' || !E.connected(A.mainPane.host);
    sh.innerHTML = '<div class="spop" role="dialog" aria-label="Transfers"><div class="stabs" role="tablist">' + tab('active', 'Active', c.active) + tab('attn', 'Needs attention', c.attn, true) + tab('queued', 'Queued', c.queued) + tab('done', 'Done', c.done) + '</div><div class="cards">' + (list.length ? list.map(A.card).join('') : '<div class="none">' + ic(st.sheetTab === 'attn' ? 'check-circle' : 'swap', { size: 22 }) + (st.sheetTab === 'active' ? 'No transfers' : st.sheetTab === 'attn' ? 'Nothing needs your attention.' : st.sheetTab === 'queued' ? 'Nothing is waiting in the queue.' : 'Completed transfers show up here.') + '</div>') + '</div><div class="sfoot"><span id="tot"></span><span class="sp"></span>' + (anyRun || !anyPaused ? '<button class="btn sm" data-sa="pauseall"' + (anyRun ? '' : ' disabled') + '>' + ic('pause') + 'Pause all</button>' : '<button class="btn sm" data-sa="resumeall">' + ic('play') + 'Resume all</button>') + (c.done ? '<button class="btn sm tx" data-sa="clear">Clear completed</button>' : '') + '</div></div>' +
      '<div class="sdock"><button class="efab" data-efab="1"' + (up ? ' disabled title="Open a connected computer first, then upload into it"' : '') + '>' + ic('upload') + 'Upload</button><button class="spill" id="spill" data-pill="1" aria-expanded="' + !!st.sheetOpen + '" aria-label="Transfers"><span class="ring" id="spr"></span><span id="spt">Transfers</span></button></div>';
    A.patchCards(sh); updateTotals();
  }
  function updateTotals() { const t = $('#tot'); if (!t) return; const sp = E.totalSpeed(); const n = E.tasks.filter((x) => x.state === 'running').length; t.textContent = n ? U.fmtSpeed(sp) + ' total · ' + n + ' running' : 'Idle'; pillUpdate(); }
  function pillUpdate() {
    const c = counts(), live = E.tasks.filter((x) => x.state === 'running' || x.state === 'paused' || x.state === 'waiting');
    const b = live.reduce((a, x) => a + (x.bytes || 0), 0), d = live.reduce((a, x) => a + (x.done || 0), 0), p = b ? Math.min(100, d / b * 100) : 0;
    const pl = $('#spill'); if (pl) { pl.classList.toggle('busy', c.active > 0); pl.classList.toggle('attn', c.attn > 0); $('#spr').style.setProperty('--p', p.toFixed(1)); $('#spr').classList.toggle('idle', !c.active); $('#spr').innerHTML = c.active ? String(Math.min(99, Math.floor(p))) : ic('swap'); $('#spt').textContent = c.active ? 'Transfers ' + (c.active + c.queued) : c.attn ? 'Needs attention ' + c.attn : 'Transfers'; }
    const bx = $('#bdx'); if (bx) { const n = c.active + c.queued + c.attn; bx.hidden = !n; bx.textContent = n; bx.classList.toggle('ok', !c.attn); }
  }
  /* Transfers float in a pill at the bottom right; the pill and the top-bar button open and fold the list above it. */
  const stripKeep = () => { layout(); renderSheet(true); };
  A.toggleTransfers = () => { st.sheetOpen = !st.sheetOpen; stripKeep(); };
  $('#sheet').addEventListener('click', (e) => {
    const sa = e.target.closest('[data-sa]'); if (sa) { ({ pauseall: E.pauseAll, resumeall: E.resumeAll, clear: E.clearDone })[sa.dataset.sa](); return; }
    if (e.target.closest('[data-pill]')) { A.toggleTransfers(); return; }
    const tb = e.target.closest('[data-stab]'); if (tb) { st.sheetTab = tb.dataset.stab; st.sheetOpen = true; renderSheet(true); return; }
    if (e.target.closest('[data-efab]:not([disabled])')) { uploadFab(); return; }
    A.cardClick(e);
  });
  A.uploadFab = () => uploadFab();
  function uploadFab() {
    const m = A.mainPane;
    if (m.host === 'local' || !E.connected(m.host)) { A.snack('Open a connected computer first to upload into it.', { error: true, action: 'Computers', onAction: () => A.go('servers') }); return; }
    if (st.view !== 'files') A.go('files');
    const sel = A.localPane.selected();
    if (sel.length) { A.localPane.transfer(); return; }
    A.chooseAndUpload();
  }

  /* ---------- servers page ---------- */
  function spark(h) {
    const v = h.slice(-30); const w = 200, hh = 36; const nums = v.filter((x) => x != null); if (nums.length < 2) return '<svg viewBox="0 0 200 36" aria-hidden="true"></svg>';
    const mx = Math.max.apply(null, nums) + 2, mn = Math.min.apply(null, nums) - 2; let d = '', pen = false;
    v.forEach((x, i) => { if (x == null) { pen = false; return; } const X = (i / (v.length - 1)) * w, Y = hh - 3 - ((x - mn) / (mx - mn || 1)) * (hh - 8); d += (pen ? 'L' : 'M') + X.toFixed(1) + ' ' + Y.toFixed(1); pen = true; });
    return '<svg viewBox="0 0 200 36" preserveAspectRatio="none"><path d="' + d + '" fill="none" stroke="var(--primary)" stroke-width="1.8" stroke-linejoin="round"/></svg>';
  }
  const STL = { login: 'Signed out', online: 'Connected', lost: 'Connection lost', offline: 'Offline', connecting: 'Connecting', trust: 'Verify key', refused: 'Key changed', disconnected: 'Not connected' };
  function serverCard(s) {
    const icn = /^(Windows|macOS|Darwin)/i.test(s.os) ? 'laptop' : /raspberry|arm/i.test(s.os) ? 'cpu' : 'server'; const hasDisk = s.disk[1] > 0; const used = hasDisk ? s.disk[0] / s.disk[1] : 0; const big = s.disk[1] >= 1000; const dsk = (v) => (big ? (v / 1000).toFixed(1) : v.toFixed(v < 10 ? 1 : 0));
    let note = '', act = '';
    const B = (a, l, k, i) => '<button class="btn sm ' + k + '" data-sa="' + a + '" data-id="' + esc(s.id) + '">' + (i ? ic(i) : '') + l + '</button>';
    if (s.state === 'online') act = B('open', 'Open', 'f', 'folder-open') + B('disconnect', 'Disconnect', '');
    else if (s.state === 'lost') { note = '<div class="note w">' + ic('wifi-off') + '<span>Lost ' + Math.round((E.now() - s.lostAt) / 1000) + ' s ago. ' + (S.autoReconnect ? 'Retrying in ' + Math.max(0, Math.ceil(s.retryIn)) + ' s. Transfers are paused.' : 'Auto-reconnect is off.') + '</span></div>'; act = B('retry', 'Retry now', 'f', 'refresh') + B('edit', 'Edit', ''); }
    else if (s.state === 'offline') { note = '<div class="note e">' + ic('alert-circle') + '<span>' + esc((s.error || 'The PC did not answer on ' + s.host + ':' + s.port).replace(/\.$/, '')) + (s.incompatible ? '. Update rfe on that computer' : '') + '. Last seen ' + (s.lastSeen ? A.ago(s.lastSeen) : 'never') + '.</span></div>'; act = B('retry', 'Try again', 'f', 'refresh') + B('edit', 'Edit', ''); }
    else if (s.state === 'connecting') { note = '<div class="note w">' + ic('spinner') + '<span>Contacting ' + esc(s.host) + '…</span></div>'; act = B('cancel', 'Cancel', ''); }
    else if (s.state === 'login') { note = '<div class="note w">' + ic('user') + '<span>' + esc(s.msg && !/^not signed in/i.test(s.msg) ? s.msg : 'Signed out on this computer. Sign in to continue. If the login was revoked, ask the owner for a new one.') + '</span></div>'; act = B('signin', 'Sign in', 'f', 'user') + B('cancel', 'Disconnect', ''); }
    else if (s.state === 'trust') { note = '<div class="note w">' + ic('shield') + '<span>Unknown computer key. Review it and compare it with the one on the computer before you trust it.</span></div>'; act = B('trust', 'Review key…', 'f', 'shield-check') + B('cancel', 'Cancel', ''); }
    else if (s.state === 'refused') { note = '<div class="note e">' + ic('shield') + '<span>The certificate changed. Connection refused to protect your files.</span></div>'; act = B('reverify', 'Review…', 'f', 'shield-check') + B('diag', 'Diagnose', ''); }
    else act = B('connect', 'Connect', 'f', 'plug') + B('edit', 'Edit', '');
    return '<div class="sc' + (A.mainPane.host === s.id ? ' sel' : '') + '"' + (A.mainPane.host === s.id ? ' aria-current="true"' : '') + ' data-sid="' + esc(s.id) + '"><div class="h"><div class="gl">' + ic(icn) + '</div><div><b data-nolocal>' + esc(s.name) + '</b><small>' + esc(s.host) + ':' + s.port + '</small></div><span class="sp"></span><span class="chipst ' + s.state + '"><i></i>' + STL[s.state] + '</span></div>' +
      '<div class="meta"><div><small>Route</small>' + esc(s.via) + '</div><div><small>System</small>' + esc(s.os) + '</div><div><small>Agent</small>' + esc(s.agent || '—') + '</div></div>' +
      '<div class="lat">' + spark(s.hist) + '<b>' + (s.state === 'online' ? s.latency + '<small> ms</small>' : '—') + '</b></div>' +
      '<div><div class="bar' + (used > 0.8 ? ' hi' : '') + '"><i style="width:' + (used * 100).toFixed(0) + '%"></i></div><div class="row2"><span>Storage</span><span>' + (hasDisk ? dsk(s.disk[0]) + (big ? ' TB' : ' GB') + ' of ' + dsk(s.disk[1]) + (big ? ' TB' : ' GB') : '—') + '</span></div></div>' + note +
      '<div class="ac">' + act + '<button class="ib sm" data-sa="more" data-id="' + esc(s.id) + '" aria-label="More for ' + esc(s.name) + '">' + ic('more-v') + '</button></div></div>';
  }
  A.serverCard = serverCard;
  /* The Computers page: the v5 one (v5-computers.js) while the window wears the v5 wrapper, otherwise the old cards. */
  function serversPage() { if (A.v5Computers && A.v5Computers.on()) A.v5Computers.page(); else pageServers(); }
  function pageServers() {
    const on = E.servers.filter((s) => s.state === 'online').length;
    const stage = $('#stage'); const html = '<h2 class="pt">Computers</h2><p class="ps">' + E.servers.length + ' saved · ' + on + ' connected</p><div class="grid3" id="sgrid">' + E.servers.map(serverCard).join('') + '<button type="button" class="sc add" data-sa="add">' + ic('plus') + '<b>Add a computer</b><span style="color:var(--on-var)">Address and port. You check its key next.</span></button></div>';
    /* This runs on every tick: an unchanged page is left alone, and a rebuilt one gives focus back to the same control, so a press or a Tab stop is not lost. */
    if (stage.querySelector('#sgrid') && stage._sv === html) return;
    const at = document.activeElement; const key = at && stage.contains(at) ? [at.dataset.sa, at.dataset.id || at.dataset.sid] : null;
    stage.innerHTML = html; stage._sv = html;
    if (key && key[0]) { const back = [...stage.querySelectorAll('[data-sa]')].find((x) => x.dataset.sa === key[0] && (x.dataset.id || x.dataset.sid) === key[1]); if (back) back.focus({ preventScroll: true }); }
  }
  $('#stage').addEventListener('click', (e) => {
    const sa = e.target.closest('[data-sa]'); if (!sa || st.view !== 'servers') return; const id = sa.dataset.id; const s = id && E.server(id);
    switch (sa.dataset.sa) {
      case 'open': A.setMainHost(id); A.go('files'); break;
      case 'connect': A.setMainHost(id); E.connect(id); A.go('files'); break;
      case 'disconnect': E.disconnect(id); break; case 'retry': E.retryNow(id); break; case 'cancel': E.cancelConnect(id); break;
      case 'reverify': A.reverifyDialog(id); break; case 'diag': A.diagnose(id); break;
      case 'trust': A.trustDialog(s); break; case 'signin': A.signInDialog(s); break; case 'edit': A.serverDialog(s); break; case 'add': A.serverDialog(); break;
      case 'more': { const r = sa.getBoundingClientRect(); A.menu(r.left, r.bottom, [{ icon: 'folder-open', label: 'Browse files', disabled: s.state !== 'online', onClick: () => { A.setMainHost(id); A.go('files'); } }, { icon: 'edit', label: 'Edit connection', onClick: () => A.serverDialog(s) }].concat(A.serverMenuExtra ? A.serverMenuExtra(s) : [], ['-', { icon: 'trash', label: 'Forget computer', danger: true, onClick: () => forgetDialog(s) }])); break; }
    }
  });
  function forgetDialog(s) {
    const n = E.tasks.filter((t) => t.host === s.id && A.groupOf(t) !== 'done').length;
    A.dialog({ icon: 'trash', cls: 'danger', title: 'Forget ' + s.name + '?', body: 'This removes the saved connection and its pinned key.' + (n ? ' <b>' + n + ' unfinished transfer' + (n === 1 ? '' : 's') + ' will be cancelled.</b>' : ''), actions: [{ label: 'Cancel' }, { label: 'Forget', kind: 'ed', cb: async () => { if (A.mainPane.host === s.id) A.mainPane.setHost('local'); try { await E.forget(s.id); A.snack('Forgot ' + s.name); } catch (e) { A.snack(e.message, { error: true }); } renderAll(); } }] });
  }
  /* Step 1 of adding a computer (type its name and address), or, given a saved computer `s`, the dialog to rename it. `pre` fills the fields again after Back. */
  A.serverDialog = (s, pre) => {
    const edit = !!s; const v = s || pre || { name: '', host: '', port: A.DEFAULT_PORT };
    const d = A.dialog({
      icon: 'plug', cls: edit ? '' : 'conn', title: edit ? 'Edit ' + s.name : 'Add a computer', width: 520, enter: 'ok',
      body: (edit || !A.connTabs ? '' : A.connTabs('manual')) + '<div class="fld"><label for="sn">Name</label><input id="sn" value="' + esc(v.name) + '" placeholder="e.g. render-farm" spellcheck="false"><span class="err" id="sne"></span></div><div class="fld two"><div class="fld" style="margin:0"><label for="sh">Address</label><input id="sh" value="' + esc(v.host) + '"' + (edit ? ' disabled' : '') + ' placeholder="100.64.0.12 or host.example.com" spellcheck="false"><span class="err" id="she"></span></div><div class="fld" style="margin:0"><label for="sp">Port</label><input id="sp" value="' + v.port + '"' + (edit ? ' disabled' : '') + ' inputmode="numeric"></div></div><div style="color:var(--on-var);font-size:.7812rem">' + (edit ? 'The address belongs to the trusted key. To use another address, add a new connection.' : "Type the new computer's address. Your current login stays saved. " + 'Nothing is sent to it until you have checked that it is the right computer.') + '</div>',
      actions: [{ label: 'Cancel' }, { id: 'ok', label: edit ? 'Save' : 'Connect', kind: 'f', icon: edit ? 'check' : 'plug', cb: (ctl) => {
        const g = (i) => $(i, ctl.el); const host = g('#sh').value.trim(), rawPort = g('#sp').value.trim(), port = rawPort ? Number(rawPort) : A.DEFAULT_PORT; const name = g('#sn').value.trim() || host; let bad = false;
        g('#sne').textContent = ''; g('#she').textContent = '';
        if (E.servers.some((x) => x.name.toLowerCase() === name.toLowerCase() && (!edit || x.id !== s.id))) { g('#sne').textContent = 'A computer with this name already exists'; bad = true; }
        if (!host || /\s/.test(host)) { g('#she').textContent = 'Enter a host or IP address'; bad = true; }
        else if (!edit) { const pa = E.util.parseAddr(host, A.DEFAULT_PORT); if (pa.error) { g('#she').textContent = pa.error; bad = true; } }
        let pe = $('#spe', ctl.el); if (!pe) { pe = document.createElement('span'); pe.className = 'err'; pe.id = 'spe'; g('#sp').parentNode.appendChild(pe); } pe.textContent = '';
        if (!Number.isInteger(port) || port < 1 || port > 65535) { pe.textContent = 'Use a port from 1 to 65535'; bad = true; }
        if (bad) return false;
        if (edit) { E.updateServer(s.id, { name }).then(() => { A.snack('Saved ' + name); renderAll(); }).catch((e) => A.snack(e.message, { error: true })); }
        else { A.addAndOpen({ name, host, port }, undefined, ctl); return false; }
      } }]
    });
    return d;
  };

  /* ---------- transfers page ---------- */
  function pageTransfers() {
    if (A.v5Pages && A.v5Pages.draws('transfers')) { A.v5Pages.mount($('#stage')); return; } /* the v5 window redraws its own list */
    const c = counts(); const T = E.tasks; const sec = (k, title, hint) => { const l = T.filter((t) => A.groupOf(t) === k).sort((a, b) => a.queuedAt - b.queuedAt); return l.length ? '<div class="sec">' + title + ' <b>' + l.length + '</b></div><div class="grid3 cwide">' + l.map(A.card).join('') + '</div>' : ''; };
    const nRun = T.filter((t) => t.state === 'running').length, nPaused = T.filter((t) => t.state === 'paused' || t.state === 'waiting').length, nFail = T.filter((t) => t.state === 'failed').length; const sessionBytes = T.filter((t) => t.state === 'done').reduce((a, t) => a + t.bytes, 0) + T.filter((t) => t.state === 'running').reduce((a, t) => a + t.done, 0);
    $('#stage').innerHTML = '<div style="display:flex;align-items:center;gap:.625rem;flex-wrap:wrap"><div style="flex:1"><h2 class="pt">Transfers</h2></div>' + (nRun ? '<button class="btn" data-pa="pauseall">' + ic('pause') + 'Pause all</button>' : '') + (nPaused ? '<button class="btn" data-pa="resumeall">' + ic('play') + 'Resume all</button>' : '') + '<button class="btn" data-pa="retryfailed"' + (T.length ? '' : ' hidden') + (nFail ? '' : ' disabled') + '>' + ic('retry') + 'Retry failed</button><button class="btn tx" data-pa="clear"' + (T.length ? '' : ' hidden') + (c.done ? '' : ' disabled') + '>Clear completed</button></div>' +
      (T.length ? '<div class="sum"><div><b id="sumSpeed">' + U.fmtSpeed(E.totalSpeed()) + '</b><small>current speed</small></div><div><b>' + c.active + '</b><small>active</small></div><div><b>' + c.queued + '</b><small>queued</small></div><div><b style="color:' + (c.attn ? 'var(--err)' : 'inherit') + '">' + c.attn + '</b><small>need attention</small></div><div><b>' + c.done + '</b><small>completed</small></div><div><b>' + U.fmtBytes(sessionBytes) + '</b><small>transferred this session</small></div></div>' : '') +
      (T.length ? sec('attn', 'Needs attention') + sec('active', 'Active') + sec('queued', 'Queued') + sec('done', 'Completed') : '<div class="es" style="min-height:18.75rem"><div class="box"><div class="ico">' + ic('swap') + '</div><h3>No transfers</h3><div class="row"><button class="btn f" data-pa="files">' + ic('folder') + 'Go to files</button></div></div></div>');
    A.patchCards($('#stage'));
  }
  $('#stage').addEventListener('click', (e) => {
    if (st.view !== 'transfers') return; const pa = e.target.closest('[data-pa]');
    if (pa) { ({ pauseall: E.pauseAll, resumeall: E.resumeAll, retryfailed: E.retryFailed, clear: E.clearDone, files: () => A.go('files') })[pa.dataset.pa](); return; }
    A.cardClick(e);
  });

  /* ---------- search ---------- */
  function startSearch() {
    const sr = (A.searchRun = { q: st.q, scope: st.scope || 'server', filter: st.sfilter || 'all', results: [], scanned: 0, done: false, tok: Math.random() });
    const hosts = sr.scope === 'server' ? [A.mainPane.host] : sr.scope === 'all' ? E.servers.filter((s) => s.state === 'online').map((s) => s.id) : ['local'];
    sr.hosts = hosts.filter((h) => E.connected(h)); sr.skipped = hosts.filter((h) => !E.connected(h));
    sr.job = E.makeSearch(sr.hosts, sr.q, { filter: sr.filter, max: 400 });
    if (A.sync) { while (!sr.job.step(1e6)); sr.results = sr.job.results; sr.scanned = sr.job.scanned; sr.done = true; renderStage(); return; }
    renderStage();
    const step = () => { if (A.searchRun !== sr) return; const done = sr.job.step(E.SEARCH.slice); sr.results = sr.job.results; sr.scanned = sr.job.scanned; sr.done = done; updateSearch(); if (!done) setTimeout(step, A.T.stepGap); };
    setTimeout(step, A.T.step);
  }
  function pageSearch() {
    const sr = A.searchRun;
    if (!sr) { $('#stage').innerHTML = '<h2 class="pt">Search</h2><div class="es" style="min-height:20rem"><div class="box"><div class="ico">' + ic('search') + '</div><h3>Search ' + esc(A.hostName(A.mainPane.host)) + '</h3><p>Type in the search bar above, or press /.</p>' + (A.searchExtra ? A.searchExtra() : '') + '</div></div>'; return; }
    const chip = (k, l, cur, key) => '<button class="chip' + (cur === k ? ' on' : '') + '" data-sf="' + key + ':' + k + '">' + (cur === k ? ic('check') : '') + l + '</button>';
    $('#stage').innerHTML = '<h2 class="pt">Results for “' + esc(sr.q) + '”' + (A.searchSave ? A.searchSave(sr) : '') + '</h2><div class="chips" style="padding:.5rem 0 0">' + chip('server', A.mainPane.host === 'local' ? 'This computer' : A.hostName(A.mainPane.host), sr.scope, 'scope') + chip('all', 'All connected computers', sr.scope, 'scope') + (A.mainPane.host === 'local' ? '' : chip('local', 'This computer', sr.scope, 'scope')) + '<span style="width:.75rem"></span>' + [['all', 'Everything'], ['folders', 'Folders'], ['packages', 'Packages'], ['media', 'Media'], ['archives', 'Archives'], ['big', 'Over 100 MB']].map((f) => chip(f[0], f[1], sr.filter, 'filter')).join('') + '</div><div class="prog" id="sprog"></div><div id="sres"></div>';
    updateSearch();
  }
  function updateSearch() {
    if (A.v5Pages && A.v5Pages.draws('search')) { if (st.view === 'search') A.v5Pages.updateSearch(); return; }
    const sr = A.searchRun; if (!sr || st.view !== 'search') return; const pg = $('#sprog'), rs = $('#sres'); if (!pg) return;
    const where = sr.hosts.map(A.hostName).join(', ');
    pg.innerHTML = sr.skipped.length && !sr.hosts.length ? ic('wifi-off') + '<span>' + esc(sr.skipped.map(A.hostName).join(', ')) + ' is not connected, so it can’t be searched.</span>' : (sr.done ? ic('check-circle') + '<span><b>' + sr.results.length + (sr.results.length >= 400 ? '+' : '') + '</b> result' + (sr.results.length === 1 ? '' : 's') + ' · scanned ' + sr.scanned.toLocaleString(U.lang()) + ' items on ' + esc(where) + '</span>' : '<div class="lin ind"><i></i></div><span>Searching ' + esc(where) + '… <b>' + sr.scanned.toLocaleString(U.lang()) + '</b> items scanned · <b>' + sr.results.length + '</b> found</span>') + (sr.skipped.length && sr.hosts.length ? '<span>· skipped ' + esc(sr.skipped.map(A.hostName).join(', ')) + ' (not connected)</span>' : '');
    if (!sr.results.length) { rs.innerHTML = sr.done ? '<div class="es" style="min-height:15rem"><div class="box"><div class="ico">' + ic('search') + '</div><h3>No matches</h3><p>Nothing named like “' + esc(sr.q) + '” in ' + esc(where || 'the selected scope') + '.</p></div></div>' : ''; return; }
    rs.innerHTML = sr.results.slice(0, 200).map((r, i) => '<div class="res" role="button" tabindex="0" data-ri="' + i + '"><div class="lead">' + fic(r.node) + '</div><div style="min-width:0"><b>' + A.hl(r.node.n, sr.q) + (!E.fs.canRead(r.node) ? '<span style="font-size:.7188rem;color:var(--warn);background:var(--warn-c);border-radius:.5rem;padding:0 .4375rem;margin-left:.5rem">' + esc(r.node.own) + ' only</span>' : '') + '</b><small>' + esc(r.dir) + '</small></div><div class="sv"><i></i>' + esc(A.hostName(r.host)) + '</div><div style="color:var(--on-var);font-size:.8125rem">' + U.fmtDate(r.node.mod) + '</div><div style="text-align:right;color:var(--on-var);font-size:.8125rem">' + A.itemSize(r.node) + '</div></div>').join('') + (sr.results.length > 200 ? '<div style="padding:.75rem;color:var(--on-var)">Showing the first 200 results. Narrow the search to see more.</div>' : '');
  }
  $('#stage').addEventListener('keydown', (e) => {
    if (st.view !== 'search' || (e.key !== 'Enter' && e.key !== ' ')) return; const r = e.target.closest && e.target.closest('[data-ri]'); if (!r || e.target !== r) return; e.preventDefault(); openResult(A.searchRun.results[+r.dataset.ri]);
  });
  $('#stage').addEventListener('click', (e) => {
    if (st.view !== 'search') return; const sf = e.target.closest('[data-sf]');
    if (sf) { const [k, v] = sf.dataset.sf.split(':'); if (k === 'scope') st.scope = v; else st.sfilter = v; startSearch(); return; }
    const r = e.target.closest('[data-ri]'); if (r) { const x = A.searchRun.results[+r.dataset.ri]; openResult(x); }
  });
  function openResult(x) {
    if (x.host === 'local') { A.go('files'); st.sideTab = 'local'; renderSide(); A.localPane.go(x.node.t === 'dir' ? x.path : x.dir); if (x.node.t !== 'dir') { A.localPane.sel = new Set([x.node.n]); A.localPane.render(); } return; }
    A.mainPane.setHost(x.host); A.go('files'); A.mainPane.go(x.node.t === 'dir' ? x.path : x.dir);
    if (x.node.t !== 'dir') { A.mainPane.sel = new Set([x.node.n]); const i = A.mainPane.items.findIndex((n) => n.n === x.node.n); A.mainPane.anchor = i; A.mainPane.render(); const rows = $('[data-rows]', A.mainPane.el); if (rows && i >= 0) { rows.scrollTop = Math.max(0, i * A.mainPane.rh - 120); A.mainPane.renderRows(); } renderDetail(); }
  }

  /* ---------- history ---------- */
  function pageHistory() {
    if (A.v5Devices && A.v5Devices.on()) { A.v5Devices.history(); return; } /* the v5 window draws History itself (v5-devices.js) */
    const F =[['all', 'All'], ['transfer', 'Transfers'], ['conn', 'Connections'], ['audit', 'Security'], ['err', 'Errors']]; const f = st.histFilter;
    const rows = E.history.filter((h) => f === 'all' || (f === 'err' ? h.error : f === 'transfer' ? h.kind === 'transfer' || h.kind === 'queue' : h.kind === f));
    $('#stage').innerHTML = '<div style="display:flex;align-items:center;gap:.625rem"><div style="flex:1"><h2 class="pt">History</h2></div><button class="btn tx" data-hc="1">Clear history</button></div><div class="chips" style="padding:.375rem 0 .625rem">' + F.map((x) => '<button class="chip' + (f === x[0] ? ' on' : '') + '" data-hf="' + x[0] + '">' + (f === x[0] ? ic('check') : '') + x[1] + '</button>').join('') + '</div>' +
      (rows.length ? rows.slice(0, 200).map((h) => '<div class="hrow ' + (h.error ? 'err' : h.ok ? 'ok' : '') + '"><div class="gi">' + ic(h.error ? 'alert-circle' : h.kind === 'audit' ? 'shield-check' : h.kind === 'conn' ? 'plug' : h.kind === 'queue' ? 'clock' : h.dir === 'up' ? 'upload' : h.dir === 'down' ? 'download' : 'check-circle') + '</div><div>' + esc(h.text) + (h.host ? '<br><small>' + esc(A.hostName(h.host)) + '</small>' : '') + '</div><time>' + (new Date(h.at).setHours(0, 0, 0, 0) < new Date().setHours(0, 0, 0, 0) ? U.fmtDate(h.at) + ' ' : '') + U.fmtTime(h.at) + '</time></div>').join('') : '<div class="es" style="min-height:17.5rem"><div class="box"><div class="ico">' + ic('history') + '</div><h3>Nothing here yet</h3><p>' + (f === 'audit' ? (A.X.auditDenied && A.X.auditDenied.size ? 'This login cannot read the audit log or the agent log.' : 'The audit log is empty.') : E.history.length ? 'No loaded event matches these filters.' : 'Finished and failed transfers, connections and drops are listed here.') + '</p></div></div>');
  }
  $('#stage').addEventListener('click', (e) => {
    if (st.view !== 'history') return; const hf = e.target.closest('[data-hf]'); if (hf) { st.histFilter = hf.dataset.hf; pageHistory(); } else if (e.target.closest('[data-hc]')) { if (!E.history.length) return; A.dialog({ title: 'Clear history?', body: '<p>This removes all ' + E.history.length + ' entries from this window’s history. It cannot be undone.</p>', modal: true, actions: [{ id: 'k', label: 'Cancel', kind: 't', cb: (c) => c.close() }, { id: 'c', label: 'Clear history', kind: 'f', cb: (c) => { c.close(); E.history.length = 0; E.emit('history'); } }] }); }
  });

  /* ---------- settings ---------- */
  function pageSettings() {
    const LIMITS = [0, 10, 25, 50];
    const limitCustom = () => { const c = !LIMITS.includes(+S.limit); return '<button data-limcustom="1" class="' + (c ? 'on' : '') + '" aria-pressed="' + c + '">' + (c ? esc(+S.limit + ' MB/s') : 'Custom') + '</button>'; };
    const seg = (key, opts) => '<div class="seg" data-set="' + key + '">' + opts.map((o) => '<button data-v="' + o[0] + '" class="' + (String(S[key]) === String(o[0]) ? 'on' : '') + '" aria-pressed="' + (String(S[key]) === String(o[0])) + '">' + o[1] + '</button>').join('') + '</div>';
    const sw = (key) => '<button class="sw' + (S[key] ? ' on' : '') + '" data-sw="' + key + '" role="switch" aria-checked="' + !!S[key] + '" aria-label="' + key + '"></button>';
    const row = (t, s, c) => { if (/class="sw/.test(c)) c = c.replace(/aria-label="[^"]*"/, 'aria-label="' + String(t).replace(/<[^>]*>/g, '').replace(/"/g, '&quot;') + '"'); /* the switch is named by its row from the first draw */ return '<div class="sr"><div class="l"><b>' + t + '</b>' + (s ? '<small>' + s + '</small>' : '') + '</div>' + c + '</div>'; };
    const body = 
      '<div class="sg"><h3>Transfers</h3>' + row('Parallel transfers', 'How many files move at the same time.', seg('parallel', [[1, '1'], [2, '2'], [3, '3'], [4, '4']])) + row('Speed limit', 'Caps the total speed of all transfers.', seg('limit', [[0, 'None'], [10, '10 MB/s'], [25, '25 MB/s'], [50, '50 MB/s']]).replace(/<\/div>$/, limitCustom() + '</div>')) + row('When a name already exists', 'What happens when a file with the same name is already there.', seg('onConflict', [['ask', 'Ask'], ['replace', 'Replace'], ['keep', 'Keep both'], ['skip', 'Skip']])) + row('Check every download', 'Makes sure each file arrives complete and unchanged', sw('verify')) + row('Download folder', '<span class="mono">' + esc(A.downloadDir()) + '</span>', '<span style="display:flex;gap:.5rem"><button class="btn" data-sx="dlfolder">' + ic('folder-open') + 'Change…</button><button class="btn tx" data-sx="dlreset">Reset</button></span>') + '</div>' +
      '<div class="sg"><h3>Connection</h3>' + row('Reconnect automatically', 'Try again by itself when a computer goes away.', sw('autoReconnect')) + '</div>' +
      '<div class="sg"><h3>Appearance</h3>' + row('Theme', 'Follows the system unless you choose', seg('theme', [['light', 'Light'], ['dark', 'Dark'], ['system', 'System']])) + row('Row density', '', seg('density', [['comfortable', 'Comfortable'], ['compact', 'Compact']])) + row('Interface size', 'Makes text and buttons bigger or smaller', seg('uiSize', [[90, '90%'], [100, '100%'], [115, '115%'], [130, '130%']])) + row('Open files and folders', 'One click opens folders and shows previewable files', seg('openMode', [['single', 'One click'], ['double', 'Two clicks']])) + '</div>' +
      (A.settingsExtra ? A.settingsExtra({ row, seg, sw }) : '') +
      '<div class="sg"><h3>Help</h3><div class="sr"><div class="l"><b>Keyboard shortcuts</b><small>Press <span class="kbd">Ctrl</span> <span class="kbd">K</span> for everything</small></div><button class="btn" data-sx="keys">' + ic('keyboard') + 'Show</button></div></div>';
    const ORDER = ['Appearance', 'Language and accessibility', 'Transfers', 'Connection', 'Files', 'Security', 'Desktop', 'Backup', 'Troubleshooting', 'Help', 'About'];
    const groups = body.split('<div class="sg"><h3>').slice(1).map((g) => { const i = g.indexOf('</h3>'); return { name: g.slice(0, i), html: g.slice(i + 5) }; });
    groups.sort((a, b) => { const x = ORDER.indexOf(a.name), y = ORDER.indexOf(b.name); return (x < 0 ? 99 : x) - (y < 0 ? 99 : y); });
    if (!st.setTab && S.setTab) st.setTab = S.setTab;
    if (!groups.some((g) => g.name === st.setTab)) st.setTab = groups[0].name;
    const cur = groups.find((g) => g.name === st.setTab);
    const prev = $('#stage .setp'), keep = prev ? prev.scrollTop : 0;
    $('#stage').innerHTML = '<h2 class="pt">Settings</h2><div class="setl"><nav class="setn" aria-label="Settings sections">' + groups.map((g) => '<button data-snav="' + esc(g.name) + '"' + (g.name === cur.name ? ' aria-current="true"' : '') + ' class="' + (g.name === cur.name ? 'on' : '') + '">' + esc(g.name) + '</button>').join('') + '</nav><section class="sg setp"><h3>' + esc(cur.name) + '</h3>' + cur.html + '</section></div>';
    const np = $('#stage .setp'); if (np && keep) np.scrollTop = keep;
    if (A.fixSw) A.fixSw();
  }
  $('#stage').addEventListener('click', (e) => {
    if (st.view !== 'settings') return;
    const stab = e.target.closest('[data-snav]'); if (stab) { st.setTab = stab.dataset.snav; S.setTab = st.setTab; save(); A.pageSettings(); const sp = $('#stage .setp'); if (sp) sp.scrollTop = 0; if (A.applyLang) A.applyLang(); return; }
    if (e.target.closest('[data-limcustom]')) {
      A.dialog({ title: 'Speed limit', body: '<label class="b-f"><input id="limv" type="number" min="1" max="10000" step="1" value="' + (+S.limit > 0 ? +S.limit : '') + '" placeholder="MB per second" aria-label="Megabytes per second" autocomplete="off"></label><p class="mut">The most all transfers may use together, in megabytes per second.</p>', enter: 'ok', actions: [{ label: 'Cancel' }, { id: 'ok', label: 'Save', kind: 'f', cb: () => { const v = Math.round(+document.getElementById('limv').value); if (!(v >= 1)) { document.getElementById('limv').focus(); return false; } S.limit = Math.min(v, 10000); save(); E.pushPrefs(); A.pageSettings(); } }] });
      setTimeout(() => { const i = document.getElementById('limv'); if (i) i.focus(); }, 0); return;
    }
    const sg = e.target.closest('[data-set] button'); if (sg) { const k = sg.closest('[data-set]').dataset.set; let v = sg.dataset.v; if (!isNaN(+v) && v !== '') v = +v; S[k] = v; save(); applyTheme(); if (k === 'uiSize') A.applyZoom(); if (k === 'parallel' || k === 'limit' || k === 'onConflict') E.pushPrefs(); A.pageSettings(); if (k === 'density') { A.mainPane.render(); A.localPane.render(); } return; }
    const sw = e.target.closest('[data-sw]'); if (sw) { S[sw.dataset.sw] = !S[sw.dataset.sw]; save(); if (sw.dataset.sw === 'verify') E.pushPrefs(); A.pageSettings(); if (A.onSwitch) A.onSwitch(sw.dataset.sw); return; }
    const sx = e.target.closest('[data-sx]'); if (sx) { const a = sx.dataset.sx; if (A.settingsAction && A.settingsAction(a)) return; if (a === 'keys') shortcutsDialog(); }
  });
  function shortcutsDialog() {
    const k = [['Ctrl+K or /', 'Search'], ['Enter', 'Open folder or preview file'], ['Backspace', 'Up one folder'], ['Alt+← →', 'Back / forward'], ['F2', 'Rename'], ['Del', 'Delete'], ['Ctrl+A', 'Select all'], ['Ctrl+D', 'Download selection'], ['Ctrl+U', 'Upload from Local files'], ['Ctrl+Shift+N', 'New folder'], ['F5', 'Refresh'], ['Ctrl+L', 'Edit path'], ['Ctrl+.', 'Collapse or expand the navigation'], ['F1 or ?', 'This list'], ['Shift+Arrow', 'Extend the selection'], ['Esc', 'Clear selection / close']];
    A.dialog({ icon: 'keyboard', title: 'Keyboard shortcuts', width: 520, body: '<div style="display:grid;grid-template-columns:auto 1fr;gap:.5rem 1.25rem">' + k.map((r) => '<span class="kbd">' + r[0] + '</span><span>' + r[1] + '</span>').join('') + '</div>', actions: [{ label: 'Close', kind: 'f' }] });
  }

  /* ---------- command palette ---------- */
  function palette() {
    const m = A.mainPane; const cur = m.host; const cmds = [];
    const C = (icon, label, run, kbd, sub) => cmds.push({ icon, label, run, kbd, sub });
    C('folder', 'Go to Files', () => A.go('files')); if (A.v5on && A.v5on()) C('server', 'Go to Devices', () => A.go('servers')); else { C('server', 'Go to Computers', () => A.go('servers')); C('phone', 'Go to Devices', () => A.go('devices')); } C('swap', 'Go to Transfers', () => A.go('transfers')); C('search', 'Go to Search', () => A.go('search')); C('layers', 'Go to Tools', () => A.go('tools')); C('history', 'Go to History', () => A.go('history')); C('gear', 'Open Settings', () => A.go('settings'));
    for (const s of E.servers) C('server', 'Switch to ' + s.name, () => { A.go('files'); A.setMainHost(s.id); if (s.state === 'disconnected') E.connect(s.id); }, '', s.state === 'online' ? s.latency + ' ms' : s.state);
    C('monitor', 'Browse this computer', () => { A.go('files'); A.setMainHost('local'); });
    C('folder-plus', 'New folder here', () => { A.go('files'); m.startNew(); }, 'Ctrl+Shift+N'); C('refresh', 'Refresh folder', () => m.refresh(true), 'F5');
    C('download', 'Download selection', () => m.transfer(), 'Ctrl+D'); C('upload', 'Upload from Local files', uploadFab, 'Ctrl+U');
    C('plug', 'Add a computer…', () => A.serverDialog()); C('menu', 'Collapse or expand the navigation', () => A.toggleRail(), 'Ctrl+.');
    C('pause', 'Pause all transfers', () => E.pauseAll()); C('play', 'Resume all transfers', () => E.resumeAll()); C('retry', 'Retry failed transfers', () => E.retryFailed()); C('check', 'Clear completed transfers', () => E.clearDone());
    (A.paletteExtra || []).forEach((f) => f(C));
    C('palette', 'Toggle dark theme', () => { S.theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark'; save(); applyTheme(); if (st.view === 'settings') pageSettings(); }); C('keyboard', 'Keyboard shortcuts', shortcutsDialog);
    let hi = 0, shown = cmds.slice(); const opener = document.activeElement;
    const d = A.dialog({ title: '', width: A.DIALOG_W, noFocus: false, body: '', actions: [] });
    const sc = d.el.parentNode; sc.innerHTML = '<div class="pal" role="dialog" aria-label="Command palette"><div class="in">' + ic('search') + '<input id="pq" placeholder="Type a command or computer name" spellcheck="false" aria-label="Command" role="combobox" aria-expanded="true" aria-controls="pl" aria-autocomplete="list"></div><div class="lst" id="pl" role="listbox" aria-label="Commands"></div></div>';
    sc.style.placeItems = 'start center';
    const draw = () => { $('#pl', sc).innerHTML = shown.length ? shown.map((c, i) => '<div class="it' + (i === hi ? ' hi' : '') + '" data-i="' + i + '" id="pal' + i + '" role="option" aria-selected="' + (i === hi) + '">' + ic(c.icon) + '<span>' + esc(c.label) + '</span>' + (c.kbd ? '<kbd>' + esc(c.kbd) + '</kbd>' : c.sub ? '<small>' + esc(c.sub) + '</small>' : '') + '</div>').join('') : '<div class="none">No matching command</div>'; const h = $('.hi', sc); const q0 = $('#pq', sc); if (q0) { if (h) q0.setAttribute('aria-activedescendant', h.id); else q0.removeAttribute('aria-activedescendant'); } if (h) h.scrollIntoView({ block: 'nearest' }); };
    const closePal = () => { d.retire(); sc.remove(); document.removeEventListener('keydown', key, true); if (opener && opener !== document.body && opener.isConnected && typeof opener.focus === 'function') opener.focus(); };
    const run = (c) => { closePal(); c.run(); };
    const key = (e) => { if (e.key === 'Escape') { e.stopPropagation(); closePal(); } else if (e.key === 'ArrowDown') { e.preventDefault(); hi = Math.min(shown.length - 1, hi + 1); draw(); } else if (e.key === 'ArrowUp') { e.preventDefault(); hi = Math.max(0, hi - 1); draw(); } else if (e.key === 'Enter') { e.preventDefault(); if (shown[hi]) run(shown[hi]); } };
    document.addEventListener('keydown', key, true);
    sc.addEventListener('mousedown', (e) => { if (e.target === sc) closePal(); });
    $('#pl', sc).addEventListener('mousemove', (e) => { const it = e.target.closest('[data-i]'); if (it && +it.dataset.i !== hi) { hi = +it.dataset.i; draw(); } });
    $('#pl', sc).addEventListener('click', (e) => { const it = e.target.closest('[data-i]:not(svg)'); if (it) run(shown[+it.dataset.i]); });
    $('#pq', sc).addEventListener('input', (e) => { const w = e.target.value.toLowerCase().split(/\s+/).filter(Boolean); shown = cmds.filter((c) => w.every((x) => (c.label + ' ' + (c.sub || '')).toLowerCase().includes(x))); hi = 0; draw(); });
    draw(); $('#pq', sc).focus();
  }
  A.palette = palette;

  /* ---------- keyboard ---------- */
  document.addEventListener('keydown', (e) => {
    const t = e.target; const typing = t.matches && t.matches('input,textarea,select');
    if (document.querySelector('.scrim')) return;
    const mod = e.ctrlKey || e.metaKey;
    /* Ctrl+K opens the search panel (v5-transfers.js handles it first); the command palette stays callable as A.palette but has no key. */
    if (mod && !e.shiftKey && !e.altKey && (e.key || '').toLowerCase() === 'k' && A.v5Pages && A.v5Pages.on && A.v5Pages.on() && A.v5Pages.panel) { e.preventDefault(); A.v5Pages.panel.open(); return; }
    if (mod && !e.shiftKey && !e.altKey && e.key === '.') { e.preventDefault(); A.toggleRail(); return; }
    if (e.key === '/' && !typing) { e.preventDefault(); if (st.view !== 'search') A.go('search'); const q = $('#q'); if (q) q.focus(); return; }
    if ((e.key === '?' || e.key === 'F1') && !typing) { e.preventDefault(); shortcutsDialog(); return; }
    if (typing) return;
    /* A focused button, tab or switch keeps its own Enter, Space and Backspace: the file keys work only in the list or on the page itself. */
    if (['Enter', 'Backspace', 'Delete', 'F2'].includes(e.key) && t.closest && t.closest('button,[role=switch],[role=tab],[role=menuitem],a[href],select,summary') && !t.closest('[data-rows]')) return;
    const p = st.view === 'files' ? A.activePane : null; if (e.key === 'F5') { e.preventDefault(); if (p) p.refresh(true); return; }
    if (!p) return; const sel = p.selected(); const one = sel.length === 1 ? sel[0] : null;
    if (mod && e.key.toLowerCase() === 'a') { e.preventDefault(); p.selectAll(); }
    else if (e.key === 'Delete') { if (sel.length) { e.preventDefault(); p.del(); } }
    else if (e.key === 'F2') { if (one) { e.preventDefault(); p.startRename(one); } }
    else if (e.key === 'Enter') { if (one) { e.preventDefault(); p.open(one); } }
    else if (e.key === 'Backspace' || (e.altKey && e.key === 'ArrowUp')) { e.preventDefault(); p.up(); }
    else if (e.altKey && e.key === 'ArrowLeft') { e.preventDefault(); p.back(); } else if (e.altKey && e.key === 'ArrowRight') { e.preventDefault(); p.forward(); }
    else if (mod && e.key.toLowerCase() === 'd') { e.preventDefault(); if (p.host !== 'local') p.transfer(); }
    else if (mod && e.key.toLowerCase() === 'u') { e.preventDefault(); uploadFab(); }
    else if (mod && e.shiftKey && e.key.toLowerCase() === 'n') { e.preventDefault(); p.startNew(); }
    else if (mod && e.key.toLowerCase() === 'l') { e.preventDefault(); p.editPath = true; p.render(); }
    else if ((e.key === 'ArrowDown' || e.key === 'ArrowUp' || ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && p.mode === 'grid')) && (e.target === document.body || (e.target.closest && e.target.closest('[data-rows]')))) {
      e.preventDefault(); const n = p.mode === 'grid' ? Math.min(p.items.length, A.GRID_CAP) : p.items.length; if (!n) return;
      const step = e.key === 'ArrowDown' ? (p.mode === 'grid' ? p.cols() : 1) : e.key === 'ArrowUp' ? (p.mode === 'grid' ? -p.cols() : -1) : e.key === 'ArrowRight' ? 1 : -1;
      const from = e.shiftKey && p.lead >= 0 ? p.lead : p.cursor >= 0 ? p.cursor : p.anchor;
      const i = from < 0 ? 0 : Math.max(0, Math.min(n - 1, from + step));
      if (mod) p.setCursor(i); else { p.selectIdx(i, e); if (e.shiftKey) p.lead = i; }
      const rows = $('[data-rows]', p.el);
      if (rows && p.mode === 'list') { const rh = p.rh; if (i * rh < rows.scrollTop) rows.scrollTop = i * rh; else if ((i + 1) * rh > rows.scrollTop + rows.clientHeight) rows.scrollTop = (i + 1) * rh - rows.clientHeight; }
      else if (rows) { const t = rows.querySelector('[data-i="' + i + '"]'); if (t && t.scrollIntoView) t.scrollIntoView({ block: 'nearest' }); }
    }
    else if (e.key === ' ' && p.cursor >= 0 && e.target.tagName !== 'BUTTON' && e.target.tagName !== 'INPUT' && (e.target === document.body || (e.target.closest && e.target.closest('[data-rows]')))) { e.preventDefault(); p.selectIdx(p.cursor, { ctrlKey: true, type: 'keydown' }); }
    else if (e.key === 'Escape') { if (p.sel.size) { p.sel.clear(); p.selChanged(); } }
  });

  /* Tabs: Left and Right (Home and End) move to the neighbouring tab and open it; the new tab keeps the focus after the page redraws. */
  document.addEventListener('keydown', (e) => {
    const t = e.target; if (!t || !t.matches || !t.matches('[role="tab"]') || e.altKey || e.ctrlKey || e.metaKey) return;
    const k = e.key; if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(k)) return;
    const list = Array.from(t.parentNode.querySelectorAll('[role="tab"]')); const i = list.indexOf(t); if (i < 0) return;
    const j = k === 'Home' ? 0 : k === 'End' ? list.length - 1 : (i + (k === 'ArrowRight' ? 1 : -1) + list.length) % list.length;
    e.preventDefault(); const n = list[j]; const attr = Array.from(n.attributes).find((a) => a.name.startsWith('data-') && a.name !== 'data-i');
    n.click(); const back = () => { const q = attr ? document.querySelector('[role="tab"][' + attr.name + '="' + attr.value.replace(/"/g, '\\"') + '"]') : n; if (q && document.activeElement !== q) q.focus(); }; back(); setTimeout(back, 0); setTimeout(back, A.T.focusSettle);
  });

  /* ---------- engine events -> UI ---------- */
  let pend = new Set(), raf = 0, lastSlow = 0;
  const sched = (k) => { pend.add(k); if (!raf) raf = requestAnimationFrame(() => { raf = 0; const p = pend; pend = new Set(); p.forEach(flush); }); };
  function flush(k) {
    if (k === 'transfers') { renderSheet(); renderRail(); if (st.view === 'transfers') pageTransfers(); }
    else if (k === 'servers') { renderChip(); renderRail(); for (const p of [A.mainPane, A.localPane]) if (p.host !== 'local' && p.el && p.el.isConnected && !p.renaming && !p.creating && !p.editPath) { p.refreshItems(); p.render(); } if (st.view === 'servers') serversPage(); renderDetail(); renderLocalFooter(); }
    else if (k === 'fs') { const tree = !!A.v5Files && A.v5Files.on(); /* the v5 tree has no mounted main Pane, but the Pane still holds the selection the details panel reads, so its items follow the engine */
      for (const p of [A.mainPane, A.localPane]) { const live = p.el && p.el.isConnected; if ((live || (tree && p === A.mainPane)) && !p.renaming && !p.creating && !p.editPath) { p.refreshItems(); if (live) { p.renderRows(); p.renderFoot(); } } } renderDetail(); }
    else if (k === 'history') { if (st.view === 'history') pageHistory(); }
    else if (k === 'bell') updateBell();
  }
  E.on((type, data) => {
    if (type === 'tick') {
      renderSheet(); if (st.view === 'transfers') { A.patchCards($('#stage')); const s = $('#sumSpeed'); if (s) s.textContent = U.fmtSpeed(E.totalSpeed()); }
      const nowMs = performance.now(); if (nowMs - lastSlow > 900) { lastSlow = nowMs; if (st.view === 'servers') serversPage(); const h = A.mainPane.host; if (st.view === 'files' && h !== 'local' && !E.connected(h) && A.mainPane.el && A.mainPane.el.isConnected) A.mainPane.render(); renderChip(); }
    } else if (type === 'notify') { sched('bell'); if (data.kind === 'error') A.snack(data.text, /connect|reach|offline|lost|network|certificate|fingerprint|sign|login|key|timed out/i.test(data.text) ? { error: true, action: 'Computers', onAction: () => A.go('servers') } : { error: true, action: 'Transfers', onAction: () => A.go('transfers') }); }
    else sched(type === 'latency' ? 'servers' : type);
  });
  addEventListener('beforeunload', save);


  /* ---------- boot ---------- */
  applyTheme(); matchMedia('(prefers-color-scheme: dark)').addEventListener && matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme);
  const q = new URLSearchParams(location.search);
  buildTop(); renderAll(); updateBell();
  /* later scripts (features, fx*) extend A: start once they have all run */
  const boot = () => E.init().then(() => {
    A.applyZoom();
    const first = E.servers.find((x) => x.signedIn && x.state !== 'disconnected') || E.servers.find((x) => x.signedIn) || E.servers[0];
    A.mainPane.setHost(q.get('host') || (first ? first.id : 'local'));
    renderAll();
    if (!E.servers.length) A.go('servers');
    if (A.maybeOnboard) A.maybeOnboard();
    if (q.get('view')) A.go(q.get('view'));
  }).catch((e) => A.snack('Could not start: ' + (e && e.message || e), { error: true }));
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot); else boot();
  if (q.get('thumb')) document.documentElement.classList.add('nofx');
  A.paused = false;
  setInterval(() => { if (!A.paused) E.tick(A.T.tick); }, A.T.tick);

  /* ---------- the open page re-reads its data on a clock (the schedule is E.liveDue) ---------- */
  /* What each due job does. They look their function up at run time; a job still running is not started again; a failure waits for the next turn. */
  const liveLast = {}, liveBusy = new Set();
  /* A job takes the computer that pushed the change (none: every computer), so one computer's event reads only that computer. */
  const LIVE_RUN = {
    transfers: () => E.pollTransfers(),
    history: (host) => A.syncAudit(host ? [host] : undefined),
    devices: () => { if (A.loadDevices && (st.view !== 'servers' || !A.v5Devices || A.v5Devices.isOpen())) A.loadDevices(); if (A.loadInbox) return A.loadInbox(); },
    tools: () => { if (A.toolOpened) A.toolOpened(); },
    files: (host) => { const p = A.mainPane; if (!p || !p.host || (host && p.host !== host) || p.renaming || p.creating || p.editPath || !E.connected(p.host)) return; return E.fs.refresh(p.host, p.path); }
  };
  /* Runs the jobs that are due at time `now`, once. Called every A.T.liveTick by a timer and, with a fresh clock, when the window shows again. */
  A.liveTick = (now) => {
    const active = E.tasks.some((t) => E.ACTIVE.includes(t.state) && t.started);
    for (const k of E.liveDue({ view: st.view, visible: document.visibilityState !== 'hidden', active, push: livePush(), now, last: liveLast })) {
      if (liveBusy.has(k) || !LIVE_RUN[k]) continue;
      liveLast[k] = now; liveBusy.add(k);
      new Promise((done) => done(LIVE_RUN[k]())).catch(A.noop).finally(() => liveDone(k));
    }
  };
  setInterval(() => { if (!A.paused) A.liveTick(Date.now()); }, A.T.liveTick);

  /* ---------- changes the computers push (the core follows each computer's GET /v1/events and tells the window) ---------- */
  /* The core's stream state per computer: 'connected', or anything else (no stream, an agent too old for it, a refused login). */
  const pushState = {}, livePending = new Set();
  /* True when every online computer's stream is connected, so the pushed pages may poll slowly; one computer without a stream keeps the normal pace. */
  const livePush = () => { const on = E.servers.filter((s) => s.signedIn && s.state === 'online'); return on.length > 0 && on.every((s) => pushState[s.id] === 'connected'); };
  /* A job's read ended (a timer's or a push's): it may run again, and when a push came during it, it runs once more now. */
  const liveDone = (k) => { liveBusy.delete(k); if (livePending.delete(k)) liveNow(k, undefined, Date.now()); };
  /* Runs one job now for a pushed change (host: the computer it came from). A job still running is marked, and runs once more when it ends, so no change is lost and a burst makes one extra read. */
  const liveNow = (k, host, now) => {
    if (!LIVE_RUN[k]) return;
    if (liveBusy.has(k)) { livePending.add(k); return; }
    liveLast[k] = now; liveBusy.add(k);
    new Promise((done) => done(LIVE_RUN[k](host))).catch(A.noop).finally(() => liveDone(k));
  };
  /* One pushed event {host, kind}: re-reads at once what the open page shows of that kind. A hidden window reads nothing now; it refreshes everything when it shows again. */
  A.liveEvent = (ev) => {
    if (!ev || document.visibilityState === 'hidden') return;
    const now = Date.now();
    for (const k of E.liveEventJobs(ev.kind, st.view)) liveNow(k, ev.host, now);
  };
  /* The core's report of one computer's stream: 'connected', 'disconnected' or 'unsupported'. */
  A.liveStream = (s) => { if (s && s.host) pushState[s.host] = s.state; };
  /* Tells the core which computers to follow; asked again only when the set of signed-in computers changes (added, removed, signed in or out). */
  let followed = null;
  const followComputers = () => {
    const ids = E.servers.filter((s) => s.signedIn).map((s) => s.id).sort().join('|');
    if (ids === followed) return; followed = ids;
    for (const id of Object.keys(pushState)) if (!E.server(id)) delete pushState[id];
    E.call('events_sync').catch(A.noop);
  };
  E.on((type) => { if (type === 'servers') followComputers(); });
  const tev = window.__TAURI__ && window.__TAURI__.event;
  if (tev && tev.listen) {
    tev.listen('agent-event', (e) => A.liveEvent(e && e.payload)).catch(A.noop);
    tev.listen('agent-stream', (e) => A.liveStream(e && e.payload)).catch(A.noop);
  }
  document.addEventListener('visibilitychange', () => { if (document.visibilityState !== 'hidden') for (const k of Object.keys(liveLast)) delete liveLast[k]; });
  A.ready = true;
});
