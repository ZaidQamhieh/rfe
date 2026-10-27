/* RFE Tonal - part 4: transfer journal, where to save, device details, security log tools, encrypted settings backup, native file picker and file drops. */
(function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, st = A.state, X = A.X, F = A.fx, H = A.fxHandlers;
  const audit = F.audit, rel = F.rel, plural = F.plural;
  const THUMB = /[?&](thumb|selftest)=1/.test(location.search);
  const X4 = A.X4 = { mine: new Set(), audit: { range: 'all', dev: 'all' } };
  if (S.askSave === undefined) S.askSave = false;
  const KEY = 'rfe.journal.v1';
  const fmtB = (b) => U.fmtBytes(b);
  const fail = (e) => A.snack((e && e.message) || String(e), { error: true });

  /* ================= transfer journal ================= */
  /* What was still moving when the window closed, so the next start can offer to queue it again. */
  const jread = () => { try { return JSON.parse(localStorage.getItem(KEY) || 'null'); } catch (e) { return null; } };
  const jwrite = (v) => { if (THUMB) return; try { if (v) localStorage.setItem(KEY, JSON.stringify(v)); else localStorage.removeItem(KEY); } catch (e) { /* ignore */ } };
  const unfinished = () => E.tasks.filter((t) => X4.mine.has(t.id) && ['queued', 'running', 'paused', 'failed', 'waiting'].includes(t.state));
  /* Entries that "Queue again" could not queue stay in the journal until they are queued or discarded. */
  const jsnap = () => { const l = unfinished().map((t) => ({ dir: t.dir, host: t.host, name: t.name, srcDir: t.srcDir, dstDir: t.dstDir, bytes: t.bytes, done: t.done })); const kept = (A.journal && A.journal.retained) || []; const all = l.concat(kept.filter((k) => !l.some((x) => x.dir === k.dir && x.host === k.host && x.srcDir === k.srcDir && x.name === k.name))); return all.length ? { at: Date.now(), tasks: all } : null; };
  A.journal = { read: jread, snap: jsnap, write: jwrite, mem: null, retained: [] };
  const keep = () => { const j = jsnap(); A.journal.mem = j; jwrite(j); };
  const eDq = A.doEnqueue;
  /* "Ask where to save" and "Save as…" make the core open the system's folder dialog for this download. */
  A.doEnqueue = function (dir, host, srcDir, dstDir, names, o) {
    o = o || {};
    if (dir === 'down' && (o.askWhere || (S.askSave && !o._picked))) o = Object.assign({}, o, { askWhere: true });
    const r = eDq.call(this, dir, host, srcDir, dstDir, names, o); if (r && r.tasks) r.tasks.forEach((t) => X4.mine.add(t.id)); keep();
    /* The core saves every download in the one folder chosen in Settings; a window cannot name another. Say so rather than let the file turn up elsewhere. */
    if (dir === 'down' && dstDir && r && r.tasks && r.tasks.length && !o.askWhere && U.norm(dstDir) !== U.norm(A.downloadDir())) A.snack('Downloads are saved in ' + A.downloadDir() + '. Change that folder in Settings.');
    return r;
  };
  let jt = 0; E.on((t) => { if (t === 'tick' || t === 'transfers') { if (Date.now() - jt > 1500) { jt = Date.now(); keep(); } } });
  window.addEventListener('beforeunload', keep);
  const resumeDialog = (j) => {
    A.dialog({ icon: 'refresh', title: 'Queue unfinished transfers again?', width: 520, body: '<p class="fxp" style="margin-top:0">RFE was closed while ' + plural(j.tasks.length, 'transfer was', 'transfers were') + ' still going. Queue them again to carry on: a download continues from what it already has, an upload sends only what the server is missing.</p>' + j.tasks.map((t) => '<div class="lrow"><div class="gi">' + ic(t.dir === 'up' ? 'upload' : 'download') + '</div><div class="dm"><b>' + esc(t.name) + '</b><small>' + (t.dir === 'up' ? 'to ' : 'from ') + esc(A.hostName(t.host)) + ' · ' + fmtB(t.done) + ' of ' + fmtB(t.bytes) + ' done</small></div></div>').join(''),
      actions: [{ label: 'Discard', kind: 'tx', cb: () => { jwrite(null); A.journal.mem = null; A.journal.retained = []; audit('Discarded ' + plural(j.tasks.length, 'unfinished transfer', 'unfinished transfers')); } }, { id: 'go', label: 'Queue again', kind: 'f', icon: 'play', cb: () => {
        // The folders the transfers came from are not listed after a restart: list them first, because a
        // transfer is queued from what the listing holds. What cannot be queued stays in the journal.
        const where = new Map(); j.tasks.forEach((t) => { const h = t.dir === 'up' ? 'local' : t.host; where.set(h + '\n' + t.srcDir, [h, t.srcDir]); });
        Promise.allSettled([...where.values()].map((w) => E.fs.load(w[0], w[1]))).then(() => {
          let n = 0; const left = [];
          j.tasks.forEach((t) => { const r = E.enqueue({ dir: t.dir, host: t.host, srcDir: t.srcDir, dstDir: t.dstDir, names: [t.name] }); if (r && r.tasks && r.tasks.length) { n++; X4.mine.add(r.tasks[0].id); } else left.push(t); });
          A.journal.retained = left; const rest = left.length ? { at: j.at, tasks: left } : null; jwrite(rest); A.journal.mem = rest;
          A.snack(n ? 'Queued ' + plural(n, 'transfer', 'transfers') + (left.length ? '. ' + plural(left.length, 'could not be queued and stays', 'could not be queued and stay') + ' noted.' : '') : 'Could not queue them. Connect to the server first.', n ? undefined : { error: true });
        });
      } }] });
  };
  A.resumeDialog = resumeDialog;
  const ap0 = A.afterPage;
  A.afterPage = function (v) {
    if (ap0) ap0.apply(this, arguments);
    if (v === 'transfers') {
      const j = A.journal.mem || jread(); const stg = $('#stage'); const n = unfinished().length;
      const show = n || (j && j.tasks.length); const card = document.createElement('div'); card.className = 'fxc jcard'; card.innerHTML = '<div class="fxh"><span class="fxi">' + ic('database') + '</span><div><b>Transfer journal</b><small>' + plural(n, 'unfinished transfer', 'unfinished transfers') + '</small></div>' + (j && j.tasks.length ? '<button class="btn sm" style="margin-left:auto" data-fx="journal.clear">Clear journal</button>' : '') + '</div>';
      if (show) stg.appendChild(card);
    }
    if (v === 'devices') {
      $$('.dvrow').forEach((r) => { const b = r.querySelector('[data-fx^="dev.perm|"],[data-fx^="dev.menu|"]'); if (!b) return; const id = b.dataset.fx.split('|')[1]; const d = document.createElement('button'); d.className = 'btn sm tx'; d.dataset.fx = 'dev.details|' + id; d.textContent = 'Details'; b.parentNode.insertBefore(d, b.parentNode.firstChild); });
    }
    if (v === 'history' && st.histFilter === 'audit') { auditTools(); A.syncAudit(); }
  };
  H['journal.clear'] = () => { jwrite(null); A.journal.mem = null; A.journal.retained = []; X4.mine.clear(); A.snack('Journal cleared'); if (st.view === 'transfers') A.go('transfers'); };

  /* ================= the servers' security log, merged into History ================= */
  /* The agents record sign-ins, pairings, device changes and share links. They are shown next to what this app did. */
  A.syncAudit = async (hostIds) => {
    const ids = hostIds || E.servers.filter((s) => s.state === 'online').map((s) => s.id); let added = 0;
    await Promise.all(ids.map((id) => E.call('audit_page', { host: id }).then((r) => {
      A.X.auditDenied = A.X.auditDenied || new Set(); if (r.forbidden) { A.X.auditDenied.add(id); return; } A.X.auditDenied.delete(id); const seen = new Set(E.history.filter((h) => h.aid).map((h) => h.aid));
      for (const e of r.entries || []) { const aid = id + '#' + e.id; if (seen.has(aid)) continue; E.history.push({ id: 'ag' + aid, aid, at: Date.parse(e.at) || 0, kind: 'audit', agent: true, host: id, text: [e.action, e.target, e.detail].filter(Boolean).join(' · ') + (e.actor ? ' (' + e.actor + ')' : '') }); added++; }
    }).catch(() => {})));
    if (added) { E.history.sort((a, b) => b.at - a.at); E.emit('history'); if (st.view === 'devices') A.pageDevices(); }
    return added;
  };

  /* ================= device details ================= */
  A.deviceDetails = (id) => {
    const d = X.devices.find((x) => x.id === id); const s = A.pairServer(); if (!d || !s) return; const a = d.a;
    const acts = E.history.filter((h) => h.kind === 'audit' && h.text.includes(d.name)).slice(0, 5);
    const on = (k) => (a ? (a[k] ? '<span class="tag ok">Allowed</span>' : '<span class="tag">Not allowed</span>') : '<span class="tag">Unknown</span>');
    A.dialog({ icon: 'phone', title: d.name, width: 560, noFocus: true,
      body: '<div class="tags" style="margin:0 0 .75rem"><span class="tag ' + (!d.revoked && d.last && E.now() - d.last < 120000 ? 'ok' : '') + '">' + (d.revoked ? 'Revoked' : d.last && E.now() - d.last < 120000 ? 'Online now' : d.last ? 'Last seen ' + rel(d.last) : 'Never seen') + '</span>' + (d.ver ? '<span class="tag">' + esc(d.ver) + '</span>' : '') + (d.addr ? '<span class="tag">' + esc(d.addr) + '</span>' : '') + '</div>' +
        '<div class="kv2"><div><b>Device id</b><small class="mono">' + esc(d.id) + '</small></div></div><div class="kv2"><div><b>Paired</b><small>' + (d.paired ? U.fmtDate(d.paired) + ' · ' + rel(d.paired) : 'Unknown') + (d.viaLogin ? ' · signed in with an account' : ' · paired with a code') + '</small></div></div>' +
        '<h3 class="dsh">What it can do</h3><table class="capm"><tbody><tr><td>Browse and preview</td><td><span class="tag ok">Allowed</span></td></tr>' + [['download', 'Download files'], ['upload', 'Upload files'], ['modify', 'Rename and move'], ['delete', 'Delete'], ['share', 'Make share links'], ['viewApps', 'See apps'], ['launchApps', 'Launch apps']].map((r) => '<tr><td>' + r[1] + '</td><td>' + on(r[0]) + '</td></tr>').join('') + (a && a.readOnly ? '<tr><td>Read-only</td><td><span class="tag warn">On</span></td></tr>' : '') + '</tbody></table>' +
        (a ? '<h3 class="dsh">Folder limit</h3><div class="fld"><input id="dvj" value="' + esc(a.jailRoot || '') + '" placeholder="No limit: all of the roots the server allows" spellcheck="false"><span class="err" id="dvje"></span></div><div class="fxsub">The device can only open this folder and what is inside it. Leave it empty for no limit.</div>' : '') +
        '<h3 class="dsh">Recent activity</h3>' + (acts.length ? acts.map((h) => '<div class="evrow">' + ic('shield-check') + '<span>' + esc(h.text) + '</span><small>' + rel(h.at) + '</small></div>').join('') : '<div class="fxsub">No security events for this device yet.</div>'),
      actions: [{ label: 'Permissions…', kind: 'tx', disabled: !a, cb: () => { setTimeout(() => H['dev.perm'](id), 0); } }].concat(a ? [{ id: 'ok', label: 'Save folder limit', kind: 'f', icon: 'check', cb: (c) => {
        const v = $('#dvj', c.el).value.trim(); if (v === (a.jailRoot || '')) { return; }
        E.call('set_device_access', { host: s.id, id: d.id, patch: { jailRoot: v }, confirmSelf: !!d.current }).then(() => { audit('Folder limit of ' + d.name + ' set to ' + (v || 'none')); A.snack(v ? d.name + ' is limited to ' + v : d.name + ' has no folder limit'); A.loadDevices(); }).catch(fail); } }] : []) });
  };
  H['dev.details'] = (id) => A.deviceDetails(id);

  /* ================= security log tools ================= */
  const auditRows = () => {
    const a = X4.audit; const cut = a.range === 'today' ? E.now() - 864e5 : a.range === '7d' ? E.now() - 7 * 864e5 : 0;
    return E.history.filter((h) => h.kind === 'audit' && h.at >= cut && (a.dev === 'all' || h.text.includes(a.dev)));
  };
  function auditTools() {
    const stg = $('#stage'); const chips = $('.chips', stg); if (!chips || $('.audtools', stg)) return; const a = X4.audit;
    const devs = X.devices.map((d) => d.name); const bar = document.createElement('div'); bar.className = 'audtools';
    bar.innerHTML = '<label>Period<select data-ad="range"><option value="all">All time</option><option value="today">Last 24 hours</option><option value="7d">Last 7 days</option></select></label><label>Device<select data-ad="dev"><option value="all">All devices</option>' + devs.map((n) => '<option>' + esc(n) + '</option>').join('') + '</select></label><span class="sp"></span><span class="fxsub" id="audN"></span><button class="btn sm" data-fx="audit.csv">' + ic('download') + 'Export CSV</button>';
    chips.after(bar); $('[data-ad="range"]', bar).value = a.range; $('[data-ad="dev"]', bar).value = a.dev;
    const apply = () => { const rows = E.history.filter((h) => h.kind === 'audit').slice(0, 200); const keepSet = new Set(auditRows()); $$('.hrow', stg).forEach((r, i) => { r.hidden = !(rows[i] && keepSet.has(rows[i])); }); $('#audN', bar).textContent = auditRows().length + ' of ' + rows.length + ' events'; };
    bar.addEventListener('change', (e) => { const s = e.target.closest('[data-ad]'); if (!s) return; a[s.dataset.ad] = s.value; apply(); }); apply();
  }
  const csvEsc = (v) => '"' + String(v).replace(/"/g, '""') + '"';
  const toCSV = (rows) => 'time,event,server\n' + rows.map((h) => [U.fmtDate(h.at) + ' ' + U.fmtTime(h.at), h.text, h.host ? A.hostName(h.host) : ''].map(csvEsc).join(',')).join('\n') + '\n';
  /* A file made by this app goes into the download folder under a name that is not taken. */
  const saveLocal = A.saveLocal = async (name, text) => { const dir = A.downloadDir(); const path = await E.call('local_save_text', { dir, name, body: text }); E.fs.refresh('local', dir).catch(() => {}); return { dir, path, name: U.baseOf(path) }; };
  A.exportAuditCSV = async () => {
    const rows = auditRows(); try { const r = await saveLocal('rfe-audit-log.csv', toCSV(rows)); audit('Exported ' + rows.length + ' security events to ' + r.name); A.snack('Saved ' + rows.length + ' events to ' + r.name, { action: 'Show', onAction: () => A.openPath('local', r.path, true) }); } catch (e) { fail(e); }
  };
  H['audit.csv'] = () => A.exportAuditCSV();

  /* ================= encrypted settings backup ================= */
  const enc = new TextEncoder(), dec = new TextDecoder();
  const b64 = (u8) => btoa(String.fromCharCode.apply(null, Array.from(u8))), unb64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
  const strength = (p) => { let s = 0; if (p.length >= 8) s++; if (p.length >= 12) s++; if (/[a-z]/.test(p) && /[A-Z]/.test(p)) s++; if (/\d/.test(p)) s++; if (/[^A-Za-z0-9]/.test(p)) s++; return p.length < 8 ? 0 : Math.min(3, Math.max(1, s - 1)); };
  const derive = async (pass, salt, use) => { const km = await crypto.subtle.importKey('raw', enc.encode(pass), 'PBKDF2', false, ['deriveKey']); return crypto.subtle.deriveKey({ name: 'PBKDF2', salt, iterations: 250000, hash: 'SHA-256' }, km, { name: 'AES-GCM', length: 256 }, false, [use]); };
  A.encrypt = async (text, pass) => {
    const salt = crypto.getRandomValues(new Uint8Array(16)); const iv = crypto.getRandomValues(new Uint8Array(12)); const key = await derive(pass, salt, 'encrypt');
    const ct = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, enc.encode(text)));
    return JSON.stringify({ rfe: 'backup', v: 1, alg: 'AES-256-GCM', kdf: 'PBKDF2-SHA256-250000', salt: b64(salt), iv: b64(iv), ct: b64(ct) });
  };
  A.decrypt = async (envText, pass) => {
    let env; try { env = JSON.parse(envText); } catch (e) { throw new Error('This is not an RFE backup file'); } if (!env || env.rfe !== 'backup' || env.alg !== 'AES-256-GCM') throw new Error('This is not an RFE backup file');
    try { const key = await derive(pass, unb64(env.salt), 'decrypt'); return dec.decode(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: unb64(env.iv) }, key, unb64(env.ct))); } catch (e) { throw new Error('Wrong passphrase, or the file was changed'); }
  };
  const snapshot = () => JSON.stringify({ at: Date.now(), app: A.appVersion ? A.appVersion() : '', settings: Object.assign({}, S), favorites: X.favs, offline: X.pins.map((p) => ({ host: p.host, path: p.path, name: p.name, b: p.b })), syncRules: X.rules.map((r) => Object.assign({}, r, { ids: [] })), savedSearches: A.X3 ? A.X3.saved : [], servers: E.servers.map((s) => ({ name: s.name, addr: s.addr })) });
  const applySnapshot = (txt) => {
    const o = JSON.parse(txt); let n = 0; Object.keys(o.settings || {}).forEach((k) => { if (k === 'downloadDir') return; S[k] = o.settings[k]; n++; });
    if (o.favorites) { X.favs.length = 0; o.favorites.forEach((f) => X.favs.push(f)); } if (o.savedSearches && A.X3) A.X3.saved = o.savedSearches; if (o.syncRules) { X.rules.length = 0; o.syncRules.forEach((r) => X.rules.push(Object.assign({}, r, { ids: [] }))); }
    A.save(); F.persist(); if (S.theme) A.setTheme(S.theme); return { n, fav: (o.favorites || []).length, rules: (o.syncRules || []).length, saved: (o.savedSearches || []).length };
  };
  const meter = (v) => '<div class="pwm" data-s="' + v + '"><i></i><i></i><i></i><span>' + ['Too short (8+ characters)', 'Weak', 'Good', 'Strong'][v] + '</span></div>';
  A.backupDialog = () => {
    A.dialog({ icon: 'lock', title: 'Back up settings', width: 500, noFocus: true,
      body: '<p class="fxp" style="margin-top:0">Saves your settings, favorites, sync rules, saved searches and the list of computers into one file, encrypted with a passphrase. <b>Passwords, tokens and keys are never included.</b> Computers are listed, and you sign in to them again after a restore.</p><div class="fld"><label for="bkp">Passphrase</label><input id="bkp" type="password" autocomplete="new-password"></div><div id="bkm">' + meter(0) + '</div><div class="fld"><label for="bkp2">Repeat passphrase</label><input id="bkp2" type="password" autocomplete="new-password"></div><div class="hint bad" id="bke" hidden></div><div class="fxsub">If you forget the passphrase the file cannot be opened. There is no reset.</div>',
      actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Save backup', kind: 'f', icon: 'download', cb: (c) => {
        const p = $('#bkp', c.el).value, p2 = $('#bkp2', c.el).value, e = $('#bke', c.el); const bad = (m) => { e.hidden = false; e.textContent = m; return false; };
        if (strength(p) < 1) return bad('Use at least 8 characters.'); if (p !== p2) return bad('The passphrases don’t match.');
        A.encrypt(snapshot(), p).then((txt) => saveLocal('rfe-backup-' + new Date().toISOString().slice(0, 10) + '.rfebak', txt)).then((r) => { audit('Exported an encrypted settings backup (' + r.name + ')'); A.snack('Saved ' + r.name + ' to ' + r.dir, { action: 'Show', onAction: () => A.openPath('local', r.path, true) }); c.close(); }).catch((er) => bad(er.message)); return false; } }],
      onOpen: (c) => { $('#bkp', c.el).addEventListener('input', (e) => { $('#bkm', c.el).innerHTML = meter(strength(e.target.value)); }); $('#bkp', c.el).focus(); } });
  };
  A.restoreDialog = () => {
    let src = null; let name = '';
    A.dialog({ icon: 'upload', title: 'Restore settings', width: 500, noFocus: true,
      body: '<p class="fxp" style="margin-top:0">Choose a backup file. Restoring replaces your settings, favorites, sync rules and saved searches.</p><div class="kv2"><div><b id="rsn">No file chosen</b><small id="rss"></small></div><button class="btn sm" id="rsd">' + ic('folder-open') + 'Choose a file…</button></div><div class="fld"><label for="rsp">Passphrase</label><input id="rsp" type="password" autocomplete="off"></div><div class="hint bad" id="rse" hidden></div>',
      actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Restore', kind: 'f', icon: 'refresh', cb: (c) => {
        const e = $('#rse', c.el); const bad = (m) => { e.hidden = false; e.textContent = m; return false; }; const p = $('#rsp', c.el).value;
        if (!src) return bad('Choose a backup file first.'); if (!p) return bad('Enter the passphrase.');
        A.decrypt(src, p).then((txt) => { const r = applySnapshot(txt); audit('Restored settings from a backup (' + r.n + ' settings)'); A.snack('Restored ' + r.n + ' settings, ' + r.fav + ' favorites, ' + r.rules + ' sync rules'); c.close(); if (st.view === 'settings') A.pageSettings(); }).catch((er) => bad(er.message)); return false; } }],
      onOpen: (c) => { $('#rsd', c.el).addEventListener('click', () => {
        A.pickFiles({ title: 'Choose a settings backup', multiple: false, filters: [{ name: 'RFE backup', extensions: ['rfebak'] }, { name: 'All files', extensions: ['*'] }] }).then((l) => {
          if (!l.length) return; const p = l[0]; return E.call('local_read_text', { path: p }).then((r) => { src = r.text; name = U.baseOf(p); $('#rsn', c.el).textContent = name; $('#rss', c.el).textContent = fmtB(r.size); $('#rse', c.el).hidden = true; });
        }).catch(fail); }); $('#rsp', c.el).focus(); } });
  };

  /* ================= the system's file dialog, and files dropped on the window ================= */
  A.pickFiles = (o) => E.call('pick_files', { title: (o && o.title) || null, multiple: !o || o.multiple !== false, startDir: (o && o.startDir) || null, filters: (o && o.filters) || null });
  A.pickFolder2 = (o) => E.call('pick_folder', { title: (o && o.title) || null, startDir: (o && o.startDir) || null });
  /* Uploads files and folders given as paths on this computer into a folder of a server. */
  A.uploadPaths = async (paths, host, dstDir) => {
    host = host || A.mainPane.host; dstDir = dstDir || A.mainPane.path;
    if (!host || host === 'local' || !E.connected(host)) { A.snack('Open a connected server first to upload into it.', { error: true, action: 'Servers', onAction: () => A.go('servers') }); return; }
    const by = new Map(); for (const p of paths) { const dir = U.parentOf(p); if (!by.has(dir)) by.set(dir, []); by.get(dir).push(U.baseOf(p)); }
    let n = 0;
    for (const [dir, names] of by) {
      try { await E.fs.load('local', dir, true); } catch (e) { fail(e); continue; }
      const have = new Set((E.fs.list('local', dir) || []).map((x) => x.n)); const ok = names.filter((x) => have.has(x));
      if (!ok.length) { A.snack('Could not find ' + names[0] + ' on this computer.', { error: true }); continue; }
      const r = A.doEnqueue('up', host, dir, dstDir, ok); if (r && r.tasks) n += r.tasks.length;
    }
    return n;
  };
  A.chooseAndUpload = async () => {
    const m = A.mainPane; if (m.host === 'local' || !E.connected(m.host)) { A.snack('Open a connected server first to upload into it.', { error: true, action: 'Servers', onAction: () => A.go('servers') }); return; }
    try { const l = await A.pickFiles({ title: 'Upload to ' + A.hostName(m.host) + ':' + m.path, startDir: A.downloadDir() }); if (l.length) await A.uploadPaths(l, m.host, m.path); } catch (e) { fail(e); }
  };
  A.chooseDownloadFolder = async () => {
    try { const p = await E.call('choose_download_folder'); if (p) { E.settings.downloadDir = p; A.snack('Downloads go to ' + p); if (st.view === 'settings') A.pageSettings(); } } catch (e) { fail(e); }
  };
  A.resetDownloadFolder = async () => { try { E.settings.downloadDir = await E.call('reset_download_folder'); A.snack('Downloads go to ' + E.settings.downloadDir); if (st.view === 'settings') A.pageSettings(); } catch (e) { fail(e); } };
  (function listenForDrops() {
    const ev = window.__TAURI__ && window.__TAURI__.event; if (!ev || !ev.listen) return;
    const hint = document.createElement('div'); hint.className = 'dohint'; hint.hidden = true; hint.innerHTML = ic('upload') + 'Drop to upload to the open folder'; document.body.appendChild(hint);
    ev.listen('tauri://drag-enter', () => { if (st.view === 'files' && A.mainPane.host !== 'local') hint.hidden = false; });
    ev.listen('tauri://drag-leave', () => { hint.hidden = true; });
    ev.listen('tauri://drag-drop', (e) => {
      hint.hidden = true; const paths = (e.payload && e.payload.paths) || []; if (!paths.length || st.view !== 'files') return;
      /* A drop on a folder row goes into that folder; anywhere else it goes to the open folder. */
      const pos = e.payload.position; const m = A.mainPane; let dir = m.path;
      if (pos) { const dpr = window.devicePixelRatio || 1; const row = document.elementFromPoint(pos.x / dpr, pos.y / dpr); const li = row && row.closest && row.closest('.li[data-i], .tile[data-i]'); const it = li && m.el && m.el.contains(li) ? m.items[+li.dataset.i] : null; if (it && it.t === 'dir') dir = U.join(m.path, it.n); }
      A.uploadPaths(paths, m.host, dir);
    });
  })();

  /* ================= settings ================= */
  const se1 = A.settingsExtra;
  A.settingsExtra = (k) => {
    let h = se1(k); const row = k.row, sw = k.sw;
    const backup = '<div class="sg"><h3>Backup</h3>' + row('Back up settings', '', '<button class="btn" data-sx="backup">' + ic('lock') + 'Back up…</button>') + row('Restore settings', '', '<button class="btn" data-sx="restore">' + ic('upload') + 'Restore…</button>') + '</div>';
    h = h.replace('<div class="sg"><h3>Files</h3>', '<div class="sg"><h3>Files</h3>' + row('Ask where to save downloads', '', sw('askSave')));
    h = h.replace('<div class="sg"><h3>About</h3>', backup + '<div class="sg"><h3>About</h3>');
    return h;
  };
  const sa1 = A.settingsAction;
  A.settingsAction = (a) => {
    if (a === 'backup') { A.backupDialog(); return true; }
    if (a === 'restore') { A.restoreDialog(); return true; }
    if (a === 'dlfolder') { A.chooseDownloadFolder(); return true; }
    if (a === 'dlreset') { A.resetDownloadFolder(); return true; }
    return sa1(a);
  };

  /* ================= palette, boot ================= */
  A.paletteExtra.push((C) => {
    C('lock', 'Back up settings…', A.backupDialog); C('upload', 'Restore settings…', () => A.restoreDialog());
    C('upload', 'Upload files…', () => A.chooseAndUpload()); C('download', 'Export security log (CSV)', () => A.exportAuditCSV());
  });
  document.addEventListener('DOMContentLoaded', () => {
    if (THUMB || new URLSearchParams(location.search).get('demo')) return;
    const j = jread(); if (j && j.tasks && j.tasks.length) { A.journal.retained = j.tasks.slice(); /* kept until the person answers */ setTimeout(() => resumeDialog(j), 1200); }
  });
  A.fx4 = { X4, strength, toCSV, auditRows, snapshot, applySnapshot, unfinished, keep, jsnap };
})();
