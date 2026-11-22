/* RFE v5 - the Settings page and the Tools pages (Favorites & offline, Storage, Trash, Share links, Backup & sync, Apps),
   drawn in the v5 mockup's markup over the existing logic.
   Settings: the old page (pages.js pageSettings plus every group the later files add through A.settingsExtra) is still
   built first, so each setting, switch and button keeps its handler; this file then re-lays that same page out like the
   mockup (section list at the left, one section of rows at the right). Nothing is invented: a row exists only if the old
   page has it.
   Tools: each tool is drawn from the same data the old tools use (A.X: favorites, offline copies, Trash, share links,
   sync rules, the computers' storage and apps) and every button keeps its old data-fx action (fpages.js, features.js).
   It is used only while the window wears the v5 wrapper (class "d5" on #app); otherwise the old pages stay. */
rfeModule('v5-settings', ['fx6'], function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, esc = A.esc, ic = A.ic, st = A.state, X = A.X, H = A.fxHandlers, F = A.fx;
  if (!X || !H || !F) return;
  const { rel, left, plural } = F;
  const scopeToHost = A.U.scopeToHost;
  const T = (A.v5Settings = {});

  T.on = A.v5on; /* true while the v5 wrapper is on the window (v5-shell.js) */
  const stage = () => $('#stage');
  const host = (id) => A.hostName(id);
  /* A mockup page header (title, small line, buttons at the right). */
  const head = (t, sub, right) => '<header class="k-ph"><div><h1 class="k-title">' + esc(t) + '</h1>' + (sub ? '<p class="k-sub">' + esc(sub) + '</p>' : '') + '</div><div class="k-ph-r">' + (right || '') + '</div></header>';
  /* A mockup button; `fx` is the old data-fx action it runs. */
  const btn = (label, o) => { o = o || {}; return '<button type="button" class="k-btn' + (o.k ? ' ' + o.k : '') + '"' + (o.fx ? ' data-fx="' + esc(o.fx) + '"' : '') + (o.dis ? ' disabled' : '') + (o.tip ? ' title="' + esc(o.tip) + '"' : '') + '>' + (o.i ? ic(o.i, { size: 16 }) : '') + (label ? '<span>' + esc(label) + '</span>' : '') + '</button>'; };
  /* A mockup icon button. */
  const ib = (fx, icon, label) => '<button type="button" class="k-ib" data-fx="' + esc(fx) + '" aria-label="' + esc(label) + '" data-tip="' + esc(label) + '">' + ic(icon, { size: 16 }) + '</button>';
  /* A mockup card: title, buttons at the right, body. */
  const sec = (t, right, body) => '<section class="k-card"><div class="k-ch"><h2>' + esc(t) + '</h2><div>' + (right || '') + '</div></div>' + body + '</section>';
  /* The mockup's small empty note for inside a card (its k-empty): a 32 px colour picture, a plain-word title, one hint line. */
  const empty = (pic, t, s) => '<div class="k-empty">' + em(pic, 32) + '<b>' + esc(t) + '</b>' + (s ? '<small>' + esc(s) + '</small>' : '') + '</div>';
  /* The mockup's whole-page empty (its b-empty): a 40 px picture, a title, one hint and, if given, one button. */
  const whole = (pic, t, s, acts) => '<div class="b-empty">' + em(pic, 40) + '<b>' + esc(t) + '</b><p>' + esc(s) + '</p>' + (acts ? '<div class="k-row">' + acts + '</div>' : '') + '</div>';
  const err = (m) => (m ? '<p class="v5err">' + esc(m) + '</p>' : '');
  const note = (text) => '<p class="k-note">' + ic('info', { size: 14 }) + '<span>' + esc(text) + '</span></p>';
  const em = A.v5em; /* a mockup colour picture, n pixels wide (v5-shell.js) */
  const EM_KIND = { dir: 'file_folder', image: 'framed_picture', video: 'film_frames', archive: 'card_file_box', pkg: 'package', text: 'memo', doc: 'page_facing_up', code: 'scroll', key: 'key', exec: 'gear', file: 'page_facing_up' };
  /* The picture for a file or folder: by its kind, with spreadsheets and music told apart by the extension, as the mockup does. */
  const fileEm = (n, size) => {
    const k = window.RFE && window.RFE.kindOf ? window.RFE.kindOf(n) : n.t === 'dir' ? 'dir' : 'file', name = n.n || '';
    return em(k === 'dir' ? 'file_folder' : /\.(csv|tsv|xls|xlsx|ods)$/i.test(name) ? 'bar_chart' : /\.(mp3|flac|wav|ogg|m4a|aac|opus)$/i.test(name) ? 'musical_notes' : EM_KIND[k] || 'page_facing_up', size);
  };
  /* A file or folder picture for a row: the bare 20 px picture of the mockup's lists, or (`boxed`) the 18 px one in the small box the tables use. */
  const fi = (n, boxed) => (boxed ? '<span class="k-fic">' + fileEm(n, 18) + '</span>' : fileEm(n, 20));
  const online = A.online;
  /* The computer a tool shows: the one remembered in X[k] if it is still connected, else the first connected one. */
  const pick = (k) => { const on = online(); if (!on.length) return null; if (!on.some((s) => s.id === X[k])) X[k] = on[0].id; return E.server(X[k]); };
  /* The computer buttons ("segments") at the top of Storage and Apps. */
  const hostSeg = (cur, fx) => '<span class="k-seg" role="group" aria-label="Computer">' + online().map((s) => '<button type="button" class="' + (s.id === cur ? 'on' : '') + '" aria-pressed="' + (s.id === cur) + '" data-fx="' + fx + '|' + esc(s.id) + '">' + esc(s.name) + '</button>').join('') + '</span>';
  const noServer = (pic, t, s) => whole(pic, t, s);
  /* Which computer's items the Tools pages show: the selected one (see Engine.util.pickToolHost) and whether Show all is on. */
  function toolScope() {
    X.toolHost = U.pickToolHost(online().map((s) => s.id), X.toolHost, A.mainPane && A.mainPane.host);
    return { sel: X.toolHost, all: !!X.toolAll };
  }
  /* The bar at the top of Trash and Share links: one button per connected computer (none lit while Show all is on) and the Show all toggle. */
  function scopeBar(sc) {
    if (!sc.sel) return '';
    return '<div class="k-fbar">' + hostSeg(sc.all ? '' : sc.sel, 'tool.host') + '<span class="sp"></span><button type="button" class="k-btn sm' + (sc.all ? ' pri' : '') + '" aria-pressed="' + sc.all + '" data-fx="tool.all"><span>' + (sc.all ? 'Only the selected computer' : 'Show all') + '</span></button></div>';
  }

  /* ================= Tools ================= */

  /* The tool tabs: the six built-in ones with their counts, then any a later file added through A.toolTabs. */
  const tabs = () => [['fav', 'star', 'Favorites & offline'], ['store', 'pie', 'Storage'], ['trash', 'trash', 'Trash', scopeToHost(X.trash, (r) => r.host, toolScope().sel, toolScope().all).length], ['share', 'link', 'Share links', scopeToHost(X.shares, (s) => s.host, toolScope().sel, toolScope().all).filter((s) => s.exp > E.now()).length], ['sync', 'refresh', 'Backup & sync'], ['apps', 'grid', 'Apps']].concat((A.toolTabs || []).map((f) => f()));

  /* Favorites, recent files and the files kept offline. */
  function toolFav() {
    const m = A.mainPane, canAdd = !!(m && m.online() && m.host !== 'local');
    const favs = X.favs.map((f, i) => '<div class="k-lrow big" role="button" tabindex="0" data-fx="fav.open|' + i + '">' + fi({ n: f.name, t: f.t === 'dir' ? 'dir' : 'file', b: 0 }) + '<span><b>' + esc(f.name) + '</b><small>' + esc(host(f.host)) + ' · ' + esc(f.path) + '</small></span><span class="k-hov">' + ib('fav.rm|' + i, 'trash', 'Remove from favorites') + '</span></div>').join('');
    const recs = X.recents.slice(0, 6).map((r, i) => '<div class="k-lrow big" role="button" tabindex="0" data-fx="rec.open|' + i + '">' + fi({ n: r.name, t: 'file', b: 0 }) + '<span><b>' + esc(r.name) + '</b><small>' + esc(host(r.host)) + ' · ' + esc(U.parentOf(r.path)) + ' · ' + rel(r.at) + '</small></span>' + ic('chevron-right', { size: 16 }) + '</div>').join('');
    const pins = X.pins.map((p, i) => {
      const pr = A.pinProg(p), t = p.tid ? E.task(p.tid) : null, bad = t && t.state === 'failed';
      const sub = bad ? (t.msg || 'The download failed') : pr >= 1 ? (t ? 'Available offline · ' : 'Downloaded earlier (not checked now) · ') + U.fmtBytes(p.b) : 'Downloading ' + Math.round(pr * 100) + '% of ' + U.fmtBytes(p.b);
      const state = bad ? '<span class="k-offb"><span class="bad">' + ic('alert-circle', { size: 16 }) + '</span></span>' : pr >= 1 ? '<span class="k-offb ok">' + ic('check', { size: 16 }) + '</span>' : '<span class="k-offb"><span class="k-bar" role="progressbar" aria-label="Download progress" aria-valuemin="0" aria-valuemax="100" aria-valuenow="' + Math.round(pr * 100) + '"><i style="width:' + Math.round(pr * 100) + '%"></i></span><em>' + Math.round(pr * 100) + '%</em></span>';
      return '<div class="k-lrow">' + fi({ n: p.name, t: 'file', b: p.b }) + '<span><b>' + esc(p.name) + '</b><small>' + esc(sub) + '</small></span>' + state + '<span class="k-hov">' + ib('pin.rm|' + i, 'x', 'Stop keeping offline') + '</span></div>';
    }).join('');
    const add = btn('Add current folder', { i: 'plus', k: 'sm', fx: 'fav.add', dis: !canAdd, tip: canAdd ? 'Adds ' + host(m.host) + ':' + m.path : 'Open a folder on a connected computer in Files first' });
    /* nothing at all to show: the mockup's whole-page empty, keeping the one real action (Add current folder) */
    if (!favs && !recs && !pins) return whole('star', 'No favorites', 'Star a folder in Files to keep it here.', add);
    return '<div class="k-g2">' + sec('Favorites', add, favs ? '<div class="k-list">' + favs + '</div>' : empty('star', 'No favorites', 'Star a folder in Files to keep it here.')) +
      sec('Recent files', '', recs ? '<div class="k-list">' + recs + '</div>' : empty('page_facing_up', 'No recent files', 'Files you open show up here.')) + '</div>' +
      sec('Available offline', '<small class="mut">Right-click a file and choose Make available offline</small>', pins ? '<div class="k-list">' + pins + '</div>' : empty('pushpin', 'Nothing offline', 'Right-click a file and choose Make available offline.'));
  }

  /* Storage of one connected computer: space, this session's transfers, the largest files and the duplicate finder. */
  function toolStore() {
    const s = pick('storeHost'); if (!s) return noServer('file_cabinet', 'No storage to show', 'Disk use appears once a computer is connected.');
    const gb = A.fmtGB;
    const done = E.tasks.filter((t) => t.state === 'done'), sum = (d) => done.filter((t) => t.dir === d).reduce((a, t) => a + (t.bytes || 0), 0);
    const failed = E.tasks.filter((t) => t.state === 'failed').length + E.history.filter((h) => h.kind === 'transfer' && h.error && !(h.tid && E.task(h.tid))).length;
    const roots = (s.roots || []).filter((r) => r.totalBytes > 0), COL = ['var(--ac)', 'var(--t2)', 'var(--t3)', 'var(--pos)', 'var(--warn)'];
    const hasDisk = s.disk && s.disk[1] > 0;
    const stats = [[hasDisk ? gb(s.disk[0]) + ' of ' + gb(s.disk[1]) : '—', hasDisk ? 'used on ' + s.name : 'disk use not reported'], [U.fmtBytes(sum('up')), 'uploaded this session'], [U.fmtBytes(sum('down')), 'downloaded this session'], [String(failed), failed === 1 ? 'failed transfer' : 'failed transfers']];
    const space = roots.length ? '<div class="k-stack">' + roots.map((r, i) => '<i style="flex:' + Math.max(0.5, r.totalBytes - r.freeBytes) + ';background:' + COL[i % COL.length] + '" title="' + esc(r.label || r.path) + '"></i>').join('') + '</div><div class="k-legend">' + roots.map((r, i) => '<span><i class="k-dot" style="background:' + COL[i % COL.length] + '"></i>' + esc(r.label || r.path) + ' ' + U.fmtBytes(r.totalBytes - r.freeBytes) + ' of ' + U.fmtBytes(r.totalBytes) + '</span>').join('') + '</div>' : hasDisk ? '<div class="k-stack"><i style="flex:' + s.disk[0] + ';background:var(--ac)"></i><span style="flex:' + Math.max(0, s.disk[1] - s.disk[0]) + '"></span></div><div class="k-legend"><span class="mut">' + gb(Math.max(0, s.disk[1] - s.disk[0])) + ' free of ' + gb(s.disk[1]) + '</span></div>' : empty('file_cabinet', 'No storage to show', 'This computer did not report its disk use.');
    const big = X.bigHost === s.id ? X.big : null, top = big && big.length ? big[0].b : 1;
    const bigBody = X.bigBusy ? '<p class="v5sub">Searching…</p>' : X.bigErr ? err(X.bigErr) : big && big.length ? '<div class="k-list">' + big.map((f) => '<div class="k-lrow" role="button" tabindex="0" data-fx="store.show|' + esc(f.p) + '" title="Show in folder">' + fi({ n: f.name, t: 'file', b: f.b }, true) + '<span><b>' + esc(f.name) + '</b><small class="mono">' + esc(U.parentOf(f.p)) + '</small><span class="k-bar"><i style="width:' + Math.max(2, f.b / top * 100) + '%"></i></span></span><span class="mono t2">' + U.fmtBytes(f.b) + '</span></div>').join('') + '</div>' : empty('bar_chart', big ? 'No big files' : 'Not scanned yet', big ? 'Nothing over ' + U.fmtBytes(50e6) + ' was found.' : 'Press Scan again to look for the largest files.');
    const dup = X.dupes;
    const dupBody = dup ? (dup.length ? '<div class="k-list">' + dup.slice(0, 6).map((g, gi) => '<div class="k-lrow big">' + fi({ n: g[0].name, t: 'file', b: g[0].size }) + '<span class="k-dup"><b>' + esc(g[0].name) + '</b><small>' + g.length + ' copies · ' + U.fmtBytes(g[0].size) + ' each · ' + U.fmtBytes(g[0].size * (g.length - 1)) + ' can be freed</small>' + g.map((f, i) => '<small class="mono' + (i === 0 ? ' keep' : '') + '">' + esc(f.path) + (i === 0 ? ' · keep, newest' : '') + '</small>').join('') + '</span>' + btn('Trash ' + (g.length - 1) + ' older', { i: 'trash', k: 'sm', fx: 'dup.clean|' + gi }) + '</div>').join('') + '</div>' : empty('check_mark_button', 'No duplicates','Of the ' + (X.dupeScanned || 0) + ' files over 1 MB checked on ' + s.name + ', none share a name and size.')) : (X.dupeErr ? err(X.dupeErr) : '<p class="v5sub">Scanning reads file names and sizes only, for up to 500 files over 1 MB. Nothing is changed.</p>');
    return '<div class="k-fbar">' + hostSeg(s.id, 'store.host') + '</div><div class="k-stats">' + stats.map((x) => '<div><b class="mono">' + esc(x[0]) + '</b><small>' + esc(x[1]) + '</small></div>').join('') + '</div>' +
      sec('Space by folder', '', space) +
      '<div class="k-g2">' + sec('Largest files', btn(X.bigBusy ? 'Scanning…' : 'Scan again', { i: 'refresh', k: 'sm ghost', fx: 'big.scan', dis: X.bigBusy }), bigBody) + sec('Duplicates', btn(X.dupeBusy ? 'Scanning…' : dup ? 'Scan again' : 'Find duplicates', { k: 'sm ghost', fx: 'dup.scan', dis: X.dupeBusy }), dupBody) + '</div>';
  }

  /* The title of the old "Delete old items" question (features.js): the Trash banner below shows it, and the dialog hook further down matches it by this title. */
  /* Deleted items from every connected computer, with the selection the Restore / Delete forever buttons act on. */
  function toolTrash() {
    const sc = toolScope();
    return scopeBar(sc) + trashBody(scopeToHost(A.trashRows(), (r) => r.r.host, sc.sel, sc.all), sc);
  }
  /* The Trash card for the given (already narrowed) rows. */
  function trashBody(rows, sc) {
    const sel = X.trashSel;
    if (!rows.length) return X.trash.length && !sc.all ? whole('wastebasket', 'Trash is empty on ' + host(sc.sel), 'Press Show all to see what other computers hold.') : X.trashErr ? sec('Trash', '', err(X.trashErr) + empty('wastebasket', 'Trash did not load', 'Try again in a moment.')) : X.trashBusy && !X.trashAt ? sec('Trash', '', empty('hourglass_not_done', 'Loading…', '')) : whole('wastebasket', 'Trash is empty', S.trash === false ? 'Trash is turned off in Settings, so deletes are permanent.' : 'Deleted files wait here before they go for good.');
    const all = rows.every((r) => sel.has(r.k)), tot = rows.reduce((a, r) => a + r.n.b, 0);
    const days = +S.trashDays || E.TRASH_DAYS_DEFAULT, cut = Date.now() - days * A.T.day, stale = X.v5TrashOld ? rows.filter((r) => r.r.at && r.r.at < cut).length : 0;
    const banner = stale ? '<div class="k-banner" role="region" aria-label="Old items in Trash">' + ic('clock', { size: 16 }) + '<span><b>Delete old items from Trash?</b> ' + stale + (stale === 1 ? ' item was' : ' items were') + ' deleted more than ' + days + ' days ago (Settings, Files).</span>' + btn('Delete them', { k: 'sm', fx: 'v5.trashold|del' }) + btn('Keep them', { k: 'sm ghost', fx: 'v5.trashold|keep' }) + '</div>' : '';
    const right = '<small class="mut">' + (sel.size ? sel.size + ' selected' : plural(rows.length, 'item', 'items') + ' · ' + U.fmtBytes(tot)) + '</small>' + btn('Restore' + (sel.size ? ' ' + sel.size : ''), { i: 'undo', k: 'sm pri', fx: 'trash.restore', dis: !sel.size }) + ib('v5.trashmore', 'more', 'Delete forever, empty Trash');
    const ck = (on, fx, label) => '<span class="k-ck' + (on ? ' on' : '') + '" role="checkbox" tabindex="0" aria-checked="' + !!on + '" aria-label="' + esc(label) + '" data-fx="' + esc(fx) + '">' + (on ? ic('check', { size: 12 }) : '') + '</span>';
    return banner + sec('Trash', right, err(X.trashErr) + '<div class="k-tbl k-trash"><div class="th">' + ck(all, 'trash.all', 'Select all') + '<span>Name</span><span>Deleted from</span><span>Deleted</span><span class="r">Size</span></div>' +
      rows.map((r) => { const on = sel.has(r.k); return '<div class="tr' + (on ? ' sel' : '') + '" role="checkbox" tabindex="0" aria-checked="' + on + '" data-fx="trash.pick|' + esc(r.k) + '">' + '<span class="k-ck" aria-hidden="true">' + (on ? ic('check', { size: 12 }) : '') + '</span><span class="dn">' + fi(r.n, true) + '<b data-nolocal>' + esc(r.n.n) + '</b></span><span class="mono t2">' + esc(host(r.t.host)) + ':' + esc(r.t.dir) + '</span><span class="t2">' + (r.t.at ? rel(r.t.at) : '—') + '</span><span class="mono t2 r">' + (r.n.t === 'dir' ? '—' : U.fmtBytes(r.n.b)) + '</span></div>'; }).join('') + '</div>');
  }

  /* The address of each link made in this session, by computer and link id. The computer keeps only a hash of a link, so
     an earlier session's link cannot be copied again; nothing is stored on disk. It is read from the answer of the
     existing "make a link" call. */
  const urls = {};
  const call0 = E.call;
  E.call = function (cmd) {
    const p = call0.apply(this, arguments);
    if (cmd === 'share_mint' && arguments[1] && p && p.then) { const h = arguments[1].host; p.then((r) => { if (r && r.tokenHash && r.url) urls[h + '~' + r.tokenHash] = r.url; }, () => {}); }
    return p;
  };
  /* Copy link: puts the address of a link made in this session on the clipboard. */
  H['v5.sharecopy'] = (i) => {
    const s = X.shares[+i], u = s && urls[s.host + '~' + s.hash];
    if (u) A.copy(u, 'Link copied'); else A.snack('This link can’t be shown again. Make a new one to copy it.', { error: true });
  };
  /* The banner of Trash: the existing "Delete old items" question (features.js), shown here instead of as a dialog while this page style is on. */
  const ask0 = A.askOldTrash;
  A.askOldTrash = function (o) {
    if (T.on() && o && Array.isArray(o.actions)) { X.v5TrashOld = o; return null; }
    return ask0.apply(this, arguments);
  };
  /* Delete them runs the dialog's own delete button; Keep them just closes the banner (the question is not asked again for the same items). */
  H['v5.trashold'] = (a) => {
    const o = X.v5TrashOld; X.v5TrashOld = null;
    const go = o && a === 'del' ? o.actions.find((x) => x.id === 'd') : null;
    if (go && go.cb) go.cb({ close() {} });
    if (X.tool === 'trash' && st.view === 'tools') A.pageTools();
  };

  /* The share links the computers hold; a live one can be turned off, an expired one removed. */
  function toolShare() {
    const sc = toolScope(), bar = scopeBar(sc);
    const rows = scopeToHost(X.shares.map((s, i) => ({ s, i })), (x) => x.s.host, sc.sel, sc.all).map(({ s, i }) => { const live = s.exp > E.now(), url = urls[s.host + '~' + s.hash]; return '<div class="k-lrow' + (live ? '' : ' dim') + '" tabindex="0" data-row="share|' + esc(s.host + '~' + s.hash) + '">' + fi({ n: s.name, t: 'file', b: 0 }) + '<span><b>' + esc(s.name) + '</b><small>On ' + esc(host(s.host)) + (live ? ' · anyone with the link can open it' : ' · expired') + ' · <span class="mono">' + esc(U.parentOf(s.path)) + '</span></small></span><span class="t2">' + (live ? 'expires in ' + left(s.exp - E.now()) : rel(s.exp)) + '</span>' + (live ? btn('Copy link', { i: 'copy', k: 'sm', fx: 'v5.sharecopy|' + i, dis: !url, tip: url ? '' : 'The computer keeps only a hash of a link, so it can be copied only in the session that made it' }) : '') + ib('share.revoke|' + i, live ? 'power' : 'trash', live ? 'Turn this link off' : 'Remove this link') + '</div>'; }).join('');
    /* no links, nothing loading and no error: the mockup's whole-page empty, keeping the "New link from selection" action */
    if (!rows && !X.sharesErr && !(X.sharesBusy && !X.sharesAt)) return bar + whole('link', 'No share links', 'Make one from a file: Share link…', btn('New link from selection', { i: 'plus', k: 'sm pri', fx: 'share.new' }));
    return bar + sec('Live share links', btn('New link from selection', { i: 'plus', k: 'sm pri', fx: 'share.new' }) + '<small class="mut">Make one from a file: Share link…</small>', err(X.sharesErr) + (rows ? '<div class="k-list">' + rows + '</div>' : X.sharesBusy && !X.sharesAt ? empty('hourglass_not_done', 'Loading…', '') : empty('link', 'No share links', 'Make one from a file: Share link…')));
  }

  /* One rule's state: running while any of its transfers is unfinished; when they all ended it records the finish time and failures (as the old page did). */
  function ruleState(r) {
    const ids = r.ids || []; if (!ids.length) return 'idle';
    if (ids.some((id) => { const t = E.task(id); return t && !['done', 'failed'].includes(t.state); })) return 'running';
    r.failed = ids.filter((id) => { const t = E.task(id); return t && t.state === 'failed'; }).length; r.ids = []; r.last = E.now(); F.persist(); return 'idle';
  }
  /* The one-way folder pairs. */
  function toolSync() {
    const rows = X.rules.map((r) => {
      const s = ruleState(r), sv = E.server(r.host), up = r.dir === 'up';
      const line = s === 'running' ? 'Syncing now' : r.last ? 'Last run ' + rel(r.last) + (r.failed ? ' · ' + plural(r.failed, 'transfer', 'transfers') + ' failed, see Transfers' : '') : 'Never run';
      return '<div class="k-lrow">' + em(up ? 'outbox_tray' : 'inbox_tray', 20) + '<span><b>' + esc(r.name) + '</b><small class="mono">' + esc(r.local) + ' ' + (up ? '→' : '←') + ' ' + esc(sv ? sv.name : r.host) + ':' + esc(r.remote) + '</small><small>' + esc(line) + '</small></span>' + btn('Run now', { i: 'play', k: 'sm', fx: 'sync.run|' + r.id, dis: s === 'running' }) + '<span class="k-hov">' + ib('sync.del|' + r.id, 'trash', 'Delete this rule') + '</span></div>';
    }).join('');
    return sec('Sync rules', btn('New rule', { i: 'plus', k: 'sm pri', fx: 'sync.new' }), '<p class="k-sub">One-way folder pairs. Run now copies only files that are new or different in size; nothing is deleted.</p>' + (rows ? '<div class="k-list">' + rows + '</div>' : empty('counterclockwise_arrows_button', 'No sync rules', 'Pair a folder here with one on a computer.'))) + note('The settings backup lives in Settings, Backup.');
  }

  /* The mockup picture for a program, guessed from its name and category (a toolbox when nothing fits). */
  const appEm = (x) => {
    const t = (x.name + ' ' + (x.category || '')).toLowerCase();
    return /gimp|paint|draw|inkscape|krita|graphic/.test(t) ? 'artist_palette' : /firefox|chrom|browser|web|internet/.test(t) ? 'globe_with_meridians' : /video|movie|player|vlc|kdenlive|obs|record/.test(t) ? 'film_frames' : /calc|sheet|excel|office|spreadsheet/.test(t) ? 'bar_chart' : /file|nautilus|dolphin|thunar|explorer/.test(t) ? 'file_cabinet' : /game|steam/.test(t) ? 'puzzle_piece' : 'toolbox';
  };
  /* The programs a connected computer lets this login start; a whole card launches. */
  function toolApps() {
    const on = online(); if (!on.length) return noServer('toolbox', 'No apps to show', 'A connected computer lists the apps it lets you start.');
    if (!X.appHost) X.appHost = toolScope().sel;
    const s = pick('appHost'), a = X.apps[s.id] || {}, seg = hostSeg(s.id, 'app.host');
    if (a.err) return '<div class="k-fbar">' + seg + '</div>' + empty('warning', 'Could not list apps', a.err);
    if (!a.cat) return '<div class="k-fbar">' + seg + '</div>' + empty('hourglass_not_done', 'Loading…','Asking ' + s.name + ' for its apps.');
    const cat = a.cat, q = (X.appQ || '').toLowerCase(), apps = cat.apps.filter((x) => !q || (x.name + ' ' + (x.category || '')).toLowerCase().includes(q));
    const cards = apps.slice(0, 60).map((x) => { const ok = x.launchable && cat.launchAllowed, busy = X.busyApp === s.id + x.id; return '<button type="button" class="k-app' + (ok ? '' : ' dim') + '"' + (ok ? ' data-fx="app.run|' + esc(x.id) + '" data-tip="Open ' + esc(x.name) + ' on ' + esc(s.name) + '’s screen"' : ' disabled') + (busy ? ' data-busy="1"' : '') + '><span class="k-appic">' + em(appEm(x), 24) + '</span><div><b>' + esc(x.name) + '</b><small>' + esc(x.description || '') + '</small><small class="mut">' + esc(x.category || 'App') + '</small></div>' + (ok ? '<span class="k-go">' + ic('play', { size: 14 }) + (busy ? 'Starting…' : 'Launch') + '</span>' : '<small class="mut">Cannot be launched</small>') + '</button>'; }).join('');
    return '<div class="k-fbar">' + seg + '<span class="sp"></span><label class="k-in sm">' + ic('filter', { size: 14 }) + '<input id="appq" placeholder="Filter apps" aria-label="Filter apps" value="' + esc(X.appQ || '') + '" spellcheck="false"></label></div>' +
      (cards ? '<div class="k-apps">' + cards + '</div>' : empty('toolbox', q ? 'No app matches' : 'No apps to show', q ? 'Try another word in the filter.' : 'This computer lists no apps to start.')) + (apps.length > 60 ? '<p class="v5sub">+ ' + (apps.length - 60) + ' more. Type in the filter to narrow the list.</p>' : '') +
      note('Apps open on ' + s.name + '’s own screen.' + (cat.launchAllowed ? '' : ' This sign-in may see the list but not start apps there.'));
  }

  /* The computer buttons and Show all of the Tools pages. Choosing a computer or flipping Show all clears the Trash selection so Restore never acts on rows that are hidden. */
  H['tool.host'] = (id) => { X.toolHost = id; X.toolAll = false; X.appHost = id; X.trashSel.clear(); A.pageTools(); };
  H['tool.all'] = () => { X.toolAll = !X.toolAll; X.trashSel.clear(); A.pageTools(); };
  const appHost0 = H['app.host'];
  H['app.host'] = (id) => { X.toolHost = id; appHost0(id); };

  const BODIES = { fav: toolFav, store: toolStore, trash: toolTrash, share: toolShare, sync: toolSync, apps: toolApps };

  /* Draws the Tools page. An unchanged page is left alone (this runs on every poll); a rebuilt one gives the focus back to the same control. */
  T.tools = () => {
    const s = stage(), t = X.tool;
    const body = BODIES[t] ? BODIES[t]() : A.toolBodies && A.toolBodies[t] ? A.toolBodies[t]() : toolFav();
    const bar = '<div class="k-ttabs" role="tablist">' + tabs().map((x) => '<button type="button" class="k-chip' + (t === x[0] ? ' on' : '') + '" role="tab" aria-selected="' + (t === x[0]) + '" data-fx="tool.tab|' + x[0] + '">' + ic(x[1], { size: 16 }) + esc(x[2]) + (x[3] ? ' <span class="cnt">' + x[3] + '</span>' : '') + '</button>').join('') + '</div>';
    const html = '<div class="v5pg">' + head('Tools', 'Favorites, storage, Trash, links, sync and your computers’ apps') + bar + '<div class="k-tbody">' + body + '</div></div>';
    s.className = 'stage page';
    if (s._tv === html && s.querySelector('.v5pg')) return;
    const a = document.activeElement; let sel = '';
    if (a && a !== document.body && s.contains(a)) { const q = (v) => String(v).replace(/"/g, '\\"'); if (a.getAttribute('data-fx')) sel = '[data-fx="' + q(a.getAttribute('data-fx')) + '"]'; else if (a.getAttribute('data-row')) sel = '[data-row="' + q(a.getAttribute('data-row')) + '"]'; else if (a.id) sel = '#' + a.id; }
    const keep = s.querySelector('.v5pg') ? s.scrollTop : 0;
    s.innerHTML = html; s._tv = html; if (keep) s.scrollTop = keep;
    if (sel) { const n = s.querySelector(sel); if (n && document.activeElement !== n) n.focus({ preventScroll: true }); }
  };
  const pageTools0 = A.pageTools;
  A.pageTools = () => { if (T.on()) T.tools(); else pageTools0(); };

  /* The "more" button of Trash: the two ways to remove for good (each asks first, in its old dialog). */
  H['v5.trashmore'] = (a, b, el) => { const r = el.getBoundingClientRect(); A.menu(r.right - 220 * A.u(), r.bottom + 4, [{ icon: 'x', label: 'Delete forever', danger: true, disabled: !X.trashSel.size, onClick: () => H['trash.purge']() }, '-', { icon: 'trash', label: 'Empty Trash…', danger: true, onClick: () => H['trash.empty']() }]); };
  /* Enter or Space on a focused row or tick box runs its action, the same as a click. */
  document.addEventListener('keydown', (e) => {
    if ((e.key !== 'Enter' && e.key !== ' ') || e.ctrlKey || e.metaKey || e.altKey) return;
    const r = e.target && e.target.closest && e.target.closest('.v5pg .k-lrow[data-fx], .v5pg .k-ck[data-fx], .v5pg .k-trash .tr[data-fx]'); if (!r || r !== e.target) return;
    e.preventDefault(); r.click();
  });
  /* The rows of the Tools lists that take the focus: Trash items, rows of a list that carry a tab stop (share links, favorites, largest files) and app cards. */
  const ROW = '.k-trash .tr[tabindex], .k-list > .k-lrow[tabindex], .k-apps > .k-app';
  /* Up and Down (also Left and Right in the app grid, Home and End) move the focus from one such row to its neighbour. */
  document.addEventListener('keydown', (e) => {
    if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey || !['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
    const r = e.target && e.target.closest && e.target.closest(ROW); if (!r || r !== e.target || !r.closest('.v5pg')) return;
    const list = [...r.parentNode.children].filter((n) => n.matches(ROW) && !n.disabled), at = list.indexOf(r), grid = r.classList.contains('k-app'), down = e.key === 'ArrowDown' || e.key === 'ArrowRight', horiz = e.key === 'ArrowLeft' || e.key === 'ArrowRight';
    let n = null;
    if (e.key === 'Home') n = list[0]; else if (e.key === 'End') n = list[list.length - 1];
    else if (horiz) { if (grid) n = list[at + (down ? 1 : -1)]; else return; }
    else if (!grid) n = list[at + (down ? 1 : -1)];
    else {
      /* a grid: the nearest card in the next line, by its distance from this card's left edge */
      const lines = list.filter((x) => (down ? x.offsetTop > r.offsetTop + 2 : x.offsetTop < r.offsetTop - 2));
      if (lines.length) { const top = down ? Math.min(...lines.map((x) => x.offsetTop)) : Math.max(...lines.map((x) => x.offsetTop)); n = lines.filter((x) => x.offsetTop === top).sort((a, b) => Math.abs(a.offsetLeft - r.offsetLeft) - Math.abs(b.offsetLeft - r.offsetLeft))[0]; }
    }
    if (!n) return;
    e.preventDefault(); n.focus(); if (n.scrollIntoView) n.scrollIntoView({ block: 'nearest' });
  });

  /* ================= Settings ================= */

  const SECTION_ICON = { Appearance: 'sun', 'Language and accessibility': 'text', Transfers: 'swap', Connection: 'plug', Files: 'folder', Security: 'shield', Desktop: 'laptop', Backup: 'database', Troubleshooting: 'activity', Help: 'keyboard', About: 'info' };
  /* The shortcut list in the mockup's order; Delete is "Move to Trash" while Trash is on, and a plain delete when it is off. */
  const KEYS = () => [['Ctrl+K or /', 'Search'],['Ctrl+\\', 'Collapse navigation'], ['Ctrl+L', 'Type a path'], ['Ctrl+D', 'Download'], ['Ctrl+U', 'Upload'], ['Ctrl+Shift+N', 'New folder'], ['F2', 'Rename'], ['Delete', S.trash === false ? 'Delete' : 'Move to Trash'], ['F5', 'Refresh'], ['Alt+←', 'Back'], ['Alt+→', 'Forward'], ['Alt+↑', 'Up'], ['Ctrl+A', 'Select all'], ['F1 or ?', 'This list'], ['Esc', 'Clear selection or close']];
  const kbds = (k) => k.split(' or ').map((p) => p.split('+').map((x) => '<kbd>' + esc(x) + '</kbd>').join('')).join(' or ');

  /* Turns the old settings row controls into the mockup's: segments, switches, buttons, selects and the closed technical fold. */
  function skinControls(root) {
    root.querySelectorAll('.seg').forEach((g) => { g.classList.remove('seg'); g.classList.add('k-seg'); });
    root.querySelectorAll('button.sw').forEach((b) => { b.classList.remove('sw'); b.classList.add('k-sw'); });
    root.querySelectorAll('button.btn').forEach((b) => {
      const c = b.classList, danger = c.contains('e') || c.contains('ed'), ghost = c.contains('tx');
      b.className = 'k-btn sm' + (danger ? ' warn' : ghost ? ' ghost' : ''); b.setAttribute('type', 'button');
      b.querySelectorAll('svg').forEach((s) => s.remove()); /* the mockup's settings buttons are words only */
    });
    root.querySelectorAll('select').forEach((s) => s.classList.add('k-sel'));
    root.querySelectorAll('details.adtech').forEach((d) => { d.className = 'b-tech'; d.querySelectorAll('dl').forEach((l) => { l.className = 'k-kv'; }); });
  }
  /* What the mockup says on a row, by the row's title in the old page: a new small line (`sub`, '' removes it; `when` limits it to rows whose
     old line matches), a new button word (`btn`) or a new title (`label`). Handlers stay: only the words change. */
  const ROW_TEXT = {
    'Parallel transfers': { sub: 'The others wait for a free slot' },
    'Speed limit': { sub: 'Shared by all transfers' },
    'When a name already exists': { sub: '' },
    'Reconnect automatically': { sub: 'Try a lost computer again and resume its transfers' },
    'Move deleted items to Trash': { sub: 'Off makes a delete permanent' },
    'Keep items in Trash for': { sub: 'Asked when you open Trash' },
    'Ask where to save downloads': { label: 'Ask where to save each download' },
    'Pairing code lifetime': { sub: 'A code also works only once' },
    'Computers you trust': { btn: 'Show…' },
    'Sign out all phones': { btn: 'Sign out all phones…' },
    'Close to the system tray': { sub: 'Transfers go on while the window is hidden', when: /^Closing the window keeps/ },
    'Test notification': { btn: 'Test notification' },
    'Computer log': { btn: 'Show…' },
    'This computer’s identity': { btn: 'New key…' },
    'Welcome tour': { btn: 'Show the welcome screen' }
  };
  /* The order of the rows of Files in the mockup; rows it does not know stay after these. */
  const FILES_ORDER = ['Move deleted items to Trash', 'Keep items in Trash for', 'Ask where to save each download', 'Show hidden files'];
  /* Lays the rows of one section out as the mockup has them: its small lines and button words, Theme in the mockup's order, the order of Files,
     and the update check as its own row (the old page keeps it next to Details). */
  function mockupRows(body, name) {
    body.querySelectorAll('.k-set').forEach((r) => {
      const l = r.firstElementChild, b = l && l.querySelector('b'), o = b && ROW_TEXT[b.textContent.trim()]; if (!o) return;
      let sm = l.querySelector(':scope > small');
      if (o.label !== undefined) b.textContent = o.label;
      if (o.sub !== undefined && (!o.when || (sm && o.when.test(sm.textContent)))) {
        if (!o.sub) { if (sm) sm.remove(); } else { if (!sm) { sm = document.createElement('small'); l.appendChild(sm); } sm.textContent = o.sub; }
      }
      if (o.btn) { const x = r.querySelector('button.k-btn'); if (x) { x.textContent = o.btn; x.removeAttribute('aria-label'); if (o.ghost) x.classList.add('ghost'); } }
    });
    const th = body.querySelector('[data-set="theme"]');
    if (th) ['system', 'light', 'dark'].forEach((v) => { const x = th.querySelector('[data-v="' + v + '"]'); if (x) th.appendChild(x); });
    if (name === 'Files') {
      const rank = (r) => { const b = r.querySelector('b'), i = b ? FILES_ORDER.indexOf(b.textContent.trim()) : -1; return i < 0 ? 99 : i; };
      [...body.querySelectorAll('.k-set')].sort((a, c) => rank(a) - rank(c)).forEach((r) => body.appendChild(r));
    }
    if (name === 'About') {
      const ver = [...body.querySelectorAll('.k-set')].find((r) => { const b = r.querySelector('b'); return b && b.textContent.startsWith(A.APP_NAME); }), upd = ver && ver.querySelector('button[data-sx="update"]');
      if (upd) {
        const det = ver.querySelector('button[data-sx="about"]'); if (det) det.classList.add('ghost');
        upd.textContent = 'Check now';
        const row = document.createElement('div'); row.className = 'k-set';
        row.innerHTML = '<div><b>Check for updates</b><small>Asks GitHub’s public list of releases; the app never installs anything</small></div>';
        const right = document.createElement('div'); right.appendChild(upd); row.appendChild(right);
        ver.after(row);
        /* the update channel sits in the same row (the generic [data-set] handler stores S.channel, as the update dialog's own buttons do) */
        const ch = S.channel === 'beta' ? 'beta' : 'stable', seg = document.createElement('span');
        seg.className = 'k-seg'; seg.setAttribute('role', 'group'); seg.setAttribute('aria-label', 'Update channel'); seg.setAttribute('data-set', 'channel');
        seg.innerHTML = [['stable', 'Stable'], ['beta', 'Beta']].map((o) => '<button type="button" data-v="' + o[0] + '" class="' + (ch === o[0] ? 'on' : '') + '" aria-pressed="' + (ch === o[0]) + '">' + o[1] + '</button>').join('');
        right.insertBefore(seg, upd);
        /* Check automatically (S.autoUpdate, read by the daily check in fx6.js) and Report a problem (the existing report dialog) */
        const auto = document.createElement('div'); auto.className = 'k-set';
        auto.innerHTML = '<div><b>Check automatically</b><small>Once a day while the app runs. Nothing is installed without you.</small></div><div><button type="button" class="k-sw' + (S.autoUpdate ? ' on' : '') + '" data-sw="autoUpdate" role="switch" aria-checked="' + !!S.autoUpdate + '" aria-label="Check automatically"></button></div>';
        row.after(auto);
        if (A.reportDialog) {
          const rep = document.createElement('div'); rep.className = 'k-set';
          rep.innerHTML = '<div><b>Report a problem</b></div><div><button type="button" class="k-btn sm" data-fx="v5.report">Report a problem</button></div>';
          auto.after(rep);
        }
      }
      /* the welcome row ends the section, as in the mockup */
      const wel = [...body.querySelectorAll('.k-set')].find((r) => { const b = r.querySelector('b'); return b && b.textContent.trim() === 'Welcome tour'; });
      if (wel) body.appendChild(wel);
    }
  }
  /* Report a problem: opens the existing report dialog (the one About details offers too). */
  H['v5.report'] = () => { if (A.reportDialog) A.reportDialog(); };
  /* The language row is one button that shows the current language and opens the app's own menu with every language in it (the menu scrolls if contributors add many). */
  function langSelect(root) {
    const g = root.querySelector('[data-set="lang"]'); if (!g) return;
    const L = [...g.querySelectorAll('button')].map((b) => ({ v: b.dataset.v, name: b.textContent, on: b.classList.contains('on') }));
    const cur = L.find((l) => l.on) || L[0]; if (!cur) return;
    const b = document.createElement('button'); b.type = 'button'; b.className = 'k-btn k-lang'; b.setAttribute('aria-label', 'Language'); b.setAttribute('aria-haspopup', 'menu'); b.setAttribute('data-v5lang', '1');
    b._langs = L; b.innerHTML = '<span lang="' + esc(cur.v) + '">' + esc(cur.name) + '</span>';
    g.replaceWith(b);
  }
  document.addEventListener('click', (e) => {
    const b = e.target && e.target.closest && e.target.closest('button[data-v5lang]'); if (!b || !b._langs) return;
    const r = b.getBoundingClientRect();
    const m = A.menu(r.right, r.bottom + 6, b._langs.map((l) => ({ icon: l.on ? 'check' : '', label: l.name, onClick: () => { S.lang = l.v; A.save(); if (A.applyLang) A.applyLang(); if (st.view === 'settings') A.pageSettings(); } })), { right: true, cls: 'k-langmenu' });
    m.style.minWidth = Math.max(r.width, 176) + 'px'; m.style.left = Math.max(8, Math.min(r.right - m.getBoundingClientRect().width, innerWidth - m.getBoundingClientRect().width - 8)) + 'px'; /* the menu was placed before it got its width: put its right edge under the button's */
  });

  /* Re-lays out the page the old code just drew: header, section list at the left, the open section at the right. */
  function skinSettings() {
    const s = stage(), nav = s.querySelector('.setn'), pane = s.querySelector('.setp'); if (!nav || !pane) return;
    const h3 = pane.querySelector('h3'), name = h3 ? h3.textContent : '';
    const navHtml = [...nav.querySelectorAll('button[data-snav]')].map((b) => { const n = b.dataset.snav, on = b.classList.contains('on'); return '<button type="button" class="' + (on ? 'on' : '') + '" data-snav="' + esc(n) + '"' + (on ? ' aria-current="true"' : '') + '>' + ic(SECTION_ICON[n] || 'gear', { size: 16 }) + esc(n) + '</button>'; }).join('');
    const kids = [...pane.children].filter((c) => c !== h3);
    kids.forEach((r) => {
      if (!r.classList.contains('sr')) return;
      const l = [...r.children].find((c) => c.classList.contains('l')), rest = [...r.children].filter((c) => c !== l);
      const right = document.createElement('div'); rest.forEach((c) => right.appendChild(c));
      if (l) l.removeAttribute('class');
      r.className = 'k-set'; r.appendChild(right);
    });
    const body = document.createElement('div'); body.className = 'k-sbody';
    body.innerHTML = '<h2>' + esc(name) + '</h2>';
    if (name === 'Help') { const k = document.createElement('div'); k.className = 'k-keys'; k.innerHTML = KEYS().map((x) => '<div><span>' + kbds(x[0]) + '</span>' + esc(x[1]) + '</div>').join(''); body.appendChild(k); }
    else kids.forEach((r) => body.appendChild(r));
    const sameTab = s._setTab === name, keep = sameTab ? s.scrollTop : 0;
    s.className = 'stage page';
    s.innerHTML = '<div class="v5pg">' + head('Settings') + '<div class="k-settings"><nav class="k-snav" aria-label="Settings sections">' + navHtml + '</nav></div></div>';
    s.querySelector('.k-settings').appendChild(body);
    s._setTab = name; skinControls(body); langSelect(body); mockupRows(body, name);
    s.scrollTop = keep;
  }
  /* The control that has the focus, as a selector that finds the same control after the page is drawn again. */
  const focusSel = () => {
    const a = document.activeElement; if (!a || !a.dataset || !stage().contains(a)) return '';
    const g = a.closest && a.closest('[data-set]');
    return a.dataset.sw ? '[data-sw="' + a.dataset.sw + '"]' : a.dataset.sx ? '[data-sx="' + a.dataset.sx + '"]' : a.dataset.snav ? '[data-snav="' + a.dataset.snav + '"]' : g && a.dataset.v !== undefined ? '[data-set="' + g.dataset.set + '"] [data-v="' + a.dataset.v + '"]' : a.getAttribute('data-v5lang') ? 'select[data-v5lang]' : '';
  };
  /* ================= Settings > Security > Account server (rfe-7np phase 3) =================
   A computer can take the account-server role (it signs short-lived sign-in assertions with its own key) and can trust one other
   computer's account server. The pin is the owner's decision: the key is looked up without trusting it, its fingerprint (worked out
   from the key by the app core) is shown to be compared with the other computer's screen, and only a ticked comparison enables
   the trust button. A session that an account server vouched for may not change the pin; the agent says 403 and the app core words it. */
  const AS_MSG = (e) => (e && e.message) || String(e);
  const asAlert = (m) => (m ? '<p class="v5err" role="alert">' + esc(m) + '</p>' : '');
  const asMono = (v) => '<code class="mono" data-nolocal style="word-break:break-all">' + esc(v) + '</code>';
  const asRow = (label, hint, ctl) => '<div class="kv2"><div><b>' + esc(label) + '</b>' + (hint ? '<small>' + esc(hint) + '</small>' : '') + '</div><div>' + ctl + '</div></div>';
  T.accountServer = () => {
    const srv = A.pairServer() || E.servers[0];
    if (!srv) { A.snack('No computer is connected'); return; }
    const s = { st: null, loading: true, err: '', busy: false, key: null, lerr: '', ok: false };
    let gen = 0, ctl = null;
    const el = () => ctl && ctl.el;
    const region = (id) => { const e = el(); return e ? e.querySelector('#' + id) : null; };
    const drawStatus = () => {
      const r = region('acs-st'); if (!r) return;
      const t = s.st;
      if (!t) { r.innerHTML = s.loading ? '<p class="fxp">Reading the settings…</p>' : asAlert(s.err); return; }
      const on = !!t.enabled, ts = t.trustedServer, off = t.offered;
      r.innerHTML =
        asRow('Account server on this computer', 'Lets accounts kept on this computer sign in to the computers that trust it', '<span>' + (on ? 'On' : 'Off') + '</span> <button type="button" class="btn sm" data-acs="toggle"' + (s.busy ? ' disabled' : '') + '>' + (on ? 'Turn off' : 'Turn on') + '</button>') +
        (on ? asRow('Name', '', asMono((off && off.name) || t.name || '')) + asRow('Key fingerprint', '', asMono(t.keyFingerprint || (off && off.keyFingerprint) || '')) + '<p class="fxsub">Other computers compare this fingerprint before they trust this account server.</p>' : '') +
        '<h3 class="dsh">Account server this computer trusts</h3>' +
        (ts ? asRow('Name', '', asMono(ts.name || '')) + asRow('Key fingerprint', '', asMono(ts.keyFingerprint || '')) + '<p class="fxsub">Accounts of that server can sign in to this computer with full owner access.</p><div class="df2"><button type="button" class="btn e sm" data-acs="untrust"' + (s.busy ? ' disabled' : '') + '>Stop trusting</button></div>' : '<p class="fxp">No account server is trusted by this computer.</p>') +
        asAlert(s.err);
    };
    const drawKey = () => {
      const r = region('acs-key'); if (!r) return;
      const k = s.key;
      r.innerHTML = (k ? asRow('Name', '', asMono(k.name)) + asRow('Key fingerprint', '', asMono(k.keyFingerprint)) +
        (s.st && s.st.trustedServer ? '<p class="fxsub">Trusting this one replaces the account server this computer trusts now, and everyone that one signed in is signed out of this computer.</p>' : '') +
        '<p class="fxsub">On that computer, open Settings, Security, Account server (or run rfe accounts status) and compare its fingerprint with the one above, character by character.</p>' +
        '<label class="all"><input type="checkbox" id="acs-ok"' + (s.ok ? ' checked' : '') + '> The fingerprint on that computer is exactly the same</label>' +
        '<div class="df2"><button type="button" class="btn f sm" data-acs="trust"' + (s.ok && !s.busy ? '' : ' disabled') + '>Trust this account server</button></div>' : '') + asAlert(s.lerr);
    };
    const draw = () => { drawStatus(); drawKey(); };
    /* One change of this computer's settings; the agent's answer is the new state. */
    const change = async (cmd, args, done, where) => {
      if (s.busy) return;
      s.busy = true; s.err = ''; s.lerr = ''; draw();
      let good = false;
      try { s.st = await E.call(cmd, Object.assign({ host: srv.id }, args)); s.loading = false; good = true; }
      catch (e) { if (where === 'key') s.lerr = AS_MSG(e); else s.err = AS_MSG(e); }
      s.busy = false;
      if (good) { if (cmd === 'account_server_trust') { s.key = null; s.ok = false; } if (done) A.snack(done); }
      draw();
    };
    const lookup = async () => {
      const a = region('acs-addr') || (el() && el().querySelector('#acs-addr')); const addr = a ? a.value.trim() : '';
      const g = ++gen; s.key = null; s.ok = false; s.lerr = '';
      if (!addr) { s.lerr = 'Enter the address of the computer you want to trust, as host:port.'; drawKey(); return; }
      let k = null, bad = '';
      try { k = await E.call('account_server_key', { hostPort: addr }); } catch (e) { bad = AS_MSG(e); }
      if (g !== gen) return; /* the address was edited, or looked up again, while this one was on its way */
      s.key = k; s.lerr = bad; drawKey();
    };
    /* Pinning a key needs the owner's account password again. It is typed into this prompt, goes with the one trust call and is cleared from the box at once; nothing keeps or logs it. */
    const askPassword = (k) => {
      A.dialog({ icon: 'shield', title: 'Trust this account server', width: 440, modal: true, enter: 'go',
        body: '<p class="fxp" style="margin-top:0">Trusting an account server lets its accounts sign in here with full owner access. Enter the password of the account you are signed in with to confirm.</p><div class="fld"><label for="acs-pw">Account password</label><input id="acs-pw" type="password" autocomplete="current-password" spellcheck="false"><span class="err" id="acs-pwe"></span></div><div class="fxsub">The password goes only to this computer and is not saved.</div>',
        onOpen: (c) => { const i = c.el.querySelector('#acs-pw'); if (i) i.focus(); },
        actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Trust this account server', kind: 'f', icon: 'shield-check', cb: (c) => {
          const i = c.el.querySelector('#acs-pw'), pw = i ? i.value : '';
          if (!pw) { const e = c.el.querySelector('#acs-pwe'); if (e) e.textContent = 'Enter your password'; if (i) i.focus(); return false; }
          i.value = '';
          change('account_server_trust', { name: k.name, publicKey: k.publicKey, keyFingerprint: k.keyFingerprint, password: pw }, 'Now trusting that account server', 'key');
        } }] });
    };
    const others = E.servers.filter((x) => x.id !== srv.id && x.addr);
    ctl = A.dialog({ icon: 'shield', title: 'Account server', width: 560, actions: [{ label: 'Close', kind: 'tx' }],
      body: '<p class="fxsub" style="margin-top:0">' + asMono(srv.name) + '</p><div id="acs-st"></div>' +
        '<h3 class="dsh">Trust an account server</h3><p class="fxsub">Look up its key, then compare the fingerprint with the one shown on that computer. Trusting it lets its accounts sign in to this computer with full owner access.</p>' +
        (others.length ? '<div class="fld"><label for="acs-pick">Computers you know</label><select id="acs-pick"><option value="">Choose a computer</option>' + others.map((x) => '<option value="' + esc(x.id) + '">' + esc(x.name) + '</option>').join('') + '</select></div>' : '') +
        '<div class="fld"><label for="acs-addr">Address of that computer (host:port)</label><input id="acs-addr" placeholder="192.168.1.20:8765" spellcheck="false" autocomplete="off"></div>' +
        '<div class="df2"><button type="button" class="btn sm" data-acs="lookup">Look up its key</button></div><div id="acs-key"></div>',
      onOpen: (c) => {
        ctl = c; const d = c.el;
        const stale = () => { gen++; if (s.key || s.lerr || s.ok) { s.key = null; s.lerr = ''; s.ok = false; drawKey(); } };
        d.addEventListener('input', (e) => { if (e.target && e.target.id === 'acs-addr') stale(); });
        d.addEventListener('change', (e) => {
          const t = e.target; if (!t) return;
          if (t.id === 'acs-pick') { const x = others.find((o) => o.id === t.value); d.querySelector('#acs-addr').value = x ? x.addr : ''; stale(); }
          else if (t.id === 'acs-ok') { s.ok = !!t.checked; const b = d.querySelector('[data-acs="trust"]'); if (b) b.disabled = !s.ok || s.busy; }
        });
        d.addEventListener('click', (e) => {
          const b = e.target.closest && e.target.closest('[data-acs]'); if (!b || b.disabled) return;
          const a = b.dataset.acs;
          if (a === 'lookup') lookup();
          else if (a === 'toggle') { const on = !(s.st && s.st.enabled); change('account_server_enable', { enabled: on }, on ? 'Account server turned on' : 'Account server turned off'); }
          else if (a === 'trust') { const k = s.key; if (k && s.ok) askPassword(k); }
          else if (a === 'untrust') {
            A.dialog({ icon: 'shield', cls: 'danger', title: 'Stop trusting this account server?', width: 440, enter: 'go',
              body: '<p class="fxp" style="margin-top:0">Its accounts can no longer sign in to this computer, and everyone it signed in is signed out of this computer. You can trust it again later by comparing its fingerprint.</p>',
              actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Stop trusting', kind: 'ed', cb: () => { change('account_server_untrust', {}, 'No longer trusting an account server'); } }] });
          }
        });
        draw();
        E.call('account_server_status', { host: srv.id }).then((st) => { s.st = st; s.loading = false; s.err = ''; draw(); }, (e) => { s.loading = false; s.err = AS_MSG(e); draw(); });
      } });
  };
  const asExtra0 = A.settingsExtra;
  A.settingsExtra = (k) => {
    const h = asExtra0(k);
    return h.replace('<div class="sg"><h3>Security</h3>', () => '<div class="sg"><h3>Security</h3>' + k.row('Account server', 'Choose whose accounts can sign in to this computer', '<button class="btn" data-sx="acctserver">Manage…</button>'));
  };
  const asAction0 = A.settingsAction;
  A.settingsAction = (a) => { if (a === 'acctserver') { T.accountServer(); return true; } return asAction0(a); };

  const pageSettings0 = A.pageSettings;
  A.pageSettings = () => {
    if (!T.on()) { pageSettings0(); return; }
    const sel = focusSel();
    pageSettings0(); skinSettings();
    if (A.fixSw) A.fixSw();
    if (sel) { const n = stage().querySelector(sel); if (n) n.focus({ preventScroll: true }); }
  };
});
