/* RFE Tonal - Devices and Tools pages, settings extras, palette commands, on real calls. Needs features.js. */
(function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, fic = A.fic, st = A.state, X = A.X, F = A.fx, H = A.fxHandlers;
  const { rel, left, audit, GRANTS, plural } = F;
  const hostLabel = (h) => A.hostName(h);
  const fail = (e) => A.snack((e && e.message) || String(e), { error: true });
  const stage = () => $('#stage');
  const online = () => E.servers.filter((s) => s.state === 'online');
  const STALE = 30000;

  /* ================= Devices ================= */
  const devIcon = (d) => (/android|phone|pixel|galaxy|iphone/i.test(d.name + ' ' + d.ver) ? 'phone' : /desktop|laptop|windows|linux|mac/i.test(d.name) ? 'laptop' : 'phone');
  const isOnline = (d) => !d.revoked && d.last && E.now() - d.last < 120000;
  const grantTags = (a) => (a ? ['Browse'].concat(GRANTS.filter((g) => a[g[0]]).map((g) => ({ download: 'Download', upload: 'Upload', modify: 'Modify', delete: 'Delete', share: 'Share', viewApps: 'Apps', launchApps: 'Launch apps' })[g[0]])).concat(a.readOnly ? ['Read-only'] : []).concat(a.jailRoot ? ['Folder limit'] : []) : []);
  let appVersion = '';
  try { const ap = window.__TAURI__ && window.__TAURI__.app; if (ap && ap.getVersion) ap.getVersion().then((v) => { appVersion = v; }).catch(() => {}); } catch (e) { /* not in the app */ }
  A.appVersion = () => appVersion || '';

  A.pageDevices = () => {
    const s = A.pairServer(); const reqs = X.inbox.slice();
    const me = X.devices.find((d) => d.current);
    const row = (d) => '<div class="dvrow' + (d.revoked ? ' dim' : '') + '"><div class="gi">' + ic(devIcon(d)) + '</div><div class="dm"><b>' + esc(d.name) + (d.current ? ' <span class="tag pri">This app</span>' : '') + '</b><small><i class="dot ' + (isOnline(d) ? 'online' : 'disconnected') + '"></i>' + (d.revoked ? 'Revoked' : isOnline(d) ? 'Online now' : d.last ? 'Last seen ' + rel(d.last) : 'Never seen') + (d.addr ? ' · ' + esc(d.addr) : '') + (d.ver ? ' · ' + esc(d.ver) : '') + '</small><div class="tags">' + (d.revoked ? '<span class="tag">No access</span>' : grantTags(d.a).map((t) => '<span class="tag">' + t + '</span>').join('')) + '</div></div><div class="ac">' + (d.a && !d.revoked ? '<button class="btn sm" data-fx="dev.perm|' + esc(d.id) + '">' + ic('shield-check') + 'Permissions</button>' : '') + '<button class="ib sm" data-fx="dev.menu|' + esc(d.id) + '" aria-label="More for ' + esc(d.name) + '">' + ic('more-v') + '</button></div></div>';
    const left1 = '<div class="fxc fxpair">' + A.pairInner() + '</div>';
    const pend = reqs.map((q) => '<div class="fxc warn"><div class="fxh"><span class="fxi">' + ic('phone') + '</span><div><b>' + esc(q.label) + ' wants to pair</b><small>' + esc(q.address || '') + ' · ' + (q.ageSeconds < 10 ? 'just now' : q.ageSeconds + ' s ago') + '</small></div></div><p class="fxp">Accept only if the code matches the one shown on the device asking.</p><div class="fp" style="margin:0 0 .75rem;font-size:1.25rem;letter-spacing:.14em">' + esc(q.matchCode) + '</div><div class="pbtns"><button class="btn f" data-fx="dev.approve|' + X.inbox.indexOf(q) + '">' + ic('check') + 'Accept…</button><button class="btn" data-fx="dev.deny|' + X.inbox.indexOf(q) + '">Deny</button></div></div>').join('');
    const inboxNote = !s ? '' : X.inboxDenied && X.inboxDenied.has(s.id) ? '<div class="fxc"><div class="fxempty"><span>This login cannot answer pairing requests. Sign out and sign in with the account, or use <span class="mono">rfe pair accept</span> on the PC.</span></div></div>' : reqs.length >= 3 ? '<div class="fxc"><div class="fxempty"><span>The agent holds at most 3 waiting requests. Answer one to let the next device ask.</span></div></div>' : '';
    const ctl = '';
    const list = !s ? '<div class="fxempty">' + ic('server', { size: 40 }) + '<b>No connected server</b><span>Devices belong to a server. Connect one first.</span></div>'
      : X.devErr ? '<div class="fxempty">' + ic('alert-circle', { size: 40 }) + '<b>Could not list devices</b><span>' + esc(X.devErr) + '</span></div>'
        : X.devices.length ? X.devices.map(row).join('') : '<div class="fxempty">' + ic('phone', { size: 40 }) + '<b>' + (X.devBusy ? 'Loading…' : 'No paired devices') + '</b></div>';
    const nOn = X.devices.filter(isOnline).length;
    const ev = (X.audit || []).slice(0, 4).map((h) => '<div class="evrow">' + ic('shield-check') + '<span>' + esc(h.text) + '</span><small>' + rel(h.at) + '</small></div>').join('');
    stage().innerHTML = '<h2 class="pt">Devices</h2>' + (s ? '<p class="ps">Showing devices paired with <b>' + esc(s.name) + '</b>. Pick another server in the pairing card.</p>' : '') + '<div class="dvg">' + left1 + '<div class="dvr">' + pend + inboxNote + ctl +
      '<div class="fxc"><div class="fxh"><span class="fxi">' + ic('shield-check') + '</span><div><b>Paired devices</b><small>' + nOn + ' of ' + X.devices.length + ' online' + (X.devForbidden && X.devices.length ? ' · permissions need the owner sign-in' : '') + '</small></div></div>' + list + '</div>' +
      '' + (ev || X.auditErr ? '<div class="fxc"><div class="fxh"><div><b>Security events</b></div><button class="btn tx sm" style="margin-left:auto" data-fx="hist.audit">View all</button></div>' + (ev || '<div class="fxempty"><span>' + esc(X.auditErr) + '</span></div>') + '</div>' : '') + '</div></div>';
    if (s && E.now() - X.devAt > STALE && !X.devBusy) A.loadDevices();
    if (s && E.now() - X.auditAt > STALE) A.loadAudit();
    A.ensureCode();
  };
  A.loadAudit = async () => {
    const s = A.pairServer(); X.auditAt = E.now(); if (!s) return; X.auditErr = '';
    try { const r = await E.call('audit_page', { host: s.id }); X.audit = r.forbidden ? [] : (r.entries || []).map((e) => ({ at: Date.parse(e.at) || 0, text: [e.action, e.target, e.detail].filter(Boolean).join(' · ') + (e.actor ? ' (' + e.actor + ')' : '') })); X.auditErr = r.forbidden ? 'This login cannot read the audit log or the agent log.' : ''; }
    catch (e) { X.audit = []; X.auditErr = e.message; }
    if (st.view === 'devices') A.pageDevices();
  };
  H['dev.menu'] = (id, a, b) => { const d = X.devices.find((x) => x.id === id); if (!d) return; const r = b.getBoundingClientRect(); A.menu(r.right - 220 * A.u(), r.bottom + 4, [].concat(d.a && !d.revoked ? [{ icon: 'shield-check', label: 'Permissions', onClick: () => H['dev.perm'](id) }] : [], [{ icon: 'clock', label: 'Activity', onClick: () => { st.histFilter = 'audit'; A.go('history'); } }, '-'], d.revoked ? [] : [{ icon: 'x-circle', label: 'Revoke access', danger: true, onClick: () => H['dev.revoke'](id) }], [{ icon: 'trash', label: 'Remove from list', danger: true, onClick: () => H['dev.remove'](id) }])); };
  H['hist.audit'] = () => { st.histFilter = 'audit'; A.go('history'); };

  /* ================= Tools ================= */
  A.toolTabs = []; A.toolBodies = {};
  const TABS = () => [['fav', 'star', 'Favorites & offline'], ['store', 'pie', 'Storage'], ['trash', 'trash', 'Trash', X.trash.length], ['share', 'link', 'Share links', X.shares.filter((s) => s.exp > E.now()).length], ['sync', 'refresh', 'Backup & sync'], ['apps', 'zap', 'Apps']].concat(A.toolTabs.map((f) => f()));
  A.pageTools = () => {
    const t = X.tool; const M = { fav: tFav, store: tStore, trash: tTrash, share: tShare, sync: tSync, apps: tApps };
    const body = M[t] ? M[t]() : A.toolBodies[t] ? A.toolBodies[t]() : tFav();
    stage().innerHTML = '<h2 class="pt">Tools</h2><div class="chips" style="padding:0 0 .875rem" role="tablist">' + TABS().map((x) => '<button class="chip' + (t === x[0] ? ' on' : '') + '" role="tab" aria-selected="' + (t === x[0]) + '" data-fx="tool.tab|' + x[0] + '">' + ic(x[1]) + x[2] + (x[3] ? ' <b class="cnt">' + x[3] + '</b>' : '') + '</button>').join('') + '</div><div id="toolBody">' + body + '</div>';
  };
  /* The device list is redrawn on every poll too: the focused control is found again by its label or data attributes. */
  const pageDevices0 = A.pageDevices;
  A.pageDevices = () => {
    const a = document.activeElement; let sel = '';
    if (a && a !== document.body && stage().contains(a)) { const q = (v) => String(v).replace(/"/g, '\\"'); if (a.getAttribute('aria-label')) sel = '[aria-label="' + q(a.getAttribute('aria-label')) + '"]'; else if (a.getAttribute('data-fx')) sel = '[data-fx="' + q(a.getAttribute('data-fx')) + '"]'; else if (a.id) sel = '#' + a.id; }
    pageDevices0();
    if (sel) { const n = stage().querySelector(sel); if (n && document.activeElement !== n) n.focus({ preventScroll: true }); }
  };
  /* The page is drawn again on every click and every poll: the control that had the focus is found again so the keyboard keeps its place. */
  const pageTools0 = A.pageTools;
  A.pageTools = () => {
    const a = document.activeElement; let sel = '';
    if (a && a !== document.body && stage().contains(a)) { if (a.getAttribute('data-fx')) sel = '[data-fx="' + a.getAttribute('data-fx').replace(/"/g, '\\"') + '"]'; else if (a.id) sel = '#' + a.id; }
    pageTools0();
    if (sel) { const n = stage().querySelector(sel); if (n && document.activeElement !== n) n.focus({ preventScroll: true }); }
  };
  /* what a tab needs from the servers is asked for when it opens, and again when it is older than half a minute */
  const WANT = { trash: () => E.now() - X.trashAt > STALE && A.loadTrash(), share: () => E.now() - X.sharesAt > STALE && A.loadShares(), apps: () => loadApps(), store: () => loadBig() };
  H['tool.tab'] = (k) => { X.tool = k; A.pageTools(); if (WANT[k]) WANT[k](); };
  A.toolOpened = () => { if (WANT[X.tool]) WANT[X.tool](); };
  const empty = (icn, t, s) => '<div class="fxempty">' + ic(icn, { size: 40 }) + '<b>' + t + '</b>' + (s ? '<span>' + s + '</span>' : '') + '</div>';
  const head = (icn, t, s, right) => '<div class="fxh"><span class="fxi">' + ic(icn) + '</span><div><b>' + t + '</b>' + (s ? '<small>' + s + '</small>' : '') + '</div>' + (right ? '<div style="margin-left:auto;display:flex;gap:.5rem">' + right + '</div>' : '') + '</div>';
  const noServer = () => empty('server', 'No connected server', 'Connect a server to use this.');
  const errLine = (m) => (m ? '<div class="hint" style="margin:.5rem 0 0;color:var(--err)">' + esc(m) + '</div>' : '');

  /* ---- favorites, recents, offline ---- */
  function tFav() {
    const fav = X.favs.map((f, i) => '<div class="lrow"><div class="gi">' + ic(f.t === 'dir' ? 'star' : 'file') + '</div><div class="dm"><b>' + esc(f.name) + '</b><small>' + esc(hostLabel(f.host)) + ' · ' + esc(f.path) + '</small></div><button class="btn sm" data-fx="fav.open|' + i + '">Open</button><button class="ib sm" data-fx="fav.rm|' + i + '" aria-label="Remove favorite">' + ic('x') + '</button></div>').join('');
    const rec = X.recents.slice(0, 6).map((r, i) => '<div class="lrow"><div class="gi">' + ic('clock') + '</div><div class="dm"><b>' + esc(r.name) + '</b><small>' + esc(hostLabel(r.host)) + ' · ' + esc(U.parentOf(r.path)) + ' · ' + rel(r.at) + '</small></div><button class="btn sm" data-fx="rec.open|' + i + '">Show</button></div>').join('');
    const pins = X.pins.map((p, i) => { const pr = A.pinProg(p); const t = p.tid ? E.task(p.tid) : null; const bad = t && t.state === 'failed'; return '<div class="lrow"><div class="gi">' + ic(bad ? 'alert-circle' : pr >= 1 ? 'check-circle' : 'download') + '</div><div class="dm"><b>' + esc(p.name) + '</b><small>' + (bad ? esc(t.msg || 'The download failed') : pr >= 1 ? (t ? 'Available offline · ' : 'Downloaded earlier (not checked now) · ') + U.fmtBytes(p.b) : 'Downloading ' + Math.round(pr * 100) + '% of ' + U.fmtBytes(p.b)) + '</small>' + (pr < 1 ? '<div class="bar" style="margin-top:.375rem"><i style="width:' + pr * 100 + '%"></i></div>' : '') + '</div><button class="btn sm" data-fx="pin.rm|' + i + '">Stop keeping offline</button></div>'; }).join('');
    return '<div class="tgrid"><div class="fxc">' + head('star', 'Favorites', '', (() => { const m = A.mainPane; const ok = m && m.online() && m.host !== 'local'; return '<button class="btn sm" data-fx="fav.add"' + (ok ? ' title="Adds ' + esc(hostLabel(m.host) + ':' + m.path) + '"' : ' disabled title="Open a folder on a connected server in Files first"') + '>' + ic('plus') + 'Add current folder</button>'; })()) + (fav || empty('star', 'No favorites', 'Open a folder in Files, then come back here and press “Add current folder”.')) + '</div><div class="fxc">' + head('clock', 'Recent files', '') + (rec || empty('clock', 'No recent files', 'Files you preview from a server show up here.')) + '</div><div class="fxc wide">' + head('pin', 'Available offline', '') + (pins || empty('pin', 'Nothing offline', 'Right-click a file on a server in Files and choose “Make available offline”.')) + '</div></div>';
  }
  Object.assign(H, {
    'fav.open': (i) => { const f = X.favs[+i]; A.openPath(f.host, f.path, f.t !== 'dir'); }, 'fav.rm': (i) => { X.favs.splice(+i, 1); F.persist(); A.pageTools(); },
    'fav.add': () => { const m = A.mainPane; if (!m.online() || m.host === 'local') { A.snack('Open a connected server in Files first.', { error: true }); return; } if (!A.isFav(m.host, m.path)) A.toggleFav(m.host, m.path, U.baseOf(m.path) || hostLabel(m.host), 'dir'); else A.snack('Already a favorite'); },
    'rec.open': (i) => { const r = X.recents[+i]; A.openPath(r.host, r.path, true); }, 'pin.rm': (i) => { const p = X.pins[+i]; if (!p) return; const t = p.tid ? E.task(p.tid) : null; if (t && ['running', 'queued', 'paused', 'waiting'].includes(t.state)) E.cancel(t.id); X.pins.splice(+i, 1); F.persist(); A.pageTools(); A.snack('Stopped keeping ' + p.name + ' offline. The copy already on this computer stays where it is.'); }
  });

  /* ---- storage ---- */
  const BIG_MIN = 50e6;
  async function loadBig() {
    const s = pickHost('storeHost'); if (!s || X.bigBusy) return; X.bigBusy = true; X.bigErr = '';
    try { const rows = await E.call('files_search', { host: s.id, options: { query: '*', limit: 500, types: [], minSize: BIG_MIN } }); X.big = rows.filter((r) => !r.isDir).sort((a, b) => b.size - a.size).slice(0, 6).map((r) => ({ p: r.path, name: r.name, b: r.size })); X.bigHost = s.id; }
    catch (e) { X.big = []; X.bigErr = e.message; }
    X.bigBusy = false; if (st.view === 'tools' && X.tool === 'store') A.pageTools();
  }
  function pickHost(k) { const on = online(); if (!on.length) return null; if (!on.some((s) => s.id === X[k])) X[k] = on[0].id; return E.server(X[k]); }
  const dupKey = (r) => r.name.replace(/ \(\d+\)(?=\.[^.]*$|$)/, '').toLowerCase() + '|' + r.size;
  async function findDupes(host) {
    X.dupeBusy = true; X.dupeErr = ''; A.pageTools();
    try {
      const rows = (await E.call('files_search', { host, options: { query: '*', limit: 500, types: [], minSize: 1e6 } })).filter((r) => !r.isDir);
      const m = new Map(); for (const r of rows) { const k = dupKey(r); if (!m.has(k)) m.set(k, []); m.get(k).push(r); }
      X.dupes = [...m.values()].filter((g) => g.length > 1).map((g) => g.sort((a, b) => Date.parse(b.modified) - Date.parse(a.modified))).sort((a, b) => b[0].size * (b.length - 1) - a[0].size * (a.length - 1)); X.dupeScanned = rows.length;
    } catch (e) { X.dupes = null; X.dupeErr = e.message; }
    X.dupeBusy = false; if (st.view === 'tools' && X.tool === 'store') A.pageTools();
  }
  function tStore() {
    const s = pickHost('storeHost'); if (!s) return noServer();
    const on = online(); const gb = (v) => (v >= 1000 ? (v / 1000).toFixed(1) + ' TB' : v.toFixed(v < 10 ? 1 : 0) + ' GB');
    const done = E.tasks.filter((t) => t.state === 'done'); const upB = done.filter((t) => t.dir === 'up').reduce((a, t) => a + (t.bytes || 0), 0), dnB = done.filter((t) => t.dir === 'down').reduce((a, t) => a + (t.bytes || 0), 0);
    const failed = E.tasks.filter((t) => t.state === 'failed').length + E.history.filter((h) => h.kind === 'transfer' && h.error && !(h.tid && E.task(h.tid))).length;
    const stat = (v, l, i) => '<div class="stat">' + ic(i) + '<b>' + v + '</b><small>' + l + '</small></div>';
    const roots = (s.roots || []).filter((r) => r.totalBytes > 0); const COL = ['var(--k-video)', 'var(--k-archive)', 'var(--k-pkg)', 'var(--k-doc)', 'var(--outline)'];
    const seg = roots.map((r, i) => '<i style="flex:' + Math.max(0.5, r.totalBytes - r.freeBytes) + ';background:' + COL[i % COL.length] + '" title="' + esc(r.label || r.path) + '"></i>').join('');
    const legend = roots.map((r, i) => '<li><i style="background:' + COL[i % COL.length] + '"></i>' + esc(r.label || r.path) + '<b>' + U.fmtBytes(r.totalBytes - r.freeBytes) + ' of ' + U.fmtBytes(r.totalBytes) + '</b></li>').join('');
    const hasDisk = s.disk[1] > 0; const usedPct = hasDisk ? s.disk[0] / s.disk[1] * 100 : 0;
    const big = X.bigHost === s.id ? X.big : null;
    const dup = X.dupes;
    return '<div class="chips" style="padding:0 0 .75rem">' + on.map((x) => '<button class="chip' + (x.id === s.id ? ' on' : '') + '" data-fx="store.host|' + esc(x.id) + '">' + ic('server') + esc(x.name) + '</button>').join('') + '</div>' +
      '<div class="fxc digest">' + head('activity', 'This session', 'Transfers since the app started') + '<div class="stats">' + stat(U.fmtBytes(upB), 'uploaded', 'upload') + stat(U.fmtBytes(dnB), 'downloaded', 'download') + stat(String(done.length), 'transfers done', 'swap') + stat(String(failed), 'failed', 'alert-circle') + stat(String(X.devices.filter((d) => isOnline(d)).length), 'devices online', 'phone') + '</div></div>' +
      '<div class="tgrid"><div class="fxc">' + head('pie', esc(s.name) + ' storage', hasDisk ? gb(s.disk[0]) + ' of ' + gb(s.disk[1]) + ' used' : 'The agent did not report disk use') + (hasDisk ? '<div class="bar' + (usedPct > 80 ? ' hi' : '') + '" style="margin:.875rem 0 .625rem"><i style="width:' + usedPct.toFixed(0) + '%"></i></div>' : '') + (roots.length ? '<div class="stack">' + seg + '</div><ul class="legend">' + legend + '</ul>' : '') + '</div>' +
      '<div class="fxc">' + head('layers', 'Largest files', 'Files over ' + U.fmtBytes(BIG_MIN) + ', biggest first', '<button class="btn sm" data-fx="big.scan"' + (X.bigBusy ? ' disabled' : '') + '>' + ic('refresh') + (X.bigBusy ? 'Scanning…' : 'Scan') + '</button>') + (X.bigBusy ? '<div class="fxsub" style="padding:.625rem 0">Searching…</div>' : X.bigErr ? errLine(X.bigErr) : big && big.length ? big.map((f) => '<div class="lrow"><div class="gi">' + fic({ n: f.name, t: 'file', b: f.b }) + '</div><div class="dm"><b>' + esc(f.name) + '</b><small>' + esc(U.parentOf(f.p)) + '</small></div><b class="sz">' + U.fmtBytes(f.b) + '</b><button class="ib sm" data-fx="store.show|' + esc(f.p) + '" aria-label="Show in folder">' + ic('folder-open') + '</button></div>').join('') : empty('layers', big ? 'No big files' : 'Not scanned yet', big ? 'Nothing over ' + U.fmtBytes(BIG_MIN) + ' was found.' : 'Press Scan to look for the largest files.')) + '</div>' +
      '<div class="fxc wide">' + head('copy', 'Duplicate finder', 'Same name and size in different folders', '<button class="btn sm' + (dup ? '' : ' f') + '" data-fx="dup.scan"' + (X.dupeBusy ? ' disabled' : '') + '>' + ic('search') + (X.dupeBusy ? 'Scanning…' : dup ? 'Scan again' : 'Scan ' + esc(s.name)) + '</button>') +
      (dup ? (dup.length ? dup.slice(0, 6).map((g, gi) => '<div class="dgrp"><div class="lrow"><div class="gi">' + fic({ n: g[0].name, t: 'file', b: g[0].size }) + '</div><div class="dm"><b>' + esc(g[0].name) + '</b><small>' + g.length + ' copies · ' + U.fmtBytes(g[0].size) + ' each · ' + U.fmtBytes(g[0].size * (g.length - 1)) + ' reclaimable</small></div><button class="btn sm" data-fx="dup.clean|' + gi + '">' + ic('trash') + 'Trash ' + (g.length - 1) + ' older</button></div>' + g.map((f, i) => '<div class="dloc' + (i === 0 ? ' keep' : '') + '">' + ic(i === 0 ? 'check' : 'dot') + '<code>' + esc(f.path) + '</code><small>' + (i === 0 ? 'keep · newest' : U.fmtDate(Date.parse(f.modified) || 0)) + '</small></div>').join('') + '</div>').join('') : empty('check-circle', 'No duplicates', 'Of the ' + (X.dupeScanned || 0) + ' files over 1 MB checked on ' + esc(s.name) + ', none share a name and size.')) : '<div class="fxsub" style="padding:.625rem 0 .125rem">' + (X.dupeErr ? '<span style="color:var(--err)">' + esc(X.dupeErr) + '</span>' : 'Scanning reads file names and sizes only, for up to 500 files over 1 MB. Nothing is changed.') + '</div>') + '</div></div>';
  }
  Object.assign(H, {
    'store.host': (id) => { X.storeHost = id; X.dupes = null; X.big = null; A.pageTools(); loadBig(); }, 'store.show': (p) => A.openPath(X.storeHost, p, true), 'big.scan': () => loadBig(),
    'dup.scan': () => findDupes(X.storeHost),
    'dup.clean': (gi) => {
      const g = X.dupes[+gi]; if (!g) return; const older = g.slice(1);
      /* Same name and size is not proof the content is the same, so the paths are listed first. */
      A.dialog({ icon: 'trash', cls: 'danger', title: 'Move ' + plural(older.length, 'copy', 'copies') + ' to Trash?', width: 520, body: 'These files have the same name and size as ' + esc(g[0].path) + '. Their content was not compared.<ul class="legend" style="margin-top:.625rem">' + older.map((f) => '<li class="mono">' + esc(f.path) + '</li>').join('') + '</ul>', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Move to Trash', kind: 'ed', icon: 'trash', cb: async (c) => {
        c.btn.go.disabled = true; let n = 0; const left = [];
        for (const f of older) { try { await E.call('files_trash', { host: X.storeHost, path: f.path }); n++; E.fs.drop(X.storeHost, U.parentOf(f.path)); } catch (e) { fail(e); left.push(f); } }
        if (n) { audit('Moved ' + plural(n, 'duplicate', 'duplicates') + ' to Trash on ' + hostLabel(X.storeHost)); A.snack('Moved ' + plural(n, 'duplicate', 'duplicates') + ' to Trash', { action: 'Show Trash', onAction: () => { X.tool = 'trash'; X.trashAt = 0; A.go('tools'); } }); X.trashAt = 0; }
        if (left.length) X.dupes[+gi] = [g[0]].concat(left); else X.dupes.splice(+gi, 1);
        c.close(); A.pageTools(); return false;
      } }] });
    }
  });

  /* ---- trash ---- */
  function tTrash() {
    const rows = A.trashRows(); const sel = X.trashSel;
    const sub = 'Deleted items stay here for ' + (S.trashDays || 30) + ' days, then they are removed when you open Trash';
    if (!rows.length) return '<div class="fxc">' + head('trash', 'Trash', sub) + (X.trashBusy && !X.trashAt ? empty('trash', 'Loading…', '') : empty('trash', 'Trash is empty', S.trash === false ? 'Trash is turned off in Settings, so deletes are permanent.' : 'Deleted files land here so you can bring them back.')) + errLine(X.trashErr) + '</div>';
    const all = rows.every((r) => sel.has(r.k)); const tot = rows.reduce((a, r) => a + r.n.b, 0);
    return '<div class="fxc">' + head('trash', 'Trash', plural(rows.length, 'item', 'items') + ' · ' + U.fmtBytes(tot), '<button class="btn sm" data-fx="trash.restore"' + (sel.size ? '' : ' disabled') + '>' + ic('undo') + 'Restore' + (sel.size ? ' ' + sel.size : '') + '</button><button class="btn sm e" data-fx="trash.purge"' + (sel.size ? '' : ' disabled') + '>' + ic('x') + 'Delete forever</button><button class="btn sm e" data-fx="trash.empty">' + ic('trash') + 'Empty Trash</button>') + errLine(X.trashErr) +
      '<div class="trh"><button class="ck' + (all ? ' on' : '') + '" data-fx="trash.all" aria-label="Select all">' + (all ? ic('check') : '') + '</button><span>Name</span><span>Deleted from</span><span>Deleted</span><span class="r">Size</span></div>' +
      rows.map((r) => '<div class="trr' + (sel.has(r.k) ? ' sel' : '') + '"><button class="ck' + (sel.has(r.k) ? ' on' : '') + '" data-fx="trash.pick|' + esc(r.k) + '" aria-label="Select ' + esc(r.n.n) + '">' + (sel.has(r.k) ? ic('check') : '') + '</button><span class="nm">' + fic(r.n) + '<b data-nolocal>' + esc(r.n.n) + '</b></span><span class="mono">' + esc(hostLabel(r.t.host)) + ':' + esc(r.t.dir) + '</span><span>' + (r.t.at ? rel(r.t.at) : '—') + '</span><span class="r">' + (r.n.t === 'dir' ? '—' : U.fmtBytes(r.n.b)) + '</span></div>').join('') + '</div>';
  }
  const selRows = () => A.trashRows().filter((r) => X.trashSel.has(r.k));
  Object.assign(H, {
    'trash.pick': (k) => { X.trashSel.has(k) ? X.trashSel.delete(k) : X.trashSel.add(k); A.pageTools(); },
    'trash.all': () => { const rows = A.trashRows(); if (rows.every((r) => X.trashSel.has(r.k))) X.trashSel.clear(); else rows.forEach((r) => X.trashSel.add(r.k)); A.pageTools(); },
    'trash.restore': () => F.restoreRows(selRows()),
    'trash.purge': () => { const r = selRows(); if (!r.length) return; A.dialog({ icon: 'trash', cls: 'danger', title: 'Delete ' + plural(r.length, 'item', 'items') + ' forever?', body: esc([...new Set(r.map((x) => hostLabel(x.t.host)))].join(', ')) + ': these ' + plural(r.length, 'item', 'items') + ' will be deleted forever. This can’t be undone.', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Delete forever', kind: 'ed', icon: 'trash', cb: () => F.purgeRows(r) }] }); },
    'trash.empty': () => { const r = A.trashRows(); if (!r.length) { A.snack('Trash is already empty'); return; } A.dialog({ icon: 'trash', cls: 'danger', title: 'Empty Trash?', body: plural(r.length, 'item', 'items') + ' (' + U.fmtBytes(r.reduce((a, x) => a + x.n.b, 0)) + ') on ' + esc([...new Set(r.map((x) => hostLabel(x.t.host)))].join(', ')) + ' will be deleted forever.', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Empty Trash', kind: 'ed', icon: 'trash', cb: () => F.purgeRows(r) }] }); }
  });

  /* ---- share links ---- */
  function tShare() {
    const rows = X.shares.map((s, i) => { const live = s.exp > E.now(); return '<div class="lrow' + (live ? '' : ' dim') + '"><div class="gi">' + ic(live ? 'link' : 'clock') + '</div><div class="dm"><b>' + esc(s.name) + '</b><small>' + esc(hostLabel(s.host)) + ' · ' + (live ? 'expires in ' + left(s.exp - E.now()) : 'expired ' + rel(s.exp)) + ' · <span class="mono">' + esc(U.parentOf(s.path)) + '</span></small></div><button class="btn sm e" data-fx="share.revoke|' + i + '">' + (live ? 'Turn off' : 'Remove') + '</button></div>'; }).join('');
    return '<div class="fxc">' + head('link', 'Share links', 'Temporary download links for single files', '<button class="btn sm f" data-fx="share.new">' + ic('plus') + 'New link from selection</button>') + errLine(X.sharesErr) + (rows || (X.sharesBusy && !X.sharesAt ? empty('link', 'Loading…', '') : empty('link', 'No links yet', 'Right-click a file in Files and choose “Share link…”. The server keeps only a hash of each link, so a link can be turned off here but not shown again.'))) + '</div>';
  }
  Object.assign(H, {
    'share.revoke': (i) => { const s = X.shares[+i]; if (!s) return; const go = () => E.call('share_revoke_link', { host: s.host, tokenHash: s.hash }).then(() => { audit('Share link for ' + s.name + (s.exp > E.now() ? ' turned off' : ' removed')); A.snack((s.exp > E.now() ? 'Turned off the link for ' : 'Removed ') + s.name); X.shares = X.shares.filter((x) => x.hash !== s.hash); A.pageTools(); }).catch(fail); if (s.exp > E.now()) A.dialog({ icon: 'link', cls: 'danger', title: 'Turn off this link?', body: 'Anyone with the link for ' + esc(s.name) + ' will no longer be able to download it. A link can’t be shown again once made.', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Turn off', kind: 'ed', cb: () => { go(); } }] }); else go(); },
    'share.new': () => { const m = A.mainPane; const s = m.selected(); if (!m.online() || m.host === 'local' || s.length !== 1 || s[0].t !== 'file') { A.snack('Select one file on a connected server in Files first.', { error: true, action: 'Open Files', onAction: () => A.go('files') }); return; } A.shareDialog(m.host, m.path, s[0]); }
  });

  /* ---- backup & sync: one-way folder pairs, run by hand through the transfers ---- */
  const ruleState = (r) => { const ids = r.ids || []; if (!ids.length) return 'idle'; const act = ids.some((id) => { const t = E.task(id); return t && !['done', 'failed'].includes(t.state); }); if (!act) { r.failed = ids.filter((id) => { const t = E.task(id); return t && t.state === 'failed'; }).length; r.ids = []; r.last = E.now(); F.persist(); return 'idle'; } return 'running'; };
  function tSync() {
    const nFail = (r) => r.failed || 0;
    const rules = X.rules.map((r) => { const s = ruleState(r); const sv = E.server(r.host); return '<div class="lrow"><div class="gi">' + ic(r.dir === 'up' ? 'upload' : 'download') + '</div><div class="dm"><b>' + esc(r.name) + '</b><small>' + esc(r.local) + ' ' + (r.dir === 'up' ? '→' : '←') + ' ' + esc(sv ? sv.name : r.host) + ':' + esc(r.remote) + '</small><small>' + (s === 'running' ? '<span class="tag pri">Syncing</span>' : r.last ? 'Last run ' + rel(r.last) + (nFail(r) ? ' · ' + plural(nFail(r), 'transfer', 'transfers') + ' failed, see Transfers' : '') : 'Never run') + '</small></div><button class="btn sm f" data-fx="sync.run|' + esc(r.id) + '"' + (s === 'running' ? ' disabled' : '') + '>' + ic('play') + 'Run now</button><button class="ib sm" data-fx="sync.del|' + esc(r.id) + '" aria-label="Delete rule">' + ic('trash') + '</button></div>'; }).join('');
    return '<div class="tgrid"><div class="fxc wide">' + head('refresh', 'Sync rules', 'One-way folder pairs. Run now copies only files that are new or different in size; nothing is deleted.', '<button class="btn sm f" data-fx="sync.new">' + ic('plus') + 'New rule</button>') + (rules || empty('refresh', 'No rules', 'Create a rule to keep a folder on this computer and a folder on a server in step.')) + '</div></div>';
  }
  async function runRule(r) {
    const sv = E.server(r.host); if (!sv || sv.state !== 'online') { A.snack((sv ? sv.name : r.host) + ' isn’t connected.', { error: true, action: 'Servers', onAction: () => A.go('servers') }); return; }
    const src = r.dir === 'up' ? ['local', r.local] : [r.host, r.remote], dst = r.dir === 'up' ? [r.host, r.remote] : ['local', r.local];
    try {
      const [sl, dl] = await Promise.all([E.fs.load(src[0], src[1], true), E.fs.load(dst[0], dst[1], true)]);
      const names = sl.filter((n) => n.t === 'file' && E.fs.canRead(n) && !dl.some((d) => d.n === n.n && d.b === n.b)).map((n) => n.n);
      if (!names.length) { r.last = E.now(); F.persist(); A.snack('“' + r.name + '” is already up to date'); A.pageTools(); return; }
      const res = A.doEnqueue(r.dir, r.host, src[1], dst[1], names); if (res && res.error) { A.snack(res.error, { error: true }); return; }
      r.ids = (res && res.tasks ? res.tasks : []).map((t) => t.id); audit('Sync “' + r.name + '” started: ' + plural(names.length, 'file', 'files')); A.pageTools();
    } catch (e) { fail(e); }
  }
  Object.assign(H, {
    'sync.run': (id) => runRule(X.rules.find((r) => r.id === id)), 'sync.del': (id) => { const gone = X.rules.find((r) => r.id === id); const at = X.rules.indexOf(gone); X.rules = X.rules.filter((r) => r.id !== id); F.persist(); A.pageTools(); if (gone) A.snack('Removed the rule “' + gone.name + '”', { action: 'Undo', onAction: () => { X.rules.splice(Math.min(at, X.rules.length), 0, gone); F.persist(); A.pageTools(); } }); },
    'sync.new': () => {
      let dir = 'up'; const on = online(); if (!on.length) { A.snack('Connect a server first.', { error: true }); return; }
      A.dialog({ icon: 'refresh', title: 'New sync rule', width: 520, body: '<div class="fld"><label for="srn">Name</label><input id="srn" placeholder="e.g. Release builds" spellcheck="false"></div><div class="fld"><label for="srh">Server</label><select id="srh">' + on.map((s) => '<option value="' + esc(s.id) + '">' + esc(s.name) + '</option>').join('') + '</select></div><div class="fld two"><div class="fld" style="margin:0"><label for="srl">This computer</label><input id="srl" value="' + esc(E.settings.downloadDir || (E.local && E.local.path) || '') + '" spellcheck="false"></div><div class="fld" style="margin:0"><label for="srr">On the server</label><input id="srr" value="/" spellcheck="false"></div></div><div class="fld"><label id="srdl">Direction</label><div class="seg" role="group" aria-labelledby="srdl"><button data-d="up" aria-pressed="true" class="on">This computer → server</button><button data-d="down" aria-pressed="false">Server → this computer</button></div></div><span class="err" id="sre" role="alert"></span>', enter: 'go',
        actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Create rule', kind: 'f', cb: (c) => {
          const g = (i) => $(i, c.el).value.trim(); const name = g('#srn') || 'Untitled rule', host = g('#srh'), local = U.norm(g('#srl')), remote = U.norm(g('#srr'));
          let er = $('#sre', c.el); if (!er) { er = document.createElement('div'); er.id = 'sre'; er.className = 'hint bad'; er.setAttribute('role', 'alert'); $('.db', c.el).appendChild(er); } er.textContent = '';
          const lab = c.btn.go.textContent; c.btn.go.disabled = true; c.btn.go.textContent = 'Checking…';
          const chk = (h, p, what) => E.fs.load(h, p).catch((e) => { throw new Error('That folder can’t be opened: ' + p + ' on ' + what + ' (' + e.message + ')'); });
          Promise.all([chk('local', local, 'this computer'), chk(host, remote, 'the server')]).then(() => { X.rules.push({ id: 'r' + Date.now().toString(36), name, local, host, remote, dir, last: 0, ids: [] }); F.persist(); A.snack('Rule “' + name + '” created'); c.close(); A.pageTools(); })
            .catch((e) => { er.textContent = e.message; c.btn.go.disabled = false; c.btn.go.textContent = lab; });
          return false;
        } }],
        onOpen: (c) => c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-d]'); if (b) { dir = b.dataset.d; $$('[data-d]', c.el).forEach((x) => { x.classList.toggle('on', x === b); x.setAttribute('aria-pressed', String(x === b)); }); } }) });
    }
  });

  /* ---- apps on the server ---- */
  async function loadApps() {
    const s = pickHost('appHost'); if (!s) return; const a = X.apps[s.id] = X.apps[s.id] || {}; if (a.busy || (a.at && E.now() - a.at < STALE)) return; a.busy = true; a.err = '';
    try { const r = await E.call('list_host_apps', { host: s.id }); a.cat = r; } catch (e) { a.err = e.message; a.cat = null; }
    a.busy = false; a.at = E.now(); if (st.view === 'tools' && X.tool === 'apps') A.pageTools();
  }
  function tApps() {
    const on = online(); if (!on.length) return noServer(); const s = pickHost('appHost'); const a = X.apps[s.id] || {};
    const chips = '<div class="chips" style="padding:0 0 .75rem">' + on.map((x) => '<button class="chip' + (x.id === s.id ? ' on' : '') + '" data-fx="app.host|' + esc(x.id) + '">' + ic('server') + esc(x.name) + '</button>').join('') + '</div>';
    if (a.err) return chips + empty('zap', 'Could not list apps', a.err);
    if (!a.cat) return chips + empty('zap', 'Loading…', 'Asking ' + esc(s.name) + ' for its apps.');
    const cat = a.cat; const q = (X.appQ || '').toLowerCase(); const apps = cat.apps.filter((x) => !q || (x.name + ' ' + (x.category || '')).toLowerCase().includes(q));
    return chips + '<div class="fxsub" style="padding:0 0 .625rem">' + plural(cat.apps.length, 'app', 'apps') + ' on ' + esc(s.name) + (cat.launchAllowed ? '' : ' · this sign-in may see the list but not start apps') + '</div><input id="appq" class="appq" placeholder="Filter apps" aria-label="Filter apps" value="' + esc(X.appQ || '') + '" spellcheck="false" style="margin:0 0 .75rem;width:100%;max-width:22rem"><div class="appg">' +
      (apps.slice(0, 60).map((x) => '<div class="fxc apc' + (x.launchable && cat.launchAllowed ? '' : ' dim') + '"><div class="fxh"><span class="fxi">' + ic('zap') + '</span><div><b>' + esc(x.name) + '</b><small>' + esc(x.description || '') + '</small></div></div><div class="meta">' + ic('shield-check') + '<span>' + esc(x.category || 'App') + '</span></div><div class="act"><button class="btn sm f" data-fx="app.run|' + esc(x.id) + '"' + (x.launchable && cat.launchAllowed ? '' : ' disabled') + '>' + ic('play') + (X.busyApp === s.id + x.id ? 'Starting…' : 'Launch') + '</button></div></div>').join('') || empty('zap', 'No apps', 'Nothing matches.')) + '</div>' + (apps.length > 60 ? '<div class="fxsub" style="padding:.625rem 0">+ ' + (apps.length - 60) + ' more. Type in the filter to narrow the list.</div>' : '');
  }
  /* The filter redraws the list and puts the typing focus back, so it also reaches apps beyond the first 60. */
  document.addEventListener('input', (e) => { if (!e.target || e.target.id !== 'appq') return; X.appQ = e.target.value; A.pageTools(); const i = document.getElementById('appq'); if (i) { i.focus(); i.setSelectionRange(i.value.length, i.value.length); } });
  Object.assign(H, {
    'app.host': (id) => { X.appHost = id; A.pageTools(); loadApps(); },
    'app.run': (id) => {
      const s = E.server(X.appHost); const app = ((X.apps[s.id] || {}).cat || { apps: [] }).apps.find((x) => x.id === id); if (!app) return;
      A.dialog({ icon: 'zap', title: 'Launch “' + app.name + '” on ' + s.name + '?', width: 480, body: '<div>' + esc(app.description || '') + '</div><div class="hint" style="margin-top:.75rem">It starts on ' + esc(s.name) + ', for the user the agent runs as. Needs the “Launch apps” grant. The launch is recorded in the security log.</div>', enter: 'go',
        actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Launch', kind: 'f', icon: 'play', cb: () => { if (X.busyApp) return; X.busyApp = s.id + id; A.pageTools(); E.call('launch_host_app', { host: s.id, id }).finally(() => { X.busyApp = null; A.pageTools(); }).then(() => { audit('App “' + app.name + '” started on ' + s.name); A.snack('Started “' + app.name + '” on ' + s.name); }).catch(fail); } }] });
    }
  });

  /* ================= settings extras ================= */
  A.settingsExtra = ({ row, seg, sw }) =>
    '<div class="sg"><h3>Security</h3>' + row('Approve new devices here', '', sw('requireApproval')) + row('Pairing code lifetime', '', seg('pairLife', [[2, '2 min'], [5, '5 min'], [10, '10 min']])) +
    row('Pinned certificates', plural(E.servers.filter((s) => s.pinned).length, 'server', 'servers'), '<button class="btn" data-sx="pins">View</button>') + row('Sign out all phones', (() => { if (!X.devAt && !X.devTried && A.loadDevices) { X.devTried = true; Promise.resolve(A.loadDevices()).then(() => { if (A.state.view === 'settings') A.pageSettings(); }).catch(() => {}); } const ps = A.pairServer(); return X.devAt ? phones().length + ' paired' + (ps ? ' on ' + ps.name : '') : 'Counting…'; })(), '<button class="btn e" data-sx="revokeall">Sign out</button>') + '</div>' +
    '<div class="sg"><h3>Files</h3>' + row('Move deleted items to Trash', 'Off means a delete cannot be undone.', sw('trash')) + row('Keep items in Trash for', 'Only used while Trash is on.', '<div' + (S.trash ? '' : ' style="opacity:.38;pointer-events:none" aria-disabled="true"') + '>' + seg('trashDays', [[7, '7 days'], [30, '30 days'], [90, '90 days']]).replace(/<button /g, S.trash ? '<button ' : '<button disabled ') + '</div>') + '</div>' +
    '<div class="sg"><h3>About</h3>' + row('RFE Desktop' + (appVersion ? ' ' + appVersion : ''), 'Tauri · talks to rfe over a pinned TLS connection', '<span style="display:flex;gap:.5rem"><button class="btn" data-sx="about">' + ic('info') + 'Details</button><button class="btn" data-sx="update">' + ic('refresh') + 'Check for updates</button></span>') + '</div>';
  /* the devices that signed in with a code or were approved: phones, not owner logins */
  const phones = () => (X.devices || []).filter((d) => !d.viaLogin && !d.current && !d.revoked);
  A.settingsAction = (a) => {
    if (a === 'pins') { A.dialog({ icon: 'shield-check', title: 'Pinned certificates', width: 560, body: E.servers.filter((s) => s.pinned).map((s) => '<div class="kv2"><div><b>' + esc(s.name) + '</b><small class="mono">SHA-256 ' + esc(s.fp) + '</small></div><button class="ib sm" data-copy="' + esc(s.fp) + '" aria-label="Copy fingerprint">' + ic('copy') + '</button></div>').join('') || '<div class="fxempty"><span>No server is pinned yet.</span></div>', actions: [{ label: 'Close', kind: 'f' }], onOpen: (c) => c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-copy]'); if (b) { A.copy(b.dataset.copy); A.snack('Fingerprint copied'); } }) }); return true; }
    if (a === 'revokeall') {
      const ph = phones(); const srv = A.pairServer();
      if (!ph.length || !srv) { A.snack('No phones are paired'); return true; }
      A.dialog({ icon: 'shield', cls: 'danger', title: 'Sign out ' + ph.length + ' phone' + (ph.length === 1 ? '' : 's') + '?', width: 440, body: 'They lose access to ' + esc(srv.name) + ' immediately and must be paired again.', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Sign out', kind: 'ed', cb: async () => {
        let n = 0; for (const d of ph) { try { await E.call('revoke_device', { host: srv.id, id: d.id, confirmSelf: false }); n++; } catch (e) { fail(e); } }
        if (n) { audit('All phones signed out of ' + srv.name + ' (' + n + ')'); A.snack('Signed out ' + plural(n, 'phone', 'phones')); } A.loadDevices(); A.pageSettings();
      } }] }); return true;
    }
    if (a === 'about' && A.aboutDialog) { A.aboutDialog(); return true; }
    return false;
  };

  /* ================= palette ================= */
  A.paletteExtra = [(C) => {
    C('phone', 'Pair a phone (QR code)…', () => A.pairDialog()); 
    C('trash', 'Open Trash', () => { X.tool = 'trash'; A.go('tools'); }); C('link', 'Share links', () => { X.tool = 'share'; A.go('tools'); }); C('refresh', 'Backup & sync', () => { X.tool = 'sync'; A.go('tools'); }); C('zap', 'Host apps', () => { X.tool = 'apps'; A.go('tools'); }); C('pie', 'Storage and duplicates', () => { X.tool = 'store'; A.go('tools'); });
    C('sun', 'Theme: Light', () => A.setTheme('light')); C('moon', 'Theme: Dark', () => A.setTheme('dark')); C('monitor', 'Theme: Match system', () => A.setTheme('system'));
    X.favs.forEach((f) => C('star', 'Favorite: ' + f.name, () => A.openPath(f.host, f.path, f.t !== 'dir'), '', hostLabel(f.host)));
  }];

  /* live refresh: offline copies and sync rules show their transfer */
  E.on((t) => { if (t === 'tick' && st.view === 'tools' && !A.paused && ((X.tool === 'sync' && X.rules.some((r) => (r.ids || []).length)) || (X.tool === 'fav' && X.pins.some((p) => A.pinProg(p) < 1)))) { if (!A.fxT || Date.now() - A.fxT > 900) { A.fxT = Date.now(); A.pageTools(); } } });
  /* the Devices page depends on which server is online: redraw when that changes */
  let devKey = '';
  E.on((t) => { if (t !== 'servers') return; const k = online().map((x) => x.id).join(); if (k === devKey) return; devKey = k; if (st.view === 'devices') { clearTimeout(A.devT); A.devT = setTimeout(() => { A.pageDevices(); A.loadDevices(); }, 150); } });
  A.renderRail();
})();
