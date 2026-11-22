/* RFE Tonal - part 6: Properties (permissions), troubleshooting (log detail, keystore, device key, agent log), wake a computer, recently changed files. */
rfeModule('fx6', ['fx5'], function () {
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
  A.serverMenuExtra = (s) => sme(s).concat(s.state === 'online' ? [{ icon: 'info', label: 'Status…', onClick: () => A.statusDialog(s.id) }] : []).concat(s.state === 'online' || E.wakeDenied(s) ? [] : [{ icon: 'power', label: 'Wake computer', disabled: !s.mac, onClick: () => wake(s) }]);

  /* ================= recently changed on a server =================
     The list is the files moved to or from that computer by this app (Engine.recentFiles), newest first; the computer's disk is never scanned. */
  A.recentOnServer = (id) => {
    const s = E.server(id); if (!s || s.state !== 'online') { A.snack('Connect to a computer first.', { error: true }); return; }
    Promise.resolve(E.recentFiles(id, { limit: 40 })).then((list) => {
      const rows = list.map((e, i) => '<div class="evrow" data-ri="' + i + '" style="cursor:pointer">' + ic(e.isDir ? 'folder' : 'file') + '<span>' + esc(e.name) + '<br><small class="mono">' + esc(e.path) + '</small></span><small>' + (e.modified ? rel(Date.parse(e.modified)) : '') + '</small></div>').join('');
      A.dialog({ icon: 'clock', title: 'Recently changed on ' + s.name, width: A.DIALOG_W, noFocus: true, body: list.length ? rows : '<div class="fxsub">No files have been moved to or from this computer yet.</div>', actions: [{ label: 'Close', kind: 'f' }],
        onOpen: (c) => c.el.addEventListener('click', (ev) => { const r = ev.target.closest('[data-ri]'); if (!r) return; const e = list[+r.dataset.ri]; c.close(); A.openPath(id, e.path, !e.isDir); }) });
    }).catch(fail);
  };

  /* ================= server status: what the agent reports about itself ================= */
  A.statusDialog = (id) => {
    const s = E.server(id); if (!s || s.state !== 'online') { A.snack('Connect to a computer first.', { error: true }); return; }
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
  const loadLevel = () => E.call('app_settings').then((r) => { T.level = r.logLevel; if (st.view === 'settings') A.pageSettings(); }).catch(A.noop);
  const logServer = () => { const m = A.mainPane.host; return (m !== 'local' && E.server(m) && E.server(m).state === 'online' && E.server(m)) || E.servers.find((x) => x.state === 'online'); };
  A.agentLogDialog = () => {
    const s = logServer(); if (!s) { A.snack('Connect to a computer to read its log.', { error: true }); return; }
    E.call('agent_log', { host: s.id }).then((r) => {
      const lines = r.lines || [];
      A.dialog({ icon: 'activity', title: 'Log of ' + s.name, width: 720, noFocus: true,
        body: r.forbidden ? '<div class="fxsub">This login cannot read the audit log or the agent log. Sign in as the owner of the computer.</div>' : '<div class="fld"><input id="lgf" placeholder="Filter the log" spellcheck="false" aria-label="Filter the log"></div><div id="lgb" class="codev" style="max-height:21.25rem;overflow:auto"></div>',
        actions: [{ label: 'Close', kind: 'f' }],
        onOpen: (c) => { if (r.forbidden) return; const draw = () => { const q = $('#lgf', c.el).value.trim().toLowerCase(); const m = lines.filter((l) => !q || (l.ts + ' ' + l.message).toLowerCase().includes(q)); $('#lgb', c.el).innerHTML = m.length ? m.map((l) => '<div class="evrow"><small class="mono">' + esc(l.ts) + '</small><span>' + esc(l.message) + '</span></div>').join('') : '<div class="fxsub">' + (lines.length ? 'No log line matches these filters.' : "The agent's log is empty.") + '</div>'; }; $('#lgf', c.el).addEventListener('input', draw); draw(); } });
    }).catch(fail);
  };
  A.backupsDialog = () => {
    const s = logServer(); if (!s) { A.snack('Connect to a computer to see its backups.', { error: true }); return; }
    E.call('backups_status', { host: s.id }).then((r) => {
      const m = (t) => t ? ' <span class="mono">' + esc(t) + '</span>' : '';
      const row = (a, b) => '<div class="evrow"><span>' + esc(a) + '</span><span>' + b + '</span></div>';
      const st = (ok, good, bad, time, err) => esc(ok ? good : bad) + m(time) + m(ok ? '' : err);
      const lb = r.lastBackup, lv = r.lastVerification, of = r.offsite || {}, sc = r.scheduler || {}, ln = r.launcher;
      const rows = [
        row('Last backup', lb ? st(lb.ok, 'Done', 'Failed', lb.time, lb.error) : esc('None yet')),
        row('Last check of a backup', lv ? st(lv.ok, 'Good', 'Failed', lv.time, lv.error) : esc('Not checked yet')),
        row('Copy to another place', of.configured ? st(of.ok, 'Done', 'Failed', of.lastTime, of.error) : esc('Not set up')),
        row('Automatic backups', esc(sc.enabled ? 'On' : 'Off') + m(sc.lastRun) + m(sc.lastError)),
        row('Safe updates', ln ? esc('On') + m(ln.current) + m(ln.pending) + m((ln.bad || []).join(', ')) : esc('Off') + m(r.launcherError)),
      ].join('');
      A.dialog({ icon: 'shield', title: 'Backups of ' + s.name, width: 640, noFocus: true,
        body: r.forbidden ? '<div class="fxsub">This login cannot read the backup status. Sign in as the owner of the computer.</div>' : '<div class="codev">' + rows + '</div>',
        actions: [{ label: 'Close', kind: 'f' }] });
    }).catch(fail);
  };
  const keystore = () => { A.snack('Testing the keystore (it may ask you to unlock it)…'); E.call('check_keystore').then(() => A.snack('The keystore works: a test secret was saved, read back and removed.')).catch((e) => A.snack('The keystore is not working. ' + ((e && e.message) || e), { error: true })); };
  const deviceKey = () => A.dialog({ icon: 'key', cls: 'danger', title: 'Create a new device key?', width: 520, enter: 'go',
    body: '<div>The key that identifies this computer to the computers is replaced the next time you sign in. The old one stays registered on each computer until it is removed there. Logins saved for your computers are removed too; you sign in to each again.</div>',
    actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Create a new device key', kind: 'ed', icon: 'key', cb: () => { E.call('reset_device_key').then(() => E.loadServers()).then(() => { audit('Device key of this computer reset'); A.snack('A new device key will be created the next time you sign in.'); A.renderAll(); }).catch(fail); } }] });
  const setLevel = (lv) => E.call('set_log_level', { level: lv }).then((r) => { T.level = r; A.pageSettings(); }).catch((e) => { fail(e); A.pageSettings(); });

  const se0 = A.settingsExtra;
  A.settingsExtra = (k) => {
    const h = se0(k); const lv = T.level || 'info';
    const grp = '<div class="sg"><h3>Troubleshooting</h3>' + k.row('Log detail', 'The last 500 events, kept in memory only','<div class="seg">' + [['error', 'Errors'], ['info', 'Normal'], ['debug', 'Detailed']].map((o) => '<button data-sx="loglevel:' + o[0] + '" class="' + (lv === o[0] ? 'on' : '') + '">' + o[1] + '</button>').join('') + '</div>') +
      k.row('Computer log', 'What the computer wrote down lately; support may ask for it', '<button class="btn" data-sx="agentlog">View</button>') +
      k.row('Backup status', 'When this computer last saved its data, and whether safe updates are on', '<button class="btn" data-sx="backups">View</button>') +
      k.row('Check secure storage', 'Saves, reads back and removes a test secret', '<button class="btn" data-sx="keystore">Check</button>') +
      k.row('This computer’s identity', 'Make this computer a new device at the next sign-in', '<button class="btn e" data-sx="devkey">Reset…</button>') + '</div>';
    return h.replace('<div class="sg"><h3>About</h3>', grp + '<div class="sg"><h3>About</h3>');
  };
  const sa0 = A.settingsAction;
  A.settingsAction = (a) => {
    if (a.startsWith('loglevel:')) { setLevel(a.slice(9)); return true; }
    if (a === 'agentlog') { A.agentLogDialog(); return true; }
    if (a === 'backups') { A.backupsDialog(); return true; }
    if (a === 'keystore') { keystore(); return true; }
    if (a === 'devkey') { deviceKey(); return true; }
    return sa0(a);
  };
  A.paletteExtra.push((C) => {
    const m = A.mainPane.host; if (m !== 'local' && E.server(m) && E.server(m).state === 'online') C('clock', 'Recently changed files on ' + A.hostName(m), () => A.recentOnServer(m));
    if (m !== 'local' && E.server(m) && E.server(m).state === 'online') C('info', 'Status of ' + A.hostName(m), () => A.statusDialog(m));
    C('activity', 'Computer log', () => A.agentLogDialog()); C('key', 'Check secure storage', keystore);
    E.servers.filter((s) => s.state !== 'online' && s.mac && !E.wakeDenied(s)).forEach((s) => C('power', 'Wake ' + s.name, () => wake(s)));
  });

  /* ================= the desktop around the window: tray, notifications, starting with the session ================= */
  const S = A.S; const D = { tray: null };
  if (S.channel === undefined) S.channel = 'stable';
  /* A.osNotify treats only an explicit false as off, so the switches of the pairing and update notices must start on, as the notices do. */
  if (S.nPair === undefined) S.nPair = true; if (S.nUpdate === undefined) S.nUpdate = true;
  /* Desktop notifications start on (the owner's decision); a saved choice, on or off, is kept. */
  if (S.desktopNotif === undefined) S.desktopNotif = true;
  const pushDesktop = () => E.call('desktop_set_prefs', { closeToTray: !!S.closeToTray, notifications: !!S.desktopNotif }).catch(A.noop);
  const loadDesktop = async () => {
    /* A failed probe means this system does not offer the feature, so it stays off. */
    try { D.tray = !!(await E.call('desktop_has_tray')); } catch (e) { D.tray = false; }
    try { S.startLogin = !!(await E.call('desktop_autostart_state')); } catch (e) { S.startLogin = false; }
    if (st.view === 'settings') A.pageSettings();
  };
  /* Shown by the system only while this window is not the one being looked at; `test` always. */
  const OS_KIND = { ok: ['done', 'notifyDone'], error: ['error', 'notifyErrors'], pair: ['pair', 'nPair'], update: ['update', 'nUpdate'] };
  A.osNotify = (kind, text) => {
    const m = OS_KIND[kind]; if (!m || !S.desktopNotif || S[m[1]] === false || document.hasFocus()) return;
    E.call('desktop_notify', { kind: m[0], title: kind === 'error' ? 'RFE: something failed' : kind === 'pair' ? 'RFE: a device wants to pair' : kind === 'update' ? 'RFE: an update is available' : 'RFE', body: text }).catch((e) => console.warn('The system did not show a notification:', (e && e.message) || e));
  };
  E.on((type, n) => { if (type === 'notify' && n) A.osNotify(n.kind, n.text); });
  A.onSwitch = (key) => {
    if (key === 'autoUpdate' && S.autoUpdate) autoCheck(); /* turned on: look now, not at the next start */
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
      row('Test notification', '', '<button class="btn" data-sx="notif" aria-label="Test notification">' + ic('bell') + 'Send</button>') + '</div>';
    return h.replace('<div class="sg"><h3>Files</h3>', desktop + '<div class="sg"><h3>Files</h3>');
  };
  const sa1 = A.settingsAction;
  A.settingsAction = (a) => {
    if (a === 'notif') { E.call('desktop_notify', { kind: 'test', title: 'RFE test notification', body: 'Notifications work. Finished transfers and new device requests appear like this.' }).then((shown) => { if (!shown) A.snack('The notification was not shown'); }).catch(fail); return true; }
    if (a === 'update') { A.updateDialog(); return true; }
    return sa1(a);
  };
  if (window.__TAURI__ && window.__TAURI__.event && window.__TAURI__.event.listen) {
    window.__TAURI__.event.listen('tray-action', (ev) => { if (ev && ev.payload === 'pair' && A.pairDialog) A.pairDialog(); }).catch(A.noop);
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
      main = '<div class="kv2"><div><b>' + A.APP_NAME + '</b><small>Installed ' + esc(r.current) + ' · channel ' + esc(S.channel) + '</small></div>' + (r.available ? '<span class="tag pri">' + esc(r.latest) + ' available</span>' : '<span class="tag ok">Up to date</span>') + '</div>' +
        (r.available ? '<h3 class="dsh">What’s new in ' + esc(r.latest) + '</h3>' + (r.notes.length ? '<ul class="notes">' + r.notes.map((n) => '<li>' + esc(n) + '</li>').join('') + '</ul>' : '<div class="fxsub">No notes were published.</div>') +
          (r.package ? '<div class="fxsub">' + esc(r.package.name) + ' · ' + U.fmtBytes(r.package.size) + ' · checked against the release’s SHA-256 list before it is kept</div>' : '<div class="fxsub">This release has no package for this system.</div>') : '') +
        (UP.dl ? '<div class="fxsub" role="status">Downloading and checking the package…</div>' : '') +
        (UP.saved ? '<div class="fxsub" role="status">Saved and checked: <span data-nolocal>' + esc(UP.saved) + '</span>. Install it with your package manager (for a .deb: <span class="mono">sudo apt install ./the-file.deb</span>; for an .rpm: <span class="mono">sudo dnf install ./the-file.rpm</span>) or run the AppImage. RFE never installs it for you.</div>' : '');
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
      drawUpdate(c); if (!UP.res || Date.now() - (S.lastUpdateCheck || 0) > A.T.minute) checkNow(c);
      c.el.addEventListener('click', (e) => {
        const ch = e.target.closest('[data-uc]'); if (ch) { S.channel = ch.dataset.uc; A.save(); UP.res = null; checkNow(c); return; }
        const sw = e.target.closest('[data-ua]'); if (sw) { S.autoUpdate = !S.autoUpdate; A.save(); drawUpdate(c); if (S.autoUpdate) autoCheck(); }
      });
    } });
  /* Looks for a newer version when the person asked for it and a day has passed since the last look; runs at start, hourly while the app is open, and when the switch is turned on. One look at a time. */
  let autoBusy = false;
  const autoCheck = async () => {
    if (autoBusy || !S.autoUpdate || Date.now() - (S.lastUpdateCheck || 0) < A.T.day) return;
    autoBusy = true;
    try {
      const r = await E.call('update_check', { channel: S.channel }); S.lastUpdateCheck = Date.now(); A.save(); UP.res = r;
      if (r.available && S.notifiedUpdate !== r.latest) { S.notifiedUpdate = r.latest; A.save(); A.snack(A.APP_NAME + ' ' + r.latest + ' is available', { action: 'View', onAction: A.updateDialog }); A.osNotify('update', A.APP_NAME + ' ' + r.latest + ' is available'); }
    } catch (e) { /* a failed look is tried again at the next tick */ } finally { autoBusy = false; }
  };
  A.paletteExtra.push((C) => { C('download', 'Check for updates', A.updateDialog); });
  pushDesktop(); loadDesktop(); setTimeout(autoCheck, A.T.autoCheck); setInterval(autoCheck, A.T.hour);
  loadLevel();
});

/* ===== tooltips: a label above the trigger inside dialogs, below it elsewhere ===== */
rfeModule('fx6-tooltips', ['ui'], function () {
  'use strict';
  const A = window.A;
  const DELAY = 450, GAP = 6, EDGE = 4;
  let tipEl = null, cur = null, timer = 0, watch = null;

  /* Where a tooltip of `size` goes for a trigger box `r` in a window `vp`: centred on the trigger and kept inside the window. It prefers above in a dialog and below elsewhere, and takes the other side only when the preferred one has no room (if neither has, it keeps the preferred one). */
  A.tipPlace = (r, size, vp, inDlg) => {
    const room = { up: r.top - GAP - size.h >= EDGE, down: r.bottom + GAP + size.h <= vp.h - EDGE };
    const want = inDlg ? 'up' : 'down', other = inDlg ? 'down' : 'up';
    const side = !room[want] && room[other] ? other : want;
    const left = Math.max(EDGE, Math.min((r.left + r.right) / 2 - size.w / 2, vp.w - size.w - EDGE));
    return { side, left, top: side === 'up' ? r.top - GAP - size.h : r.bottom + GAP };
  };

  /* The one tooltip element. It lives on <body>, outside every dialog, so no dialog's overflow or stacking can cut it off. */
  const el = () => {
    if (!tipEl) { tipEl = document.createElement('div'); tipEl.className = 'tip'; tipEl.setAttribute('role', 'tooltip'); tipEl.setAttribute('aria-hidden', 'true'); document.body.appendChild(tipEl); }
    return tipEl;
  };

  /* Hides the tooltip and forgets its trigger. */
  const hide = () => {
    clearTimeout(timer); timer = 0; cur = null;
    if (watch) { watch.disconnect(); watch = null; }
    if (tipEl) tipEl.classList.remove('on');
  };

  /* An icon-only dialog button (no visible text, no title or data-tip) shows its aria-label as the label. */
  const iconName = (t) => {
    if (!t.matches('.dlg button.ib[aria-label]:not([title]):not([data-tip])') || t.textContent.trim()) return '';
    return t.getAttribute('aria-label').trim();
  };

  /* Shows the label of `t`. Inside a dialog a plain title is taken over (its native tooltip would be slow and unthemed), keeping the title's text as the accessible name when the control has none. */
  const show = (t) => {
    if (!t.isConnected) return;
    const inDlg = !!t.closest('.dlg');
    if (inDlg && t.hasAttribute('title')) {
      const ti = t.getAttribute('title'); t.removeAttribute('title');
      if (ti && !t.hasAttribute('data-tip')) t.setAttribute('data-tip', ti);
      if (ti && !t.hasAttribute('aria-label') && !t.textContent.trim()) t.setAttribute('aria-label', ti);
    }
    const text = t.getAttribute('data-tip') || iconName(t); if (!text) return;
    const p = el(); p.textContent = text; p.classList.remove('on', 'up', 'down');
    const s = p.getBoundingClientRect();
    const pl = A.tipPlace(t.getBoundingClientRect(), { w: s.width, h: s.height }, { w: window.innerWidth, h: window.innerHeight }, inDlg);
    p.style.left = pl.left + 'px'; p.style.top = pl.top + 'px'; p.classList.add(pl.side, 'on');
    cur = t;
    if (!watch) { watch = new MutationObserver(() => { if (cur && !cur.isConnected) hide(); }); watch.observe(document.body, { childList: true, subtree: true }); }
  };

  /* The element that carries a tooltip: any [data-tip], any [title] inside a dialog, and an icon-only button inside a dialog. */
  const target = (n) => {
    if (!n || !n.closest) return null;
    const t = n.closest('[data-tip], .dlg [title]'); if (t) return t;
    const b = n.closest('.dlg button.ib[aria-label]'); return b && iconName(b) ? b : null;
  };

  document.addEventListener('mouseover', (e) => {
    const t = target(e.target); if (t === cur && t) return;
    hide(); if (t) timer = setTimeout(() => show(t), DELAY);
  });
  document.addEventListener('mouseout', (e) => { if (!cur && !timer) return; const to = e.relatedTarget; if (to && cur && cur.contains(to)) return; hide(); });
  document.addEventListener('focusin', (e) => { const t = target(e.target); hide(); let kb = false; try { kb = !!t && t.matches(':focus-visible'); } catch (x) { /* no such selector */ } if (kb) show(t); });
  document.addEventListener('focusout', hide);
  document.addEventListener('keydown', hide, true);
  document.addEventListener('mousedown', hide, true);
  window.addEventListener('scroll', hide, true);
  window.addEventListener('blur', hide);
});
