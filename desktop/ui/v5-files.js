/* RFE v5 - Files as ONE tree across the computers (the v5 mockup's Files page), drawn over the existing file logic.
   Nothing here lists, moves or deletes a file by itself: the tree keeps its selection in the existing main Pane
   (A.mainPane: host, folder, selected names) so the details panel (renderDetail), downloads, delete, preview,
   favorites, the context menu and the transfer engine all run unchanged. The tree only draws the folders the
   engine has already listed (E.fs) and asks it for more when a folder is opened.
   It is used only while the window wears the v5 wrapper (class "d5" on #app); otherwise the old Files page stays. */
rfeModule('v5-files', ['features', 'pages'], function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, $ = A.$, esc = A.esc, ic = A.ic, st = A.state, R = window.RFE;
  const T = (A.v5Files = { open: new Set(), ren: null, mk: null, rows: [], seeded: false });
  const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  T.on = A.v5on; /* true while the v5 wrapper is on the window (v5-shell.js) */
  const P = () => A.mainPane;
  const key = (h, p) => h + '\u0001' + p;
  const isOpen = (h, p) => T.open.has(key(h, p));
  const kb = (g) => (g >= 1000 ? +(g / 1000).toFixed(1) + ' TB' : Math.round(g) + ' GB');
  /* "1 Nov 09:12" (the year is added only when it is not this year). */
  const when = (ms) => { if (!ms) return ''; const d = new Date(ms); const two = (n) => String(n).padStart(2, '0'); return d.getDate() + ' ' + MON[d.getMonth()] + (d.getFullYear() !== new Date(E.now()).getFullYear() ? ' ' + d.getFullYear() : ' ' + two(d.getHours()) + ':' + two(d.getMinutes())); };
  const dot = (k) => '<i class="k-dot ' + k + '"></i>';
  /* The mockup marks things (file types, computers, places) with its small colour pictures; they sit in ui/emoji next to this script. */
  const em = A.v5em; /* a mockup colour picture, n pixels wide (v5-shell.js) */
  const EM_KIND = { dir: 'file_folder', image: 'framed_picture', video: 'film_frames', archive: 'card_file_box', pkg: 'package', text: 'memo', doc: 'page_facing_up', code: 'scroll', key: 'key', exec: 'gear', file: 'page_facing_up' };
  /* The picture for a file of kind `kind` (the app's own kind words) and name `name`: spreadsheets and music get theirs from the extension, as the mockup does. */
  const emKind = (kind, name, n) => em(kind === 'dir' ? 'file_folder' : /\.(csv|tsv|xls|xlsx|ods)$/i.test(name) ? 'bar_chart' : /\.(mp3|flac|wav|ogg|m4a|aac|opus)$/i.test(name) ? 'musical_notes' : EM_KIND[kind] || 'page_facing_up', n);
  const fileEm = (it, n) => emKind(R.kindOf(it), it.n || '', n);
  /* Shared with the details panel code (pages.js), which draws the big preview tile in these pictures itself. */
  T.em = em; T.emKind = emKind;
  /* Newest first, folders before files: the order of the mockup's folder (and the Pane's sort, so Shift-click ranges follow what is drawn). */
  const ORDER = { key: 'mod', dir: -1 };
  const ib =(a, icon, label, extra) => '<button type="button" class="k-ib" tabindex="-1" data-a="' + a + '" aria-label="' + esc(label) + '" data-tip="' + esc(label) + '"' + (extra || '') + '>' + ic(icon, { size: 16 }) + '</button>';
  /* The rows of one folder in the order the computer listed them, folders first (as the mockup draws them); hidden files are filtered by the engine's view. */
  const items = (h, dir) => {
    const l = E.fs.list(h, dir); if (!l) return null;
    const at = new Map(l.map((n, i) => [n, i]));
    return E.fs.view(l, { key: ORDER.key, dir: ORDER.dir, filter: 'all', q: '' }).sort((a, b) => (a.t === 'dir') !== (b.t === 'dir') ? (a.t === 'dir' ? -1 : 1) : at.get(a) - at.get(b));
  };

  /* The servers that show in the tree plus "This computer" (only once a computer exists: with none, the page shows its empty state). */
  const hosts = () => (E.servers.length ? E.servers.map((s) => s.id).concat(['local']) : []);
  const mine = (n, h) => n && n.host === h;

  /* The folders a computer shows first: the places the agent lets this login start from (or its start folder). */
  function rootsOf(h) {
    const s = h === 'local' ? null : E.server(h);
    if (s && s.virtualRoot) { const l = E.fs.list(h, s.virtualRoot); return l ? l.map((n) => Object.assign({ root: true }, n)) : null; }
    const p = E.start(h); if (!p) return []; return [{ n: p, t: 'dir', path: p, root: true, perm: '', total: 0, free: 0, cc: null, mod: 0 }];
  }
  const nodePath = (dir, n) => n.path || U.join(dir, n.n);
  const rootIcon = (p) => (/^(\/home\/|\/Users\/|[A-Za-z]:\\Users\\)/.test(p) ? 'house' : 'computer_disk');

  /* Counts for the special entries: shown only when the Tools pages already loaded them (nothing is invented). */
  function specialMeta(kind, h) {
    const X = A.X || {};
    if (kind === 'trash') return X.trashAt && X.trash ? (X.trash.filter((r) => mine(r, h)).length + ' items') : '';
    if (kind === 'share') return X.sharesAt && X.shares ? (X.shares.filter((r) => mine(r, h)).length + ' live') : '';
    const a = X.apps && X.apps[h]; return a && a.cat && a.cat.apps ? String(a.cat.apps.length) : '';
  }

  /* ---- Trash, Share links and Apps: rows that open in place like folders and list the entries the Tools pages already load. ---- */
  const SPK = '*'; /* the open-state key of a special row is key(host, '*trash') etc.: no folder path starts with '*' */
  const SPECIALS = [['trash', 'Trash', 'trash', 'wastebasket'], ['share', 'Share links', 'link', 'link'], ['apps', 'Apps', 'grid', 'toolbox']];
  const spOpen = (h, kind) => isOpen(h, SPK + kind);
  /* The entry of the special rows that is shown in the details panel ({h, kind, id}; id '' = the group itself). It counts only while nothing else is selected in the pane. */
  const spCur = () => { if (T.sp && P().sel.size > 0) T.sp = null; return T.sp || null; };
  const spIs = (h, kind, id) => { const c = spCur(); return !!c && c.h === h && c.kind === kind && c.id === id; };
  /* The entries of one special group for computer `h` as plain objects (id, name, icon markup, quiet meta, extra facts); null while the Tools data is not loaded yet, {err} when it failed. */
  function spEntries(kind, h) {
    const X = A.X || {};
    if (kind === 'trash') return X.trashAt ? X.trash.filter((r) => mine(r, h)).map((r) => ({ id: r.host + '~' + r.id, name: r.name, fi: fileEm({ n: r.name, t: r.isDir ? 'dir' : 'file' }, 18), meta: [r.isDir ? '' : U.fmtBytes(r.b), when(r.at)], facts: [['Was in', r.path, 'mono'], ['Deleted', when(r.at)]] })) : null;
    if (kind === 'share') return X.sharesAt ? X.shares.filter((r) => mine(r, h)).map((r) => ({ id: r.hash, name: r.name, fi: em('link', 18), meta: [r.exp ? 'until ' + when(r.exp) : ''], facts: [['Shares', r.path, 'mono'], ['Valid until', when(r.exp)]] })) : null;
    const a = X.apps && X.apps[h]; if (a && a.err) return { err: a.err };
    return a && a.cat ? a.cat.apps.map((x) => ({ id: x.id, name: x.name, fi: em('toolbox', 18), meta: [x.category || ''], facts: [['Kind', x.category || 'App'], ['About', x.description || '']] })) : null;
  }
  /* Asks the existing Tools loaders for a group's data when it is missing or older than half a minute; the tree redraws when the answer arrives. */
  function spLoad(kind, h) {
    const X = A.X; if (!X) return; const old = (at) => !at || E.now() - at > A.T.stale; const done = () => later();
    if (kind === 'trash' && old(X.trashAt) && A.loadTrash) Promise.resolve(A.loadTrash()).then(done, done);
    else if (kind === 'share' && old(X.sharesAt) && A.loadShares) Promise.resolve(A.loadShares()).then(done, done);
    else if (kind === 'apps' && A.loadApps) Promise.resolve(A.loadApps(h)).then(done, done);
  }
  /* Opens the Tools page on the tab of a special group (restore, empty and remove live there). */
  function goTools(h, kind) { const X = A.X; X.tool = kind; if (kind === 'apps') X.appHost = h; A.go('tools'); }
  /* Makes a special row (or one of its entries) the thing the details panel shows: the pane's selection is cleared and the panel redrawn. */
  function spSelect(h, kind, id) {
    const p = P(); T.sp = { h, kind, id };
    p.sel = new Set(); p.anchor = -1; p.refreshItems(); if (A.renderDetail) A.renderDetail(); T.paint();
  }
  /* The details panel for a selected special row or entry (null when none is): a plain tile, the name, one facts line, a few rows and a ghost "Manage" button that opens Tools. */
  T.spDetail = () => {
    const c = spCur(); if (!c) return null; const spec = SPECIALS.find((x) => x[0] === c.kind); const host = esc(A.hostName(c.h));
    const list = spEntries(c.kind, c.h); const all = Array.isArray(list) ? list : []; const it = c.id ? all.find((x) => x.id === c.id) : null;
    const facts = it ? it.meta.filter(Boolean).join(' · ') : list == null ? 'Loading…' : list.err ? 'Could not load this list' : all.length + (c.kind === 'trash' ? (all.length === 1 ? ' item' : ' items') : c.kind === 'share' ? ' live' : (all.length === 1 ? ' app' : ' apps'));
    const rows = [['On', host]].concat(it ? it.facts.filter((f) => f[1]).map((f) => [f[0], esc(f[1]), f[2]]) : []);
    const kv = '<dl class="k-kv">' + rows.map((r) => '<dt>' + r[0] + '</dt><dd' + (r[2] ? ' class="' + r[2] + '"' : '') + '>' + r[1] + '</dd>').join('') + '</dl>';
    return '<div class="dprev">' + em(spec[3], 56) + '</div><h3 data-nolocal>' + (it ? esc(it.name) : spec[1]) + '</h3><p class="k-sub">' + esc(facts) + '</p>' + kv +
      '<div class="k-dact"><div class="k-row"><button type="button" class="k-btn ghost sm" data-do="manage" title="Restore, empty and remove happen in Tools">' + ic('sliders', { size: 16 }) + '<span class="bl">Manage</span></button></div></div>';
  };

  /* One computer's rows, depth-first. `out` collects plain objects that draw() turns into markup. */
  function buildHost(h, out) {
    const s = h === 'local' ? null : E.server(h); const st0 = s ? s.state : 'online';
    out.push({ k: 'host', h, d: 0, s, state: st0 });
    if (st0 !== 'online' || !isOpen(h, '')) return;
    const rs = rootsOf(h);
    if (!rs) { out.push({ k: 'note', d: 1, txt: 'Loading…' }); } else if (!rs.length) { out.push({ k: 'note', d: 1, txt: 'No folders to show on this computer yet' }); } else rs.forEach((r) => { const p = nodePath('/', r); out.push({ k: 'dir', h, dir: s && s.virtualRoot ? s.virtualRoot : U.parentOf(p), n: r, p, d: 1, root: true }); if (isOpen(h, p)) kids(h, p, 2, out); });
    if (h !== 'local') SPECIALS.forEach((x) => { const o = spOpen(h, x[0]); out.push({ k: 'special', h, kind: x[0], label: x[1], icon: x[2], em: x[3], d: 1, o }); if (o) spKids(h, x[0], out); });
  }
  /* The child rows of an open special row: its entries, or a short note while they load, fail to load or do not exist. */
  function spKids(h, kind, out) {
    const l = spEntries(kind, h);
    if (!l) { out.push({ k: 'note', d: 2, txt: 'Loading…' }); return; }
    if (l.err) { out.push({ k: 'note', d: 2, txt: 'Could not load this list: ' + l.err }); return; }
    if (!l.length) { out.push({ k: 'note', d: 2, txt: kind === 'trash' ? 'Trash is empty' : kind === 'share' ? 'No share links' : 'No apps' }); return; }
    l.slice(0, A.ROW_CAP).forEach((e) => out.push({ k: 'sitem', h, kind, d: 2, e }));
    if (l.length > A.ROW_CAP) out.push({ k: 'note', d: 2, txt: 'and ' + (l.length - A.ROW_CAP) + ' more in Tools' });
  }
  /* The children of an open folder (or a note while they load / when they cannot be read). */
  function kids(h, p, d, out) {
    if (T.mk && T.mk.h === h && T.mk.dir === p) out.push({ k: 'mk', d });
    const l = items(h, p);
    if (!l) { out.push({ k: 'note', d, txt: E.fs.state(h, p) === 'error' ? 'Cannot open this folder: ' + ((E.fs.error(h, p) || {}).message || 'it did not answer') : 'Loading…' }); return; }
    if (!l.length && !(T.mk && T.mk.h === h && T.mk.dir === p)) out.push({ k: 'note', d, txt: 'This folder is empty' });
    l.forEach((n) => { const cp = nodePath(p, n); out.push({ k: n.t === 'dir' ? 'dir' : 'file', h, dir: p, n, p: cp, d }); if (n.t === 'dir' && isOpen(h, cp)) kids(h, cp, d + 1, out); });
  }

  /* The markup of one row (the mockup's .n row: chevron, icon, name, quiet meta, hover actions). */
  function draw(r, i) {
    const pad = ' style="padding-left:' + (0.5 + r.d * 1.5) + 'rem"'; const P0 = P();
    if (r.k === 'note') return '<div class="n note" role="none"' + pad + '><span class="l">' + esc(r.txt) + '</span></div>';
    if (r.k === 'mk') return '<div class="n" role="none"' + pad + '><span class="l"><span class="tw"></span><span class="k-fic k-fic-dir">' + em('file_folder', 18) + '</span><input class="rn" data-mk aria-label="New folder name" placeholder="New folder" spellcheck="false"></span><span class="m"><span class="err" data-err></span></span></div>';
    const id = 'v5r' + i; const tw = (open) => '<span class="tw">' + ic(open ? 'chevron-down' : 'chevron-right', { size: 14 }) + '</span>';
    if (r.k === 'host') {
      const s = r.s, h = r.h; const local = h === 'local'; const name = local ? 'This computer' : s.name; const online = local || s.state === 'online';
      const o = !!isOpen(h, '') && online; const os = local ? '' : (s.os || '').split(' ')[0]; const via = local ? '' : s.via === 'LAN' ? 'home network' : (s.via || '');
      const sub = online ? [os, via].filter(Boolean).join(' · ') : s.state === 'trust' || s.state === 'refused' ? 'looks different from last time' : s.state === 'connecting' ? 'connecting…' : s.state === 'disconnected' ? 'not connected' : 'offline' + (s.lastSeen ? ' · last seen ' + when(s.lastSeen) : '');
      const dk = online ? 'ok' : s.state === 'trust' || s.state === 'refused' ? 'bad' : s.state === 'connecting' ? 'warn' : 'off';
      const meta = online ? (!local && s.disk && s.disk[1] > 0 ? kb(s.disk[0]) + ' of ' + kb(s.disk[1]) : '') : s.state === 'trust' || s.state === 'refused' ? '<span class="bad">Waiting for your OK</span>' : '';
      const act = online ? '<span class="ia">' + (local ? '' : ib('upload', 'upload', 'Upload to ' + name)) + ib('more', 'more', 'More for ' + name) + '</span>'
        : s.state === 'trust' || s.state === 'refused' ? '<span class="ia on"><button type="button" class="k-btn sm" tabindex="-1" data-a="review">Check…</button></span>'
          : s.state === 'connecting' ? '' : '<span class="ia on"><button type="button" class="k-btn sm" tabindex="-1" data-a="connect">' + ic('plug', { size: 14 }) + (s.state === 'disconnected' ? 'Connect' : 'Try again') + '</button></span>';
      const sel = P0.host === h && P0.sel.size === 0 && online && !spCur();
      return '<div class="n pc' + (sel ? ' sel' : '') + '" id="' + id + '" role="treeitem" data-i="' + i + '" aria-level="1"' + (online ? ' aria-expanded="' + o + '"' : '') + ' aria-selected="' + !!sel + '"' + pad + '><span class="l">' + (online ? tw(o) : '<span class="tw"></span>') + dot(dk) + em(!local && /^(Windows|macOS|Darwin)/i.test(s.os || '') ? 'laptop' : 'desktop_computer', 18) + esc(name) + (sub ? ' <small>' + esc(sub) + '</small>' : '') + '</span><span class="m">' + meta + '</span>' + act + '</div>';
    }
    if (r.k === 'special') {
      const m = specialMeta(r.kind, r.h); const ss = spIs(r.h, r.kind, '');
      return '<div class="n' + (ss ? ' sel' : '') + '" id="' + id + '" role="treeitem" data-i="' + i + '" aria-level="2" aria-expanded="' + !!r.o + '" aria-selected="' + ss + '"' + pad + '><span class="l"><span class="tw">' + ic(r.o ? 'chevron-down' : 'chevron-right', { size: 14 }) + '</span>' + em(r.em, 16) + r.label + '</span><span class="m"><span>' + m + '</span></span>' + (r.kind === 'trash' ? '<span class="ia">' + ib('trash', 'trash', 'Open Trash') + '</span>' : '') + '</div>';
    }
    if (r.k === 'sitem') {
      const ss = spIs(r.h, r.kind, r.e.id);
      return '<div class="n' + (ss ? ' sel' : '') + '" id="' + id + '" role="treeitem" data-i="' + i + '" aria-level="3" aria-selected="' + ss + '"' + pad + '><span class="l"><span class="tw"></span><span class="k-fic">' + r.e.fi + '</span><span data-nolocal>' + esc(r.e.name) + '</span></span><span class="m">' + (r.e.meta.filter(Boolean).map((x) => '<span>' + esc(x) + '</span>').join('') || '<span></span>') + '</span></div>';
    }
    const n = r.n; const dir = r.k === 'dir'; const sel = P0.host === r.h && P0.path === r.dir && P0.sel.has(n.n); const o = dir && isOpen(r.h, r.p);
    const renaming = T.ren && T.ren.h === r.h && T.ren.dir === r.dir && T.ren.name === n.n;
    const nm = renaming ? '<input class="rn" data-rn value="' + esc(n.n) + '" aria-label="New name" spellcheck="false">' : r.root ? esc(r.p) : esc(n.n);
    const cnt = dir && !(n.cc == null && !n.kids) ? E.fs.itemsLabel(n) : '';
    const total = r.root && n.total > 0 ? kb(Math.max(0, n.total - (n.free || 0)) / 1e9) + ' used' : '';
    /* As in the mockup: the folders inside the folder the details follow say their item count beside the name and keep the date at the right; the folders above it (the way down to it) say the count at the right. */
    const inside = dir && !r.root && P0.host === r.h && P0.path === r.dir;
    const small = inside && cnt && !renaming ? '<small>' + esc(cnt) + '</small>' : '';
    const meta = inside ? '<span></span><span>' + (n.mod ? when(n.mod) : '') + '</span>' : dir ? '<span>' + esc(total || cnt) + '</span>' + (n.mod && !r.root ? '<span>' + when(n.mod) + '</span>' : '') : '<span>' + U.fmtBytes(n.b) + '</span><span>' + when(n.mod) + '</span>';
    const rem = r.h !== 'local';
    const ia = '<span class="ia">' + (rem ? ib('download', 'download', dir ? 'Download as a .zip' : 'Download (Ctrl D)') : '') + (rem && !dir ? ib('share', 'link', 'Share link…') : '') + (rem && dir && !r.root ? ib('upload', 'upload', 'Upload into ' + n.n) : '') + ib('more', 'more', 'More for ' + n.n) + '</span>';
    const icon = r.root ? em(rootIcon(r.p), 16) : '<span class="k-fic k-fic-' + (dir ? 'dir' : 'file') + '">' + fileEm(n, 18) + '</span>';
    return '<div class="n' + (sel ? ' sel' : '') + '" id="' + id + '" role="treeitem" data-i="' + i + '" aria-level="' + (r.d + 1) + '"' + (dir ? ' aria-expanded="' + o + '"' : '') + ' aria-selected="' + sel + '"' + pad + '><span class="l">' + (dir ? '<span class="tw" data-tw>' + ic(o ? 'chevron-down' : 'chevron-right', { size: 14 }) + '</span>' : '<span class="tw"></span>') + icon + nm + small + '</span><span class="m">' + meta + '</span>' + ia + '</div>';
  }

  /* The empty page (no computer added yet): what the tree will hold, and the way to add one. */
  function emptyHTML() {
    return '<div class="ol"><div class="tr emp"><h1 class="k-title">Files</h1><div class="b-empty">' + em('open_file_folder', 40) + '<b>Your computers’ files appear here as one tree</b><p>Each computer you add becomes a branch: its folders, and the files in them, with the actions on each row.</p><div class="k-row"><button type="button" class="k-btn pri" data-a="add">' + ic('plus', { size: 16 }) + '<span>Add a computer</span></button><button type="button" class="k-btn" data-a="find">' + ic('radar', { size: 16 }) + '<span>Find on network</span></button></div></div></div>' +
      '<aside class="olside" aria-label="Details"><h3>Set up a PC</h3><ol class="b-steps"><li>Install RFE on the PC and open it once.</li><li>Here, choose <b>Add a computer</b> and pick the PC.</li><li>Check that both screens show the same number, then click Allow on the PC.</li></ol></aside></div>';
  }

  /* Draws the tree into `#stage` (the whole page the first time, only the rows afterwards). */
  T.mount = (stage) => {
    T.stage = stage;
    if (!E.servers.length) { stage.innerHTML = emptyHTML(); T.tree = null; T.bindOnce(stage); return; }
    if (!T.seeded) { T.seeded = true; const m = P().host; const h = m && m !== 'local' && E.server(m) ? m : E.servers[0].id; T.open.add(key(h, '')); const f = rootsOf(h); if (f && f[0]) T.open.add(key(h, nodePath('/', f[0]))); T.seedHost = h; }
    stage.innerHTML = '<div class="ol"><div class="olmain"><div class="k-ftop" id="ftop"></div><div class="tr" role="tree" aria-label="Files on your computers" tabindex="0"></div></div><aside class="olside" aria-label="Details"><div class="detail" id="detail"></div></aside></div>';
    T.tree = $('.tr', stage); T.bindOnce(stage); P().sort = Object.assign({}, ORDER);
    /* rows slide under a still pointer while the list scrolls: no hover fades until it has stopped for a moment */
    T.tree.addEventListener('scroll', () => { T.tree.classList.add('scrolling'); clearTimeout(T.scEnd); const tr = T.tree; T.scEnd = setTimeout(() => tr.classList.remove('scrolling'), A.T.scrollEnd); }); T.paint(); if (A.renderDetail) A.renderDetail();
  };
  /* Redraws the rows, keeping the scroll place and the keyboard focus. */
  T.paint = () => {
    const tr = T.tree; if (!tr || !tr.isConnected) return; const sc = tr.scrollTop; const had = document.activeElement === tr;
    if (!T.seeded && E.servers.length) return;
    const out = []; hosts().forEach((h) => buildHost(h, out)); out.push({ k: 'add', d: 0 }); T.rows = out;
    tr.innerHTML = out.map((r, i) => r.k === 'add' ? '<div class="n pc add" role="none" data-a="add" style="padding-left:0.5rem;color:var(--t2);cursor:pointer"><span class="l"><span class="tw"></span>' + ic('plus', { size: 14 }) + 'Add a computer</span></div>' : draw(r, i)).join('');
    tr.scrollTop = sc; const sel = $('.n.sel', tr); tr.setAttribute('aria-activedescendant', sel ? sel.id : ''); if (had) tr.focus({ preventScroll: true });
    const inp = $('[data-mk],[data-rn]', tr); if (inp && document.activeElement !== inp) { inp.focus(); inp.select && inp.select(); }
    T.paintTop();
  };
  let queued = 0;
  const later = () => { if (queued || !T.on() || st.view !== 'files') return; queued = requestAnimationFrame(() => { queued = 0; T.paint(); if (spCur() && A.renderDetail) A.renderDetail(); }); };
  T.later = later;
  T.hist = []; T.fwd = []; /* places the page was in before (Back) and after (Forward) the current one: { h, dir, names } */

  /* Makes `names` (in folder `dir` of computer `h`) the selection of the main Pane, which feeds the details panel and every action. */
  function select(h, dir, names) {
    const p = P(); const prev = { h: p.host, dir: p.path, names: [...p.sel] }; const moved = p.host !== h || p.path !== dir; T.sp = null;
    /* a real move to another folder is remembered for the Back and Forward buttons (a step taken by those buttons is not) */
    if (moved && !T.walking && prev.h) { T.hist.push(prev); if (T.hist.length > 50) T.hist.shift(); T.fwd.length = 0; }
    if (p.host !== h) { p.host = h; p.hist = []; p.fwd = []; }
    p.sort = Object.assign({}, ORDER); p.paths[h] = dir; p.sel = new Set(names); p.anchor = -1; p.q = ''; p.filter = 'all'; p.renaming = null; p.creating = false; p.refreshItems();
    if (moved && p.o.onNav) p.o.onNav(p); else if (p.o.onSelect) p.o.onSelect(p);
    T.paint();
  }
  /* The folder the page is "in": the one folder that is selected, otherwise the folder the selection sits in. */
  function curDir() { const p = P(); const one = p.sel.size === 1 ? p.selected()[0] : null; return one && one.t === 'dir' ? nodePath(p.path, one) : p.path; }
  /* Goes to folder `tp` of computer `h`: its ancestors open in the tree and the folder becomes the selected row (a drive or filesystem root has no row of its own: its computer is selected). */
  function goDir(h, tp) {
    T.open.add(key(h, '')); for (let i = 0; i <= U.segs(tp).length; i++) T.open.add(key(h, U.upTo(tp, i))); /* from 0: a computer whose row is the drive or "/" itself opens too */
    if (U.isRoot(tp)) select(h, tp, []); else select(h, U.parentOf(tp), [U.baseOf(tp)]);
  }
  /* The place a computer's own row leads to (the same one a click on that row opens). */
  const homeOf = (h) => (h === 'local' ? E.start('local') : (E.server(h) || {}).virtualRoot || E.start(h));
  /* Draws the bar above the tree: Up, the path of the current folder as clickable steps (computer first), and New folder / Upload for it. Only real data; nothing is drawn for a computer that is not connected. */
  T.paintTop = () => {
    const bar = T.stage && $('#ftop', T.stage); if (!bar) return; const p = P(); const h = p.host; let html = '';
    if (h && hosts().indexOf(h) >= 0) {
      const online = p.online(); const dir = online ? curDir() : ''; const root = !dir || U.isRoot(dir) || !!(A.atVirtualRoot && A.atVirtualRoot(h, dir));
      const name = h === 'local' ? 'This computer' : A.hostName(h);
      let parts = []; if (dir) { const top = U.upTo(dir, 0); if (top && top !== '/') parts.push({ n: top, p: top }); U.segs(dir).forEach((s, i) => parts.push({ n: s, p: U.upTo(dir, i + 1) })); }
      let pre = ''; if (parts.length > 3) { pre = '<button type="button" data-top="crumb" data-p="' + esc(parts[parts.length - 4].p) + '" title="' + esc(parts[parts.length - 4].p) + '">…</button><span class="sep">/</span>'; parts = parts.slice(-3); }
      const trail = pre + parts.map((s, i) => (i === parts.length - 1 ? '<b>' + esc(s.n) + '</b>' : '<button type="button" data-top="crumb" data-p="' + esc(s.p) + '">' + esc(s.n) + '</button>')).join('<span class="sep">/</span>');
      const star = online && h !== 'local' && dir && A.isFav && A.isFav(h, dir);
      html = '<span class="k-nav"><button type="button" class="k-ib" data-top="back" aria-label="Back" data-tip="Back"' + (T.hist.length ? '' : ' disabled') + '>' + ic('arrow-left', { size: 16 }) + '</button><button type="button" class="k-ib" data-top="fwd" aria-label="Forward" data-tip="Forward"' + (T.fwd.length ? '' : ' disabled') + '>' + ic('arrow-right', { size: 16 }) + '</button><button type="button" class="k-ib" data-top="up" aria-label="Up one folder" data-tip="Up one folder"' + (root ? ' disabled' : '') + '>' + ic('arrow-up', { size: 16 }) + '</button></span>' +
        '<nav class="k-trail" aria-label="Path"><button type="button" class="host" data-top="host">' + dot(online ? 'ok' : 'off') + esc(name) + '</button>' + (parts.length ? '<span class="sep">:</span>' : '') + trail + '</nav>' + (online && h !== 'local' && dir ? '<button type="button" class="k-ib" data-top="star" aria-pressed="' + !!star + '" aria-label="' + (star ? 'Remove this folder from favorites' : 'Add this folder to favorites') + '" data-tip="' + (star ? 'Remove from favorites' : 'Add to favorites') + '">' + ic('star', { size: 16 }) + '</button>' : '') + '<span class="sp"></span>' +
        (online ? '<button type="button" class="k-ib" data-top="mk" aria-label="New folder" data-tip="New folder">' + ic('folder-plus', { size: 16 }) + '</button>' + (h === 'local' ? '' : '<button type="button" class="k-btn sm pri" data-top="upload">' + ic('upload', { size: 14 }) + '<span>Upload</span></button>') : '');
    }
    if (bar._h !== html) { bar._h = html; bar.innerHTML = html; }
  };
  /* Takes the latest place off the `from` list, puts the current one on the `to` list, and goes there with its ancestors opened in the tree. A computer that has been removed since is skipped. */
  function walk(from, to) {
    const s = from.pop(); const p = P(); if (!s) return;
    if (hosts().indexOf(s.h) < 0) { T.paintTop(); return; }
    to.push({ h: p.host, dir: p.path, names: [...p.sel] });
    T.open.add(key(s.h, '')); for (let i = 0; i <= U.segs(s.dir).length; i++) T.open.add(key(s.h, U.upTo(s.dir, i)));
    T.walking = true; try { select(s.h, s.dir, s.names); } finally { T.walking = false; }
  }
  /* A click on one of the bar's controls. */
  function topClick(el) {
    const p = P(); const h = p.host; const a = el.dataset.top; if (el.disabled) return;
    if (a === 'host') select(h, homeOf(h), []);
    else if (a === 'crumb') goDir(h, el.dataset.p);
    else if (a === 'up') goDir(h, U.parentOf(curDir()));
    else if (a === 'back') walk(T.hist, T.fwd);
    else if (a === 'fwd') walk(T.fwd, T.hist);
    else if (a === 'star') { const d = curDir(); if (d && h !== 'local') { A.toggleFav(h, d, U.baseOf(d) || A.hostName(h), 'dir'); T.paintTop(); } }
    else if (a === 'mk') T.startNew();
    else if (a === 'upload') { select(h, curDir(), []); A.uploadFab(); }
  }
  const toggle = (h, p) => { const k = key(h, p); if (T.open.has(k)) T.open.delete(k); else T.open.add(k); T.paint(); };

  /* A row was clicked. The row is always selected (the details panel follows it). Then the Settings choice decides: "One click"
     opens a folder (or previews a file that has a viewer) at once; "Two clicks" opens it on a second click within 450 ms. */
  function activate(r, again, e) {
    /* Trash, Share links and Apps open in place (a click on the open, selected one folds it) and show what the Tools pages hold; Tools itself opens from the panel's Manage button. */
    if (r.k === 'special') { const was = spIs(r.h, r.kind, ''); if (!spOpen(r.h, r.kind)) { T.open.add(key(r.h, SPK + r.kind)); spLoad(r.kind, r.h); } else if (was) T.open.delete(key(r.h, SPK + r.kind)); spSelect(r.h, r.kind, ''); return; }
    if (r.k === 'sitem') { spSelect(r.h, r.kind, r.e.id); return; }
    if (r.k === 'host') { const home = r.h === 'local' ? E.start('local') : (E.server(r.h) || {}).virtualRoot || E.start(r.h); select(r.h, P().host === r.h ? P().path : home, []); if (!e.ctrl) toggle(r.h, ''); return; }
    const p = P(); const was = p.host === r.h && p.path === r.dir && p.sel.has(r.n.n);
    if (e.ctrl && p.host === r.h && p.path === r.dir) { const s = new Set(p.sel); if (s.has(r.n.n)) s.delete(r.n.n); else s.add(r.n.n); select(r.h, r.dir, [...s]); return; }
    /* a Shift range is the rows drawn between the anchor and this row, in the order they are drawn */
    const drawn = () => T.rows.filter((x) => (x.k === 'dir' || x.k === 'file') && !x.root && x.h === r.h && x.dir === r.dir);
    if (e.shift && p.host === r.h && p.path === r.dir && drawn().length) { const ds = drawn(); const a = p.anchor >= 0 ? p.anchor : Math.max(0, ds.findIndex((x) => p.sel.has(x.n.n))); const b = ds.findIndex((x) => x.n.n === r.n.n); const [lo, hi] = a < b ? [a, b] : [b, a]; select(r.h, r.dir, ds.slice(lo, hi + 1).map((x) => x.n.n)); p.anchor = a; return; }
    select(r.h, r.dir, [r.n.n]); p.anchor = drawn().findIndex((x) => x.n.n === r.n.n);
    if (e.lead || p.sel.size !== 1) return; /* the icon only selects */
    const now = Date.now();
    if (A.S.openMode === 'double') {
      /* "Two clicks": the first click redraws the rows, so the browser never reports a double click on one element; two clicks on the same item within 450 ms are counted here. */
      const k = key(r.h, r.p); const again2 = T.lc && T.lc.k === k && now - T.lc.t <= 450; T.lc = again2 ? null : { k, t: now };
      if (again2) { T.opened = now; openRow(r); }
    } else if (r.k === 'dir') { if (!isOpen(r.h, r.p) || was) toggle(r.h, r.p); } /* one click: a folder opens (a click on the open, selected one folds it) */
    else if (!A.hasViewer || A.hasViewer(r.h, r.n)) p.preview(r.n); /* and a file is shown only when something can show it; any other file is just selected */
  }
  /* Opens the folder of a row (or folds it) or previews its file. */
  function openRow(r) { if (r.k === 'dir') toggle(r.h, r.p); else if (r.k === 'file') P().preview(r.n); }

  /* Opens the folder (or previews the file) of the selected row. When the selection is not a drawn row (its folder is
     not open in the tree), the Pane's own selection is used: a file previews, a folder is opened (never folded). */
  function openSel() {
    const r = T.cur();
    if (!r) { const p = P(); const one = p.selected()[0]; if (!one) return; if (one.t === 'dir') { T.open.add(key(p.host, '')); T.open.add(key(p.host, nodePath(p.path, one))); T.paint(); } else p.preview(one); return; } if (r.k === 'dir') toggle(r.h, r.p); else if (r.k === 'file') P().preview(r.n); else if (r.k === 'host') toggle(r.h, ''); }
  /* The row of the current selection (the first one, when several are selected). */
  T.cur = () => { const p = P(); return T.rows.find((r) => (r.k === 'dir' || r.k === 'file') && r.h === p.host && r.dir === p.path && p.sel.has(r.n.n)) || T.rows.find((r) => (r.k === 'special' && spIs(r.h, r.kind, '')) || (r.k === 'sitem' && spIs(r.h, r.kind, r.e.id))) || (p.sel.size === 0 ? T.rows.find((r) => r.k === 'host' && r.h === p.host) : null); };
  /* Moves the selection one visible row up or down. */
  function step(d) {
    const rs = T.rows.map((r, i) => [r, i]).filter((x) => ['host', 'dir', 'file', 'special', 'sitem'].includes(x[0].k)); const c = T.cur(); let i = rs.findIndex((x) => x[0] === c) + d; i = Math.max(0, Math.min(rs.length - 1, i)); if (rs[i]) { const n = rs[i][0]; if (n.k === 'special') spSelect(n.h, n.kind, ''); else if (n.k === 'sitem') spSelect(n.h, n.kind, n.e.id); else activate(n, false, { ctrl: true }); } /* ctrl: select only, never fold a computer */
  }

  /* Rename in place: the name becomes a field; Enter sends it through the engine, Escape leaves it. */
  T.startRename = (n) => { const p = P(); if (!n) return; T.ren = { h: p.host, dir: p.path, name: n.n }; T.paint(); };
  async function commitRename(inp) {
    const r = T.ren; if (!r || T.busy) return; const v = inp.value.trim(); if (!v || v === r.name) { T.ren = null; T.paint(); return; }
    T.busy = true;
    try { await E.fs.rename(r.h, r.dir, r.name, v); T.ren = null; select(r.h, r.dir, [v]); A.snack('Renamed to “' + v + '”'); }
    catch (e) { inp.setAttribute('aria-invalid', 'true'); inp.title = e.message; A.snack(e.message, { error: true }); inp.focus(); } finally { T.busy = false; }
  }
  /* New folder in the folder that is selected (or the one the selection sits in). */
  T.startNew = () => {
    const p = P(); const c = T.cur(); let dir = p.path; if (c && c.k === 'dir' && p.sel.size === 1) dir = c.p;
    if (A.atVirtualRoot(p.host, dir)) { A.snack('Open one of the folders first.', { error: true }); return; }
    T.open.add(key(p.host, '')); T.open.add(key(p.host, dir)); T.mk = { h: p.host, dir }; T.paint();
  };
  async function commitNew(inp) {
    const m = T.mk; if (!m || T.busy) return; const v = inp.value.trim(); if (!v) { T.mk = null; T.paint(); return; }
    T.busy = true;
    try { await E.fs.mkdir(m.h, m.dir, v); T.mk = null; select(m.h, m.dir, [v]); A.snack('Created folder “' + v + '”'); } catch (e) { inp.setAttribute('aria-invalid', 'true'); A.snack(e.message, { error: true }); inp.focus(); } finally { T.busy = false; }
  }

  /* True when `path` is the folder `rp` or lies inside it (the match ends at a separator, so "/home/u" does not take "/home/user2"). */
  const underRoot = (path, rp) => { const b = rp.replace(/[\\/]+$/, ''); return path === rp || path === b || (path.startsWith(b) && /[\\/]/.test(path.charAt(b.length))); };
  /* Opens every folder on the way to `path` on computer `h` (waiting for each listing) and selects `name` there: used by "show in folder" style jumps. */
  T.reveal = async (h, path, name) => {
    if (!T.on()) return false; const s = E.server(h); T.open.add(key(h, ''));
    const wait = async (dir) => { for (let i = 0; i < 60; i++) { if (E.fs.list(h, dir)) return true; if (E.fs.state(h, dir) === 'error') return false; await sleep(50); } return false; };
    const base = s && s.virtualRoot ? s.virtualRoot : '/'; if (!(await wait(base))) return false;
    const root = (rootsOf(h) || []).map((r) => nodePath('/', r)).filter((rp) => underRoot(path, rp)).sort((a, b) => b.length - a.length)[0] || base;
    let cur = root; T.open.add(key(h, cur)); if (!(await wait(cur))) return false;
    const rest = path.slice(root.length).split(/[\\/]/).filter(Boolean);
    for (const seg of rest) { cur = U.join(cur, seg); T.open.add(key(h, cur)); if (!(await wait(cur))) break; }
    if (name) select(h, path, [name]); else T.paint();
    const sel = $('.n.sel', T.tree || document); if (sel && sel.scrollIntoView) sel.scrollIntoView({ block: 'nearest' }); return true;
  };

  /* Context menu of a row: the Pane's own menu, with Rename and Open made to work on the tree. */
  function menuItems() {
    const p = P(); const one = p.selected()[0];
    return p.ctxItems(true).map((m) => (m && m.label === 'Rename' ? Object.assign({}, m, { onClick: () => T.startRename(one) }) : m && m.label === 'Open' && one && one.t === 'dir' ? Object.assign({}, m, { onClick: () => toggle(p.host, nodePath(p.path, one)) }) : m));
  }
  function menu(x, y, r) {
    const p = P();
    const it = menuItems();
    if (r && r.k === 'dir' && p.host !== 'local') it.splice(it.length - 2, 0, { icon: 'star', label: A.isFav && A.isFav(p.host, r.p) ? 'Remove from favorites' : 'Add folder to favorites', onClick: () => A.toggleFav(p.host, r.p, r.n.n, 'dir') }, '-');
    A.menu(x, y, it);
  }
  function hostMenu(x, y, r) {
    const s = r.s; const items2 = [{ icon: 'refresh', label: 'Refresh', onClick: () => { const h = r.h; const rs = rootsOf(h) || []; E.fs.refresh(h, s && s.virtualRoot ? s.virtualRoot : '/').catch(A.noop); rs.forEach((n) => isOpen(h, nodePath('/', n)) && E.fs.refresh(h, nodePath('/', n)).catch(A.noop)); } }];
    if (s) items2.push({ icon: 'server', label: 'Computers', onClick: () => A.go('servers') });
    A.menu(x, y, items2);
  }

  let bound = new WeakSet();
  /* One set of listeners on the page (clicks, keys, the details panel's buttons); the page is redrawn but never rebound. */
  T.bindOnce = (stage) => {
    if (bound.has(stage)) return; bound.add(stage);
    const rowOf = (t) => { const n = t.closest && t.closest('[data-i]:not(svg)'); /* an icon (svg) carries data-i too: it is not a row */ return n ? T.rows[+n.dataset.i] : null; };
    stage.addEventListener('click', (e) => {
      if (st.view !== 'files') return; /* the stage is shared: rows of the other pages (recent files, search results) carry data-i too */
      const tp = e.target.closest && e.target.closest('#ftop [data-top]'); if (tp) { topClick(tp); return; }
      const t = e.target; const a = t.closest('[data-a]'); const od = t.closest('.olside [data-do]');
      if (od) {
        const d = od.dataset.do; if (od.disabled) return;
        if (d === 'manage') { const c = spCur(); if (c) goTools(c.h, c.kind); return; }
        if (d === 'rename') { T.startRename(P().selected()[0]); return; } if (d === 'open') { openSel(); return; } if (d === 'newfolder') { T.startNew(); return; }
        if (d === 'more') { A.openDetailMenu(od, menuItems()); return; }
        if (A.sideClick) A.sideClick(e); return;
      }
      if (a && !t.closest('.rn')) {
        const r = rowOf(a); const act = a.dataset.a;
        if (act === 'add') { A.serverDialog(); return; }
        if (act === 'find') { A.serverDialog(); A.defer(() => A.clickByText(document.querySelector('#layer'), /Find on network/)); return; }
        if (!r) return; e.stopPropagation();
        if (act === 'connect') { E.connect(r.h).catch(A.noop); return; }
        if (act === 'review') { const s = E.server(r.h); if (s && s.state === 'refused' && A.reverifyDialog) A.reverifyDialog(s.id); else if (A.trustDialog) A.trustDialog(s); return; }
        if (r.k === 'special') { goTools(r.h, r.kind); return; } /* the row's "Open Trash" button */
        /* the button is measured first: selecting redraws the rows, and a menu placed from the detached button would open in the window's corner */
        const b = a.getBoundingClientRect();
        if (r.k === 'host') { select(r.h, P().host === r.h ? P().path : (E.server(r.h) || {}).virtualRoot || E.start(r.h), []); if (act === 'upload') A.uploadFab(); else hostMenu(b.left - 150, b.bottom, r); return; }
        if (!(P().host === r.h && P().path === r.dir && P().sel.has(r.n.n))) select(r.h, r.dir, [r.n.n]);
        if (act === 'download') P().transfer(); else if (act === 'link') { const H = A.fxHandlers; H && H['share.new'] && H['share.new'](); } else if (act === 'upload') { select(r.h, r.p, []); A.uploadFab(); } else if (act === 'more') menu(b.left - 150, b.bottom, r);
        return;
      }
      if (t.closest('.rn')) return;
      const r = rowOf(t); if (!r) return; if (t.closest('[data-tw]') && r.k === 'dir') { toggle(r.h, r.p); return; }
      activate(r, false, { ctrl: e.ctrlKey || e.metaKey, shift: e.shiftKey, lead: !!t.closest('.k-fic') });
    });
    /* A real double click opens the row in "Two clicks" mode, unless the counted pair of clicks just did (it would open twice). */
    stage.addEventListener('dblclick', (e) => {
      if (st.view !== 'files' || A.S.openMode !== 'double' || Date.now() - (T.opened || 0) < 700 || e.target.closest('input,button,[data-a],[data-tw]')) return;
      const r = rowOf(e.target); if (!r || (r.k !== 'dir' && r.k !== 'file')) return; openRow(r);
    });
    stage.addEventListener('contextmenu', (e) => {
      if (st.view !== 'files') return;
      const r = rowOf(e.target); if (!r) return; e.preventDefault();
      if (r.k === 'host') { select(r.h, P().path, []); hostMenu(e.clientX, e.clientY, r); return; } if (r.k !== 'dir' && r.k !== 'file') return;
      if (!(P().host === r.h && P().path === r.dir && P().sel.has(r.n.n))) select(r.h, r.dir, [r.n.n]); menu(e.clientX, e.clientY, r);
    });
    stage.addEventListener('keydown', (e) => {
      if (st.view !== 'files') return;
      const t = e.target;
      if (t.matches && t.matches('[data-rn]')) { if (e.key === 'Enter') { e.preventDefault(); commitRename(t); } else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); T.ren = null; T.paint(); } return; }
      if (t.matches && t.matches('[data-mk]')) { if (e.key === 'Enter') { e.preventDefault(); commitNew(t); } else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); T.mk = null; T.paint(); } return; }
      if (!t.matches || !t.matches('.tr')) return; const p = P(); const c = T.cur();
      if (e.key === 'ArrowDown') { e.preventDefault(); step(1); } else if (e.key === 'ArrowUp') { e.preventDefault(); step(-1); }
      else if (e.key === 'ArrowRight' && c && c.k === 'dir' && !isOpen(c.h, c.p)) { e.preventDefault(); toggle(c.h, c.p); }
      else if (e.key === 'ArrowLeft' && c && c.k === 'dir' && isOpen(c.h, c.p)) { e.preventDefault(); toggle(c.h, c.p); }
      else if (e.key === 'Enter') { e.preventDefault(); openSel(); } else if (e.key === ' ' && c && c.k === 'file') { e.preventDefault(); p.preview(c.n); }
      else if (e.key === 'Delete' && c && (c.k === 'dir' || c.k === 'file')) { e.preventDefault(); p.del(); } else if (e.key === 'F2' && c && (c.k === 'dir' || c.k === 'file')) { e.preventDefault(); T.startRename(c.n); }
      else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'd' && c && (c.k === 'dir' || c.k === 'file') && p.host !== 'local') { e.preventDefault(); p.transfer(); }
    });
    stage.addEventListener('focusout', (e) => { const t = e.target; if (t.matches && (t.matches('[data-rn]') || t.matches('[data-mk]')) && !(e.relatedTarget && stage.contains(e.relatedTarget) && e.relatedTarget.matches('.rn'))) { if (t.matches('[data-rn]')) commitRename(t); else commitNew(t); } });
  };

  /* The page changes with the computers and the folders the engine knows: redraw when either does. */
  E.on((t, d) => { if (t === 'fs' || t === 'servers' || t === 'tasks') later(); });
  /* The counts of Share links come from the agents: asked once when the tree first shows, drawn when they arrive. */
  T.warm = () => { const X = A.X; if (X && !X.sharesAt && !X.sharesBusy && A.loadShares && E.servers.some((s) => s.state === 'online')) Promise.resolve(A.loadShares()).then(later).catch(A.noop); };
});
