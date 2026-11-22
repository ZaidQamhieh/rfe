/* RFE v5 - the Overview, Transfers and Search pages of the v5 mockup, drawn over the existing client state.
   Nothing here talks to a computer by itself: the pages read what the engine already knows (E.tasks, E.servers,
   the lists the Devices page loads, the pairing inbox, the recent-files call) and every button reuses an existing
   action (the transfer card actions in A.cardClick, the pairing handlers in features.js, A.openPath, A.transferNames).
   The one thing it keeps itself is a small list of speed samples for the Overview chart, taken in the browser while
   the app is open; the chart is empty until a transfer really moves.
   It is used only while the window wears the v5 wrapper (class "d5" on #app); otherwise the old pages stay. */
rfeModule('v5-transfers', ['v5-files', 'fx6'], function () {
  'use strict';
  const A = window.A; if (!A) return;
  const E = A.E, U = A.U, $ = A.$, esc = A.esc, ic = A.ic, fic = A.fic, st = A.state;
  const V = (A.v5Pages = { pick: null, rf: null, rfTok: null, recent: { list: [], at: 0, busy: false }, samples: [] });
  const VIEWS = { home: 1, transfers: 1, search: 1 };
  const em = A.v5em; /* a mockup colour picture, n pixels wide (v5-shell.js) */
  /* The picture of a computer: a laptop for Windows and macOS machines, else a desktop. */
  const pcPic = (s, n) => em(s && /^(Windows|macOS|Darwin)/i.test(s.os || '') ? 'laptop' : 'desktop_computer', n);

  V.on = A.v5on; /* true while the v5 wrapper is on the window (v5-shell.js) */
  /* True when this file draws the given page. */
  V.draws = (v) => !!VIEWS[v] && V.on();

  /* ---- small pieces of markup, the same classes the mockup uses ---- */
  const btn = (label, o) => { o = o || {}; return '<button type="button" class="k-btn' + (o.k ? ' ' + o.k : '') + '"' + (o.a || '') + (o.dis ? ' disabled' : '') + '>' + (o.i ? ic(o.i, { size: 16 }) : '') + (label ? '<span>' + label + '</span>' : '') + '</button>'; };
  const head = (t, sub, right) => '<header class="k-ph"><div><h1 class="k-title">' + t + '</h1>' + (sub ? '<p class="k-sub">' + sub + '</p>' : '') + '</div><div class="k-ph-r">' + (right || '') + '</div></header>';
  const bar = (p) => '<span class="k-bar"><i style="width:' + Math.max(0, Math.min(100, p)).toFixed(0) + '%"></i></span>';
  const dot = (k) => '<i class="k-dot ' + k + '"></i>';
  const empty = (pic, t, s, acts) => '<div class="b-empty">' + em(pic, 40) + '<b>' + t + '</b><p>' + s + '</p>' + (acts ? '<div class="k-row">' + acts + '</div>' : '') + '</div>';
  const ib = (a, icon, label, extra) => '<button type="button" class="k-ib" data-x="' + a + '"' + (extra || '') + ' aria-label="' + esc(label) + '" data-tip="' + esc(label) + '">' + ic(icon, { size: 16 }) + '</button>';
  const plural = A.plural;
  const two = (n) => String(n).padStart(2, '0');
  const hhmm = (ms) => { const d = new Date(ms); return two(d.getHours()) + ':' + two(d.getMinutes()); };
  /* A pairing code in groups of four ("4829 1307"). */
  const grp = (c) => String(c || '').replace(/\s+/g, '').replace(/(.{4})(?=.)/g, '$1 ');

  /* ====================================================================== Transfers ====================================================================== */
  const ORDER = { attn: 0, active: 1, queued: 2, done: 3 };
  /* All transfers in the mockup's order: needs you, moving, waiting, finished. */
  const ordered = () => E.tasks.slice().sort((a, b) => (ORDER[A.groupOf(a)] - ORDER[A.groupOf(b)]) || (a.queuedAt - b.queuedAt));
  const pctOf = (t) => (t.bytes ? Math.min(100, t.done / t.bytes * 100) : 0);
  /* The plain word for where a transfer stands. */
  const word = (t) => {
    switch (t.state) {
      case 'conflict': return 'Action required';
      case 'paused': case 'waiting': return 'Paused';
      case 'failed': return 'Failed';
      case 'done': return 'Done';
      case 'cancelled': return 'Cancelled';
      case 'queued': return 'Waiting';
      default: return pctOf(t) >= 99.9 ? 'Checking' : 'Moving';
    }
  };
  /* The right-hand status of a list row: a percentage while moving, else the word. */
  const rowStatus = (t) => {
    const w = word(t);
    if (t.state === 'failed') return '<span class="bad">' + w + '</span>';
    if (t.state === 'conflict' || t.state === 'waiting') return '<span class="warn">' + w + '</span>';
    return t.state === 'running' && w === 'Moving' ? Math.floor(pctOf(t)) + '%' : w;
  };
  /* "computer · folder": the far end of a transfer, as the mockup writes it. */
  const whereOf = (t) => A.hostName(t.host) + ' · ' + (t.dir === 'up' ? t.dstDir : t.srcDir || t.dstDir);
  /* The line under the state in the right panel: what is happening, in plain words. */
  function detailOf(t) {
    const sv = A.hostName(t.host);
    switch (t.state) {
      case 'running': return pctOf(t) >= 99.9 ? 'Checking the file' : U.fmtBytes(t.done) + ' of ' + U.fmtBytes(t.bytes) + ' · ' + U.fmtSpeed(t.speed) + (isFinite(E.eta(t)) ? ' · ' + U.fmtDur(E.eta(t)) + ' left' : '');
      case 'queued': return 'Waiting for a free slot · ' + U.fmtBytes(t.bytes);
      case 'paused': return 'Paused at ' + Math.floor(pctOf(t)) + '% · ' + U.fmtBytes(t.done) + ' of ' + U.fmtBytes(t.bytes);
      case 'waiting': return (t.msg || sv + ' stopped answering') + '. It continues when ' + sv + ' is back';
      case 'failed': return (t.msg || 'It did not finish') + (t.fix ? '. ' + t.fix : '');
      case 'conflict': return t.conflict && t.conflict.name && t.isDir ? '“' + t.conflict.name + '” already exists on ' + sv : 'A file with this name already exists on ' + sv;
      case 'done': return U.fmtBytes(t.bytes) + ' · ' + U.fmtDur(((t.finishedAt || 0) - (t.startedAt || t.queuedAt)) / 1000) + (t.verified ? ' · Checksum verified' : '');
      default: return t.msg || '';
    }
  }
  /* The buttons a transfer offers, wired to the existing card actions (data-t inside a data-tid wrapper). */
  function actionsOf(t) {
    const b = (a, l, i, k) => '<button type="button" class="k-btn' + (k ? ' ' + k : '') + '" data-t="' + a + '">' + ic(i, { size: 16 }) + '<span>' + l + '</span></button>';
    switch (t.state) {
      case 'conflict': return b('keep', 'Keep both', 'copy', 'pri') + b('replace', 'Replace', 'refresh') + b('skip', 'Skip', 'undo', 'ghost');
      case 'failed': return b('retry', 'Try again', 'refresh', 'pri') + b('dismiss', 'Remove', 'trash', 'ghost');
      case 'waiting': return (A.S.autoReconnect === false ? b('servers', 'Open Devices', 'server', 'pri') : '<button type="button" class="k-btn pri" disabled>' + ic('play', { size: 16 }) + '<span>Resumes when ' + esc(A.hostName(t.host)) + ' is back</span></button>') + b('cancel', 'Cancel', 'x', 'ghost');
      case 'paused': return b('resume', 'Resume', 'play', 'pri') + b('cancel', 'Cancel', 'x', 'ghost');
      case 'queued': return b('pause', 'Pause', 'pause', 'pri') + b('cancel', 'Cancel', 'x', 'ghost');
      case 'done': return b('reveal', t.dir === 'down' ? 'Show in folder' : 'Show in Files', 'folder', 'pri') + b('dismiss', 'Remove from list', 'x', 'ghost');
      case 'cancelled': return b('dismiss', 'Remove from list', 'x', 'ghost');
      default: return pctOf(t) >= 99.9 ? b('cancel', 'Cancel', 'x', 'ghost') : b('pause', 'Pause', 'pause', 'pri') + b('reveal', 'Show in Files', 'folder') + b('cancel', 'Cancel', 'x', 'ghost');
    }
  }
  /* The right panel for one transfer. */
  function panelOf(t) {
    const up = t.dir === 'up', sv = A.hostName(t.host);
    const sub = t.state === 'done' && !up ? 'Downloaded to this computer · ' + esc(t.dstDir) : (up ? 'Upload to ' : 'Download from ') + esc(sv) + ' · ' + esc(up ? t.dstDir : t.srcDir || t.dstDir);
    const showBar = t.state === 'running' || t.state === 'paused' || t.state === 'waiting' || t.state === 'done';
    return em(up ? 'outbox_tray' : 'inbox_tray', 40) + '<h2 data-nolocal>' + esc(t.name) + '</h2><p class="k-sub">' + sub + '</p>' + (showBar ? '<span data-f="pbar">' + bar(t.state === 'done' ? 100 : pctOf(t)) + '</span>' : '') +
      '<dl class="k-kv pdl"><dt>State</dt><dd data-f="pstate">' + word(t) + (t.state === 'running' && pctOf(t) < 99.9 ? ' · ' + Math.floor(pctOf(t)) + '%' : '') + '</dd><dt>Details</dt><dd data-f="pdet">' + esc(detailOf(t)) + '</dd></dl>' +
      '<div class="k-dact" data-tid="' + esc(t.id) + '">' + actionsOf(t) + '</div>' +
      A.v5tech([['From', up ? 'This computer · ' + t.srcDir : sv + ' · ' + (t.srcDir || '')], ['To', up ? sv + ' · ' + t.dstDir : 'This computer · ' + t.dstDir], ['Size', U.fmtBytes(t.bytes) + (t.isDir ? ' · ' + plural(t.files || 0, 'file', 'files') : ''), true]]);
  }
  /* The notice for transfers that were still going when the window was last closed and are waiting for an answer (kept by the journal). */
  function banner() {
    const n = ((A.journal && A.journal.retained) || []).length; if (!n) return '';
    return '<div class="k-banner">' + ic('clock', { size: 16 }) + '<span><b>' + plural(n, 'transfer was', 'transfers were') + ' left unfinished</b> when RFE closed.</span>' + btn('Carry on…', { k: 'sm', a: ' data-x="carry"' }) + '</div>';
  }
  /* The whole Transfers page: header buttons, the list and the panel on the right. */
  function drawTransfers(stage) {
    const T = ordered(); const nRun = T.filter((t) => t.state === 'running').length, nQ = T.filter((t) => t.state === 'queued').length, nPaused = T.filter((t) => t.state === 'paused').length;
    const nFail = T.filter((t) => t.state === 'failed').length, nDone = T.filter((t) => t.state === 'done' || t.state === 'cancelled').length, nNeed = T.filter((t) => A.groupOf(t) === 'attn').length;
    const sub = T.length ? (nRun ? nRun + ' moving' +(nNeed ? ' · ' + nNeed + ' need you' : '') : nNeed ? nNeed + ' need you' : nQ ? plural(nQ, 'waiting', 'waiting') : nPaused ? nPaused + ' paused' : 'Nothing moving') : 'Nothing moving';
    /* A queued transfer can be paused too, so Pause all stays on while only queued ones are left; Resume all follows the paused ones. */
    const acts = btn('Pause all', { i: 'pause', k: 'sm', a: ' data-pa="pauseall"', dis: !(nRun + nQ) }) + btn('Resume all', { i: 'play', k: 'sm', a: ' data-pa="resumeall"', dis: !nPaused }) + btn('Retry failed', { i: 'refresh', k: 'sm', a: ' data-pa="retryfailed"', dis: !nFail }) + btn('Clear done', { i: 'trash', k: 'sm', a: ' data-pa="clear"', dis: !nDone });
    if (!T.length) { stage.innerHTML = '<div class="v5main">' + head('Transfers', sub, '') + banner() + empty('updown_arrow', 'Nothing moving', 'Uploads and downloads appear here. They resume on their own after a lost connection or a restart.') + '</div>'; return; }
    if (!T.some((t) => t.id === V.pick)) V.pick = T[0].id;
    const sel = T.find((t) => t.id === V.pick);
    const rows = T.map((t) => '<div class="ro' + (t.id === V.pick ? ' sel' : '') + '" role="button" tabindex="0" data-x="pick" data-id="' + esc(t.id) + '">' + em(t.dir === 'up' ? 'outbox_tray' : 'inbox_tray', 24) + '<span><b data-nolocal>' + esc(t.name) + '</b><small data-nolocal>' + esc(whereOf(t)) + '</small></span><span class="st" data-f="rst">' + rowStatus(t) + '</span></div>').join('');
    stage.innerHTML = '<div class="v5main">' + head('Transfers', sub, acts) + banner() + '<div class="li">' + rows + '</div></div><aside class="v5rail" aria-label="Transfer details">' + panelOf(sel) + '</aside>';
  }
  /* Keeps the numbers of the open Transfers page moving without redrawing it (a redraw would drop hover and focus). */
  function patchTransfers(stage) {
    for (const t of E.tasks) {
      if (t.state !== 'running') continue;
      const r = stage.querySelector('.ro[data-id="' + t.id + '"] [data-f="rst"]'); if (r) { const v = rowStatus(t); if (r.innerHTML !== v) r.innerHTML = v; }
      if (t.id !== V.pick) continue;
      const set = (f, v, html) => { const n = stage.querySelector('[data-f="' + f + '"]'); if (n && (html ? n.innerHTML : n.textContent) !== v) { if (html) n.innerHTML = v; else n.textContent = v; } };
      set('pbar', bar(pctOf(t)), true); set('pstate', word(t) + (pctOf(t) < 99.9 ? ' · ' + Math.floor(pctOf(t)) + '%' : '')); set('pdet', detailOf(t));
    }
  }

  /* ====================================================================== Overview ====================================================================== */
  /* ---- speed samples: the current speed of the running transfers, noted every two seconds while the app is open ---- */
  const WINDOW = 15 * 60 * 1000;
  let lastSample = 0;
  function sample() {
    const now = Date.now(); if (now - lastSample < 1900) return; lastSample = now;
    let up = 0, dn = 0; for (const t of E.tasks) if (t.state === 'running') { if (t.dir === 'up') up += t.speed || 0; else dn += t.speed || 0; }
    V.samples.push({ t: now, up, dn }); while (V.samples.length && now - V.samples[0].t > WINDOW) V.samples.shift();
  }
  const BYTES_PER_MB = 1048576;
  const NICE = [1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000];
  /* The speed chart as an SVG: download and upload lines in MB/s over the last minutes. */
  function chartSvg() {
    const S = V.samples, mb = BYTES_PER_MB; const live = S.some((s) => s.up > 0 || s.dn > 0);
    const peak = Math.max.apply(null, [0].concat(S.map((s) => Math.max(s.up, s.dn) / mb))); const top = NICE.find((n) => n >= peak) || Math.ceil(peak);
    const t1 = S.length ? S[S.length - 1].t : Date.now(), t0 = Math.min(S.length ? S[0].t : t1, t1 - A.T.minute);
    const X = (t) => 40 + (t - t0) / (t1 - t0) * 800, Y = (v) => 200 - v / top * 180;
    const line = (k) => S.map((s, i) => (i ? 'L' : 'M') + X(s.t).toFixed(1) + ' ' + Y(s[k] / mb).toFixed(1)).join('');
    const fmt = (v) => String(+v.toFixed(1));
    const grid = [0, 1 / 3, 2 / 3, 1].map((f) => '<path d="M40 ' + Y(top * f).toFixed(1) + 'H840" stroke="var(--line)"/><text x="0" y="' + (Y(top * f) + 4).toFixed(1) + '">' + fmt(top * f) + '</text>').join('') +
      [0, 1, 2, 3].map((i) => { const t = t0 + (t1 - t0) * i / 3; return '<text x="' + (40 + i * 266 - 16) + '" y="226">' + hhmm(t) + '</text>'; }).join('');
    return '<svg viewBox="0 0 860 232" role="img" aria-label="Transfer speed over the last minutes">' + grid + (live ? '<path d="' + line('dn') + '" fill="none" stroke="var(--ac)" stroke-width="2"/><path d="' + line('up') + '" fill="none" stroke="var(--t3)" stroke-width="1.5"/>' : '') + '</svg>' + (live ? '' : '<div class="ov">The line starts with your first transfer</div>');
  }
  const pcTone = (state) => (A.PC_STATE[state] || [])[0] || 'off'; /* the dot tone of a connection state, from pages.js */
  /* Adds each finished transfer to the total once, so clearing the list does not take it off "Moved today". */
  V.movedBytes = 0; const counted = new Set();
  function tally() { for (const t of E.tasks) if (t.state === 'done' && !counted.has(t.id)) { counted.add(t.id); V.movedBytes += t.bytes; } }
  /* The four numbers across the top, all from what the app knows right now. */
  function stats() {
    const T = E.tasks, run = T.filter((t) => t.state === 'running'), S = E.servers, X = A.X || {};
    tally(); const moved = V.movedBytes + run.reduce((a, t) => a + t.done, 0);
    const etas = run.map((t) => E.eta(t)).filter(isFinite); const left = etas.length ? Math.max.apply(null, etas) : 0;
    const up = S.filter((s) => s.state === 'online').length;
    const ask = S.filter((s) => s.state === 'trust' || s.state === 'refused').map((s) => s.name + ' needs your OK'), off = S.filter((s) => s.state !== 'online' && s.state !== 'trust' && s.state !== 'refused').map((s) => s.name + ' off');
    const note = ask.concat(off).slice(0, 2).join(' · ') + (ask.length + off.length > 2 ? ' · and ' + (ask.length + off.length - 2) + ' more' : '');
    const dev = (X.devices || []).filter((d) => !d.revoked).length, asking = (X.inbox || []).length;
    return [
      ['Moving now', run.length ? U.fmtSpeed(E.totalSpeed()) : '—', run.length ? plural(run.length, 'transfer', 'transfers') + (left ? ' · ' + U.fmtDur(left) + ' left' : '') : T.length ? 'nothing moving now' : 'nothing to move yet', 'spd'],
      ['Moved today', U.fmtBytes(moved), 'since RFE started'],
      ['Computers up', S.length ? up + ' of ' + S.length : '0', S.length ? note || 'all connected' : 'add one to start'],
      ['Devices', S.length ? (X.devAt ? String(dev) : '—') + (X.devAt ? ' allowed' : '') : '0', asking ? plural(asking, 'is', 'are') + ' asking to pair' : S.length ? (X.devAt ? 'none asking to pair' : 'not loaded yet') : 'pair a phone after'],
    ];
  }
  /* What waits for the person: phones asking to join first, then transfers that stopped for a choice or an error. */
  function needs() {
    const X = A.X || {}; const out = [];
    (X.inbox || []).forEach((q, i) => out.push('<div class="need">' + em('mobile_phone', 20) + '<div>' + esc(q.label || q.name || 'A phone') + ' asks to join ' + esc(A.hostName(q.host)) + '<small>Same number: <b class="b-n">' + esc(grp(q.code)) + '</b></small></div>' + btn('Allow…', { k: 'sm pri', a: ' data-fx="dev.approve|' + i + '"' }) + '</div>'));
    E.tasks.filter((t) => A.groupOf(t) === 'attn').forEach((t) => {
      const sv = A.hostName(t.host); const act = t.state === 'conflict' ? btn('Keep both', { k: 'sm', a: ' data-t="keep"' }) : t.state === 'failed' ? btn('Retry', { k: 'sm', i: 'refresh', a: ' data-t="retry"' }) : '';
      const title = t.state === 'conflict' ? esc(t.name) + ' exists on ' + esc(sv) : t.state === 'failed' ? esc(t.name) + ' did not finish' : esc(t.name) + ' is paused';
      out.push('<div class="need" data-tid="' + esc(t.id) + '">' + (t.state === 'conflict' ? em('page_facing_up', 20) : '<span class="b-ic">' + ic(t.state === 'waiting' ? 'wifi-off' : 'alert', { size: 18 }) + '</span>') + '<div>' + title + '<small>' + esc(detailOf(t)) + '</small></div>' + act + '</div>');
    });
    return out;
  }
  /* The recent files of the computers that are up: the latest files moved to or from each one by this app (Engine.recentFiles,
     built from finished transfers, so a file shows under its own computer only and the disks are never scanned). Redraws the
     Overview only when the list changed. */
  function loadRecent() {
    const R = V.recent, hosts = E.servers.filter((s) => s.state === 'online');
    const list = [].concat.apply([], hosts.map((s) => E.recentFiles(s.id, { limit: 6 }))).filter((e) => !e.isDir).sort((a, b) => (Date.parse(b.modified) || 0) - (Date.parse(a.modified) || 0)).slice(0, 3);
    const key = (l) => l.map((e) => e.host + '|' + e.path + '|' + e.modified).join('\n');
    const changed = key(list) !== key(R.list); R.list = list; R.at = Date.now();
    if (changed && st.view === 'home') drawHome($('#stage'));
  }
  /* Recent-file tiles show the picture itself for an image (a small thumbnail asked from its computer, kept for the session); other files keep the type icon. */
  const RTHUMBS = new Map();
  let armHome = () => {}; /* set at the end of the module: starts the Overview's timer */
  function paintRecentThumbs(stage) {
    const keyOf = (r) => r.host + '|' + r.path + '|' + (r.modified || '');
    /* the picture box of the tile that shows `key` right now (the list may have been reordered or redrawn while a thumbnail was on its way) */
    const boxOf = (key) => { const t = Array.from(stage.querySelectorAll('.rcf')).find((el) => { const r = V.recent.list[+el.dataset.i]; return !!r && keyOf(r) === key; }); return t ? $('.rct', t) : null; };
    V.recent.list.forEach((r) => {
      if (U.kindOf({ n: r.name, t: 'file' }) !== 'image' || !r.path) return;
      const key = keyOf(r); if (!boxOf(key)) return;
      const put = (src) => { const b = boxOf(key); if (src && b) { b.classList.add('th'); b.innerHTML = '<img alt="" src="' + esc(src) + '">'; } };
      if (RTHUMBS.has(key)) return put(RTHUMBS.get(key));
      E.call('files_thumb', { host: r.host, path: r.path, size: 256 }).then((src) => { if (typeof src === 'string' && /^data:image\//.test(src)) { RTHUMBS.set(key, src); if (RTHUMBS.size > 30) RTHUMBS.delete(RTHUMBS.keys().next().value); put(src); } }, A.noop);
    });
  }
  /* Asks for what the Overview shows that the app does not hold yet: the devices of the main computer and the recent files. */
  function warmHome() {
    const X = A.X;
    if (X && A.loadDevices && A.pairServer && A.pairServer() && !X.devBusy && E.now() - (X.devAt || 0) > A.T.minute) Promise.resolve(A.loadDevices()).then(() => { if (st.view === 'home') drawHome($('#stage')); }).catch(A.noop);
    loadRecent();
  }
  /* The Overview page: four numbers, the speed chart, what waits for you, the computers and the recent files. */
  function drawHome(stage) {
    const S = E.servers, N = needs(), R = V.recent.list;
    const pcs =S.map((s) => { const has = s.disk && s.disk[1] > 0, pct = has ? Math.round(s.disk[0] / s.disk[1] * 100) : 0; return '<div class="pcr" role="button" tabindex="0" data-x="pc" data-id="' + esc(s.id) + '">' + dot(pcTone(s.state)) + '<span data-nolocal>' + pcPic(s, 16) + esc(s.name) + '</span>' + (has ? bar(pct) + '<small>' + pct + '% full</small>' : '<span></span><small>—</small>') + '</div>'; }).join('');
    const rec = R.map((r, i) => '<div class="rcf" role="button" tabindex="0" data-x="rec" data-i="' + i + '"><span class="rct">' + fic({ n: r.name, t: 'file' }) + '</span><b data-nolocal>' + esc(r.name) + '</b>' + esc(A.hostName(r.host)) + (r.modified ? ' · ' + A.ago(Date.parse(r.modified)) : '') + '</div>').join('');
    stage.innerHTML = '<div class="db">' + stats().map((s, i) => '<div class="stat"><small>' + em(['updown_arrow', 'check_mark_button', 'desktop_computer', 'mobile_phone'][i] || 'bar_chart', 36) + s[0] + '</small><b' + (s[3] ? ' data-f="' + s[3] + '"' : '') + '>' + esc(s[1]) + '</b><span' + (s[3] ? ' data-f="' + s[3] + 's"' : '') + '>' + esc(s[2]) + '</span></div>').join('') +
      '<div class="chart"><h2>' + em('bar_chart', 32) + 'Speed <small><i class="lgd dn"></i> Download · <i class="lgd up"></i> Upload · MB/s · last 15 minutes</small></h2><div data-f="chart">' + chartSvg() + '</div></div>' +
      '<div class="card"><h2>' + (N.length ? '' : em('inbox_tray', 32)) + 'Waiting for you ' + (N.length ? '<em class="k-badge bad">' + N.length + '</em>' : '') + '</h2>' + (N.length ? N.join('') : '<div class="nil">' + 'Nothing waits for you.<small>Phones asking to join and files that need a choice show up here.</small></div>') + '</div>' +
      '<div class="card w"><h2>' + (S.length ? '' : em('desktop_computer', 32)) + 'Computers</h2>' + (S.length ? pcs : '<div class="nil">' + 'No computers yet.' + btn('Add a computer', { i: 'plus', k: 'sm pri', a: ' data-x="add"' }) + '</div>') + '</div>' +
      '<div class="card w"><h2>' + (rec ? '' : em('page_facing_up', 32)) + 'Recent files</h2>' + (rec ? '<div class="rc">' + rec + '</div>' : '<div class="nil">' + 'Files you open or move show here.</div>') + '</div></div>';
    paintRecentThumbs(stage); armHome();
  }
  /* Moves the live numbers of an open Overview (speed, chart) without redrawing the rest. */
  function patchHome(stage) {
    const s = stats()[0]; const b = stage.querySelector('[data-f="spd"]'), sub = stage.querySelector('[data-f="spds"]'), ch = stage.querySelector('[data-f="chart"]');
    if (b && b.textContent !== s[1]) b.textContent = s[1]; if (sub && sub.textContent !== s[2]) sub.textContent = s[2]; if (ch) ch.innerHTML = chartSvg();
  }

  /* ====================================================================== Search ====================================================================== */
  const KIND = { Pictures: ['image'], Videos: ['video'], Documents: ['doc'], Folders: ['dir'], Text: ['text', 'code'] };
  const DAY = A.T.day;
  const FILTER_MB = 1e6; /* the filter's megabyte is the decimal one the sizes are shown in (U.fmtBytes) */
  const mbOf = (n) => (n.t === 'dir' ? -1 : (n.b || 0) / FILTER_MB);
  /* Which results the table's column filters let through. The Name filter starts as the search itself, which the engine has already applied, so it only narrows once it is edited. */
  const passes = (r) => {
    const f = V.rf; const n = r.node; if (!f) return true; const q = A.searchRun && A.searchRun.q;
    return (f.kind === 'Any' || (KIND[f.kind] || []).indexOf(U.kindOf(n)) >= 0) && (f.name === q || n.n.toLowerCase().includes(f.name.toLowerCase())) && (f.pc === 'Any' || r.host === f.pc) &&
      (r.dir || '').toLowerCase().includes(f.folder.toLowerCase()) && (f.when === 'Any time' || (Date.now() - (n.mod || 0)) <= (f.when === 'This week' ? 7 : 31) * DAY) && (f.size === 'Any' || mbOf(n) > (f.size === 'Over 1 MB' ? 1 : 100));
  };
  const crumbs = (dir) => String(dir || '').split('/').filter(Boolean).slice(-2).join(' › ') || '/';
  /* The folder, size and date written under a result's name. */
  const meta = (r) => esc(crumbs(r.dir)) + ' · ' + A.itemSize(r.node) + ' · ' + U.fmtDate(r.node.mod);
  /* The folder and download buttons of a result; pre is 's' on the page and 'p' in the panel, so each one's clicks stay its own. */
  const rowActs = (r, i, pre) => { const p = pre || 's'; return '<span class="ia">' + ib(p + 'folder', 'folder', 'Open the folder', ' data-i="' + i + '"') + (r.host !== 'local' ? ib(p + 'dl', 'download', 'Download', ' data-i="' + i + '"') : '') + '</span>'; };
  /* Results grouped by computer, the way the mockup lists them in the Search panel: 3 per computer, the rest linked to the full table. */
  function groupsHtml(sr, pre, keep) {
    const p = pre || 'p', per = 3, ok = keep || (() => true);
    const hs = sr.hosts.filter((h) => sr.results.some((r) => r.host === h && ok(r)));
    return hs.map((h) => {
      const idx = []; sr.results.forEach((r, i) => { if (r.host === h && ok(r)) idx.push(i); }); const shown = idx.slice(0, per);
      return '<div class="s-g">' + dot('ok') + '<span data-nolocal>' + esc(A.hostName(h)) + '</span><span class="mut">· ' + idx.length + '</span></div>' +
        shown.map((i) => { const r = sr.results[i]; return '<div class="s-r" role="button" tabindex="0" data-x="' + p + 'row" data-i="' + i + '"><span class="k-fic">' + fic(r.node) + '</span><span><b>' + A.hl(r.node.n, sr.q) + (!E.fs.canRead(r.node) ? ' <em class="k-pill">' + esc(r.node.own) + ' only</em>' : '') + '</b><small data-nolocal>' + meta(r) + '</small></span>' + rowActs(r, i, p) + '</div>'; }).join('') +
        (idx.length > per ? '<a class="s-mo" role="button" tabindex="0" data-x="' + p + 'more" data-h="' + esc(h) + '">' + (idx.length - per) + ' more on ' + esc(A.hostName(h)) + '</a>' : '');
    }).join('');
  }
  /* The kinds of result the search engine can narrow to (its filter names); the page and the panel both list them. */
  const FILTERS = [['all', 'Everything'], ['folders', 'Folders'], ['packages', 'Packages'], ['media', 'Media'], ['archives', 'Archives'], ['big', 'Over 100 MB']];
  /* Saves the search being shown, or removes it from the saved ones if it is there (the saved-searches handler does the work). */
  function saveSearch(sr) { const tmp = document.createElement('button'); tmp.dataset.fx = 'q.save|' + sr.q; tmp.hidden = true; document.body.appendChild(tmp); tmp.click(); tmp.remove(); }
  /* The table's filter defaults: every column on "Any", and the Name filter holding the search text. */
  const rfFor = (q, host) => ({ kind: 'Any', name: q || '', pc: host || 'Any', folder: '', when: 'Any time', size: 'Any' });
  /* The table's header cells (a filter in each) for the given computers; its data cells are drawn apart so typing in a filter keeps the focus. */
  function tableHead(hosts) {
    const f = V.rf; const opt = (k, a) => '<select aria-label="' + k + '" data-rf="' + k + '">' + a.map((o) => '<option value="' + esc(o[0]) + '"' + (f[k] === o[0] ? ' selected' : '') + '>' + esc(o[1]) + '</option>').join('') + '</select>';
    const same = (a) => a.map((x) => [x, x]); const col = (t, c) => '<div class="hc"><b>' + t + '</b>' + c + '</div>';
    return col('Kind', opt('kind', same(['Any'].concat(Object.keys(KIND))))) + col('Name', '<input value="' + esc(f.name) + '" placeholder="Name contains" aria-label="Name contains" data-rf="name">') +
      col('Computer', opt('pc', [['Any', 'Any']].concat(hosts.map((h) => [h, A.hostName(h)])))) + col('Folder', '<input value="' + esc(f.folder) + '" placeholder="Inside…" aria-label="Folder contains" data-rf="folder">') +
      col('Changed', opt('when', same(['Any time', 'This week', 'This month']))) + col('Size', opt('size', same(['Any', 'Over 1 MB', 'Over 100 MB'])));
  }
  const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  /* "1 Nov": the day and month a file changed, with the year only when it is not this year. */
  const shortDate = (ms) => { if (!ms) return '—'; const d = new Date(ms); return d.getDate() + ' ' + MONTHS[d.getMonth()] + (d.getFullYear() !== new Date(E.now()).getFullYear() ? ' ' + d.getFullYear() : ''); };
  /* A file's size in the table's short form ("3.8 MB", "1 KB"); a folder says how many items it holds, or "—" when that is not known. */
  function shortSize(n) {
    if (n.t === 'dir') return A.itemSize(n);
    const b = n.b || 0; if (b < 1000) return b + ' B';
    const u = ['KB', 'MB', 'GB', 'TB']; let v = b, i = -1; do { v /= 1000; i++; } while (v >= 1000 && i < 3);
    const rd = (x) => (x < 10 ? Math.round(x * 10) / 10 : Math.round(x));
    if (rd(v) >= 1000 && i < 3) { v /= 1000; i++; } /* 999.96 KB rounds to 1000: it is "1 MB", not "1,000 KB" */
    return rd(v).toLocaleString(U.lang() + '-u-nu-latn') + ' ' + u[i];
  }
  /* The colour picture of a file or folder, as Files draws it. */
  const kindPic = (n) => { const T = A.v5Files; return '<span class="k-fic k-fic-' + (n.t === 'dir' ? 'dir' : 'file') + '">' + (T && T.emKind ? T.emKind(U.kindOf(n), n.n, 18) : fic(n)) + '</span>'; };
  /* The data cells of the table, six per result that the column filters let through: picture, name, computer, folder, date, size with the open-folder button. */
  const tableCells = (sr) => {
    const out = [];
    sr.results.forEach((r, i) => {
      if (!passes(r)) return; const x = ' data-x="srow" data-i="' + i + '"';
      out.push('<span class="c"' + x + '>' + kindPic(r.node) + '</span><span class="c"' + x + '><span>' + A.hl(r.node.n, (V.rf && V.rf.name) || sr.q) + '</span></span><span class="c mono"' + x + '>' + esc(A.hostName(r.host)) + '</span><span class="c mono"' + x + '><span>' + esc(crumbs(r.dir)) + '</span></span><span class="c mono"' + x + '>' + shortDate(r.node.mod) + '</span><span class="c mono"' + x + '>' + esc(shortSize(r.node)) + '<span class="ia">' + ib('sfolder', 'folder', 'Open the folder', ' data-i="' + i + '"') + '</span></span>');
    });
    return out.join('');
  };
  /* "a", "a and b", "a, b and c". */
  const nameList = (a) => (a.length < 2 ? a.join('') : a.slice(0, -1).join(', ') + ' and ' + a[a.length - 1]);
  /* The Search results page frame: title, the line saying what was searched, Save this search, and the results area (the filter header, then a row per match). */
  function drawSearch(stage) {
    const sr = A.searchRun;
    if (!sr) {
      const extra = A.searchExtra ? A.searchExtra() : '', none = !E.servers.length;
      V.rfTok = null; V.rf = rfFor('');
      stage.innerHTML = head('Search results', none ? 'Nothing to search yet' : 'Press <kbd class="b-kbd">/</kbd> to search your computers', btn('Save this search', { k: 'sm ghost', i: 'star', a: ' data-x="save"', dis: true })) + '<div class="rt">' + tableHead([]) + '</div>' +
        (none ? '<div class="s-no">' + em('desktop_computer', 36) + '<b>Add a computer to search it</b><p>Until then, search looks in this computer’s Downloads folder.</p>' + btn('Add a computer', { i: 'plus', k: 'pri', a: ' data-x="add"' }) + '</div>' : '') + (extra ? '<div class="s-ch">' + extra + '</div>' : '');
      return;
    }
    if (V.rfTok !== sr.tok) { V.rfTok = sr.tok; V.rf = rfFor(sr.q); }
    const saved = A.searchSave ? /aria-pressed="true"/.test(A.searchSave(sr)) : false;
    stage.innerHTML = head('Search results', '<span id="sprog"></span>', btn(saved ? 'Saved' : 'Save this search', { k: 'sm ghost', i: 'star', a: ' data-x="save"' })) + '<div id="sres"></div>';
    drawSearchResults();
  }
  /* Fills the progress line and the table; called again as the search finds more. */
  function drawSearchResults() {
    const sr = A.searchRun, stage = $('#stage'); if (!sr) return; const pg = $('#sprog', stage), rs = $('#sres', stage); if (!pg || !rs) return;
    const where = nameList(sr.hosts.map(A.hostName)), nOk = sr.results.length;
    const out = E.servers.filter((s) => sr.hosts.indexOf(s.id) < 0).map((s) => A.hostName(s.id));
    pg.innerHTML = sr.skipped.length && !sr.hosts.length ? esc(nameList(sr.skipped.map(A.hostName))) + ' is not connected, so it can’t be searched.' :
      (sr.done ? nOk + (nOk >= 400 ? '+' : '') + ' for “' + esc(sr.q) + '” on ' + esc(where) : 'Searching ' + esc(where) + '… ' + nOk + ' found so far') + (out.length ? ' · ' + esc(nameList(out)) + (out.length === 1 ? ' was' : ' were') + ' left out' : '');
    let rt = rs.querySelector('.rt');
    if (!rt) { rs.innerHTML = '<div class="rt">' + tableHead(sr.hosts) + '</div><div id="snone"></div>'; rt = rs.querySelector('.rt'); }
    Array.from(rt.querySelectorAll('.c')).forEach((c) => c.remove()); rt.insertAdjacentHTML('beforeend', tableCells(sr));
    const none = $('#snone', rs);
    if (none) none.innerHTML = !nOk && sr.done ? '<div class="s-no">' + ic('search', { size: 36 }) + '<b>No matches</b><p>Nothing named like “' + esc(sr.q) + '” on ' + esc(where || 'the selected computers') + '.</p></div>' : '';
  }
  /* Opens a result in Files: a folder opens itself, a file opens its folder with the file selected. */
  function openResult(r) { if (r.node.t === 'dir') A.openPath(r.host, r.path, false); else A.openPath(r.host, U.join(r.dir, r.node.n), true); }

  /* ====================================================================== Search panel ====================================================================== */
  /* The mockup's search: a panel of 27.5 rem that opens beside the sidebar over whatever page is showing (Files stays where
     it is), from the sidebar's Search item or the / key. It runs the same engine search as the Search page (E.makeSearch,
     kept in A.searchRun so saved and recent searches work), grouped by computer, with the same scope and kind choices the
     engine really has. "Show all" hands the run to the Search page's table; that page stays as the route A.go('search'). */
  const P = (V.panel = { el: null, q: '', cur: -1, tm: 0, timer: 0, back: null });
  /* True while the panel is showing. */
  P.isOpen = () => !!(P.el && P.el.classList.contains('open'));
  /* The hosts a scope means: the computer shown in Files, every computer that is online, or this computer. */
  const scopeHosts = (sc) => (sc === 'server' ? [A.mainPane.host] : sc === 'all' ? E.servers.filter((s) => s.state === 'online').map((s) => s.id) : ['local']);
  /* The panel's fixed frame: the field, then three areas (where and what to search, the results, the keys) that are refilled while typing. */
  const panelFrame = () => '<div class="m" role="dialog" aria-label="Search"><label class="s-f">' + ic('search', { size: 18 }) + '<input type="text" id="srq" placeholder="Search your computers" aria-label="Search" autocomplete="off" spellcheck="false"><kbd class="b-kbd">Esc</kbd></label><div class="s-sc"></div><div class="s-ls"></div><div class="s-ft"></div></div>';
  /* After a search has results: how many fall in each kind and on each computer, as buttons that narrow the list already found (nothing is chosen before searching). */
  const kindOfRes = (r) => { const k = U.kindOf(r.node); return Object.keys(KIND).find((n) => KIND[n].indexOf(k) >= 0) || ''; };
  const keepRes = (r) => (!P.fk || kindOfRes(r) === P.fk) && (!P.fh || r.host === P.fh);
  function panelScope(sr) {
    if (!sr || !sr.results.length) return '';
    const kc = {}, hc = {}; sr.results.forEach((r) => { if (!P.fh || r.host === P.fh) { const k = kindOfRes(r); if (k) kc[k] = (kc[k] || 0) + 1; } if (!P.fk || kindOfRes(r) === P.fk) hc[r.host] = (hc[r.host] || 0) + 1; });
    const btnF = (x, v, label, n, on) => '<button type="button" class="' + (on ? 'on' : '') + '" data-x="' + x + '" data-v="' + esc(v) + '" aria-pressed="' + !!on + '">' + label + (n != null ? ' <i>' + n + '</i>' : '') + '</button>';
    const total = sr.results.filter(keepRes).length;
    const kinds = btnF('pfk', '', 'All', total, !P.fk) + Object.keys(KIND).filter((k) => kc[k]).map((k) => btnF('pfk', k, k, kc[k], P.fk === k)).join('');
    const hosts = Object.keys(hc);
    const pcs = hosts.length > 1 ? '<div class="s-pl">' + hosts.map((h) => btnF('pfh', h, esc(A.hostName(h)), hc[h], P.fh === h)).join('') + '</div>' : '';
    return '<div class="s-pl">' + kinds + '</div>' + pcs;
  }
  /* The results area: saved and recent searches (or the add-a-computer note) before a search, then the progress line, the results by computer, or why there are none. */
  function panelList(sr) {
    const extra = A.searchExtra ? A.searchExtra() : '';
    if (!sr) {
      if (!E.servers.length) return '<div class="s-no">' + em('desktop_computer', 36) + '<b>Add a computer to search it</b><p>Until then, search looks only on this computer.</p>' + btn('Add a computer', { i: 'plus', k: 'pri', a: ' data-x="padd"' }) + '</div>' + (extra ? '<div class="s-ch">' + extra + '</div>' : '');
      return extra ? '<div class="s-ch">' + extra + '</div>' : '<div class="s-no">' + ic('search', { size: 36 }) + '<b>Type in the box above</b><p>Results are grouped by computer.</p></div>';
    }
    const where = sr.hosts.map(A.hostName).join(', '), skipped = sr.skipped.map(A.hostName).join(', '), n = sr.results.length;
    if (!sr.hosts.length) return '<div class="s-no"><b>No computer is switched on</b><p>' + esc(skipped) + ' is not connected, so it can’t be searched.</p>' + btn('Computers', { k: 'sm', a: ' data-x="pcomp"' }) + '</div>';
    const prog = sr.done ? '' : '<p class="s-pg">Searching ' + esc(where) + '… ' + n + ' found so far</p>';
    const left = sr.skipped.length ? '<p class="s-pg">' + esc(skipped) + (sr.skipped.length === 1 ? ' was' : ' were') + ' left out</p>' : '';
    if (!n) return prog + left + (sr.done ? '<div class="s-no">' + ic('search', { size: 36 }) + '<b>No matches</b><p>Nothing named like “' + esc(sr.q) + '” on ' + esc(where) + '.</p></div>' : '');
    return prog + groupsHtml(sr, 'p', keepRes) + left;
  }
  /* The keys line, and (once there are results) Save and Show all. */
  function panelFoot(sr) {
    const n = sr ? sr.results.length : 0, saved = sr && A.searchSave ? /aria-pressed="true"/.test(A.searchSave(sr)) : false;
    return '<span class="k"><kbd class="b-kbd">Enter</kbd> <span>open</span></span> <span class="k"><kbd class="b-kbd">Ctrl D</kbd> <span>download</span></span> <span class="sp"></span>' +
      (n ? btn(saved ? 'Saved' : 'Save', { k: 'sm ghost', i: 'star', a: ' data-x="psave"' }) + ' ' + btn('Show all <b class="n">' + n + '</b>', { k: 'sm', a: ' data-x="pall"' }) : '');
  }
  const rowsOf = () => Array.from(P.el.querySelectorAll('.s-ls .s-r'));
  /* Makes row i the chosen one (light gray); the Enter key and Ctrl D act on it. */
  function setCur(rs, i, scroll) {
    rs.forEach((r, k) => { r.classList.toggle('on', k === i); if (k === i) r.setAttribute('aria-current', 'true'); else r.removeAttribute('aria-current'); });
    P.cur = rs[i] ? +rs[i].dataset.i : -1; if (scroll && rs[i] && rs[i].scrollIntoView) rs[i].scrollIntoView({ block: 'nearest' });
  }
  /* Moves the chosen row up or down; with focus the row itself takes the keyboard focus. */
  function move(d, focus) {
    const rs = rowsOf(); if (!rs.length) return; const i = Math.max(0, Math.min(rs.length - 1, rs.findIndex((r) => r.classList.contains('on')) + d));
    setCur(rs, i, true); if (focus) rs[i].focus();
  }
  /* Draws the three areas from the current search, keeping the list's scroll place and the chosen row while results stream in. */
  function fill() {
    if (!P.el) return; const sr = A.searchRun, q = (k) => P.el.querySelector(k), ls = q('.s-ls'), top = ls.scrollTop;
    if (P.fh && !(sr && sr.results.some((r) => r.host === P.fh))) P.fh = ''; if (P.fk && !(sr && sr.results.some((r) => kindOfRes(r) === P.fk))) P.fk = '';
    q('.s-sc').innerHTML = panelScope(sr); ls.innerHTML = panelList(sr); q('.s-ft').innerHTML = panelFoot(sr); ls.scrollTop = top;
    const rs = rowsOf(); if (rs.length) setCur(rs, Math.max(0, rs.findIndex((r) => +r.dataset.i === P.cur))); else P.cur = -1;
  }
  /* Runs a search for v in the panel (commit true records it in the recent searches); an empty text clears the results. Same hosts and engine call as the page's search. */
  /* Reads a running search one slice at a time into the panel until it is done. Closing the panel stops it; opening the panel picks it up again. */
  const pump = (sr) => {
    clearTimeout(P.timer);
    const step = () => { if (A.searchRun !== sr) return; const done = sr.job.step(E.SEARCH.slice); sr.results = sr.job.results; sr.scanned = sr.job.scanned; sr.done = done; fill(); if (!sr.done) P.timer = setTimeout(step, A.T.stepGap); };
    P.timer = setTimeout(step, A.T.step);
  };
  P.run = (v, commit) => {
    const q = String(v || '').trim(); clearTimeout(P.tm); clearTimeout(P.timer); P.q = q; st.q = q;
    const inp = P.el && P.el.querySelector('#srq'); if (inp && inp.value.trim() !== q) inp.value = String(v || '');
    if (!q) { A.searchRun = null; P.cur = -1; fill(); return; }
    if (commit && A.recordQuery) A.recordQuery(q);
    P.fk = ''; P.fh = ''; const scope = 'all', hs = ['local'].concat(scopeHosts('all'));
    const sr = (A.searchRun = { q, scope, filter: 'all', results: [], scanned: 0, done: false, tok: Math.random() });
    sr.hosts = hs.filter((h) => E.connected(h)); sr.skipped = hs.filter((h) => !E.connected(h));
    sr.job = E.makeSearch(sr.hosts, q, { filter: sr.filter, max: 400 }); P.cur = -1;
    const pull = (done) => { sr.results = sr.job.results; sr.scanned = sr.job.scanned; sr.done = done; };
    if (A.sync) { while (!sr.job.step(1e6)); pull(true); fill(); return; }
    fill(); pump(sr);
  };
  /* Opens the panel (with the last search still in it) and puts the cursor in the field; text starts a new search. */
  P.open = (text) => {
    if (!V.on()) return false; const app = $('#app');
    if (!P.el) { const el = document.createElement('div'); el.className = 'b-sr'; el.id = 'srp'; el.innerHTML = panelFrame(); app.appendChild(el); P.el = el; wirePanel(el); }
    if (!P.isOpen()) P.back = document.activeElement;
    P.el.classList.add('open'); app.classList.add('sr-open');
    if (A.searchRun === undefined) A.searchRun = null; /* no search has run yet: null, not undefined */
    const inp = P.el.querySelector('#srq'), sr = A.searchRun;
    if (text != null) P.run(text, false); else { if (!inp.value && sr) inp.value = sr.q; P.q = inp.value.trim(); fill(); if (sr && sr.job && !sr.done) pump(sr); }
    inp.focus(); inp.select(); return true;
  };
  /* Closes the panel. nav is true when a page change follows, so the focus and the page are left to it; otherwise the focus goes back and an open Search page is redrawn. */
  P.close = (nav) => {
    if (!P.isOpen()) return; clearTimeout(P.tm); if (!nav) clearTimeout(P.timer); P.el.classList.remove('open'); $('#app').classList.remove('sr-open');
    const b = P.back; P.back = null; if (nav) return;
    if (b && b.focus && document.contains(b)) b.focus();
    if (st.view === 'search' && V.draws('search')) drawSearch($('#stage'));
  };
  /* Hands the search being shown to the Search page's table (optionally narrowed to one computer) and closes the panel. */
  function showAll(host) {
    const sr = A.searchRun; if (!sr) return;
    V.rfTok = sr.tok; V.rf = rfFor(sr.q, host || P.fh); if (P.fk) V.rf.kind = P.fk;
    const q = $('#q'); if (q) q.value = sr.q;
    P.close(true); if (st.view === 'search') drawSearch($('#stage')); else A.go('search');
  }
  /* Opens a result in Files and closes the panel; the search text goes to the recent searches. */
  function openRes(r) { if (A.recordQuery && P.q) A.recordQuery(P.q); P.close(true); openResult(r); }
  const curRes = () => { const sr = A.searchRun; return sr && P.cur >= 0 ? sr.results[P.cur] : null; };
  /* Downloads a result from its computer (a result on this computer has nothing to download). */
  const dlRes = (r) => { if (r && r.host !== 'local') A.transferNames(r.host, r.dir, [r.node.n]); };
  /* The panel's own events: typing (searches after a short pause), clicks on its buttons and results, and the keys. */
  function wirePanel(el) {
    el.addEventListener('input', (e) => { if (e.target.id !== 'srq') return; clearTimeout(P.tm); const v = e.target.value; P.tm = setTimeout(() => P.run(v, false), A.T.search); });
    el.addEventListener('click', (e) => {
      if (e.target === el) { P.close(); return; }
      const x = e.target.closest('[data-x]');
      if (!x) { if (e.target.closest('[data-fx]')) setTimeout(fill, 0); return; } /* a saved or recent search was picked: its handler runs the search through A.runQuery */
      const a = x.dataset.x, sr = A.searchRun, r = sr && x.dataset.i != null ? sr.results[+x.dataset.i] : null, text = el.querySelector('#srq').value;
      if (a === 'pfk') { P.fk = x.dataset.v; fill(); }
      else if (a === 'pfh') { P.fh = P.fh === x.dataset.v ? '' : x.dataset.v; fill(); }
      else if (a === 'prow' || a === 'pfolder') { if (r) openRes(r); }
      else if (a === 'pdl') dlRes(r);
      else if (a === 'pmore') showAll(x.dataset.h);
      else if (a === 'pall') showAll();
      else if (a === 'psave') { if (sr) { saveSearch(sr); fill(); } }
      else if (a === 'padd') { P.close(true); if (A.serverDialog) A.serverDialog(); }
      else if (a === 'pcomp') A.go('servers');
    });
    el.addEventListener('keydown', (e) => {
      const t = e.target, row = t.closest && t.closest('.s-r, .s-mo');
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); move(e.key === 'ArrowDown' ? 1 : -1, !!row); return; }
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'd') { e.preventDefault(); dlRes(curRes()); return; }
      if (e.key === 'Enter' && t.id === 'srq') { e.preventDefault(); if (t.value.trim() !== P.q || !A.searchRun) P.run(t.value, true); else { const r = curRes(); if (r) openRes(r); } return; }
      if ((e.key === 'Enter' || e.key === ' ') && row === t) { e.preventDefault(); t.click(); }
    });
  }
  /* Esc closes the panel (unless a dialog is open and keeps the key). */
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && P.isOpen() && !document.querySelector('.scrim')) { e.preventDefault(); e.stopPropagation(); P.close(); } }, true);
  /* The top bar's Search button and the / key open the panel instead of the Search page (before the page's own handlers see them). */
  document.addEventListener('click', (e) => {
    const b = e.target.closest && e.target.closest('#btnSearch'); if (!b || !V.on()) return;
    e.preventDefault(); e.stopImmediatePropagation(); if (P.isOpen()) P.close(); else P.open();
  }, true);
  document.addEventListener('keydown', (e) => {
    if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey || !V.on() || document.querySelector('.scrim')) return;
    const t = e.target; if (t.matches && t.matches('input,textarea,select')) return;
    e.preventDefault(); e.stopImmediatePropagation(); P.open();
  }, true);
  /* Ctrl+K (Cmd+K on a Mac) opens the same panel from any page and from inside text fields, as the mockup does; it replaces the command palette. A dialog keeps the keys to itself. */
  document.addEventListener('keydown', (e) => {
    if (!(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey || (e.key || '').toLowerCase() !== 'k' || !V.on() || document.querySelector('.scrim')) return;
    e.preventDefault(); e.stopImmediatePropagation(); P.open();
  }, true);
  /* Any page change closes the panel; a search started from a saved or recent search while the panel is open runs in the panel. */
  const go0 = A.go; A.go = function () { P.close(true); return go0.apply(this, arguments); };
  const runQuery0 = A.runQuery; A.runQuery = function (v) { if (P.isOpen()) { P.run(v, true); return; } return runQuery0.apply(this, arguments); };

  /* ====================================================================== wiring ====================================================================== */
  /* Draws the page of the current view into the stage (called by pages.js). */
  V.mount = (stage) => {
    const v = st.view; stage.className = v === 'transfers' ? 'stage d5 v5x rfe-pages split' : 'stage page d5 v5x rfe-pages' + (v === 'search' ? ' srch' : v === 'home' ? ' ovw' : '');
    if (v === 'transfers') drawTransfers(stage); else if (v === 'home') { drawHome(stage); warmHome(); } else if (v === 'search') drawSearch(stage);
    document.title = ({ home: 'Overview', transfers: 'Transfers', search: 'Search' }[v]) + ' - RFE';
  };
  /* The pages.js hooks for the search page: the progress callback of a running search and the page body. */
  V.updateSearch = () => { drawSearchResults(); if (P.isOpen()) fill(); };

  const stage = $('#stage');
  /* A transfer that is still being set up has no file ids yet, so the engine's Pause refuses it ("Wait until the transfer
     has started"). Pause all already handles that case by holding it: the state turns to Paused at once and the engine
     pauses the files as soon as they exist. This does the same for the Pause button of one transfer, and keeps Resume
     right for such a held transfer (it starts it, or lets the setup that is already running go on). It runs before the
     page's own click handler (capture phase) and only takes over for a transfer without file ids. */
  stage.addEventListener('click', (e) => {
    if (st.view !== 'transfers' || !V.draws('transfers') || !e.target.closest) return;
    const b = e.target.closest('[data-t="pause"], [data-t="resume"], [data-pa="resumeall"]'); if (!b) return;
    const bare = (t) => t && !t.rids.length;
    if (b.dataset.pa) { E.tasks.forEach((t) => { if (bare(t) && t.state === 'paused' && t.held) { t.held = false; if (t.starting) t.state = 'queued'; } }); return; }
    const card = b.closest('[data-tid]'), t = card ? E.task(card.dataset.tid) : null; if (!bare(t)) return;
    if (b.dataset.t === 'pause' && t.state === 'queued') { t.held = true; t.state = 'paused'; t.speed = 0; t.pauseAt = E.now(); }
    else if (b.dataset.t === 'resume' && t.state === 'paused' && t.held) { t.held = false; if (t.starting) t.state = 'queued'; else E.retry(t.id); }
    else return;
    e.stopImmediatePropagation(); e.preventDefault(); drawTransfers(stage);
  }, true);
  stage.addEventListener('click', (e) => {
    if (!V.draws(st.view)) return; const t = e.target;
    if (st.view === 'home' || st.view === 'search') { if (t.closest('[data-t]') && t.closest('[data-tid]')) { A.cardClick(e); return; } }
    const x = t.closest('[data-x]'); if (!x) return; const a = x.dataset.x; const sr = A.searchRun, r = sr && x.dataset.i != null ? sr.results[+x.dataset.i] : null;
    if (a === 'pick') { V.pick = x.dataset.id; drawTransfers(stage); }
    else if (a === 'pc') { const s = E.server(x.dataset.id); if (s && (s.state === 'online' || s.state === 'disconnected')) { A.go('files'); A.setMainHost(s.id); if (s.state === 'disconnected') E.connect(s.id); } else A.go('servers'); }
    else if (a === 'add') { if (A.serverDialog) A.serverDialog(); }
    else if (a === 'carry') { const kept = (A.journal && A.journal.retained) || []; if (kept.length && A.resumeDialog) A.resumeDialog({ at: Date.now(), tasks: kept.slice() }); }
    else if (a === 'rec') { const f = V.recent.list[+x.dataset.i]; if (f) A.openPath(f.host, f.path, true); }
    else if (a === 'srow' || a === 'sfolder') { if (r) openResult(r); }
    else if (a === 'save' && sr) { saveSearch(sr); drawSearch(stage); }
  });
  stage.addEventListener('keydown', (e) => {
    if (!V.draws(st.view) || (e.key !== 'Enter' && e.key !== ' ')) return; const x = e.target.closest && e.target.closest('[data-x][role="button"]'); if (!x || e.target !== x) return; e.preventDefault(); x.click();
  });
  /* The table's filters narrow the results already found; the cells are redrawn, the filter boxes stay. */
  const filt = (e) => { const f = e.target.closest && e.target.closest('.rt [data-rf]'); if (!f || st.view !== 'search' || !V.rf) return; V.rf[f.dataset.rf] = f.value; drawSearchResults(); };
  stage.addEventListener('input', filt); stage.addEventListener('change', filt);

  /* Redraw what is open when the engine reports a change; numbers alone move on the tick. */
  let raf = 0;
  E.on((type) => {
    if (type === 'tick') sample();
    if (type === 'tick' || type === 'transfers') tally();
    if (!V.draws(st.view)) return;
    if (type === 'tick') { if (st.view === 'home') { tickPatched = Date.now(); patchHome(stage); } else if (st.view === 'transfers') patchTransfers(stage); return; }
    if (type === 'transfers' || type === 'servers') {
      if (raf) return;
      raf = requestAnimationFrame(() => { raf = 0; if (!V.draws(st.view)) return; if (st.view === 'home') drawHome(stage); else if (st.view === 'transfers' && type === 'servers') drawTransfers(stage); });
    }
  });
  /* The engine ticks only while something happens, so a timer keeps the chart's time axis honest while the app sits idle. */
  /* The Overview's own timer: it runs only while that page is open (it stops itself on any other page; drawHome starts it again) and skips a patch the engine's tick just made. */
  let homeTimer = 0, tickPatched = 0;
  armHome = () => {
    if (homeTimer) return;
    homeTimer = setInterval(() => {
      if (!(V.draws(st.view) && st.view === 'home')) { clearInterval(homeTimer); homeTimer = 0; return; }
      sample(); if (Date.now() - tickPatched >= A.T.poll) patchHome(stage);
    }, A.T.poll);
  };
  armHome();
});
