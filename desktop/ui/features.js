/* RFE Tonal - file features on real calls: favorites, recents, offline copies, trash, share links, copy / move, archives, checksums,
   batch rename, phone pairing (QR) and the devices of a server. */
rfeModule('features', ['ui'], function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, fic = A.fic, st = A.state;
  const DAY = A.T.day, HOUR = A.T.hour;
  const rel = A.ago;
  const left = (ms) => { if (ms <= 0) return 'expired'; const d = Math.floor(ms / DAY), h = Math.floor((ms % DAY) / HOUR), m = Math.floor((ms % HOUR) / A.T.minute); return d ? d + ' d ' + h + ' h' : h ? h + ' h ' + m + ' min' : Math.max(1, m) + ' min'; };
  const audit = (text, extra) => { E.log('audit', text, extra); };
  /* a payload too long for the encoder must not break the card that shows it */
  A.qrSvg = (text, o) => { try { return QR.svg(text, o); } catch (e) { return '<div class="fxsub" role="img" aria-label="No QR code">This is too long for a QR code. Use the code or the link instead.</div>'; } };
  const fail = (e) => A.snack((e && e.message) || String(e), { error: true });
  const plural = A.plural;

  /* ---------- state ---------- */
  /* Favorites, recent files, offline copies and sync rules belong to this app on this computer. */
  const UKEY = 'rfe-tonal-user-v1';
  let saved = A.store.get(UKEY, {}) || {};
  const persist = () => { A.store.set(UKEY, { favs: X.favs, recents: X.recents, pins: X.pins.map((p) => Object.assign({}, p, { task: undefined })), rules: X.rules }); };
  const X = A.X = {
    favs: saved.favs || [], recents: saved.recents || [], pins: saved.pins || [], rules: saved.rules || [],
    trash: [], trashAt: 0, trashBusy: false, trashErr: '', trashSel: new Set(),
    shares: [], sharesAt: 0, sharesBusy: false, sharesErr: '',
    devices: [], devAt: 0, devBusy: false, devErr: '', devForbidden: false, devHost: null,
    inbox: [], pending: null,
    pair: { host: null, code: '', qr: '', exp: 0, life: 0, busy: false, err: '' },
    audit: [], auditAt: 0,
    tool: new URLSearchParams(location.search).get('tool') || 'fav', dupes: null, dupeBusy: false, storeHost: null, big: null, bigBusy: false, runs: {}, apps: {}, appHost: null
  };
  A.persistUser = persist;
  const online = A.online;
  const key = (h, p) => h + '|' + p;

  /* ---------- favorites, recents, offline copies ---------- */
  A.isFav = (h, p) => X.favs.some((f) => f.host === h && f.path === p);
  A.toggleFav = (h, p, name, t) => {
    const i = X.favs.findIndex((f) => f.host === h && f.path === p);
    if (i >= 0) { X.favs.splice(i, 1); A.snack('Removed “' + name + '” from favorites'); } else { X.favs.push({ host: h, path: p, name, t: t || 'dir' }); A.snack('Added “' + name + '” to favorites', { action: 'View', onAction: () => { X.tool = 'fav'; A.go('tools'); } }); }
    persist(); if (st.view === 'tools') A.pageTools();
  };
  /* How many favorites the quick menu lists. */
  const FAV_MENU_MAX = 6;
  A.favItems = () => (X.favs.length ? ['-', { head: 'Favorites' }].concat(X.favs.slice(0, FAV_MENU_MAX).map((f) => ({ icon: f.t === 'dir' ? 'star' : 'file', label: f.name, sub: A.hostName(f.host), onClick: () => A.openPath(f.host, f.path, f.t !== 'dir') }))) : []);
  /* Opens a folder, or a file inside its folder with the file selected. */
  A.openPath = (host, path, select) => {
    A.go('files'); if (A.mainPane.host !== host) A.setMainHost(host);
    if (!E.connected(host) && host !== 'local') { E.connect(host); A.snack('Connecting to ' + A.hostName(host) + '…'); return; }
    const m = A.mainPane; const dir = select ? U.parentOf(path) : path; m.go(dir);
    if (select) { const name = U.baseOf(path); E.fs.load(host, dir).then(() => { m.sel = new Set([name]); m.refreshItems(); m.anchor = m.items.findIndex((x) => x.n === name); m.render(); A.renderDetail(); }).catch(A.noop); }
  };
  const addRecent = (host, path, name) => { X.recents = X.recents.filter((r) => !(r.host === host && r.path === path)); X.recents.unshift({ host, path, name, at: E.now() }); X.recents.length = Math.min(X.recents.length, 12); persist(); };
  A.addRecent = addRecent;
  const pd0 = A.previewDialog; A.previewDialog = (host, dir, n) => { addRecent(host, U.join(dir, n.n), n.n); return pd0(host, dir, n); };
  /* An offline copy is a download kept in a folder of this computer; it counts as there once the transfer is done. */
  const offlineDir = () => (E.settings.downloadDir || (E.local && E.local.path) || '') + '/RFE offline';
  A.isPinned = (h, p) => X.pins.some((x) => x.host === h && x.path === p);
  A.togglePin = (h, p, n) => {
    const i = X.pins.findIndex((x) => x.host === h && x.path === p);
    if (i >= 0) { X.pins.splice(i, 1); persist(); A.snack('Removed from offline: “' + n.n + '”'); return; }
    const res = E.enqueue({ dir: 'down', host: h, srcDir: U.parentOf(p), dstDir: offlineDir(), names: [n.n] });
    if (res.error) { A.snack(res.error, { error: true }); return; }
    X.pins.push({ host: h, path: p, name: n.n, b: n.b, at: E.now(), tid: res.tasks[0] ? res.tasks[0].id : null }); persist();
    A.snack('Making “' + n.n + '” available offline (' + U.fmtBytes(n.b) + ')', { action: 'View', onAction: () => { X.tool = 'fav'; A.go('tools'); } });
  };
  /* 0..1 while its download runs, 1 when done (or when the transfer is no longer listed: it finished before). */
  A.pinProg = (p) => { const t = p.tid ? E.task(p.tid) : null; if (!t) return 1; return t.state === 'done' ? 1 : t.state === 'failed' ? 1 : Math.min(0.99, t.bytes ? t.done / t.bytes : 0); };

  /* ---------- trash (the agent keeps it) ---------- */
  A.loadTrash = async () => {
    if (X.trashBusy) return; X.trashBusy = true; X.trashErr = ''; const hosts = online();
    const rows = []; const errs = [];
    await Promise.all(hosts.map((s) => E.call('trash_items', { host: s.id }).then((list) => { for (const it of list) rows.push({ host: s.id, id: it.id, name: it.name, path: it.originalPath, at: Date.parse(it.deletedAt) || 0, b: it.size || 0, isDir: !!it.isDir }); }).catch((e) => { errs.push(s.name + ': ' + e.message); })));
    /* Items older than the chosen number of days are removed for good as the Trash opens. */
    const cut = Date.now() - (+S.trashDays || E.TRASH_DAYS_DEFAULT) * DAY; const stale = rows.filter((r) => r.at && r.at < cut);
    if (stale.length) {
      /* They are removed for good, so the person is asked first (once for each set of items), and what is older than the setting stays listed until they answer. */
      const key = stale.map((r) => r.host + r.id).sort().join(','); const days = +S.trashDays || E.TRASH_DAYS_DEFAULT;
      if (X.trashAsked !== key) {
        X.trashAsked = key;
        A.askOldTrash({ icon: 'trash', title: 'Delete old items from Trash?', width: 460, modal: true, body: '<p>' + stale.length + (stale.length === 1 ? ' item has' : ' items have') + ' been in Trash longer than ' + days + ' days (your Keep items in Trash for setting). Deleting them cannot be undone.</p>',
          actions: [{ id: 'k', label: 'Keep them', kind: 't', cb: (c) => c.close() }, { id: 'd', label: 'Delete ' + stale.length, kind: 'f', cb: async (c) => {
            c.close(); const by = {}; stale.forEach((r) => { (by[r.host] = by[r.host] || []).push(r); }); let gone = 0; const bad = [];
            for (const h of Object.keys(by)) { try { await E.call('trash_empty_items', { host: h, ids: by[h].map((r) => r.id) }); gone += by[h].length; } catch (e) { bad.push(A.hostName(h) + ': ' + e.message); } }
            if (gone) A.snack('Removed ' + gone + (gone === 1 ? ' item' : ' items') + ' older than ' + days + ' days from Trash'); if (bad.length) A.snack(bad.join(' · '), { error: true });
            X.trashAt = 0; if (A.state.view === 'tools') A.pageTools(); } }] });
      }
    }
    rows.sort((a, b) => b.at - a.at); X.trash = rows; X.trashAt = E.now(); X.trashBusy = false; X.trashErr = errs.join(' · ');
    for (const k of [...X.trashSel]) if (!rows.some((r) => r.host + '~' + r.id === k)) X.trashSel.delete(k);
    if (st.view === 'tools') A.pageTools(); if (A.renderRail) A.renderRail(); if (A.v5Files && A.v5Files.later) A.v5Files.later();
  };
  const trashRows = () => X.trash.map((r) => ({ t: { host: r.host, dir: U.parentOf(r.path), at: r.at }, n: { n: r.name, t: r.isDir ? 'dir' : 'file', b: r.b, mod: r.at }, k: r.host + '~' + r.id, r }));
  A.trashRows = trashRows;
  async function restoreRows(rows) {
    const by = {}; rows.forEach((r) => { (by[r.r.host] = by[r.r.host] || []).push(r); });
    let ok = 0;
    for (const h of Object.keys(by)) {
      try { await E.call('trash_restore_items', { host: h, ids: by[h].map((r) => r.r.id) }); ok += by[h].length; by[h].forEach((r) => X.trashSel.delete(r.k)); const dirs = new Set(by[h].map((r) => r.t.dir)); dirs.forEach((d) => E.fs.drop(h, d)); }
      catch (e) { fail(e); }
    }
    if (ok) { audit('Restored ' + plural(ok, 'item', 'items') + ' from Trash'); A.snack('Restored ' + plural(ok, 'item', 'items')); }
    await A.loadTrash(); A.mainPane.refresh && A.mainPane.refresh(true);
  }
  async function purgeRows(rows) {
    const by = {}; rows.forEach((r) => { (by[r.r.host] = by[r.r.host] || []).push(r.r.id); });
    let ok = 0;
    for (const h of Object.keys(by)) { try { await E.call('trash_empty_items', { host: h, ids: by[h] }); ok += by[h].length; } catch (e) { fail(e); } }
    rows.forEach((r) => X.trashSel.delete(r.k));
    if (ok) { audit('Deleted ' + plural(ok, 'item', 'items') + ' permanently from Trash'); A.snack('Deleted forever: ' + plural(ok, 'item', 'items')); }
    await A.loadTrash();
  }

  /* ---------- folder picker (copy / move) ---------- */
  A.pickFolder = (o, cb) => {
    let path = o.start || '/'; const host = o.host;
    const draw = (ctl) => {
      const lst = E.fs.list(host, path); const loading = lst === null && E.fs.state(host, path) !== 'error'; const err = E.fs.error(host, path);
      const kids = (lst || []).filter((x) => x.t === 'dir').sort((a, b) => a.n.localeCompare(b.n));
      const parts = U.segs(path); let acc = ''; const crumbs = ['<button data-p="/">' + esc(A.hostName(host)) + '</button>'].concat(parts.map((p) => { acc += '/' + p; return '<button data-p="' + esc(acc) + '">' + esc(p) + '</button>'; })).join('<i>›</i>');
      const body = loading ? '<div class="pnone">Loading…</div>' : err ? '<div class="pnone" style="color:var(--err)">' + esc(err.message) + '</div>' : kids.length ? kids.slice(0, A.ROW_CAP).map((k) => '<div class="pi" data-p="' + esc(k.path || U.join(path, k.n)) + '">' + ic(E.fs.canRead(k) ? 'folder' : 'lock') + '<span>' + esc(k.n) + '</span></div>').join('') : '<div class="pnone">No subfolders</div>';
      $('.fxpick', ctl.el).innerHTML = '<div class="fxcr">' + crumbs + '</div><div class="fxpl">' + (path !== '/' ? '<div class="pi" data-p="' + esc(U.parentOf(path)) + '">' + ic('arrow-up') + '<span>Up</span></div>' : '') + body + '</div><div class="fxsel">' + o.verb + ' into <b>' + esc(path) + '</b>' + (!E.fs.canWrite(null, host) ? ' · <span style="color:var(--err)">read-only for you</span>' : '') + '</div>';
      const b = ctl.btn.go; if (b) b.disabled = loading || !!err || !E.fs.canWrite(null, host);
    };
    let off = null;
    A.dialog({
      icon: o.icon, title: o.title, width: 520, body: '<div class="fxpick"></div>', enter: 'go',
      actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: o.verb, kind: 'f', icon: o.icon, cb: () => { if (off) off(); cb(path); } }],
      onOpen: (ctl) => {
        const go = (p) => { path = p; draw(ctl); E.fs.load(host, p).catch(A.noop); };
        draw(ctl); E.fs.load(host, path).catch(A.noop);
        off = E.on((t, d) => { if (t === 'fs' && d && d.host === host && ctl.el.isConnected) draw(ctl); else if (!ctl.el.isConnected && off) off(); });
        ctl.el.addEventListener('click', (e) => { const p = e.target.closest('[data-p]'); if (p) go(p.dataset.p); });
      }
    });
  };
  async function fileOp(pane, kind) {
    const s = pane.selected(); if (!s.length) return; const names = s.map((x) => x.n); const move = kind === 'move';
    A.pickFolder({ host: pane.host, start: pane.path, icon: move ? 'move' : 'copy', title: (move ? 'Move ' : 'Copy ') + (names.length === 1 ? '“' + names[0] + '”' : names.length + ' items') + ' to…', verb: move ? 'Move here' : 'Copy here' }, async (dst) => {
      try {
        if (pane.host === 'local') throw new Error('Copy and move work on computers. Drag files between folders on this computer in your file manager.');
        const done = await E.fs.copy(pane.host, pane.path, names, dst, move);
        pane.sel.clear(); pane.refreshItems(); pane.render(); A.renderDetail();
        audit((move ? 'Moved ' : 'Copied ') + plural(done.length, 'item', 'items') + ' to ' + dst + ' on ' + A.hostName(pane.host));
        A.snack((move ? 'Moved ' : 'Copied ') + plural(done.length, 'item', 'items') + ' to ' + dst, { action: 'Show', onAction: () => A.openPath(pane.host, dst, false) });
      } catch (e) { fail(e); }
    });
  }

  /* ---------- archives, checksum, batch rename ---------- */
  const isArc = (n) => n.t === 'file' && /\.(zip|tar|tar\.gz|tgz|tar\.zst|7z|rar)$/i.test(n.n);
  const pathOf = (pane, n) => n.path || U.join(pane.path, n.n);
  async function compress(pane) {
    const s = pane.selected(); if (!s.length) return;
    const base = s.length === 1 ? s[0].n.replace(/\.[^.]+$/, '') : 'Archive'; let name = base + '.zip'; if (E.fs.exists(pane.host, pane.path, name)) name = E.fs.uniqueName(pane.host, pane.path, name);
    A.snack('Compressing ' + plural(s.length, 'item', 'items') + '…');
    try {
      await E.call('files_compress', { host: pane.host, sources: s.map((n) => pathOf(pane, n)), dest: U.join(pane.path, name) });
      await E.fs.refresh(pane.host, pane.path); pane.sel = new Set([name]); pane.refreshItems(); pane.render(); A.renderDetail();
      audit('Compressed ' + plural(s.length, 'item', 'items') + ' into ' + name + ' on ' + A.hostName(pane.host)); A.snack('Created ' + name);
    } catch (e) { fail(e); }
  }
  async function extract(pane, n) {
    A.snack('Extracting ' + n.n + '…');
    try {
      const r = await E.call('files_extract', { host: pane.host, archive: pathOf(pane, n), destDir: pane.path });
      await E.fs.refresh(pane.host, pane.path); pane.refreshItems(); pane.render();
      audit('Extracted ' + n.n + ' on ' + A.hostName(pane.host)); A.snack('Extracted to “' + (r && r.name ? r.name : pane.path) + '”', { action: 'Open', onAction: () => pane.go(r && r.path ? r.path : pane.path) });
    } catch (e) { fail(e); }
  }
  const ALGOS = [['sha256', 'SHA-256'], ['sha1', 'SHA-1'], ['md5', 'MD5']];
  function checksumDialog(pane, n) {
    let algo = 'sha256'; let value = ''; let err = '';
    const d = A.dialog({ icon: 'hash', title: 'Checksum', width: 520, body: '<div class="fxkv"><b>' + esc(n.n) + '</b><small>' + U.fmtBytes(n.b) + '</small></div><div class="seg" id="ckalg" style="margin:0 0 .75rem">' + ALGOS.map((a) => '<button data-v="' + a[0] + '" class="' + (a[0] === algo ? 'on' : '') + '">' + a[1] + '</button>').join('') + '</div><div class="fxhash"><small id="cklab">SHA-256</small><code id="ckval">Computing…</code></div><div class="hint" style="margin:.75rem 0 0">Computed on the computer by <span class="mono">rfe</span>. Compare it with the value the publisher lists.</div>',
      actions: [{ label: 'Close', kind: 'tx' }, { id: 'copy', label: 'Copy', kind: 'f', icon: 'copy', cb: () => { if (value) { A.copy(value, 'Checksum copied'); } return false; } }],
      onOpen: (ctl) => {
        const run = () => {
          value = ''; err = ''; const v = $('#ckval', ctl.el); v.textContent = 'Computing…'; $('#cklab', ctl.el).textContent = ALGOS.find((a) => a[0] === algo)[1]; if (ctl.btn.copy) ctl.btn.copy.disabled = true; const mine = algo;
          E.call('files_checksum', { host: pane.host, path: pathOf(pane, n), algo }).then((r) => { if (mine !== algo || !ctl.el.isConnected) return; value = r.checksum; v.textContent = value; if (ctl.btn.copy) ctl.btn.copy.disabled = false; })
            .catch((e) => { if (mine !== algo || !ctl.el.isConnected) return; v.textContent = e.message; v.style.color = 'var(--err)'; });
        };
        ctl.el.addEventListener('click', (e) => { const b = e.target.closest('#ckalg button'); if (b) { algo = b.dataset.v; $$('#ckalg button', ctl.el).forEach((x) => x.classList.toggle('on', x === b)); $('#ckval', ctl.el).style.color = ''; run(); } });
        run();
      } });
    return d;
  }
  function batchRename(pane) {
    const s = pane.selected(); if (s.length < 2) return; let pat = '{name}-{n}'; const start = 1;
    const calc = () => s.map((n, i) => { const m = /^(.*?)(\.[^.]+)?$/.exec(n.n); return pat.replace(/\{name\}/g, m[1]).replace(/\{n\}/g, String(start + i).padStart(2, '0')).replace(/\{ext\}/g, (m[2] || '').slice(1)) + (/\{ext\}/.test(pat) ? '' : m[2] || ''); });
    A.dialog({
      icon: 'edit', title: 'Rename ' + s.length + ' items', width: 520,
      body: '<div class="fld"><label>Pattern</label><input id="brp" value="' + pat + '" spellcheck="false"><span class="err" id="bre"></span></div><div class="hint" style="margin:0 0 .625rem"><span class="mono">{name}</span> original name · <span class="mono">{n}</span> counter · <span class="mono">{ext}</span> extension</div><div class="fxbr" id="brl"></div>',
      enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Rename', kind: 'f', icon: 'edit', cb: (ctl) => {
        const out = calc(); const set = new Set(out); const err = $('#bre', ctl.el);
        if (set.size !== out.length) { err.textContent = 'Names would collide'; return false; }
        const clash = out.find((o, i) => o !== s[i].n && E.fs.exists(pane.host, pane.path, o) && !s.some((x) => x.n === o)); if (clash) { err.textContent = '“' + clash + '” already exists'; return false; }
        (async () => {
          let done = 0; try { for (let i = 0; i < s.length; i++) { if (s[i].n !== out[i]) { await E.fs.rename(pane.host, pane.path, s[i].n, out[i]); done++; } } } catch (e) { fail(e); }
          pane.sel = new Set(out); pane.refreshItems(); pane.render(); if (done) { audit('Renamed ' + done + ' items on ' + A.hostName(pane.host)); A.snack('Renamed ' + plural(done, 'item', 'items')); }
        })();
      } }],
      onOpen: (ctl) => { const draw = () => { $('#brl', ctl.el).innerHTML = calc().slice(0, 8).map((o, i) => '<div><span>' + esc(s[i].n) + '</span>' + ic('arrow-right') + '<b>' + esc(o) + '</b></div>').join('') + (s.length > 8 ? '<div class="more">+ ' + (s.length - 8) + ' more</div>' : ''); }; draw(); $('#brp', ctl.el).addEventListener('input', (e) => { pat = e.target.value; $('#bre', ctl.el).textContent = ''; draw(); }); }
    });
  }

  /* The "Delete old items from Trash?" question. A page style that shows it differently (the v5 Trash banner) replaces this. */
  A.askOldTrash = (o) => A.dialog(o);

  /* ---------- share links (the agent mints them and keeps only their hashes) ---------- */
  const LIFE = [['15m', 900, '15 min', '15 minutes'], ['1h', 3600, '1 hour', '1 hour'], ['24h', 86400, '24 hours', '24 hours']];
  A.loadShares = async () => {
    if (X.sharesBusy) return; X.sharesBusy = true; X.sharesErr = ''; const rows = []; const errs = [];
    await Promise.all(online().map((s) => E.call('share_links', { host: s.id }).then((list) => { for (const l of list) rows.push({ host: s.id, hash: l.tokenHash, path: l.path, name: U.baseOf(l.path) || l.path, exp: (l.expiresAt || 0) * 1000 }); }).catch((e) => { errs.push(s.name + ': ' + e.message); })));
    rows.sort((a, b) => b.exp - a.exp); X.shares = rows; X.sharesAt = E.now(); X.sharesBusy = false; X.sharesErr = errs.join(' · ');
    if (st.view === 'tools') A.pageTools(); if (A.v5Files && A.v5Files.later) A.v5Files.later();
  };
  function shareDialog(host, dir, n) {
    let life = '24h'; const path = n.path || U.join(dir, n.n);
    A.dialog({
      icon: 'link', title: 'Share link', width: 500,
      body: '<div class="fxkv"><b>' + esc(n.n) + '</b><small>' + U.fmtBytes(n.b) + ' on ' + esc(A.hostName(host)) + '</small></div><div class="fld"><label>Link expires after</label><div class="seg" data-g="life">' + LIFE.map((o) => '<button data-v="' + o[0] + '" class="' + (o[0] === life ? 'on' : '') + '">' + o[2] + '</button>').join('') + '</div></div><div class="hint" style="margin:0">Anyone with the link can download this one file until it expires. You can turn the link off from Tools → Share links.</div>',
      enter: 'go',
      actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Create link', kind: 'f', icon: 'link', cb: () => {
        E.call('share_mint', { host, path, expiresInSeconds: LIFE.find((o) => o[0] === life)[1] }).then((r) => {
          const sh = { host, hash: r.tokenHash, path, name: n.n, exp: (r.expiresAt || 0) * 1000, url: r.url };
          audit('Share link created for ' + n.n + ' (' + LIFE.find((o) => o[0] === life)[3] + ')'); shareResult(sh); X.sharesAt = 0; if (st.view === 'tools') A.loadShares();
        }).catch(fail);
      } }],
      onOpen: (ctl) => { ctl.el.addEventListener('click', (e) => { const b = e.target.closest('.seg button'); if (b) { $$('button', b.parentNode).forEach((x) => x.classList.toggle('on', x === b)); life = b.dataset.v; } }); }
    });
  }
  function shareResult(sh) {
    /* The agent keeps only a hash of the link: closing before it is copied loses it, so the first close asks. */
    let copied = false, warned = false;
    const guard = () => { if (copied || warned) return undefined; warned = true; A.snack('This link can’t be shown again. Copy it, or press Done again to close.'); return false; };
    A.dialog({ icon: 'check-circle', title: 'Link ready', width: 460, body: '<div class="qrwrap sm"><div class="qrbox">' + A.qrSvg(sh.url, { px: 168, fg: '#0b1110', bg: 'var(--qr-bg)', border: 2 }) + '</div></div><div class="fxhash"><small>' + esc(sh.name) + ' · expires in ' + left(sh.exp - E.now()) + '</small><code>' + esc(sh.url) + '</code></div>', actions: [{ label: 'Done', kind: 'tx', cb: () => guard() }, { label: 'Copy link', kind: 'f', icon: 'copy', cb: () => { copied = true; A.copy(sh.url, 'Link copied'); return false; } }], beforeClose: () => guard() });
  }
  A.shareDialog = shareDialog; A.shareResult = shareResult;

  /* ---------- context menu additions ---------- */
  const P = A.Pane.prototype, ctx0 = P.ctxItems;
  P.ctxItems = function (onItem) {
    const base = ctx0.call(this, onItem); if (!onItem || !this.online()) return base;
    const s = this.selected(); const one = s.length === 1 ? s[0] : null; const remote = this.host !== 'local'; const f = (l) => base.find((i) => i && i.label === l);
    const full = one ? pathOf(this, one) : '';
    const out = [base[0], base[1], '-'];
    if (remote) {
      out.push({ icon: 'copy', label: 'Copy to…', onClick: () => fileOp(this, 'copy') }, { icon: 'move', label: 'Move to…', onClick: () => fileOp(this, 'move') });
      out.push(one && isArc(one) ? { icon: 'archive', label: 'Extract here', onClick: () => extract(this, one) } : { icon: 'archive', label: 'Compress to .zip', onClick: () => compress(this) });
    }
    out.push(f('Rename'));
    if (s.length > 1) out.push({ icon: 'edit', label: 'Rename ' + s.length + ' items…', onClick: () => batchRename(this) });
    out.push('-');
    if (remote && one && one.t === 'file') out.push({ icon: 'link', label: 'Share link…', onClick: () => shareDialog(this.host, this.path, one) });
    if (remote && one) out.push({ icon: 'star', label: A.isFav(this.host, full) ? 'Remove from favorites' : 'Add to favorites', onClick: () => A.toggleFav(this.host, full, one.n, one.t === 'dir' ? 'dir' : 'file') });
    if (remote && one && one.t === 'file') out.push({ icon: 'pin', label: A.isPinned(this.host, full) ? 'Remove from offline' : 'Make available offline', onClick: () => A.togglePin(this.host, full, one) });
    if (remote && one && one.t === 'file') out.push({ icon: 'hash', label: 'Checksum…', onClick: () => checksumDialog(this, one) });
    out.push('-', f('Copy path'), f('Properties'), '-', f('Delete'));
    return out.filter((x, i, a) => !(x === '-' && (a[i - 1] === '-' || i === 0 || i === a.length - 1)) && x !== undefined);
  };

  /* ---------- pairing a phone ---------- */
  const pairSrv = () => {
    /* The Devices page follows the server chosen in the top bar; a pick made in the pairing card holds until the top bar changes. */
    const mh = A.mainPane && A.mainPane.host; const ms = mh && mh !== 'local' ? E.server(mh) : null;
    if (mh !== X.pair.lastMain) { X.pair.lastMain = mh; if (ms && ms.state === 'online' && X.pair.host !== ms.id) { X.pair.host = ms.id; X.pair.code = ''; X.devices = []; X.devAt = 0; } }
    let s = E.server(X.pair.host); if (!s || s.state !== 'online') { s = online()[0] || (A.localPC && A.localPC.srv()) || null; if ((s && s.id) !== X.pair.host) { X.pair.host = s ? s.id : null; X.pair.code = ''; X.devices = []; X.devAt = 0; } } return s; };
  A.pairServer = pairSrv;
  /* No computer added: the code comes from the agent on this PC, so a phone can still be paired with it. */
  const LOCAL = { id: '', name: 'this PC', fp: '', local: true };
  const qrObj = () => { try { return JSON.parse(X.pair.qr || '{}'); } catch (e) { return {}; } };
  const life = () => X.pair.life || 600000;
  const mmss = (ms) => { const s = Math.max(0, Math.ceil(ms / 1000)); return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0'); };
  async function newCode(silent) {
    const s = pairSrv() || LOCAL; if (!s || X.pair.busy) return; X.pair.busy = true; X.pair.err = ''; refreshPair();
    try {
      const r = await E.call('generate_pairing_code', { host: s.id, ttlSeconds: (+S.pairLife || E.PAIR_LIFE_DEFAULT) * 60 });
      if (r.status !== 'ok') { X.pair.code = ''; X.pair.qr = ''; X.pair.err = 'This login cannot create pairing codes. Sign in to ' + s.name + ' with its owner account.'; }
      else { X.pair.code = r.code; X.pair.qr = r.qr; X.pair.life = r.expiresInSeconds * 1000; X.pair.exp = Date.now() + X.pair.life; if (!silent) audit('New pairing code generated for ' + s.name); }
    } catch (e) { X.pair.code = ''; X.pair.qr = ''; X.pair.err = e.message; }
    X.pair.busy = false; refreshPair();
  }
  A.pairNewCode = newCode;
  function pairInner() {
    const s = pairSrv() || (X.pair.isLocal || X.pair.wantLocal ? LOCAL : null); const p = X.pair; const ms = p.exp - Date.now(); const dead = !!p.code && ms <= 0; const q = qrObj();
    const chips = '<div class="fxchips" role="group" aria-label="Computer">' + (online().length ? online() : []).map((x) => '<button class="chip' + (s && x.id === s.id ? ' on' : '') + '" aria-pressed="' + !!(s && x.id === s.id) + '" data-fx="pair.srv|' + esc(x.id) + '">' + ic('server') + esc(x.name) + '</button>').join('') + '</div>';
    const head = '<div class="fxh"><span class="fxi">' + ic('phone') + '</span><div><b>Pair a phone</b></div></div>';
    if (!s) return head + '<div class="fxempty">' + ic('server', { size: 40 }) + '<b>No connected computer</b><span>Connect a computer first. Its agent makes the pairing code.</span></div>';
    if (p.err) return head + chips + '<div class="fxempty">' + ic('shield', { size: 40 }) + '<b>No code</b><span>' + esc(p.err) + '</span></div><div class="pbtns"><button class="btn t sm" data-fx="pair.new">' + ic('refresh') + 'Try again</button></div>';
    if (!p.code) return head + chips + '<div class="fxempty">' + ic('refresh', { size: 40 }) + '<b>' + (p.busy ? 'Asking ' + esc(s.name) + '…' : 'No code yet') + '</b><span>The agent makes a one-time code that works for ' + Math.round(life() / A.T.minute) + ' minutes.</span></div>';
    const addrs = [q.address && ['LAN', q.address], q.tailscaleAddress && ['Tailscale', q.tailscaleAddress]].filter(Boolean);
    return head + chips +
      '<div class="qrwrap"><div class="qrbox' + (dead ? ' dead' : '') + '" id="pairQR">' + A.qrSvg(p.qr, { px: 212, fg: '#0b1110', bg: 'var(--qr-bg)', border: 2, label: 'Pairing QR code for ' + s.name }) + (dead ? '<div class="qrx"><b>This pairing code has expired</b><button class="btn f sm" data-fx="pair.new">' + ic('refresh') + 'New code</button></div>' : '') + '</div></div>' +
      '<div class="pcode"><span class="srOnly">Pairing code: </span><span id="pairCode">' + esc(p.code.slice(0, 4) + ' ' + p.code.slice(4)) + '</span><button class="ib sm" data-fx="pair.copycode" title="Copy code" aria-label="Copy code">' + ic('copy') + '</button></div>' +
      '<div class="ptimer"><div class="bar"><i id="pairBar" style="width:' + Math.max(0, Math.min(100, ms / life() * 100)) + '%"></i></div><small id="pairT">' + (dead ? 'Expired' : 'Expires in ' + mmss(ms)) + '</small></div>' +
      '<details class="adtech ptech"><summary>Technical details</summary>' + (addrs.length ? addrs.map((a) => '<div class="paddr"><span>' + ic(a[0] === 'LAN' ? 'wifi' : 'globe') + '<code>' + esc(a[1]) + '</code></span></div>').join('') : '') +
      '<div class="paddr"><span class="fp">SHA-256 ' + esc(s.fp.slice(0, 23)) + '…</span></div></details>' +
      '<div class="pbtns"><button class="btn t sm" data-fx="pair.new">' + ic('refresh') + 'New code</button><button class="btn sm" data-fx="pair.link">' + ic('copy') + 'Copy code data</button><button class="btn sm" data-fx="pair.save">' + ic('download') + 'Save QR</button></div>';
  }
  A.pairInner = pairInner;
  function refreshPair() { $$('.fxpair').forEach((el) => { el.innerHTML = pairInner(); }); }
  setInterval(() => {
    if (!X.pair.code) return; const ms = X.pair.exp - Date.now(); const t = $('#pairT'); if (!t) return;
    if (ms <= 0) { if (!$('#pairQR.dead')) refreshPair(); return; } t.textContent = 'Expires in ' + mmss(ms); const b = $('#pairBar'); if (b) b.style.width = Math.max(0, ms / life() * 100) + '%';
  }, 1000);
  /* A card is shown: make sure there is a code for it. */
  A.ensureCode = (allowLocal) => { let s = pairSrv(); if (s && X.pair.isLocal) { X.pair.isLocal = false; X.pair.code = ''; } if (!s && allowLocal) { s = LOCAL; X.pair.wantLocal = true; if (!X.pair.isLocal) X.pair.code = ''; } if (s && (!X.pair.code || X.pair.exp - Date.now() <= 0) && !X.pair.busy && !X.pair.err) newCode(true); };
  A.pairDialog = () => { A.dialog({ title: '', width: 440, cls: 'fxpd', body: '<div class="fxpair fxc flat"></div>', actions: [{ label: 'Done', kind: 'tx' }, { label: 'Manage devices', kind: 'f', icon: 'phone', cb: () => { A.go('devices'); } }], onOpen: (ctl) => { $('.fxpair', ctl.el).innerHTML = pairInner(); A.ensureCode(true); }, noFocus: true }); const h = $('.dlg.fxpd h2'); if (h) h.remove(); };

  /* ---------- the devices of the server and the pairing requests waiting for an answer ---------- */
  /* True while the page that shows the devices list is open: the old Devices page, or (v5 window) the Computers page with its devices section. */
  A.devPageOpen = () => st.view === 'devices' || (st.view === 'servers' && !!A.v5on && A.v5on());
  A.loadDevices = async () => {
    const s = pairSrv(); if (!s || X.devBusy) return; X.devBusy = true; X.devErr = '';
    try {
      const [plain, access] = await Promise.all([E.call('list_devices', { host: s.id }), E.call('device_access_all', { host: s.id }).catch(() => null)]);
      const acc = {}; (access || []).forEach((a) => { acc[a.id] = a; });
      X.devForbidden = !access;
      X.devices = plain.map((d) => ({ id: d.id, name: d.label || 'Device', paired: (d.created || 0) * 1000, last: (d.lastSeen || 0) * 1000, revoked: !!d.revoked, current: !!d.current, addr: d.lastAddress || '', ver: d.lastVersion || '', viaLogin: !!d.viaLogin, a: acc[d.id] || null }));
    } catch (e) { X.devErr = 'The list could not be refreshed: ' + e.message; X.devices = []; }
    X.devBusy = false; X.devAt = E.now(); if (A.devPageOpen()) A.pageDevices();
  };
  /* The computers whose waiting phones are listed: the online ones, or this PC's own agent when none is. */
  const inboxHosts = () => { const o = online(); const l = o.length ? null : A.localPC && A.localPC.srv(); return l ? [l] : o; };
  /* A failed look at one computer keeps that computer's last requests and records why in X.inboxErr (host -> message), so the page shows the old list with a Try again button rather than a list that looks empty. */
  A.loadInbox = async () => {
    if (S.requireApproval === false) { if (X.inbox.length || (X.inboxErr && X.inboxErr.size)) { X.inbox = []; X.pending = null; X.inboxErr = new Map(); A.renderRail(); if (A.devPageOpen()) A.pageDevices(); } return; }
    const list = []; const prev = X.inbox; const hosts = inboxHosts(); X.inboxDenied = X.inboxDenied || new Set(); X.inboxErr = X.inboxErr || new Map();
    const errKey0 = [...X.inboxErr].join();
    await Promise.all(hosts.map((s) => E.call('list_pair_requests', { host: s.id }).then((r) => { X.inboxErr.delete(s.id); if (r.forbidden) X.inboxDenied.add(s.id); else X.inboxDenied.delete(s.id); for (const q of r.requests || []) list.push(Object.assign({ host: s.id }, q)); }).catch((e) => { X.inboxErr.set(s.id, (e && e.message) || String(e)); for (const q of prev) if (q.host === s.id) list.push(q); })));
    for (const id of [...X.inboxErr.keys()]) if (!hosts.some((s) => s.id === id)) X.inboxErr.delete(id);
    const key0 = X.inbox.map((x) => x.host + x.id).join(); X.inbox = list; X.pending = list[0] || null;
    if (list.map((x) => x.host + x.id).join() !== key0) A.renderRail();
    if (list.map((x) => x.host + x.id).join() !== key0 || [...X.inboxErr].join() !== errKey0) { if (A.devPageOpen()) A.pageDevices(); }
    const fresh = list.find((q) => !X.seen || !X.seen.has(q.host + q.id)); X.seen = X.seen || new Set(); list.forEach((q) => X.seen.add(q.host + q.id));
    if (fresh && !A.devPageOpen()) A.snack(fresh.label + ' wants to pair', { action: 'Review', onAction: () => A.go('devices') });
    if (fresh && A.osNotify) A.osNotify('pair', fresh.label + ' wants to pair');
  };
  /* The inline notice for a failed look at the pairing requests ('' when the last look worked): what went wrong and a Try again button (`retry` is that button's markup, since each page styles buttons its own way). The list shown with it is the last good one. */
  A.inboxErrHtml = (retry) => { const e = X.inboxErr && X.inboxErr.size ? [...X.inboxErr.values()][0] : ''; return e ? '<p class="k-note prn inberr" role="alert">' + ic('warning', { size: 14 }) + 'Pairing requests could not be refreshed: ' + esc(e) + '. The list below may be out of date. ' + retry + '</p>' : ''; };
  /* Whether to look for pairing requests now: not while paused or with no computer online, and not while the window is hidden unless
     desktop notifications are on (the request is then announced by the system, which is how a hidden window learns of it). */
  A.inboxPollDue = (o) => !o.paused && !!o.online && (o.visible || !!o.notify);
  setInterval(() => { if (A.inboxPollDue({ paused: A.paused, online: inboxHosts().length, visible: document.visibilityState !== 'hidden', notify: !!A.S.desktopNotif })) A.loadInbox(); }, A.T.inboxPoll);
  const GRANTS = [['download', 'Download files', 'Save files from this computer'], ['upload', 'Upload files', 'Add files'], ['modify', 'Rename and move', 'Change existing files'], ['delete', 'Delete', 'Move files to the Trash'], ['share', 'Share links', 'Make links others can open'], ['wake', 'Wake computers', 'Send Wake-on-LAN packets from this computer'], ['viewApps', 'See apps', 'List apps on this computer'], ['launchApps', 'Launch apps', 'Start apps on this computer']];
  const CAPS = GRANTS.map((g) => [g[0], g[1], g[2]]);
  /* The switches for what a device may do, as HTML for editing in place: Browse is always on, then one switch per grant, then Read-only. `d.a` holds the current grants. A device signed in with the account is not limited by the five file grants (the agent does not check them), so those rows are fixed text, not switches. */
  const FILE_GRANTS = ['download', 'upload', 'modify', 'delete', 'share', 'wake'];
  A.grantRows = (d) => {
    const cur = d.a || {}; const row = (k, label, sub, on, off) => '<div class="sr" style="padding:.375rem 0"><div class="l"><b>' + label + '</b><small>' + sub + '</small></div><button class="sw' + (on ? ' on' : '') + (off ? ' lock' : '') + '"' + (k ? ' data-c="' + k + '"' : ' disabled') + ' role="switch" aria-checked="' + !!on + '" aria-label="' + label + (off ? ', always on' : '') + '"></button></div>';
    const fixed = (label, sub) => '<div class="sr fixed" style="padding:.375rem 0"><div class="l"><b>' + label + '</b><small>' + sub + '</small></div><span class="tag ok">Allowed</span></div>';
    const note = d.viaLogin ? '<div class="fxsub" id="dvacct" style="padding:.25rem 0">Signed in with the account: file actions are always allowed</div>' : '';
    return row('', 'Browse files', 'Always on while paired', true, true) + note + GRANTS.map((c) => (d.viaLogin && FILE_GRANTS.includes(c[0]) ? fixed(c[1], c[2]) : row(c[0], c[1], c[2], cur[c[0]]))).join('') + row('readOnly', 'Read-only', 'Blocks every change, whatever else is on', cur.readOnly);
  };
  /* Makes the switches inside `root` save on the click that flips them: one set_device_access call per click, no Save step. Only the clicked switch is disabled while its call runs. On success the switches show what the agent answered; on any failure they show what the agent holds now (a fresh read of the device list, the one read the dev stand-in also has), and only if that read fails too the values from before the click. Apps: Launch needs See, so the two move together. */
  A.bindGrants = (root, d) => {
    const s = pairSrv(); const cur = Object.assign({}, d.a); const inflight = new Map();
    const paint = () => $$('[data-c]', root).forEach((b) => { if (inflight.has(b.dataset.c)) return; const on = !!cur[b.dataset.c]; b.classList.toggle('on', on); b.setAttribute('aria-checked', on); });
    root.addEventListener('click', (e) => {
      const b = e.target.closest('[data-c]'); if (!b || b.disabled || !s) return;
      if (d.current) { A.dialog({ icon: 'lock', title: 'Change this device’s own access?', width: 440, body: '<p class="fxp">This is the device you are using. Turning access off can stop it from working with ' + esc(s.name) + '.</p>', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Change', kind: 'f', cb: () => apply(b) }] }); return; }
      apply(b);
    });
    const apply = (b) => {
      const k = b.dataset.c; const before = Object.assign({}, cur); const patch = {}; const v = !cur[k]; cur[k] = v; patch[k] = v;
      if (k === 'launchApps' && v && !cur.viewApps) { cur.viewApps = true; patch.viewApps = true; }
      if (k === 'viewApps' && !v && cur.launchApps) { cur.launchApps = false; patch.launchApps = false; }
      paint(); b.classList.toggle('on', v); b.setAttribute('aria-checked', v); b.disabled = true; inflight.set(k, 1);
      const settle = (acc) => { if (acc && typeof acc === 'object') { Object.assign(cur, acc); if (d.a) Object.assign(d.a, acc); } };
      E.call('set_device_access', { host: s.id, id: d.id, patch, confirmSelf: !!d.current })
        .then((res) => { settle(res && typeof res === 'object' ? res : patch); audit(d.name + ' permissions changed on ' + s.name); A.snack('Saved the access of ' + d.name); A.loadDevices(); })
        .catch((err) => E.call('device_access_all', { host: s.id }).then((all) => { const acc = (all || []).find((x) => x.id === d.id); if (!acc) throw err; settle(acc); }).catch(() => { Object.assign(cur, before); }).then(() => fail(err)))
        .finally(() => { b.disabled = false; inflight.delete(k); paint(); });
    };
  };
  function approveDialog(q) {
    const s = E.server(q.host);
    A.dialog({
      icon: 'phone', title: 'Accept ' + q.label + '?', width: 460,
      body: '<p class="fxp">' + esc(q.label) + ' (' + esc(q.address || 'unknown address') + ') asked to pair with ' + esc(s ? s.name : q.host) + '. Accept only if the code matches the one shown on the device asking.</p><div class="pcode" style="margin:.625rem 0"><span>' + esc(q.matchCode) + '</span></div>' + (q.replaces ? '<div class="hint" style="margin:0">Approving replaces the paired device “' + esc(q.replaces) + '”.</div>' : '') + '<div class="hint" style="margin:.5rem 0 0">An approved device starts with browse access only. Change what it may do under Permissions.</div>',
      enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Accept', kind: 'f', icon: 'check', cb: () => answer(q, true) }]
    });
  }
  async function answer(q, approve) {
    try {
      const r = await E.call('answer_pair_request', { host: q.host, id: q.id, approve });
      if (r === 'gone') A.snack('That request already expired or was answered on the PC.');
      else { audit((approve ? 'Accepted' : 'Refused') + ' pairing request from ' + q.label); A.snack((approve ? 'Accepted ' : 'Refused ') + q.label); }
    } catch (e) { fail(e); }
    await A.loadInbox(); A.loadDevices();
  }

  /* ---------- delegated actions ---------- */
  const H = {
    'inbox.retry': () => A.loadInbox(), 'pair.new': () => newCode(), 'pair.link': () => { A.copy(X.pair.qr, 'Pairing data copied'); }, 'pair.copycode': () => { A.copy(X.pair.code, 'Code copied'); },
    'pair.save': () => { const s = pairSrv(); let svg; try { svg = QR.svg(X.pair.qr, { px: 512, border: 4 }); } catch (e) { fail(e); return; } A.saveLocal('rfe-pairing-' + s.name.replace(/[^\w.-]+/g, '-') + '.svg', svg).then((r) => A.snack('Saved ' + r.name + ' to ' + r.dir, { action: 'Show', onAction: () => A.openPath('local', r.path, true) })).catch(fail); },
    'pair.srv': (id) => { X.pair.host = id; X.pair.code = ''; X.pair.err = ''; X.devices = []; X.devAt = 0; refreshPair(); newCode(true); A.loadDevices(); },
    'dev.approve': (i) => { const q = X.inbox[+i || 0]; if (q) approveDialog(q); },
    'dev.deny': (i) => { const q = X.inbox[+i || 0]; if (!q) return; A.dialog({ icon: 'phone', cls: 'danger', title: 'Refuse ' + q.label + '?', body: 'It will have to ask again with a new code or request.', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Refuse', kind: 'ed', cb: () => { answer(q, false); } }] }); },
    'dev.perm': (id) => { if (X.devices.find((x) => x.id === id)) A.deviceDetails(id); },
    'dev.revoke': (id) => {
      const d = X.devices.find((x) => x.id === id); const s = pairSrv(); if (!d) return;
      A.dialog({ icon: 'shield', cls: 'danger', title: 'Revoke “' + d.name + '”?', width: 440, body: '<div><b class="dnt">' + esc(d.name) + '</b> will be signed out and can no longer connect.' + (d.current ? ' <b>This is the sign-in this app uses on ' + esc(s.name) + '.</b> This computer is signed out when you confirm.' : '') + ' To use it again you pair it again with a new code.</div>', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Revoke', kind: 'ed', icon: 'x-circle', cb: () => {
        E.call('revoke_device', { host: s.id, id, confirmSelf: !!d.current }).then((r) => { audit(d.name + ' revoked on ' + s.name); A.snack('Revoked ' + d.name + (r && r.signedOut ? '. This computer is signed out; sign in again to continue.' : '')); if (r && r.signedOut) E.loadServers().then(() => E.check(E.server(s.id))); A.loadDevices(); }).catch(fail);
      } }] });
    },
    'dev.remove': (id) => {
      const d = X.devices.find((x) => x.id === id); const s = pairSrv(); if (!d) return;
      A.dialog({ icon: 'trash', cls: 'danger', title: 'Remove “' + d.name + '” from the list?', width: 440, body: 'This deletes the device record. It is signed out if it still was.', enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Remove', kind: 'ed', icon: 'trash', cb: () => { E.call('remove_device', { host: s.id, id, confirmSelf: !!d.current }).then(() => { audit(d.name + ' removed on ' + s.name); A.snack('Removed ' + d.name); A.loadDevices(); }).catch(fail); } }] });
    }
  };
  A.fxHandlers = H;
  document.addEventListener('click', (e) => { const b = e.target.closest('[data-fx]'); if (!b) return; const raw = b.dataset.fx, cut = raw.indexOf('|'), act = cut < 0 ? raw : raw.slice(0, cut), a1 = cut < 0 ? undefined : raw.slice(cut + 1), a2 = undefined; /* only the first | separates: a search text may contain one */ if (H[act]) { e.preventDefault(); H[act](a1, a2, b, e); } });

  A.fx = { restoreRows, purgeRows, shareDialog, fileOp, compress, extract, checksumDialog, batchRename, audit, rel, left, CAPS, GRANTS, FILE_GRANTS, plural, persist };
  A.renderRail();
});
