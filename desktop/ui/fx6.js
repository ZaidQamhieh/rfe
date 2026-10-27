/* RFE Tonal - part 6: Properties (permissions), troubleshooting (log detail, keystore, device key, agent log), wake a computer, recently changed files. */
(function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, st = A.state, F = A.fx, H = A.fxHandlers;
  const { audit, rel } = F;
  const fail = (e) => A.snack((e && e.message) || String(e), { error: true });

  /* ================= Properties ================= */
  const SYM = ['r', 'w', 'x'];
  const bits = (oct) => { const o = String(oct).padStart(4, '0').slice(1); return o.split('').map((d) => [!!(d & 4), !!(d & 2), !!(d & 1)]); };
  A.propsDialog = (host, path, node) => {
    const dir = node ? path : U.parentOf(path); const n = node || E.fs.get(host, path); if (!n) return;
    const full = node ? U.join(path, n.n) : path; const loc = host === 'local';
    const unix = !!n.perm && n.perm.length === 10; const canChange = !loc && unix && E.fs.canWrite(null, host);
    const cur = bits(U.permOctal(n.perm));
    const who = ['Owner', 'Group', 'Others'];
    const grid = '<table class="capm" id="pmg"><thead><tr><th></th><th>Read</th><th>Write</th><th>Run</th></tr></thead><tbody>' + who.map((w, i) => '<tr><td>' + w + '</td>' + [0, 1, 2].map((j) => '<td><input type="checkbox" data-pm="' + i + j + '" aria-label="' + w + ' ' + ['read', 'write', 'run'][j] + '"' + (cur[i][j] ? ' checked' : '') + (canChange ? '' : ' disabled') + '></td>').join('') + '</tr>').join('') + '</tbody></table>';
    const kv = (k, v, mono) => '<div class="kv2"><div><b>' + k + '</b><small' + (mono ? ' class="mono"' : '') + '>' + v + '</small></div></div>';
    const octal = () => { let s = '0'; for (let i = 0; i < 3; i++) s += ($$('[data-pm^="' + i + '"]', $('#pmg')).reduce((a, c, j) => a + (c.checked ? [4, 2, 1][j] : 0), 0)); return s; };
    A.dialog({ icon: 'info', title: n.n || '/', width: 520, noFocus: true,
      body: kv('Location', esc((loc ? '' : A.hostName(host) + ':') + full), true) + kv('Kind', U.KINDS[U.kindOf(n)] + (n.link ? ' · link' + (n.to ? ' to ' + esc(n.to) : '') : '')) + (n.t === 'dir' ? (n.cc != null || n.kids ? kv('Contains', E.fs.itemsLabel(n, (c) => c.toLocaleString(U.lang()))) : '') : kv('Size', U.fmtBytes(n.b) + ' (' + n.b.toLocaleString(U.lang()) + ' bytes)')) + kv('Modified', n.mod ? U.fmtDate(n.mod) + ' · ' + rel(n.mod) : 'Unknown') +
        (n.perm ? kv('Permissions', esc(n.perm) + ' · ' + U.permOctal(n.perm), true) : '') + (unix ? '<h3 class="dsh">Permissions</h3>' + grid + '<div class="fxsub">' + (canChange ? 'Changes the mode on ' + esc(A.hostName(host)) + '. The agent refuses it if this login may not modify files.' : loc ? 'Change permissions of local files in your file manager.' : 'This login cannot change permissions here.') + '</div>' : ''),
      actions: [{ label: 'Close', kind: 'tx' }, { label: 'Copy path', kind: 'tx', icon: 'copy', cb: () => { A.copy(full); return false; } }].concat(canChange ? [{ id: 'go', label: 'Apply permissions', kind: 'f', icon: 'check', cb: (c) => {
        const mode = octal(); if (mode === U.permOctal(n.perm)) return;
        E.call('files_chmod', { host, path: full, mode }).then(() => { audit('Permissions of ' + n.n + ' on ' + A.hostName(host) + ' set to ' + mode); A.snack('Permissions of “' + n.n + '” are now ' + mode); return E.fs.refresh(host, dir); }).catch(fail);
      } }] : []) });
  };

  /* ================= wake a computer ================= */
  const wake = (s) => { if (!s.mac) { A.snack('The MAC address of ' + s.name + ' is not known yet. Connect to it once so it can be woken later.', { error: true }); return; } E.call('wake_computer', { host: s.id, mac: s.mac }).then(() => { audit('Wake-on-LAN sent to ' + s.name); A.snack('Wake signal sent to ' + s.name + '. It can take a minute to start.'); }).catch(fail); };
  const sme = A.serverMenuExtra;
  A.serverMenuExtra = (s) => sme(s).concat(s.state === 'online' ? [{ icon: 'info', label: 'Status…', onClick: () => A.statusDialog(s.id) }] : []).concat(s.state === 'online' ? [] : [{ icon: 'power', label: 'Wake computer', disabled: !s.mac, onClick: () => wake(s) }]);

  /* ================= recently changed on a server ================= */
  A.recentOnServer = (id) => {
    const s = E.server(id); if (!s || s.state !== 'online') { A.snack('Connect to a server first.', { error: true }); return; }
    E.call('files_recent', { host: id, limit: 40 }).then((list) => {
      const rows = list.map((e, i) => '<div class="evrow" data-ri="' + i + '" style="cursor:pointer">' + ic(e.isDir ? 'folder' : 'file') + '<span>' + esc(e.name) + '<br><small class="mono">' + esc(e.path) + '</small></span><small>' + (e.modified ? rel(Date.parse(e.modified)) : '') + '</small></div>').join('');
      A.dialog({ icon: 'clock', title: 'Recently changed on ' + s.name, width: 600, noFocus: true, body: list.length ? rows : '<div class="fxsub">Nothing was changed lately in the folders this login can open.</div>', actions: [{ label: 'Close', kind: 'f' }],
        onOpen: (c) => c.el.addEventListener('click', (ev) => { const r = ev.target.closest('[data-ri]'); if (!r) return; const e = list[+r.dataset.ri]; c.close(); A.openPath(id, e.path, !e.isDir); }) });
    }).catch(fail);
  };

  /* ================= server status: what the agent reports about itself ================= */
  A.statusDialog = (id) => {
    const s = E.server(id); if (!s || s.state !== 'online') { A.snack('Connect to a server first.', { error: true }); return; }
    const NR = '<span class="tag">not reported</span>';
    const v = (x, f) => (x == null ? NR : f(x));
    const kv = (k, val) => '<div class="kv2"><div><b>' + k + '</b><small>' + val + '</small></div></div>';
    const draw = (c, snap) => {
      const h = snap.health || {}, st0 = snap.status, m = snap.metrics;
      const note = (txt) => '<div class="hint" style="margin:.5rem 0 0">' + txt + '</div>';
      $('.db', c.el).innerHTML = kv('Agent', esc(h.name || s.name) + ' · ' + (h.version ? 'rfe ' + esc(h.version) : NR)) + kv('System', h.os ? esc(h.os) : NR) + kv('Platform', st0 && st0.platform ? esc(st0.platform) : NR) +
        kv('Uptime', st0 ? v(st0.uptimeSeconds, (x) => U.fmtDur(x)) : NR) + kv('Reachable at', [h.address && 'LAN ' + esc(h.address), h.tailscaleAddress && 'Tailscale ' + esc(h.tailscaleAddress), h.macAddress && 'MAC ' + esc(h.macAddress)].filter(Boolean).join(' · ') || NR) +
        kv('Changes', h.readOnly ? 'The agent is read-only' : 'Allowed') + kv('Disk', st0 && st0.totalBytes ? U.fmtBytes(st0.totalBytes - (st0.freeBytes || 0)) + ' used of ' + U.fmtBytes(st0.totalBytes) : NR) +
        (st0 ? '' : note('Disk and uptime were not reported' + (snap.statusNote ? ': ' + esc(snap.statusNote) : '') + '.')) +
        '<h3 class="dsh">Load</h3>' + (m ? kv('Processor', v(m.cpuPercent, (x) => x.toFixed(0) + ' %')) + kv('Memory', v(m.ramPercent, (x) => x.toFixed(0) + ' %')) + kv('Network since the agent started', v(m.rxBytes, (x) => U.fmtBytes(x)) + ' received · ' + v(m.txBytes, (x) => U.fmtBytes(x)) + ' sent') :
          snap.metricsForbidden ? note('Metrics are for administrators. This computer signed in with a pairing code or was approved on the PC; sign out and sign in with the account to see them.') : note('No metrics were reported' + (snap.metricsNote ? ': ' + esc(snap.metricsNote) : '') + '.'));
    };
    const load = (c) => E.call('agent_health', { host: id }).then((snap) => draw(c, snap)).catch((e) => { $('.db', c.el).innerHTML = '<div class="fxsub">' + esc((e && e.message) || String(e)) + '</div>'; });
    A.dialog({ icon: 'activity', title: 'Status of ' + s.name, width: 560, noFocus: true, body: '<div class="fxsub">Asking ' + esc(s.name) + '…</div>', actions: [{ label: 'Close', kind: 'tx' }, { id: 'rf', label: 'Refresh', kind: 'f', icon: 'refresh', cb: (c) => { load(c); return false; } }], onOpen: (c) => load(c) });
  };

  /* ================= troubleshooting ================= */
  const T = { level: '' };
  const loadLevel = () => E.call('app_settings').then((r) => { T.level = r.logLevel; if (st.view === 'settings') A.pageSettings(); }).catch(() => {});
  const logServer = () => { const m = A.mainPane.host; return (m !== 'local' && E.server(m) && E.server(m).state === 'online' && E.server(m)) || E.servers.find((x) => x.state === 'online'); };
  A.agentLogDialog = () => {
    const s = logServer(); if (!s) { A.snack('Connect to a server to read its log.', { error: true }); return; }
    E.call('agent_log', { host: s.id }).then((r) => {
      const lines = r.lines || [];
      A.dialog({ icon: 'activity', title: 'Agent log of ' + s.name, width: 720, noFocus: true,
        body: r.forbidden ? '<div class="fxsub">This login cannot read the audit log or the agent log. Sign in as the owner of the computer.</div>' : '<div class="fld"><input id="lgf" placeholder="Filter the log" spellcheck="false" aria-label="Filter the log"></div><div id="lgb" class="codev" style="max-height:21.25rem;overflow:auto"></div>',
        actions: [{ label: 'Close', kind: 'f' }],
        onOpen: (c) => { if (r.forbidden) return; const draw = () => { const q = $('#lgf', c.el).value.trim().toLowerCase(); const m = lines.filter((l) => !q || (l.ts + ' ' + l.message).toLowerCase().includes(q)); $('#lgb', c.el).innerHTML = m.length ? m.map((l) => '<div class="evrow"><small class="mono">' + esc(l.ts) + '</small><span>' + esc(l.message) + '</span></div>').join('') : '<div class="fxsub">' + (lines.length ? 'No log line matches these filters.' : "The agent's log is empty.") + '</div>'; }; $('#lgf', c.el).addEventListener('input', draw); draw(); } });
    }).catch(fail);
  };
  const keystore = () => { A.snack('Testing the keystore (it may ask you to unlock it)…'); E.call('check_keystore').then(() => A.snack('The keystore works: a test secret was saved, read back and removed.')).catch((e) => A.snack('The keystore is not working. ' + ((e && e.message) || e), { error: true })); };
  const deviceKey = () => A.dialog({ icon: 'key', cls: 'danger', title: 'Create a new device key?', width: 520, enter: 'go',
    body: '<div>The key that identifies this computer to the servers is replaced the next time you sign in. The old one stays registered on each server until it is removed there. Logins saved for your servers are removed too; you sign in to each again.</div>',
    actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Create a new device key', kind: 'ed', icon: 'key', cb: () => { E.call('reset_device_key').then(() => E.loadServers()).then(() => { audit('Device key of this computer reset'); A.snack('A new device key will be created the next time you sign in.'); A.renderAll(); }).catch(fail); } }] });
  const setLevel = (lv) => E.call('set_log_level', { level: lv }).then((r) => { T.level = r; A.pageSettings(); }).catch((e) => { fail(e); A.pageSettings(); });

  const se0 = A.settingsExtra;
  A.settingsExtra = (k) => {
    const h = se0(k); const lv = T.level || 'info';
    const grp = '<div class="sg"><h3>Troubleshooting</h3>' + k.row('Log detail', '', '<div class="seg">' + [['error', 'Errors'], ['info', 'Normal'], ['debug', 'Detailed']].map((o) => '<button data-sx="loglevel:' + o[0] + '" class="' + (lv === o[0] ? 'on' : '') + '">' + o[1] + '</button>').join('') + '</div>') +
      k.row('Agent log', '', '<button class="btn" data-sx="agentlog">View</button>') +
      k.row('Check the keystore', '', '<button class="btn" data-sx="keystore">Check</button>') +
      k.row('Device key', '', '<button class="btn e" data-sx="devkey">New key…</button>') + '</div>';
    return h.replace('<div class="sg"><h3>About</h3>', grp + '<div class="sg"><h3>About</h3>');
  };
  const sa0 = A.settingsAction;
  A.settingsAction = (a) => {
    if (a.startsWith('loglevel:')) { setLevel(a.slice(9)); return true; }
    if (a === 'agentlog') { A.agentLogDialog(); return true; }
    if (a === 'keystore') { keystore(); return true; }
    if (a === 'devkey') { deviceKey(); return true; }
    return sa0(a);
  };
  A.paletteExtra.push((C) => {
    const m = A.mainPane.host; if (m !== 'local' && E.server(m) && E.server(m).state === 'online') C('clock', 'Recently changed files on ' + A.hostName(m), () => A.recentOnServer(m));
    if (m !== 'local' && E.server(m) && E.server(m).state === 'online') C('info', 'Status of ' + A.hostName(m), () => A.statusDialog(m));
    C('activity', 'Agent log', () => A.agentLogDialog()); C('key', 'Check the keystore', keystore);
    E.servers.filter((s) => s.state !== 'online' && s.mac).forEach((s) => C('power', 'Wake ' + s.name, () => wake(s)));
  });

  /* ================= the desktop around the window: tray, notifications, starting with the session ================= */
  const S = A.S; const D = { tray: null };
  if (S.channel === undefined) S.channel = 'stable';
  const pushDesktop = () => E.call('desktop_set_prefs', { closeToTray: !!S.closeToTray, notifications: !!S.desktopNotif }).catch(() => {});
  const loadDesktop = async () => {
    try { D.tray = !!(await E.call('desktop_has_tray')); } catch (e) { D.tray = false; }
    try { S.startLogin = !!(await E.call('desktop_autostart_state')); } catch (e) { S.startLogin = false; }
    if (st.view === 'settings') A.pageSettings();
  };
  /* Shown by the system only while this window is not the one being looked at; `test` always. */
  const OS_KIND = { ok: ['done', 'notifyDone'], error: ['error', 'notifyErrors'], pair: ['pair', 'nPair'], update: ['update', 'nUpdate'] };
  A.osNotify = (kind, text) => {
    const m = OS_KIND[kind]; if (!m || !S.desktopNotif || S[m[1]] === false || document.hasFocus()) return;
    E.call('desktop_notify', { kind: m[0], title: kind === 'error' ? 'RFE: something failed' : kind === 'pair' ? 'RFE: a device wants to pair' : kind === 'update' ? 'RFE: an update is available' : 'RFE', body: text }).catch(() => {});
  };
  E.on((type, n) => { if (type === 'notify' && n) A.osNotify(n.kind, n.text); });
  A.onSwitch = (key) => {
    if (key === 'closeToTray' || key === 'desktopNotif') { pushDesktop(); if (key === 'desktopNotif') A.pageSettings(); }
    else if (key === 'startLogin') {
      E.call('desktop_set_autostart', { on: !!S.startLogin }).then((on) => { S.startLogin = !!on; A.save(); A.snack(on ? 'RFE starts hidden in the tray when you sign in' : 'RFE no longer starts when you sign in'); A.pageSettings(); })
        .catch((e) => { S.startLogin = !S.startLogin; A.save(); fail(e); A.pageSettings(); });
    }
  };
  const sx1 = A.settingsExtra;
  A.settingsExtra = (k) => {
    const h = sx1(k); const row = k.row, sw = k.sw;
    const noTray = D.tray === false;
    const desktop = '<div class="sg"><h3>Desktop</h3>' +
      row('Close to the system tray', noTray ? 'This system shows no tray icon, so closing the window closes the app' : 'Closing the window keeps transfers running in the tray', noTray ? '<button class="sw" disabled role="switch" aria-checked="false" aria-label="closeToTray"></button>' : sw('closeToTray')) +
      row('Start RFE when I sign in', '', sw('startLogin')) +
      row('Desktop notifications', '', sw('desktopNotif')) +
      (S.desktopNotif ? row('Notify when a transfer finishes', '', sw('notifyDone')) + row('Notify about errors', '', sw('notifyErrors')) + row('New device requests', '', sw('nPair')) + row('Updates', '', sw('nUpdate')) : '') +
      row('Try it', '', '<button class="btn" data-sx="notif">' + ic('bell') + 'Test notification</button>') + '</div>';
    return h.replace('<div class="sg"><h3>Files</h3>', desktop + '<div class="sg"><h3>Files</h3>');
  };
  const sa1 = A.settingsAction;
  A.settingsAction = (a) => {
    if (a === 'notif') { E.call('desktop_notify', { kind: 'test', title: 'RFE test notification', body: 'Notifications work. Finished transfers and new device requests appear like this.' }).then((shown) => { if (!shown) A.snack('The notification was not shown'); }).catch(fail); return true; }
    if (a === 'update') { A.updateDialog(); return true; }
    return sa1(a);
  };
  if (window.__TAURI__ && window.__TAURI__.event && window.__TAURI__.event.listen) {
    window.__TAURI__.event.listen('tray-action', (ev) => { if (ev && ev.payload === 'pair' && A.pairDialog) A.pairDialog(); }).catch(() => {});
  }

  /* ================= looking for a newer version (nothing installs by itself) ================= */
  const UP = { busy: false, res: null, err: '', saved: '', dl: false };
  const drawUpdate = (c) => {
    const body = $('.db', c.el); if (!body) return;
    const r = UP.res; const v = A.appVersion ? A.appVersion() : '';
    let main;
    if (UP.busy) main = '<div class="fxsub">Looking for a new version…</div>';
    else if (UP.err) main = '<div class="fxsub" role="alert">' + esc(UP.err) + '</div>';
    else if (r) {
      main = '<div class="kv2"><div><b>RFE Desktop</b><small>Installed ' + esc(r.current) + ' · channel ' + esc(S.channel) + '</small></div>' + (r.available ? '<span class="tag pri">' + esc(r.latest) + ' available</span>' : '<span class="tag ok">Up to date</span>') + '</div>' +
        (r.available ? '<h3 class="dsh">What’s new in ' + esc(r.latest) + '</h3>' + (r.notes.length ? '<ul class="notes">' + r.notes.map((n) => '<li>' + esc(n) + '</li>').join('') + '</ul>' : '<div class="fxsub">No notes were published.</div>') +
          (r.package ? '<div class="fxsub">' + esc(r.package.name) + ' · ' + U.fmtBytes(r.package.size) + ' · checked against the release’s SHA-256 list before it is kept</div>' : '<div class="fxsub">This release has no package for this system.</div>') : '') +
        (UP.dl ? '<div class="fxsub" role="status">Downloading and checking the package…</div>' : '') +
        (UP.saved ? '<div class="fxsub" role="status">Saved and checked: <span class="mono">' + esc(UP.saved) + '</span>. Install it with your package manager (for a .deb: <span class="mono">sudo apt install ./the-file.deb</span>; for an .rpm: <span class="mono">sudo dnf install ./the-file.rpm</span>) or run the AppImage. RFE never installs it for you.</div>' : '');
    } else main = '';
    body.innerHTML = main +
      '<div class="sr" style="padding:.75rem 0 0"><div class="l"><b>Update channel</b><small>Beta also offers pre-releases and may be less stable</small></div><div class="seg">' + [['stable', 'Stable'], ['beta', 'Beta']].map((o) => '<button data-uc="' + o[0] + '" class="' + (S.channel === o[0] ? 'on' : '') + '">' + o[1] + '</button>').join('') + '</div></div>' +
      '<div class="sr" style="padding:.5rem 0 0"><div class="l"><b>Check automatically</b><small>Once a day while the app runs. Nothing is installed without you.</small></div><button class="sw' + (S.autoUpdate ? ' on' : '') + '" data-ua="1" role="switch" aria-checked="' + !!S.autoUpdate + '" aria-label="Check automatically"></button></div>';
    const go = $('[data-id="go"]', c.el); if (go) { go.hidden = !(r && r.available && r.package && !UP.saved); go.disabled = UP.dl; }
    const again = $('[data-id="again"]', c.el); if (again) again.disabled = UP.busy || UP.dl;
    const show = $('[data-id="show"]', c.el); if (show) show.hidden = !UP.saved;
  };
  const checkNow = async (c) => {
    UP.busy = true; UP.err = ''; UP.saved = ''; if (c) drawUpdate(c);
    try { UP.res = await E.call('update_check', { channel: S.channel }); S.lastUpdateCheck = Date.now(); A.save(); } catch (e) { UP.err = (e && e.message) || String(e); UP.res = null; }
    UP.busy = false; if (c && c.el.isConnected) drawUpdate(c);
  };
  A.updateDialog = () => A.dialog({ icon: 'download', title: 'Check for updates', width: 520, noFocus: true, body: '<div class="db"></div>',
    actions: [{ label: 'Close', kind: 'tx' }, { id: 'show', label: 'Show in folder', kind: 'tx', icon: 'folder-open', cb: () => { if (UP.saved) E.call('local_open', { path: UP.saved.slice(0, UP.saved.lastIndexOf('/')) || '/' }).catch(fail); return false; } },
      { id: 'again', label: 'Check again', kind: 'tx', icon: 'refresh', cb: (c) => { checkNow(c); return false; } },
      { id: 'go', label: 'Download', kind: 'f', icon: 'download', cb: (c) => {
        const r = UP.res; if (!r || !r.package) return false; UP.dl = true; drawUpdate(c);
        E.call('update_download', { version: r.latest, name: r.package.name }).then((path) => { UP.saved = path; audit('Update ' + r.latest + ' downloaded and checked'); }).catch((e) => { UP.err = (e && e.message) || String(e); }).finally(() => { UP.dl = false; if (c.el.isConnected) drawUpdate(c); });
        return false; } }],
    onOpen: (c) => {
      drawUpdate(c); if (!UP.res || Date.now() - (S.lastUpdateCheck || 0) > 60000) checkNow(c);
      c.el.addEventListener('click', (e) => {
        const ch = e.target.closest('[data-uc]'); if (ch) { S.channel = ch.dataset.uc; A.save(); UP.res = null; checkNow(c); return; }
        const sw = e.target.closest('[data-ua]'); if (sw) { S.autoUpdate = !S.autoUpdate; A.save(); drawUpdate(c); }
      });
    } });
  /* Once a day, when the person asked for it. */
  const autoCheck = async () => {
    if (!S.autoUpdate || Date.now() - (S.lastUpdateCheck || 0) < 864e5) return;
    try {
      const r = await E.call('update_check', { channel: S.channel }); S.lastUpdateCheck = Date.now(); A.save(); UP.res = r;
      if (r.available && S.notifiedUpdate !== r.latest) { S.notifiedUpdate = r.latest; A.save(); A.snack('RFE Desktop ' + r.latest + ' is available', { action: 'View', onAction: A.updateDialog }); A.osNotify('update', 'RFE Desktop ' + r.latest + ' is available'); }
    } catch (e) { /* a failed look is tried again at the next start */ }
  };
  A.paletteExtra.push((C) => { C('download', 'Check for updates', A.updateDialog); });
  pushDesktop(); loadDesktop(); setTimeout(autoCheck, 8000);
  loadLevel();
})();
