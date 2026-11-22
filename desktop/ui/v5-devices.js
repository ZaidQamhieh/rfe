/* RFE v5 - the "Devices on this computer" section of the Computers page, and the History page (the v5 mockup's list + right panel), drawn over the existing logic.
   Nothing here talks to a computer by itself: the lists come from the data the app already loads (A.X.devices, A.X.inbox,
   A.X.audit, E.history), and every action goes through the existing handlers (data-fx="dev.approve|0", "dev.deny|0",
   "dev.revoke|id", "dev.remove|id", "hist.audit"; A.bindGrants for the permission switches; A.pairDialog to pair a phone).
   It is used only while the window wears the v5 wrapper (class "d5" on #app); otherwise the old pages stay. */
rfeModule('v5-devices', ['fpages'], function () {
  'use strict';
  const A = window.A; if (!A) return;
  const E = A.E, U = A.U, $ = A.$, esc = A.esc, ic = A.ic, st = A.state, X = A.X, F = A.fx;
  const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const WD = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const KINDS = ['Transfers', 'Connections', 'Security', 'Devices', 'Shares'];
  const ICON = { Transfers: 'swap', Connections: 'plug', Security: 'lock', Devices: 'phone', Shares: 'link' };
  const OUTC = { Succeeded: 'ok', Failed: 'bad', Waiting: 'warn', Running: '' };
  const T = (A.v5Devices = { sel: null, hsel: null, open: null, focus: false, f: { q: '', Kind: 'Kind', When: 'When', Computer: 'Computer', Device: 'Device', Outcome: 'Outcome' } });
  const two = (n) => String(n).padStart(2, '0');

  T.on = A.v5on; /* true while the v5 wrapper is on the window (v5-shell.js) */
  const hostName = (h) => (h ? A.hostName(h) : '');
  const devIcon = (d) => (/desktop|laptop|windows|linux|mac|thinkpad|book/i.test(d.name) && !/phone|pixel|galaxy|tab/i.test(d.name) ? 'laptop' : 'phone');
  const em = A.v5em; /* a mockup colour picture, n pixels wide (v5-shell.js) */
  const devPic = (d, n) => em(devIcon(d) === 'laptop' ? 'laptop' : 'mobile_phone', n);
  const isOnline = E.isOnline;
  /* "09:01" for today, "Tue" within the last week, else "12 Oct". */
  const stamp = (ms) => { const d = new Date(ms), n = new Date(E.now()); if (d.toDateString() === n.toDateString()) return two(d.getHours()) + ':' + two(d.getMinutes()); return E.now() - ms < 6 * A.T.day ? WD[d.getDay()] : d.getDate() + ' ' + MON[d.getMonth()]; };
  const dayOf = (ms) => { const d = new Date(ms), n = new Date(E.now()); if (d.toDateString() === n.toDateString()) return 'Today'; if (new Date(E.now() - A.T.day).toDateString() === d.toDateString()) return 'Yesterday'; return d.getDate() + ' ' + MON[d.getMonth()]; };
  const btn = (label, icon, cls, attrs) => '<button type="button" class="k-btn' + (cls ? ' ' + cls : '') + '" ' + (attrs || '') + '>' + (icon ? ic(icon, { size: 16 }) : '') + '<span>' + label + '</span></button>';
  const ib = (icon, label, attrs) => '<button type="button" class="k-ib" aria-label="' + esc(label) + '" data-tip="' + esc(label) + '" ' + attrs + '>' + ic(icon, { size: 16 }) + '</button>';
  const pageHead = (title, sub, right) => '<header class="k-ph"><div><h1 class="k-title">' + title + '</h1><p class="k-sub">' + sub + '</p></div><div class="k-ph-r">' + (right || '') + '</div></header>';
  const empty = (icon, title, text, acts) => '<div class="b-empty">' + em(icon, 40) + '<b>' + title + '</b><p>' + text + '</p>' + (acts ? '<div class="k-row">' + acts + '</div>' : '') + '</div>';
  const tech = (rows, extra) => A.v5tech(rows, extra, true);

  /* Sets the page classes: the stage wears the mockup's padding; the right panel is shown (or hidden) and the grid column is set. */
  function chrome(v, railOn) {
    const s = $('#stage'); s.classList.add('v5p');
    const side = $('#side'), m = $('#main');
    m.classList.toggle('nosid', !railOn); m.classList.toggle('v5r', !!railOn);
    side.style.display = railOn ? '' : 'none'; side.classList.toggle('v5rail', !!railOn);
    if (!railOn) side.innerHTML = '';
  }
  /* The device list's selected device (the first one until a row is picked). */
  const selDev = () => { const l = X.devices; return l.find((d) => d.id === T.sel) || l[0] || null; };

  /* True when the right panel has something to show for the current view (the layout in pages.js asks this). */
  T.rail = () => {
    if (!T.on()) return false;
    if (st.view === 'history') return !!(E.servers.length && E.history.length);
    return false;
  };

  /* ---------------- Devices ---------------- */
  const grantList = [['download', 'Download'], ['upload', 'Upload'], ['modify', 'Rename and move'], ['delete', 'Delete'], ['share', 'Make share links'], ['wake', 'Wake computers'], ['viewApps', 'See apps'], ['launchApps', 'Open apps']];
  const FILE = ['download', 'upload', 'modify', 'delete', 'share', 'wake'];
  /* One permission row: the label and a switch. `k` is the grant key the existing A.bindGrants saves on click; no key = fixed. */
  const setRow = (label, on, k, fixed) => '<div class="k-set"><div><b>' + label + '</b></div><button type="button" class="k-sw' + (on ? ' on' : '') + '" role="switch" aria-checked="' + !!on + '" aria-label="' + esc(label) + '"' + (k && !fixed ? ' data-c="' + k + '"' : ' disabled') + '></button></div>';

  /* The right panel for one device: what it may do (switches that save on click), its folder limit, and what can be done to it. */
  function devRail(d) {
    const a = d.a, off = d.revoked || !a;
    const rows = setRow('See files', !d.revoked, '', true) + grantList.map((g) => setRow(g[1], !off && !!a[g[0]] || (!d.revoked && d.viaLogin && FILE.includes(g[0])), g[0], off || (d.viaLogin && FILE.includes(g[0])))).join('') + setRow('Only look, never change', !off && !!a.readOnly, 'readOnly', off);
    const how = d.viaLogin ? 'Account' : 'Pairing code';
    return '<div class="rail cdd" id="v5rail">' + devPic(d, 40) + '<h2><span class="dnt">' + esc(d.name) + '</span>' + (d.current ? ' <em class="k-pill">This app</em>' : '') + '</h2><p class="k-sub">' + how + ' · seen ' + (isOnline(d) ? 'now' : d.last ? F.rel(d.last) : 'never') + '</p>' +
      (a || d.revoked ? '' : '<p class="k-note">' + ic('info', { size: 14 }) + 'What it may do needs the owner sign-in.</p>') +
      '<div class="k-perms" data-v5perms>' + rows + '</div>' +
      '<div class="b-fl v5-lim"><small>Only this folder (leave blank for every shared folder)</small><span class="b-f">' + ic('folder', { size: 16 }) + '<input data-v5lim readonly value="' + esc(a && a.jailRoot || '') + '" placeholder="Every shared folder" aria-label="Only this folder" title="Pick the folder with Choose folder" spellcheck="false"' + (off ? ' disabled' : '') + '></span><span class="k-row">' + btn('Choose folder…', 'folder', 'sm', 'data-v5limpick' + (off ? ' disabled' : '')) + (a && a.jailRoot ? btn('No limit', '', 'ghost sm', 'data-v5limclear' + (off ? ' disabled' : '')) : '') + '</span><span class="err" data-v5err role="alert"></span></div>' +
      '<div class="k-dact">' + btn('What it did', 'history', '', 'data-fx="hist.audit"') + btn(d.revoked ? 'Remove from list…' : 'Remove access…', 'trash', 'warn', 'data-fx="' + (d.revoked ? 'dev.remove' : 'dev.revoke') + '|' + esc(d.id) + '"') + '</div>' +
      tech([['Address', d.addr], ['Version', d.ver], ['Device id', d.id]]) + '</div>';
  }

  /* The cards for the pairing requests: one compact table (Device, Number, Time left, Allow…/Refuse); addresses stay in a closed fold. */
  function reqCard(s, reqs) {
    const left = (q) => (typeof q.expiresInSeconds === 'number' ? Math.floor(Math.max(0, q.expiresInSeconds) / 60) + ':' + String(Math.max(0, q.expiresInSeconds) % 60).padStart(2, '0') : Number(q.ageSeconds) < 10 ? 'just now' : (Number(q.ageSeconds) || 0) + ' s ago');
    const denied = X.inboxDenied && X.inboxDenied.has(s.id);
    const errored = !!(X.inboxErr && X.inboxErr.size);
    const rows = reqs.map((q, i) => '<tr><td><div class="b-rqd prw">' + em('mobile_phone', 24) + '<div><b>' + esc(q.label) + '</b><small>' + (q.replaces ? 'Replace <b>' + esc(q.replaces) + '</b>, access starts over' : 'See files on ' + esc(s.name)) + '</small></div></div></td><td class="mono b-rqn prc" title="The number on ' + esc(q.label) + '">' + esc(q.matchCode) + '</td><td class="b-rqt prl">' + left(q) + '</td><td class="b-rqa pra">' + btn('Allow…', 'check', 'pri sm', 'data-fx="dev.approve|' + i + '"') + btn('Refuse', 'x', 'warn sm','data-fx="dev.deny|' + i + '"') + '</td></tr>').join('');
    const fold = reqs.length ? A.v5tech(reqs.map((q) => [q.label, q.address || 'address unknown']), '', false, 'prd') : '';
    const note = denied ? 'This login cannot answer pairing requests. Sign in with the account, or answer on the PC.' : reqs.length >= 3 ? 'The PC holds at most 3 waiting requests. Answer one to let the next device ask.' : 'Allow a device only if it shows the same number.';
    return '<section class="k-card prq"><div class="k-ch"><h2>Asking to join <span class="k-count">' + reqs.length + '</span></h2></div>' +
      (reqs.length ? '<table class="b-rq prt"><thead><tr><th>Device</th><th>Number</th><th>Time left</th><th><span class="prs srOnly">Actions</span></th></tr></thead><tbody>' + rows + '</tbody></table>' : denied || errored ? '' /* a login that cannot answer, or a look that failed, cannot tell whether anyone is asking: only the note below */ : '<div class="b-rqe"><b>Nobody is asking to join</b><small>When a phone or computer scans your code, it shows up here with a number to check.</small></div>') +
      A.inboxErrHtml(btn('Try again', 'refresh', 'sm', 'data-fx="inbox.retry"')) + '<p class="k-note prn">' + ic('info', { size: 14 }) + note + '</p>' + fold + '</section>';
  }

  /* The "Devices on this computer" section of the Computers page, for computer s (the one pairing runs against): pairing requests,
     the paired devices, the picked device's permissions panel and the recent security events. It opens by itself while a phone is asking. */
  T.section = (s) => {
    const reqs = X.inbox.slice(), d = selDev();
      const row = (x) => '<div class="ro' + (d && x.id === d.id ? ' sel' : '') + '" role="button" tabindex="0" data-v5pick="' + esc(x.id) + '">' + devPic(x, 24) + '<span><b><span class="dnt">' + esc(x.name) + '</span>' + (x.current ? ' <em class="k-pill">This app</em>' : '') + '</b><small>' + (x.viaLogin ? 'Account' : 'Pairing code') + ' · ' + (isOnline(x) ? 'Now' : x.last ? F.rel(x.last) : 'Never seen') + '</small></span><span class="st">' + (x.revoked ? '<span class="bad">Removed</span>' : x.a && x.a.jailRoot ? 'only ' + esc(x.a.jailRoot.split(/[\\/]/).filter(Boolean).pop() || x.a.jailRoot) : 'every folder') + '</span></div>';
      const list = X.devErr ? empty('warning', 'Could not list devices', esc(X.devErr)) : X.devices.length ? '<div class="li">' + X.devices.map(row).join('') + '</div>' : empty(X.devBusy ? 'hourglass_not_done' : 'mobile_phone', X.devBusy ? 'Loading…' : 'No paired devices', X.devBusy ? 'Reading the list of devices.' : 'Pair a phone to see it here.', X.devBusy ? '' : btn('Pair a phone', 'qr', 'pri', 'data-v5="pair"'));
      const evs = (X.audit || []).slice(0, 4).map((e) => { const f = A.auditFriendly(e.text); const p = A.auditShort(e.text).split(' · '); const b = f ? f.title : p[0]; const bad = f ? f.bad : /fail|refus|denied|wrong/i.test(b), ok = !f && /^sign-?in/i.test(b); return '<div class="k-ev ' + (bad ? 'bad' : ok ? 'ok' : '') + '" title="' + esc(e.text) + '"><time class="mono">' + stamp(e.at) + '</time><b>' + esc(b) + '</b><span>' + esc(f ? f.detail : p.slice(1).join(', ').replace(/ from [0-9a-f.:]+/i, ' from another computer')) + '</span></div>'; }).join('');
    const open = T.isOpen();
    const evBlock = (evs ? '<section class="k-card"><div class="k-ch"><h2>Recent security events</h2><div>' + btn('View all', '', 'sm ghost', 'data-fx="hist.audit"') + '</div></div><div class="k-evs">' + evs + '</div></section>' : X.auditErr ? '<p class="k-note">' + esc(X.auditErr) + '</p>' : '');
    return '<details class="k-card cdv" data-cdv' + (open ? ' open' : '') + '><summary><span class="cdt">Devices on ' + esc(s.name) + '</span> ' + (X.devAt ? '<span class="k-count">' + X.devices.length + '</span>' : '') + (reqs.length ? ' <em class="k-pill">Asking to join</em>' : '') + '</summary><div class="v5pg cdvb"><div class="cdvh">' + btn('Pair a phone', 'qr', '', 'data-v5="pair"') + '</div>' + reqCard(s, reqs) + list + (d ? devRail(d) : '') + evBlock + '</div></details>';
  };
  /* The section for computer cur: its devices when it is online (pairing is pointed at it first), else a note that says why there are none. */
  T.sectionFor = (cur) => {
    if (!T.on() || !cur) return '';
    if (cur.state !== 'online') return '<section class="k-card cdv off"><div class="k-ch"><h2>Devices on ' + esc(cur.name) + '</h2></div><p class="k-note">' + ic('info', { size: 14 }) + 'Connect this computer to see the phones paired with it.</p></section>';
    /* Pairing is pointed at cur only when the shown computer changed, or the pairing server in use is no longer valid
       (pairServer fell back to another one); a computer picked in the pairing dialog is otherwise left alone. */
    const picked = X.pair.host, p = A.pairServer(), changed = T.shown !== cur.id; T.shown = cur.id;
    if ((changed || !p || p.id !== picked) && (!p || p.id !== cur.id)) { X.pair.host = cur.id; X.pair.code = ''; X.devices = []; X.devAt = 0; X.audit = []; X.auditAt = 0; }
    return T.section(cur);
  };
  /* True while the devices section is unfolded: the person's choice, else open only while a phone is asking. */
  T.isOpen = () => (T.open === null ? X.inbox.length > 0 : T.open);
  /* Reads the lists the section shows when they are stale (devices and security events) and makes sure a pairing code is ready;
     a folded section reads nothing, so a visit to Computers costs no extra requests. */
  T.loads = () => {
    const s = A.pairServer(); if (!s || !T.on() || !T.isOpen()) return;
    if (E.now() - X.devAt > A.T.stale && !X.devBusy) A.loadDevices();
    if (E.now() - X.auditAt > A.T.stale) A.loadAudit();
    A.ensureCode();
  };
  /* After the page was drawn: switches the permission panel's toggles on, and brings the section into view when it was asked for. */
  T.bind = () => {
    const root = $('#stage [data-cdv]'); if (!root) return;
    const d = selDev(), p = $('[data-v5perms]', root); if (p && d && d.a && !d.revoked) A.bindGrants(p, d);
    if (T.focus) { T.focus = false; if (root.scrollIntoView) root.scrollIntoView({ block: 'start' }); }
  };
  /* True while a folder box of the section has the keyboard (a poll must not rebuild the page then). */
  T.typing = () => !!(document.activeElement && document.activeElement.closest && document.activeElement.closest('[data-cdv] [data-v5lim]'));
  /* A.go('devices') lands here: the Computers page opens with the devices section shown, for the computer that is asking or the pairing one. */
  T.reveal = () => {
    T.open = true; T.focus = true; const q = X.inbox[0], p = A.pairServer();
    const id = q ? q.host : p && p.id; if (id && E.server(id) && A.v5Computers) A.v5Computers.pick = id;
  };
  /* Redraws the page that holds the devices section (the Computers page). */
  T.devices = () => A.v5Computers.page();

  /* ---------------- History ---------------- */
  /* The five kinds the filter offers: transfers, connections, and the audit entries sorted by what their words say. */
  const kindOf = (h) => h.cat || (h.kind === 'transfer' || h.kind === 'queue' ? 'Transfers' : h.kind === 'audit' ? (/share/i.test(h.text) ? 'Shares' : /pair|device|revoked|removed/i.test(h.text) ? 'Devices' : 'Security') : 'Connections');
  const outOf = (h) => h.out || (h.error ? 'Failed' : h.kind === 'queue' ? 'Waiting' : 'Succeeded');
  const devOf = (h) => h.dev || '';
  /* The events that pass the filters, newest first, each with its list index. */
  function histRows() {
    const f = T.f, k = f.Kind.split(' · ')[0], now = E.now();
    return E.history.slice().sort((a, b) => b.at - a.at).filter((h) => {
      if (k === 'Only what the PCs recorded' ? !(h.agent || h.src === 'The PC') : k !== 'Kind' && kindOf(h) !== k) return false;
      if (f.When === 'Today' && dayOf(h.at) !== 'Today') return false;
      if (f.When === 'Yesterday' && dayOf(h.at) !== 'Yesterday') return false;
      if (f.When === 'Last 7 days' && now - h.at > 7 * A.T.day) return false;
      if (f.When === 'Last 30 days' && now - h.at > 30 * A.T.day) return false;
      if (f.Computer !== 'Computer' && hostName(h.host) !== f.Computer) return false;
      if (f.Device !== 'Device' && devOf(h) !== f.Device.replace(' (this one)', '') && !E.mentionsDevice(h.text, f.Device.replace(' (this one)', ''), X.devices.map((d) => d.name))) return false;
      if (f.Outcome !== 'Outcome' && outOf(h) !== f.Outcome) return false;
      return !f.q || (h.text + ' ' + hostName(h.host) + ' ' + devOf(h) + ' ' + (h.more || '')).toLowerCase().includes(f.q.toLowerCase());
    });
  }
  const filtersOn = () => Object.keys(T.f).some((k) => (k === 'q' ? T.f.q : T.f[k] !== k));
  const evIcon = (h) => ic(ICON[kindOf(h)] || 'activity', { size: 16 });
  /* The list part of the History page: summary line and the events grouped by day. Redrawn alone while typing in the filter. */
  function histList(rows, sel) {
    let day = '', list = '';
    rows.forEach((h) => { const dd = dayOf(h.at); if (dd !== day) { day = dd; list += '<h3 class="b-day">' + dd + '</h3>'; } list += '<div class="b-ev' + (sel && h.id === sel.id ? ' sel' : '') + '" role="button" tabindex="0" data-v5ev="' + esc(h.id) + '"><time class="mono">' + two(new Date(h.at).getHours()) + ':' + two(new Date(h.at).getMinutes()) + '</time><span class="b-ic">' + evIcon(h) + '</span><span class="tx">' + esc(h.text) + '<small>' + esc([hostName(h.host), devOf(h)].filter(Boolean).join(' · ')) + '</small></span><span class="b-out ' + OUTC[outOf(h)] + '">' + outOf(h) + '</span></div>'; });
    return '<p class="b-fsum">' + rows.length + ' of ' + E.history.length + ' events · newest first' + (filtersOn() ? btn('Clear filters', '', 'sm ghost', 'data-v5="clearf"') : '') + '</p><div class="b-hist">' + list + '</div>' + (E.history.length && !rows.length ? '<div class="k-empty">' + em('scroll', 32) + '<b>No events match</b><small>Clear the filters to see them all.</small></div>' : '') + (E.servers.length && E.history.length ? '' : '<p class="mut v5-note">Connections, transfers, sign-ins and pairing are recorded here as they happen.</p>');
  }
  /* The right panel for one event. */
  function histRail(h) {
    const k = kindOf(h), o = outOf(h), acts = { Transfers: btn('Show in Files', 'folder', 'pri', 'data-v5go="files"'), Devices: btn('Open in Devices', 'phone', 'pri', 'data-v5go="devices"'), Connections: btn('Open in Devices', 'server', 'pri', 'data-v5go="servers"'), Security: btn('Open in Devices', 'server', 'pri', 'data-v5go="servers"') };
    return '<aside class="rail" id="v5rail"><span class="b-tile">' + ic(ICON[k] || 'activity', { size: 20 }) + '</span><h2>' + k + '</h2><p class="k-sub"><span class="b-out ' + OUTC[o] + '">' + o + '</span> · ' + dayOf(h.at) + ', ' + two(new Date(h.at).getHours()) + ':' + two(new Date(h.at).getMinutes()) + '</p><p class="v5-evtext">' + esc(h.text) + '</p>' +
      '<dl class="k-kv">' + (h.host ? '<dt>Computer</dt><dd>' + esc(hostName(h.host)) + '</dd>' : '') + (devOf(h) ? '<dt>Device</dt><dd>' + esc(devOf(h)) + '</dd>' : '') + (h.more ? '<dt>Details</dt><dd>' + esc(h.more) + '</dd>' : '') + '</dl>' +
      '<div class="k-dact">' + (acts[k] || '') + btn('Copy details', 'copy', '', 'data-v5="copy"') + '</div>' + tech([['Recorded by', h.src || (h.agent ? 'The PC' : 'This app')], ['From address', h.ip]]) + '</aside>';
  }
  const sel = (label, opts) => '<label class="b-fs" title="' + label + '"><select class="k-sel" aria-label="' + label + '" data-v5f="' + label + '">' + opts.map((o) => '<option' + (T.f[label] === o || (label === 'Kind' && T.f.Kind === o.split(' · ')[0]) ? ' selected' : '') + '>' + esc(o) + '</option>').join('') + '</select></label>';

  /* Draws the History page (stage + right panel). */
  T.history = () => {
    const rows = histRows(), cur = rows.find((h) => h.id === T.hsel) || rows[0] || null; const railOn = T.rail() && !!cur; chrome('history', railOn);
    const cnt = (c) => E.history.filter((h) => kindOf(h) === c).length;
    const pcs = E.servers.map((s) => s.name), devs = X.devices.map((d) => d.name + (d.current ? ' (this one)' : '')).concat(['Unknown']);
    const head = pageHead('History', 'What happened here and on your PCs', ib('download', 'Export as CSV', 'data-v5="csv"') + ib('trash', 'Clear this app’s history', 'data-v5="clear"'));
    const filt = '<div class="b-filt"><label class="k-in sm b-fq">' + ic('filter', { size: 14 }) + '<input placeholder="Filter" aria-label="Filter by words, a file or an address" data-v5f="q" value="' + esc(T.f.q) + '"></label>' + sel('Kind', ['Kind'].concat(KINDS.map((c) => c + ' · ' + cnt(c)), ['Only what the PCs recorded'])) + sel('When', ['When', 'Today', 'Yesterday', 'Last 7 days', 'Last 30 days']) + sel('Computer', ['Computer'].concat(pcs)) + sel('Device', ['Device'].concat(devs)) + sel('Outcome', ['Outcome', 'Succeeded', 'Failed', 'Waiting', 'Running']) + '</div>';
    $('#stage').innerHTML = '<div class="d5 rfe-pages v5pg">' + head + filt + '<div data-v5hl>' + histList(rows, cur) + '</div></div>';
    if (railOn) $('#side').innerHTML = '<div class="d5 rfe-pages v5rw">' + histRail(cur) + '</div>';
    T.cur = cur;
  };

  /* True when the view is one this file draws and the wrapper is on. */
  T.owns = (v) => T.on() && (v === 'servers' || v === 'history');

  /* ---------------- actions (clicks and filter changes) ---------------- */
  const redraw = () => (st.view === 'history' ? T.history() : T.devices());
  document.addEventListener('click', (e) => {
    if (!T.owns(st.view)) return;
    const t = e.target; const pick = t.closest('[data-v5pick]'), ev = t.closest('[data-v5ev]'), act = t.closest('[data-v5]'), go = t.closest('[data-v5go]');
    if (pick) { T.sel = pick.dataset.v5pick; T.devices(); return; }
    if (ev) { T.hsel = ev.dataset.v5ev; T.history(); return; }
    if (go) { A.go(go.dataset.v5go); return; }
    if (t.closest('[data-v5limpick]')) { pickLimit(); return; }
    if (t.closest('[data-v5limclear]')) { setLimit(''); return; }
    if (!act) return; const a = act.dataset.v5;
    if (a === 'pair') A.pairDialog();
    else if (a === 'add') A.serverDialog();
    else if (a === 'addlocal') A.serverDialog(undefined, { host: '127.0.0.1', port: A.DEFAULT_PORT });
    else if (a === 'clearf') { Object.assign(T.f, { q: '', Kind: 'Kind', When: 'When', Computer: 'Computer', Device: 'Device', Outcome: 'Outcome' }); T.history(); }
    else if (a === 'copy' && T.cur) A.copy([T.cur.text, hostName(T.cur.host), T.cur.more].filter(Boolean).join('\n'), 'Details copied');
    else if (a === 'csv') {
      const q = E.csvCell;
      const csv = ['When,Kind,Computer,Outcome,What'].concat(histRows().map((h) => [new Date(h.at).toISOString(), kindOf(h), hostName(h.host), outOf(h), h.text].map(q).join(','))).join('\n');
      A.saveLocal('rfe-history.csv', csv).then((r) => A.snack('Saved ' + r.name + ' to ' + r.dir)).catch((err) => A.snack((err && err.message) || String(err), { error: true }));
    } else if (a === 'clear') {
      if (!E.history.length) return;
      A.dialog({ title: 'Clear history?', body: '<p>This removes all ' + E.history.length + ' entries from this window’s history. It cannot be undone.</p>', modal: true, actions: [{ id: 'k', label: 'Cancel', kind: 't', cb: (c) => c.close() }, { id: 'c', label: 'Clear history', kind: 'f', cb: (c) => { c.close(); E.history.length = 0; E.emit('history'); } }] });
    }
  });
  document.addEventListener('keydown', (e) => {
    if (!T.owns(st.view)) return;
    const t = e.target;
    if ((e.key === 'Enter' || e.key === ' ') && t.closest && (t.closest('[data-v5pick]') || t.closest('[data-v5ev]'))) { e.preventDefault(); t.click(); }
  });
  /* The folder to limit a device to, from the answer of the system's folder dialog: the path as chosen (the root is fine), or null
     when the person cancelled or the answer is not a usable path. */
  T.limitFromPick = (picked) => (typeof picked === 'string' && picked.trim() ? picked.trim() : null);
  /* Opens the system's folder dialog (on this computer) and, only if a folder was chosen, saves it as the shown device's folder limit. */
  function pickLimit() {
    const d = selDev(); if (!d) return;
    A.pickFolder2({ title: 'Folder ' + d.name + ' may open', startDir: (d.a && d.a.jailRoot) || null })
      .then((p) => { const v = T.limitFromPick(p); if (v !== null) setLimit(v); })
      .catch((x) => A.snack((x && x.message) || String(x), { error: true }));
  }
  /* Saves the folder limit of the shown device ('' removes it) and shows the agent's reason in the panel when it refuses. */
  function setLimit(v) {
    const d = selDev(), s = A.pairServer(); if (!d || !s) return; const err = $('[data-v5err]', $('#stage'));
    if (v === (d.a && d.a.jailRoot || '')) return; if (err) err.textContent = '';
    E.call('set_device_access', { host: s.id, id: d.id, patch: { jailRoot: v }, confirmSelf: !!d.current })
      .then(() => { A.snack(v ? d.name + ' is limited to ' + v : d.name + ' has no folder limit'); A.loadDevices(); })
      .catch((x) => { if (err) err.textContent = (x && x.message) || String(x); A.snack((x && x.message) || String(x), { error: true }); });
  }
  document.addEventListener('input', (e) => {
    const f = e.target.closest && e.target.closest('[data-v5f="q"]'); if (!f || !T.owns(st.view)) return;
    T.f.q = f.value; const rows = histRows(); const box = $('[data-v5hl]'); if (box) box.innerHTML = histList(rows, T.cur);
  });
  document.addEventListener('change', (e) => {
    const f = e.target.closest && e.target.closest('select[data-v5f]'); if (!f || !T.owns(st.view)) return;
    T.f[f.dataset.v5f] = f.dataset.v5f === 'Kind' ? f.value.split(' · ')[0] : f.value; T.history(); /* the Kind option text carries a count that changes; keep only the kind */
  });

  /* The Devices page keeps its old name: when the wrapper is on, the v5 page is drawn instead of the old one. */
  const old = A.pageDevices;
  A.pageDevices = () => { if (T.on()) { const a = document.activeElement; const key = a && a.closest ? (a.closest('[data-v5pick]') ? 'pick' : '') : ''; T.devices(); if (key) { const n = $('.ro.sel', $('#stage')); if (n) n.focus({ preventScroll: true }); } } else old(); };
  /* The open/closed choice of the devices section is kept across redraws (it opens by itself only while a phone is asking). */
  $('#stage').addEventListener('toggle', (e) => {
    const n = e.target; if (!n.matches || !n.matches('[data-cdv]')) return;
    if (n.open !== T.isOpen()) { T.open = n.open; if (n.open) T.loads(); }
  }, true);
  A.v5Rerender = redraw;
});
