/* v4 base: the parts every v4 design shares, unchanged between designs.
   Fixed: navigation (v2 12), Files (v2 11), Settings (v2 15), Overview (v3 2), Servers, Devices and Transfers
   (v3 4: a list with an inspector on the right), Tools (v3 2), History (with filters and an inspector).
   No search bar anywhere: Search, Ctrl K and / open the design's search menu. Each design supplies its
   colours, its top bar and that search menu. Each page shows twice: full of data, and empty (first run). */
(function () {
'use strict';
const K = window.K, I = K.I, E = K.E, D = K.D, B = {};
const q = new URLSearchParams(location.search);
B.empty = q.get('v') === 'empty';
window.B = B;
const EM = B.empty;

/* ---------- plain words: what a PC is doing, and where it is; the technical part stays folded ---------- */
B.HI = { Transfers: 'transfer', Connections: 'plug', Security: 'lock', Devices: 'phone', Shares: 'link' };
B.st = (s) => (s.dot === 'ok' ? 'Connected' : s.dot === 'bad' ? 'Needs your OK' : 'Offline');
B.where = (s) => (s.route === 'LAN' ? 'home network' : 'away from home');
B.tech = (rows, extra = '') => `<details class="b-tech"><summary>Technical details</summary><dl class="k-kv">${rows.map((r) => `<dt>${r[0]}</dt><dd class="mono">${r[1]}</dd>`).join('')}</dl>${extra}</details>`;
K.tag = () => '';

/* ---------- data ---------- */
B.HIST = [
  { d: 'Today', t: '10:46', cat: 'Devices', em: 'mobile_phone', tx: 'Pixel 9 asked to pair with studio-pc', pc: 'studio-pc', dev: 'Pixel 9', out: 'Waiting', src: 'The PC', ip: '192.168.1.40', more: 'Match code 4829 1307 · expires 10:48' },
  { d: 'Today', t: '10:44', cat: 'Transfers', em: 'outbox_tray', tx: 'Uploading walkthrough-full.mov to studio-pc', pc: 'studio-pc', dev: 'desk-linux', out: 'Running', src: 'This app', more: '4.1 GB · 71% · /home/sam/Projects/garden-plan' },
  { d: 'Today', t: '10:41', cat: 'Transfers', em: 'inbox_tray', tx: 'Downloaded north-bed-before.jpg, checksum verified', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'This app', more: '4.1 MB · ~/Downloads/RFE Desktop' },
  { d: 'Today', t: '10:02', cat: 'Transfers', em: 'warning', tx: 'bed-layout.pdf waits: the name exists on work-laptop', pc: 'work-laptop', dev: 'desk-linux', out: 'Waiting', src: 'This app', more: 'Upload to /Users/sam/Documents' },
  { d: 'Today', t: '09:01', cat: 'Security', em: 'locked', tx: 'Signed in to studio-pc as sam', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'The PC', ip: '192.168.1.12', more: 'With the account password' },
  { d: 'Today', t: '08:58', cat: 'Connections', em: 'antenna_bars', tx: 'Connected to studio-pc on the home network', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'This app' },
  { d: 'Today', t: '08:58', cat: 'Security', em: 'warning', tx: 'nas looks different from last time, so RFE stopped connecting to it', pc: 'nas', dev: 'desk-linux', out: 'Failed', src: 'This app', more: 'RFE waits until you say this is fine' },
  { d: 'Today', t: '08:14', cat: 'Connections', em: 'antenna_bars', tx: 'media-mini stopped answering; its transfers are paused', pc: 'media-mini', dev: 'desk-linux', out: 'Failed', src: 'This app' },
  { d: 'Today', t: '07:55', cat: 'Security', em: 'locked', tx: 'Someone typed a wrong password for "admin"', pc: 'studio-pc', dev: 'unknown', out: 'Failed', src: 'The PC', ip: '10.0.0.9', more: 'Wrong password · 3rd try in 10 min' },
  { d: 'Yesterday', t: '18:20', cat: 'Shares', em: 'link', tx: 'Share link made for north-bed-before.jpg, 24 hours', pc: 'studio-pc', dev: 'Pixel 8', out: 'Succeeded', src: 'The PC' },
  { d: 'Yesterday', t: '16:03', cat: 'Devices', em: 'mobile_phone', tx: 'old-phone was removed', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'The PC' },
  { d: 'Yesterday', t: '09:30', cat: 'Connections', em: 'globe_with_meridians', tx: 'RFE restarted on studio-pc', pc: 'studio-pc', dev: '—', out: 'Succeeded', src: 'The PC' },
];
const res = D.search.res.map((r) => ({ n: r[0], k: r[1], path: r[2], pc: r[3], date: r[4], size: r[5] }));
B.S = { q: 'bed', res, off: new Set(), recent: ['invoice 2025', 'walkthrough', 'thesis draft'], saved: D.search.saved, line: '6 results · 48,210 items on studio-pc and work-laptop · media-mini skipped (offline)',
  types: [['Images', 'img', 2], ['Documents', 'doc', 2], ['Folders', 'dir', 1], ['Text', 'txt', 1]], pcs: [['studio-pc', 4], ['work-laptop', 2], ['media-mini', 0, 'offline'], ['nas', 0, 'needs your OK']] };
B.hl = (s, qq = B.S.q) => K.esc(s).replace(qq, `<mark>${qq}</mark>`);

/* ---------- empty data: a first run, nothing added yet ---------- */
const W = (icon, t, s, acts = '') => `<div class="b-empty">${E(icon, 40)}<b>${t}</b><p>${s}</p>${acts ? `<div class="k-row">${acts}</div>` : ''}</div>`;
const add = K.btn('Add a computer', { i: 'plus', k: 'pri', a: 'dlg:newconn' }) + K.btn('Find on network', { i: 'radar', a: 'dlg:newconn:Find on network' });
B.W = W; B.addActs = add;
if (EM) {
  Object.assign(D, { servers: [], files: [], roots: [], requests: [], devices: [], events: [], history: [], favs: [], recent: [], offline: [], big: [], dupes: [], trash: [], shares: [], rules: [], apps: [], notes: [] });
  D.xfer = { stats: [], attention: [], active: [], queued: [], done: [] };
  B.HIST = [{ d: 'Today', t: '10:40', cat: 'Connections', em: 'sunrise', tx: 'RFE Desktop started for the first time', pc: '—', dev: 'desk-linux', out: 'Succeeded', src: 'This app', more: 'Version 1.1.0 · nothing added yet' }];
  Object.assign(B.S, { q: '', res: [], recent: [], saved: [], line: '', types: [], pcs: [] });
  K.C.navItems.forEach((n) => (n.length = 3));
  const a = K.C.actions; K.C.actions = () => a().replace('<i class="k-nb"></i>', '').replace('Notifications: 3 new', 'Notifications');
  const TE = { fav: ['star', 'No favorites', 'Star a folder in Files to keep it here.'], store: ['file_cabinet', 'No storage to show', 'Disk use appears once a computer is connected.'], trash: ['wastebasket', 'Trash is empty', 'Deleted files wait here before they go for good.'], share: ['link', 'No share links', 'Make one from a file: Share link…'], sync: ['counterclockwise_arrows_button', 'No sync rules', 'Pair a folder here with one on a computer.', K.btn('New rule', { i: 'plus', a: 'dlg:newrule' })], apps: ['toolbox', 'No apps to show', 'A connected computer lists the apps it lets you start.'] };
  Object.keys(TE).forEach((t) => (K.V.tool[t] = () => W(...TE[t])));
}

/* ---------- navigation (v2 12) ---------- */
B.side = (v) => `<div class="lg">${I('key', 16)}<span>RFE</span><button class="sb-t" aria-label="Collapse navigation" title="Collapse navigation (Ctrl+\\)">${I('panel', 16)}</button></div><button class="k-navi${v === 'home' ? ' on' : ''}" data-k="go:home">${I('grid', 18)}<span>Overview</span></button>${K.C.nav(v)}<h4>Computers</h4>${D.servers.map((s) => `<button class="pc" title="${s.name}" data-k="${s.dot === 'ok' ? 'go:files' : s.dot === 'bad' ? 'dlg:keychanged' : 'pick:servers:' + s.id}">${K.dot(s.dot)}${s.name}<small>${s.dot === 'ok' ? '' : s.dot === 'bad' ? 'needs OK' : 'offline'}</small></button>`).join('')}<button class="pc" title="Add a computer" data-k="dlg:newconn">${I('plus', 14)}Add a computer</button><span class="gap"></span>`;

/* ---------- Files (v2 11) ---------- */
const ia = (a) => `<span class="ia">${a.map((x) => K.ib(...x)).join('')}</span>`;
const fA = ia([['download', 'Download (Ctrl D)', 'toast:Downloading to ~/Downloads/RFE Desktop'], ['link', 'Share link…', 'dlg:share'], ['more', 'Rename, move, delete and more', 'menu:file']]);
const n = (d, tw, l, m, cls = '', act = '', row = '') => `<div class="n ${cls}" style="padding-left:${8 + d * 24}px" ${row ? `data-row="${row}"` : ''}><span class="l"><span class="tw">${tw ? I(tw === 'o' ? 'chevd' : 'chevr', 14) : ''}</span>${l}</span><span class="m">${m}</span>${act}</div>`;
function outlineFull() {
  let h = '';
  h += n(0, 'o', `${K.dot('ok')}${E('server', 18)}studio-pc <small>Fedora · home network</small>`, '<span>812 GB of 2 TB</span>', 'pc', ia([['upload', 'Upload to studio-pc', 'dlg:upload'], ['more', 'Status, diagnostics, roots', 'menu:server']]));
  h += n(1, 'o', `${E('home', 16)}/home/sam`, '<span>812 GB used</span>');
  h += n(2, 'c', `${K.fic('dir')}Documents`, '<span>38 items</span>');
  h += n(2, 'o', `${K.fic('dir')}Projects`, '<span>9 items</span>');
  h += n(3, 'o', `${K.fic('dir')}garden-plan`, '<span>11 items</span>', '', ia([['upload', 'Upload into garden-plan', 'dlg:upload'], ['folder-plus', 'New folder (Ctrl+Shift+N)', 'dlg:newfolder']]));
  D.files.forEach((f) => {
    h += n(4, f.k === 'dir' ? 'c' : '', `${K.fic(f.k)}${f.n}${f.k === 'dir' ? `<small>${f.c} items</small>` : ''}`, `<span>${f.s || ''}</span><span>${f.m}</span>`, f.n === D.sel ? 'sel' : '', f.k === 'dir' ? ia([['download', 'Download as .zip'], ['more', 'More', 'menu:file']]) : fA, f.n);
    if (f.n === 'walkthrough.mp4') h += n(4, '', `${K.fic('vid')}walkthrough-full.mov <small>uploading</small>`, `${K.bar(71)}<b class="mono" style="color:var(--t2)">71%</b>`, '', ia([['pause', 'Pause'], ['x', 'Cancel', 'dlg:cancelx']]));
  });
  h += n(1, 'c', `${E('drive', 16)}/srv/media`, '<span>1.1 TB used</span>') + n(1, 'c', `${E('wastebasket', 16)}Trash`, '<span>5 items</span>', '', ia([['trash', 'Empty Trash', 'dlg:emptytrash']])) + n(1, 'c', `${E('link', 16)}Share links`, '<span>2 live</span>') + n(1, 'c', `${E('toolbox', 16)}Apps`, '<span>6</span>');
  h += n(0, 'c', `${K.dot('ok')}${E('laptop', 18)}work-laptop <small>Windows · Tailscale</small>`, '<span>402 GB of 512 GB</span>', 'pc', ia([['upload', 'Upload here', 'dlg:upload'], ['more', 'More', 'menu:server']]));
  h += n(0, '', `${K.dot('bad')}${E('server', 18)}nas <small>looks different from last time</small>`, '<span class="bad">Waiting for your OK</span>', 'pc', `<span style="display:flex">${K.btn('Check…', { k: 'sm', a: 'dlg:keychanged' })}</span>`);
  h += n(0, '', `${K.dot('off')}${E('server', 18)}media-mini <small>offline · last seen 08:14</small>`, '<span>1 transfer paused</span>', 'pc', `<span style="display:flex">${K.btn('Wake computer', { k: 'sm', i: 'power' })}</span>`);
  h += n(0, '', `${I('plus', 14)}Add a computer`, '', 'pc" data-k="dlg:newconn" style="cursor:pointer;color:var(--t2)');
  const f = D.files.find((x) => x.n === D.sel);
  return `<div class="ol"><div class="tr">${h}</div><aside class="olside"><div data-k="dlg:preview" style="cursor:zoom-in">${K.photo(2, 280, 200, 8)}</div><h3>${E('img', 16)}${f.n}</h3><p class="k-sub">3.8 MB · 4032 × 3024 · ${f.m}</p><dl class="k-kv" style="margin-top:12px"><dt>On</dt><dd>studio-pc</dd><dt>Folder</dt><dd>Projects › garden-plan</dd></dl><div class="k-dact" style="margin-top:12px">${K.btn('Download', { i: 'download', k: 'pri', kc: 'Ctrl D' })}${K.btn('Open', { i: 'play', a: 'dlg:stream' })}<div class="k-row">${K.btn('Share link…', { i: 'link', a: 'dlg:share' })}${K.ib('more', 'Offline, properties, checksum, copy path', 'menu:detmore')}</div></div></aside></div>`;
}
B.outline = () => EM ? `<div class="ol"><div class="tr emp"><h1 class="k-title">Files</h1><div class="b-empty">${E('open_file_folder', 40)}<b>Your computers’ files appear here as one tree</b><p>Each computer you add becomes a branch: its folders, and the files in them, with the actions on each row.</p><div class="k-row">${add}</div></div></div><aside class="olside"><h3>Set up a PC</h3><ol class="b-steps"><li>Install RFE on the PC and open it once.</li><li>Here, choose <b>Add a computer</b> and pick the PC.</li><li>Check that both screens show the same number, then click Allow on the PC.</li></ol></aside></div>` : outlineFull();

/* ---------- Overview (v3 2) ---------- */
const up = [2, 3, 2, 6, 14, 22, 18, 30, 41, 38, 47, 52, 49, 53, 52, 51], dn = [1, 1, 4, 9, 6, 4, 12, 8, 6, 11, 9, 7, 14, 10, 6, 8];
const X = (i) => 40 + i * (800 / 15), Y = (v) => 200 - v * 3, line = (a) => a.map((v, i) => `${i ? 'L' : 'M'}${X(i).toFixed(1)} ${Y(v)}`).join(' ');
const grid = `${[0, 20, 40, 60].map((v) => `<path d="M40 ${Y(v)}H840" stroke="var(--line)"/><text x="0" y="${Y(v) + 4}">${v}</text>`).join('')}${['08:00', '09:00', '10:00', '11:00'].map((t, i) => `<text x="${40 + i * 266 - 16}" y="226">${t}</text>`).join('')}`;
B.home = () => {
  const stats = EM ? [['Moving now', '—', 'nothing to move yet'], ['Moved today', '0 B', 'since RFE started'], ['Computers up', '0', 'add one to start'], ['Devices', '0', 'pair a phone after']] : [['Moving now', '52.4 MB/s', '2 transfers · 23 s left'], ['Moved today', '1.9 GB', '+640 MB vs yesterday'], ['Computers up', '2 of 4', 'nas needs your OK · media-mini off'], ['Devices', '4 allowed', '2 asking to pair']];
  return `<div class="db">${stats.map((s) => `<div class="stat"><small>${s[0]}</small><b>${s[1]}</b><span>${s[2]}</span></div>`).join('')}
<div class="chart"><h2>Speed today <small><i style="display:inline-block;width:12px;height:2px;background:var(--ac);vertical-align:middle"></i> Download · <i style="display:inline-block;width:12px;height:2px;background:var(--t3);vertical-align:middle"></i> Upload · MB/s</small></h2><svg viewBox="0 0 860 232">${grid}${EM ? '' : `<path d="${line(dn)}" fill="none" stroke="var(--t3)" stroke-width="1.5"/><path d="${line(up)}" fill="none" stroke="var(--ac)" stroke-width="2"/>`}</svg>${EM ? '<div class="ov">The line starts with your first transfer</div>' : ''}</div>
<div class="card"><h2>Waiting for you ${EM ? '' : '<em class="k-badge bad">4</em>'}</h2>${EM ? '<div class="nil">Nothing waits for you.<small>Phones asking to join and files that need a choice show up here.</small></div>' : [['phone', 'Pixel 9 asks to join studio-pc', 'Same number: <b class="b-n">4829 1307</b> · 1:42 left', K.btn('Allow…', { k: 'sm pri', a: 'dlg:accept' })], ['laptop', 'sam-thinkpad wants to replace work-laptop', 'Same number: <b class="b-n">0316 8842</b>', K.btn('Review…', { k: 'sm', a: 'dlg:accept' })], ['doc', 'bed-layout.pdf exists on work-laptop', 'Upload to /Users/sam/Documents', K.btn('Keep both', { k: 'sm' })], ['!alert', 'notes.txt was damaged while uploading', 'Discarded; try again', K.btn('Retry', { k: 'sm', i: 'refresh' })]].map((x) => `<div class="need">${x[0][0] === '!' ? `<span class="b-ic">${I(x[0].slice(1), 18)}</span>` : E(x[0], 20)}<div>${x[1]}<small>${x[2]}</small></div>${x[3]}</div>`).join('')}</div>
<div class="card w"><h2>Computers</h2>${EM ? `<div class="nil">No computers yet.${K.btn('Add a computer', { i: 'plus', k: 'sm pri', a: 'dlg:newconn' })}</div>` : D.servers.map((s) => `<div class="pcr" data-k="pick:servers:${s.id}">${K.dot(s.dot)}<span>${K.mach(s, 16)}${s.name}</span>${K.bar(Math.round(s.used / s.total * 100))}<small>${Math.round(s.used / s.total * 100)}% full</small></div>`).join('')}</div>
<div class="card w"><h2>Recent files</h2>${EM ? '<div class="nil">Files you open or move show here.</div>' : `<div class="rc">${[[2, 'north-bed-after.jpg', 'studio-pc · 12 min ago'], [1, 'north-bed-before.jpg', 'downloaded'], [3, 'garden-sketch.png', 'this computer']].map((r) => `<div data-k="dlg:preview" style="cursor:pointer">${K.photo(r[0], 180, 100, 8)}<b>${r[1]}</b>${r[2]}</div>`).join('')}</div>`}</div></div>`;
};

/* ---------- Servers, Devices, Transfers: a list and an inspector (v3 4) ---------- */
const pick = { servers: 'studio', devices: 'Pixel 8', transfers: 'walkthrough-full.mov', history: 4 };
B.pick = pick;
const ro = (page, id, icon, t, s, st) => `<div class="ro${String(pick[page]) === String(id) ? ' sel' : ''}" data-k="pick:${page}:${id}">${icon}<span><b>${t}</b><small>${s}</small></span><span class="st">${st}</span></div>`;
const allX = () => [...D.xfer.attention, ...D.xfer.active, ...D.xfer.queued, ...D.xfer.done];
const xst = (t) => (t.st ? { conflict: 'Action required', paused: 'Paused', failed: 'Failed' }[t.st] : t.pct === 100 ? (/Checking/.test(t.msg) ? 'Checking' : 'Done') : t.pct ? 'Moving' : 'Waiting');
const gb = (n) => (n >= 1000 ? +(n / 1000).toFixed(1) + ' TB' : n + ' GB');
const OUT = { Succeeded: 'ok', Failed: 'bad', Waiting: 'warn', Running: '' };
/* each device's own rights, read from what it may do */
const rights = (d) => { const m = d.may.toLowerCase(), own = /owner/.test(m), ro = /read-only/.test(m), has = (re) => !d.revoked && (own || re.test(m));
  return [['See files', !d.revoked], ['Download', has(/download/)], ['Upload', !ro && has(/upload/)], ['Rename and move', !ro && has(/change/)], ['Delete', !ro && has(/delete/)], ['Make share links', has(/share/)], ['Wake computers', has(/wake/)], ['See apps', has(/see apps/)], ['Open apps', has(/start apps/)], ['Only look, never change', ro]].map((x) => [x[0], x[1] ? 1 : 0]); };
const INSP = {
  servers: () => { const s = K.S(pick.servers); return `${K.mach(s, 40)}<h2>${KIND(20)}${s.name}</h2><p class="k-sub">${K.dot(s.dot)} ${B.st(s)} · ${B.where(s)}</p><dl class="k-kv"><dt>Space</dt><dd>${gb(s.used)} of ${gb(s.total)} used${K.bar(s.used / s.total * 100)}</dd></dl><div class="k-dact">${s.dot === 'ok' ? K.btn('Open files', { i: 'folder', k: 'pri', a: 'go:files' }) : s.dot === 'bad' ? K.btn('Check what changed…', { i: 'shield', k: 'warn', a: 'dlg:keychanged' }) : K.btn('Wake computer', { i: 'power', k: 'pri' })}${K.btn('Check the connection', { i: 'activity', a: 'dlg:diag' })}${K.btn(`Settings for ${s.name}…`, { i: 'sliders', a: 'dlg:srvset' })}${K.btn('Folders it shares…', { i: 'folder', a: 'dlg:roots' })}${K.btn('Changed lately…', { i: 'clock', a: 'dlg:recent' })}${K.btn('Remove…', { i: 'trash', k: 'ghost rm', a: 'dlg:forget' })}</div>${B.tech([['System', s.sys], ['Address', s.addr], ['RFE version', s.ver], ['Connection', s.route + (s.ms ? ', ' + s.ms + ' ms' : '')], ['Key', s.tag + ' …']])}`; },
  transfers: () => { const t = allX().find((x) => x.n === pick.transfers) || allX()[0]; const [pc, dir] = t.where.split(' · '), st = xst(t), local = !dir;
    const acts = t.st === 'conflict' ? K.btn('Keep both', { i: 'copy', k: 'pri' }) + K.btn('Replace', { i: 'refresh' }) + K.btn('Skip', { i: 'undo', k: 'ghost' }) : t.st === 'failed' ? K.btn('Try again', { i: 'refresh', k: 'pri' }) + K.btn('Remove', { i: 'trash', k: 'ghost' }) : t.st === 'paused' ? K.btn('Resumes when ' + pc + ' is back', { i: 'play', k: 'pri', dis: true }) + K.btn('Cancel', { i: 'x', k: 'ghost' }) : st === 'Checking' ? K.btn('Cancel', { i: 'x', k: 'ghost' }) : t.pct === 100 ? K.btn(local ? 'Show in folder' : 'Show in Files', { i: 'folder', k: 'pri', a: local ? 'toast:Opened ' + pc : 'go:files' }) : K.btn('Pause', { i: 'pause', k: 'pri' }) + K.btn('Show in Files', { i: 'folder', a: 'go:files' }) + K.btn('Cancel', { i: 'x', k: 'ghost' });
    return `${E(t.d === 'up' ? 'outbox_tray' : 'inbox_tray', 40)}<h2>${t.n}</h2><p class="k-sub">${local ? 'Downloaded to this computer · ' + pc : (t.d === 'up' ? 'Upload to ' : 'Download from ') + pc + ' · ' + dir}</p>${t.pct != null ? K.bar(t.pct) : ''}<dl class="k-kv" style="margin-top:12px"><dt>State</dt><dd>${st}${t.pct != null && t.pct < 100 ? ' · ' + t.pct + '%' : ''}</dd><dt>Details</dt><dd>${t.msg}</dd></dl><div class="k-dact">${acts}</div>`; },
  history: () => { const h = B.HIST[pick.history] || B.HIST[0]; const acts = { Transfers: K.btn('Show in Files', { i: 'folder', k: 'pri', a: 'go:files' }), Devices: K.btn('Open in Devices', { i: 'phone', k: 'pri', a: 'go:devices' }), Shares: K.btn('Stop sharing…', { i: 'link', k: 'pri' }), Connections: K.btn('Open in Devices', { i: 'server', k: 'pri', a: 'go:servers' }), Security: K.btn('Open in Devices', { i: 'server', k: 'pri', a: 'go:servers' }) }; return `<span class="b-tile">${I(B.HI[h.cat] || 'activity', 20)}</span><h2>${h.cat}</h2><p class="k-sub"><span class="b-out ${OUT[h.out]}">${h.out}</span> · ${h.d}, ${h.t}</p><p style="margin:0 0 16px">${h.tx}</p><dl class="k-kv"><dt>Computer</dt><dd>${h.pc}</dd><dt>Device</dt><dd>${h.dev}</dd>${h.more ? `<dt>Details</dt><dd>${h.more}</dd>` : ''}</dl><div class="k-dact">${acts[h.cat] || ''}${K.btn('Copy details', { i: 'copy' })}</div>${B.tech([['Recorded by', h.src], ...(h.ip ? [['From address', h.ip]] : [])], h.src === 'The PC' ? K.btn('Open the PC’s log', { k: 'sm ghost', i: 'text', a: 'dlg:agentlog' }) : '')}`; },
};
/* the picked device's panel, drawn under the device list inside the Devices card (the shipped window does the same) */
const devRail = (d) => '<div class="rail cdd">' + E(d.kind, 40) + '<h2><span class="dnt">' + d.n + '</span>' + (d.me ? ' <em class="k-pill">This app</em>' : '') + '</h2><p class="k-sub">' + d.how + ' · seen ' + d.seen + '</p><div class="k-perms">' + rights(d).map((x) => '<div class="k-set"><div><b>' + x[0] + '</b></div>' + K.sw(x[1]) + '</div>').join('') + '</div>' +
  '<div class="b-fl v5-lim"><small>Only this folder (leave blank for every shared folder)</small><span class="b-f">' + I('folder', 16) + '<input value="' + (d.lim || '') + '" placeholder="Every shared folder" aria-label="Only this folder" readonly></span><span class="k-row">' + K.btn('Choose folder…', { i: 'folder', k: 'sm' }) + (d.lim ? K.btn('No limit', { k: 'ghost sm' }) : '') + '</span></div>' +
  '<div class="k-dact">' + K.btn('What it did', { i: 'history', a: 'go:history' }) + K.btn('Remove access…', { i: 'trash', k: 'warn', a: d.me ? 'dlg:revokeme' : 'dlg:revoke' }) + '</div>' + B.tech([['Address', d.addr], ['Version', d.ver], ['Device id', d.id]]) + '</div>';
const KIND = (n) => '<span class="k-kind" data-kind="pc" data-tip="PC">' + I('monitor', n) + '</span>';
const LIST = {
  servers: () => K.head('Devices', `${D.servers.length} saved`, K.btn('Add a computer', { i: 'plus', k: 'pri', a: 'dlg:newconn' })) + `<div class="li">${D.servers.map((s) => ro('servers', s.id, K.mach(s, 24), KIND(14) + s.name, (s.dot === 'ok' ? s.sys.replace(/ [\d.]+$/, '') + ' · ' : '') + B.where(s), K.dot(s.dot) + B.st(s))).join('')}</div>` + LIST.devicesPart(),
  /* the phones paired with the picked computer: a section of the one Devices page, drawn under the computers */
  devicesPart: () => { const dv = D.devices.find((x) => x.n === pick.devices) || D.devices[1];
    return '<details class="k-card cdv"><summary><span class="cdt">Devices on studio-pc</span> <span class="k-count">' + D.devices.length + '</span> <em class="k-pill">Asking to join</em></summary><div class="v5pg cdvb"><div class="cdvh">' + K.btn('Pair a phone', { i: 'qr', a: 'dlg:pairphone' }) + '</div>' +
      K.sec('Asking to join <span class="k-count">2</span>', '', K.V.requests() + B.tech(D.requests.map((r) => [r.n, r.from]))) +
      '<div class="li">' + D.devices.map((d) => ro('devices', d.n, E(d.kind, 24), d.n + (d.me ? ' <em class="k-pill">This app</em>' : ''), d.how + ' · ' + d.seen, d.revoked ? '<span class="bad">Removed</span>' : d.lim ? 'only ' + d.lim.split('/').pop() : 'every folder')).join('') + '</div>' + devRail(dv) +
      (EM ? '' : K.sec('Recent security events', K.btn('View all', { k: 'sm ghost', a: 'go:history' }), '<div class="k-evs">' + D.events.slice(0, 4).map((e) => '<div class="k-ev ' + e[3] + '"><time class="mono">' + e[0] + '</time><b>' + e[1] + '</b><span>' + e[2].replace('Agent', 'PC').replace(/ from [0-9.]+/, ' from another computer') + '</span></div>').join('') + '</div>')) + '</div></details>'; },
  transfers: () => K.head('Transfers', '2 moving · 3 need you', K.btn('Pause all', { i: 'pause', k: 'sm' }) + K.btn('Resume all', { i: 'play', k: 'sm', a: 'toast:Everything paused is moving again' }) + K.btn('Retry failed', { i: 'refresh', k: 'sm', a: 'toast:Trying notes.txt again' }) + K.btn('Clear done', { i: 'trash', k: 'sm', a: 'toast:Cleared 2 finished transfers' })) + (EM ? '' : `<div class="k-banner">${I('clock')}<span><b>2 transfers were left unfinished</b> when RFE closed.</span>${K.btn('Carry on…', { k: 'sm', a: 'dlg:resume' })}</div>`) + `<div class="li">${allX().map((t) => ro('transfers', t.n, E(t.d === 'up' ? 'outbox_tray' : 'inbox_tray', 24), t.n, t.where, t.st ? `<span class="${t.st === 'failed' ? 'bad' : 'warn'}">${xst(t)}</span>` : t.pct && t.pct < 100 ? t.pct + '%' : xst(t))).join('')}</div>`,
};
/* one page: the route 'devices' is an alias of 'servers' that selects a phone in the right panel */
LIST.devices = () => LIST.servers();
if (EM) {
  LIST.servers = () => K.head('Devices', 'None saved yet') + W('desktop_computer', 'No devices yet', 'Install RFE on a PC and open it once. Then add it here: it shows up by itself on the same network.', add);
  LIST.transfers = () => K.head('Transfers', 'Nothing moving') + W('updown_arrow', 'Nothing moving', 'Uploads and downloads appear here. They resume on their own after a lost connection or a restart.');
}

/* ---------- History: filters on every field an event has, and an inspector ---------- */
const CATS = [['All', ''], ['Transfers', 'updown_arrow'], ['Connections', 'antenna_bars'], ['Security', 'locked'], ['Devices', 'mobile_phone'], ['Shares', 'link']];
const sel = (label, opts) => `<label class="b-fs" title="${label}"><select class="k-sel" aria-label="${label}" data-hf="${label}">${opts.map((o) => `<option${HF[label] === o || (label === 'Kind' && HF.Kind === o.split(' · ')[0]) ? ' selected' : ''}>${o}</option>`).join('')}</select></label>`;
const HF = { q: '', Kind: 'Kind', When: 'When', Computer: 'Computer', Device: 'Device', Outcome: 'Outcome' };
B.HF = HF;
const hmatch = (h) => {
  const k = HF.Kind.split(' · ')[0];
  if (k === 'Only what the PCs recorded' ? h.src !== 'The PC' : k !== 'Kind' && h.cat !== k) return false;
  if (HF.When === 'Today' || HF.When === 'Yesterday' ? h.d !== HF.When : false) return false;
  if (HF.Computer !== 'Computer' && h.pc !== HF.Computer) return false;
  if (HF.Device !== 'Device' && h.dev !== HF.Device.replace(' (this one)', '')) return false;
  if (HF.Outcome !== 'Outcome' && h.out !== HF.Outcome) return false;
  return !HF.q || (h.tx + ' ' + h.pc + ' ' + h.dev + ' ' + (h.more || '')).toLowerCase().includes(HF.q.toLowerCase());
};
B.history = (st) => {
  if (st.hist && st.hist !== 'All') HF.Kind = st.hist; else if (st.hist === 'All') { Object.assign(HF, { q: '', Kind: 'Kind', When: 'When', Computer: 'Computer', Device: 'Device', Outcome: 'Outcome' }); st.hist = ''; }
  const rows = B.HIST.map((h, i) => [h, i]).filter(([h]) => hmatch(h)), on = Object.keys(HF).some((k) => k === 'q' ? HF.q : HF[k] !== k);
  const cnt = (c) => (c === 'All' ? B.HIST.length : B.HIST.filter((h) => h.cat === c).length);
  let day = '', list = '';
  rows.forEach(([h, i]) => { if (h.d !== day) { day = h.d; list += `<h3 class="b-day">${day}</h3>`; } list += `<div class="b-ev${pick.history === i ? ' sel' : ''}" data-k="pick:history:${i}"><time class="mono">${h.t}</time><span class="b-ic">${I(B.HI[h.cat] || 'activity', 16)}</span><span class="tx">${h.tx}<small>${h.pc} · ${h.dev}</small></span><span class="b-out ${OUT[h.out]}">${h.out}</span></div>`; });
  return K.head('History', 'What happened here and on your PCs', K.ib('download', 'Export as CSV') + K.ib('trash', 'Clear this app’s history')) +
    `<div class="b-filt"><label class="k-in sm b-fq">${I('filter', 14)}<input placeholder="Filter" aria-label="Filter by words, a file or an address" data-hf="q" value="${K.esc(HF.q)}"></label>${sel('Kind', ['Kind', ...CATS.slice(1).map((c) => c[0] + ' · ' + cnt(c[0])), 'Only what the PCs recorded'])}${sel('When', ['When', 'Today', 'Yesterday', 'Last 7 days', 'Last 30 days'])}${sel('Computer', ['Computer', 'studio-pc', 'work-laptop', 'nas', 'media-mini'])}${sel('Device', ['Device', 'desk-linux (this one)', 'Pixel 8', 'Pixel 9', 'Galaxy Tab S9', 'Unknown'])}${sel('Outcome', ['Outcome', 'Succeeded', 'Failed', 'Waiting', 'Running'])}</div>
<p class="b-fsum">${rows.length} of ${B.HIST.length} events · newest first${on ? K.btn('Clear filters', { k: 'sm ghost', a: 'hist:All' }) : ''}</p><div class="b-hist">${list}</div>${EM ? '<p class="mut" style="margin-top:16px">Connections, transfers, sign-ins and pairing are recorded here as they happen.</p>' : ''}`;
};

/* ---------- the rail: an inspector for the selected row ---------- */
B.rail = (v) => {
  if (EM) return '';
  if (v === 'history' || (INSP[v] && LIST[v]) || v === 'devices') return INSP[v === 'devices' ? 'servers' : v]();
  return '';
};


/* ---------- dialogs in plain words ---------- */
const dl = (t, body, acts, w = 520, cls = '') => `<div class="k-dlg ${cls}" role="dialog" aria-label="${t}" style="width:${w}px"><div class="k-dh"><h2>${t}</h2>${K.ib('x', 'Close (Esc)', 'close')}</div><div class="k-db">${body}</div>${acts ? `<div class="k-da">${acts}</div>` : ''}</div>`;
const live = (t) => `<span class="b-live"><i></i>${t}</span>`;
K.V.requests = () => D.requests.length ? `<table class="b-rq"><thead><tr><th>Device</th><th>Number</th><th>Time left</th><th></th></tr></thead><tbody>${D.requests.map((r) => `<tr><td><div class="b-rqd">${E('phone', 24)}<div><b>${r.n}</b><small>${r.over ? `Replace <b>${r.over}</b>, access starts over` : 'See files on studio-pc'}</small></div></div></td><td class="mono b-rqn" title="The number on ${r.n}">${r.code}</td><td class="b-rqt">${r.exp}</td><td class="b-rqa">${K.btn('Allow…', { i: 'check', k: 'pri sm', a: 'dlg:accept' })}${K.btn('Refuse', { i: 'x', k: 'warn sm', a: 'toast:' + r.n + ' was turned away' })}</td></tr>`).join('')}</tbody></table><p class="k-note">${I('info', 14)}Allow a device only if it shows the same number.</p>` : `<div class="b-rqe"><b>Nobody is asking to join</b><small>New requests appear in this list.</small></div>`;
const FOUND = EM ? [] : [{ n: 'garage-pi', os: 'Raspberry Pi', em: 'server' }, { n: 'den-desktop', os: 'Windows', em: 'desktop_computer' }];
const STEPS = [['Find your PC', 'on this network or by its address'], ['Allow it on the PC', 'the same number on both'], ['Ready', 'name it, open its files']];
const rail = (n) => `<div class="b-sx">${STEPS.map((x, i) => `<div class="${i + 1 === n ? 'on' : i + 1 < n ? 'done' : ''}"><i>${i + 1 < n ? I('check', 12) : i + 1}</i><span>${x[0]}<small>${x[1]}</small></span></div>`).join('')}</div>`;
const step1 = () => `<h3>Which PC do you want to reach?</h3><p>These PCs have RFE and are on the same network as this one.</p>${EM ? `<div class="b-none">${E('desktop_computer', 32)}<span><b>No PCs found yet</b>Install RFE on the PC you want to reach and open it once. It shows up here by itself.</span>${live('Looking')}</div>` : FOUND.map((p, i) => `<button class="b-pc${i ? '' : ' on'}">${E(p.em, 28)}<span><b>${p.n}</b><small>${p.os}</small></span>${i ? '' : I('check', 16)}</button>`).join('') + `<button class="b-pc" disabled>${E('desktop_computer', 28)}<span><b>studio-pc</b><small>already added</small></span></button>`}
<details class="b-more"><summary>Not in the list?</summary><p>PCs at another place, or reached over Tailscale, are not listed. Type the address RFE shows on that PC, under Status.</p><label class="b-f">${I('globe', 16)}<input placeholder="Its address" aria-label="Its address"></label></details><div class="b-kind"><b>What kind of device is it?</b>${K.seg(['PC', 'Laptop', 'Server', 'Mobile'], 'PC')}</div>`;
const allow = (pc, addr, key, num) => `<h3>Allow this computer on ${pc}</h3><p>On ${pc}, RFE now asks <b>“Let desk-linux in?”</b> Check that it shows this number, then click Allow there.</p><div class="b-num mono">${num}</div><p class="b-wait">${live('Waiting for ' + pc)} · 1:58 left</p>
<details class="b-more"><summary>Nobody near ${pc}?</summary><p>If ${pc} has an RFE account, sign in with its password instead.</p>${K.btn('Use a password instead', { k: 'sm' })}</details>${B.tech([['Address', addr], ['Key', key], ['RFE version', '1.3.0']], '<p class="b-tn">RFE remembers this key and warns you if it ever changes.</p>')}`;
const step2 = () => (EM ? `<h3>Allow this computer on the PC</h3><p>Pick a PC in step 1 first. Then RFE on that PC asks to let this computer in, with a number to compare.</p>` : allow('garage-pi', '192.168.1.44:8765', '5C1E 88A0 77B0 2D19 …', '3071 5592'));
const step3 = () => `<div class="b-done"><span class="b-tile ok">${I('check', 22)}</span><h3>garage-pi is ready</h3><p>It is added to your computers. You can change what it shares on the PC.</p><label class="b-fl"><small>Name it here</small><span class="b-f">${I('pencil', 16)}<input value="garage-pi" aria-label="Name"></span></label></div>`;
K.DLG.newconn = (n = '1') => { n = +n || 1; const body = `${rail(n)}<div class="b-pane">${[step1, step2, step3][n - 1]()}</div>`;
  const acts = n === 1 ? K.btn('Cancel', { a: 'close' }) + K.btn('Next', { k: 'pri', i: 'right', a: 'dlg:newconn:2', dis: EM }) : n === 2 ? K.btn('Back', { a: 'dlg:newconn:1' }) + `<span class="b-auto k-sub">${EM ? '' : 'Moves on by itself once you click Allow on the PC'}</span>` : K.btn('Back', { a: 'dlg:newconn:2' }) + K.btn('Add another', { k: 'ghost', a: 'dlg:newconn:1' }) + K.btn('Open its files', { k: 'pri', i: 'folder', a: 'go:files' });
  if (n === 2 && !EM && !/still/.test(location.hash)) setTimeout(() => { const on = document.querySelector('.k-layer .b-sx > div.on > span'); if (on && /^Allow it on the PC/.test(on.textContent)) K.openDlg('newconn', '3'); }, window.RFE_ADV_MS || 5000);
  return dl('Add a computer', body, acts, 720, 'b-add'); };
K.DLG.trust = () => K.DLG.newconn('2');
K.DLG.keychanged = () => EM ? noPc('A computer changed') : dl('nas looks different from last time', `<p>RFE stopped connecting to nas to keep your files safe. This is normal after RFE was reinstalled or the PC was reset.</p><ul class="b-ul"><li><b>If that happened</b>, connect again and allow this computer on nas, like the first time.</li><li><b>If not</b>, someone else may be pretending to be nas. Leave it and ask whoever looks after it.</li></ul>${B.tech([['Before', '41DE 0B93 77B0 …'], ['Now', 'E2A7 51C0 9D44 …']])}`, K.btn('Leave it for now', { a: 'close' }) + K.btn('Reconnect', { k: 'pri', a: 'dlg:reconnect' }), 540);
if (EM) K.DLG.pairphone = () => noPc('Pair a phone');
K.DLG.acctserver = () => dl('Account server', `<p class="k-sub">Accounts kept on this computer can sign in to the computers that trust it.</p><h3 class="dsh">Account server this computer trusts</h3><p class="fxp">No account server is trusted by this computer.</p><h3 class="dsh">Trust an account server</h3><p>Look up its key, then check that its code matches the one shown on that computer. Trusting it lets its accounts sign in to this computer with full owner access.</p><label class="b-fl"><small>Address of that computer</small><span class="b-f"><input placeholder="192.168.1.20:8765" aria-label="Address"></span></label><div class="k-dact">${K.btn('Look up its key', { k: 'sm' })}</div>`, K.btn('Close', { a: 'close' }) + K.btn('Stop trusting', { k: 'warn', a: 'dlg:acctuntrust' }) + K.btn('Trust this account server', { k: 'pri', a: 'close' }), 560);
K.DLG.acctuntrust = () => dl('Stop trusting this account server?', '<p>Its accounts can no longer sign in to this computer with full owner access. Trust it again to let them back in.</p>', K.btn('Cancel', { a: 'close' }) + K.btn('Stop trusting', { k: 'warn', a: 'close' }), 440);
K.DLG.forget = () => dl('Remove media-mini?', '<p>It leaves your computers here. Transfers to it that have not finished stop. Nothing on media-mini is deleted.</p>', K.btn('Cancel', { a: 'close' }) + K.btn('Remove', { k: 'warn', a: 'close' }));
K.DLG.revoke = () => dl('Remove Pixel 8’s access?', '<p>Pixel 8 can no longer open anything on studio-pc. To come back it has to be allowed again.</p>', K.btn('Cancel', { a: 'close' }) + K.btn('Remove access', { k: 'warn', a: 'close' }));
K.DLG.revokeme = () => dl('Remove this computer’s own access?', '<p>This is the sign-in this app uses. If you remove it, this window is signed out and you have to sign in to studio-pc again.</p>', K.btn('Cancel', { a: 'close' }) + K.btn('Remove and sign out', { k: 'warn', a: 'toast:Signed out of studio-pc' }));
K.DLG.accept = () => dl('Allow Pixel 9?', `<p>Allow it only if Pixel 9 shows this same number.</p><div class="b-num mono">4829 1307</div><p class="k-sub">It can see files to start with. Give it more in Devices.</p>`, K.btn('Cancel', { a: 'close' }) + K.btn('Same number: allow', { k: 'pri', a: 'close' }));
K.DLG.diag = () => dl('Connection to studio-pc', `<div class="k-list">${[['Found it on the network'], ['It is the same PC as before'], ['Signed in'], ['Working normally']].map((x) => `<div class="k-lrow">${I('check')}<span>${x[0]}</span></div>`).join('')}</div>${B.tech([['Address', '192.168.1.20:8765'], ['Answer time', '12 ms'], ['Key', '9F3A 1EC4 …']])}`, K.btn('Copy for support', { i: 'copy', k: 'ghost' }) + K.btn('Check again', { i: 'refresh' }));
K.DLG.roots = () => dl('Folders studio-pc shares', `<p>Only these folders can be opened from here. Change them on studio-pc.</p><div class="k-list">${D.roots.map((r) => `<div class="k-lrow">${E(r.k, 18)}<span>${r.n.split('/').pop()}</span><small class="mut mono">${r.n}</small></div>`).join('')}</div>`, K.btn('Done', { a: 'close' }));
K.DLG.reconnect = () => EM ? noPc('Connect again') : dl('Connect to nas again', `<div class="b-pane">${allow('nas', '192.168.1.5:8765', 'E2A7 51C0 9D44 …', '6150 7724')}</div>`, K.btn('Back', { a: 'dlg:keychanged' }) + K.btn('Done', { k: 'pri', a: 'toast:nas is connected again' }), 560);
const noPc = (t) => dl(t, `<div class="b-empty">${E('desktop_computer', 32)}<b>No computer yet</b><p>Add a computer first. Its settings and status show up here.</p></div>`, K.btn('Cancel', { a: 'close' }) + K.btn('Add a computer', { k: 'pri', i: 'plus', a: 'dlg:newconn' }));
const pcNow = () => K.S(B.pick.servers);
K.DLG.status = () => { if (EM) return noPc('Status'); const s = pcNow(); return dl(`How ${s.name} is doing`, `<dl class="k-kv cols"><dt>State</dt><dd>${K.dot(s.dot)} ${B.st(s)} · ${B.where(s)}</dd><dt>System</dt><dd>${s.sys}</dd><dt>Space</dt><dd>${s.used} GB of ${s.total} GB used</dd><dt>Running for</dt><dd>${s.up || '3 days'}</dd></dl><h3>Right now</h3><div class="k-stats sm"><div><b>18%</b><small>busy</small></div><div><b>41%</b><small>memory in use</small></div></div>${B.tech([['RFE version', s.ver], ['Address', s.addr], ...(s.tail ? [['Tailscale', s.tail + ':8765']] : []), ['Platform', s.plat]])}`, K.btn('Check the connection', { k: 'ghost', a: 'dlg:diag' }) + K.btn('Refresh', { i: 'refresh' })); };
K.DLG.share = () => EM ? noPc('Share a file') : dl('Share north-bed-after.jpg', `<p>Anyone with the link can open this file in a browser, without signing in, until it ends.</p>${K.seg(['15 minutes', '1 hour', '24 hours'], '1 hour')}<p class="k-sub" style="margin-top:12px">${I('link', 14)} Link ready · works where studio-pc can be reached</p>${B.tech([['Link', 'https://192.168.1.20:8765/s/Qe7hW2rN']])}`, K.btn('Turn off', { k: 'ghost' }) + K.btn('Copy link', { i: 'copy', k: 'pri', a: 'toast:Link copied' }));
K.DLG.props = () => dl('About north-bed-after.jpg', `<dl class="k-kv cols"><dt>Size</dt><dd>3.8 MB</dd><dt>Changed</dt><dd>1 Nov 2026, 09:12</dd><dt>Opened</dt><dd>1 Nov 2026, 10:20</dd><dt>Where</dt><dd>studio-pc › Projects › garden-plan</dd></dl>${B.tech([['Full path', '/home/sam/Projects/garden-plan/north-bed-after.jpg'], ['Bytes', '3,984,211'], ['Permissions', '644 · rw-r--r--']], K.btn('File fingerprint…', { k: 'sm ghost', a: 'dlg:checksum' }))}`, K.btn('Copy where it is', { i: 'copy', k: 'ghost' }) + K.btn('Done', { k: 'pri', a: 'close' }));
K.DLG.checksum = () => dl('Is it the same file?', `<p>Paste the code the sender published. RFE tells you whether this file matches it exactly.</p><label class="b-fl"><small>Code from the sender</small><span class="b-f">${I('check', 16)}<input placeholder="Paste it here" aria-label="Code from the sender"></span></label>${B.tech([['Kind', 'SHA-256'], ['This file', '6c1f 92ad 07e4 b51c 3f88 2a90 d6e1 77c4 0b5e 9a13 f2c7 64d8 e019 5b3a 81fe a09e']])}`, K.btn('Done', { a: 'close' }));
K.DLG.launch = () => dl('Open GIMP on studio-pc?', '<p>It opens on studio-pc’s own screen, not on this one. studio-pc notes that you opened it.</p>', K.btn('Cancel', { a: 'close' }) + K.btn('Open', { k: 'pri', a: 'toast:Asked studio-pc to open GIMP. It can take a moment to appear on its screen.' }));
K.DLG.agentlog = () => dl('studio-pc’s log', `<p>What RFE on studio-pc wrote down lately. You only need it when someone helping you asks for it.</p>${B.tech([['Lines', String(D.agentlog.length)]], `<pre class="k-log mono">${D.agentlog.map(K.esc).join('\n')}</pre>`)}`, K.btn('Copy for support', { i: 'copy', k: 'ghost' }) + K.btn('Done', { k: 'pri', a: 'close' }), 680);
K.DLG.pins = () => dl('Computers you trust', `<p>RFE recognises each of these computers and warns you if one ever looks different.</p><div class="k-list">${D.servers.map((s) => `<div class="k-lrow">${K.mach(s, 20)}<span><b>${s.name}</b><small>${s.dot === 'bad' ? 'looks different from last time' : 'recognised'}</small></span></div>`).join('')}</div>${B.tech(D.servers.map((s) => [s.name, s.addr + ' · ' + s.tag + ' …']))}`, K.btn('Done', { a: 'close' }));
K.DLG.updates = () => dl('Check for updates', `<div class="k-lrow">${I('download')}<span><b>RFE 1.1.1</b><small>A new version · 96 MB</small></span></div><p class="k-sub">RFE checks that the download is complete and genuine before it keeps it.</p>`, K.btn('Later', { a: 'close' }) + K.btn('Download', { k: 'pri', a: 'close' }));
K.DLG.welcome = () => dl('Welcome to RFE', `<div class="k-wel">${[['shield', 'Your computers, recognised', 'RFE remembers each computer and warns you if one ever looks different.'], ['phone', 'You let phones in', 'Nothing joins until you see the same number on both screens.'], ['transfer', 'Transfers that resume', 'Pause, lose the network, come back: files continue where they stopped.']].map((x) => `<div>${I(x[0], 24)}<b>${x[1]}</b><small>${x[2]}</small></div>`).join('')}</div>`, K.btn('Skip', { a: 'close' }) + K.btn('Add a computer', { k: 'pri', a: 'dlg:newconn' }), 640);
K.DLG.backup = () => dl('Back up settings', `<label class="b-fl"><small>Password</small><span class="b-f"><input type="password" value="••••••••••" aria-label="Password"></span></label><label class="b-fl"><small>Repeat password</small><span class="b-f"><input type="password" value="••••••••••" aria-label="Repeat password"></span></label><p class="k-sub">The file is locked with this password. It holds no passwords or keys of your computers.</p>`, K.btn('Cancel', { a: 'close' }) + K.btn('Save backup…', { k: 'pri', a: 'close' }));

K.DLG.recent = () => EM ? noPc('Changed lately') : dl('Changed lately on studio-pc', `<div class="k-list">${D.recent.filter((r) => r[1] === 'studio-pc').map((r) => `<div class="k-lrow" data-k="go:files">${K.fic(/\.md$/.test(r[0]) ? 'txt' : 'img')}<span><b>${r[0]}</b><small class="mut">in ${r[2]}</small></span><span class="t2">${r[3]}</span></div>`).join('')}</div><p class="k-sub" style="margin-top:12px">Only folders this login can open are looked at.</p>`, K.btn('Close', { k: 'pri', a: 'close' }));
K.DLG.devkey = () => dl('Make this computer a new device?', '<p>The next time you sign in, studio-pc and the others see this computer as a new device. Saved sign-ins here are removed. The old device stays on each computer until you remove it there.</p>', K.btn('Cancel', { a: 'close' }) + K.btn('Make a new device', { k: 'warn', a: 'toast:A new device key will be made at your next sign-in' }));
K.DLG.resume = () => dl('Carry on with unfinished transfers?', '<p>RFE was closed while 2 transfers were still going. A download continues from what it already has. An upload sends only what is missing.</p>', K.btn('Not now', { a: 'close' }) + K.btn('Carry on', { k: 'pri', a: 'toast:2 transfers are back in the queue' }));
K.DLG.cancelx = () => dl('Stop this transfer?', '<p>walkthrough-full.mov is 71% sent. What was already sent is kept for a while, so starting it again carries on from there.</p>', K.btn('Keep going', { a: 'close' }) + K.btn('Stop it', { k: 'warn', a: 'toast:Stopped walkthrough-full.mov' }));
K.DLG.clearhist = () => dl('Clear the history?', '<p>This removes every entry from this list. Files and computers are not touched. Each computer keeps its own record.</p>', K.btn('Cancel', { a: 'close' }) + K.btn('Clear it', { k: 'warn', a: 'toast:History cleared' }));
const seg2 = (o, c) => K.seg(o, c);
const srow = (t, sub, c) => `<div class="k-set"><div><b>${t}</b>${sub ? `<small>${sub}</small>` : ''}</div><div>${c}</div></div>`;
K.DLG.srvset = () => { if (EM) return noPc('Settings for a computer'); const s = K.S(B.pick.servers); return dl(`Settings for ${s.name}`, `<div class="b-ss">
<h3>This computer here</h3>${srow('Name', 'Only changes how it is called in this app', `<span class="b-f" style="width:200px"><input value="${s.name}" aria-label="Name"></span>`)}${srow('Kind of device', 'Hides the options that do not fit it', seg2(['PC', 'Laptop', 'Server', 'Mobile'], 'PC'))}${srow('Connect when RFE starts', '', K.sw(1))}${srow('Show a notice when it goes offline', '', K.sw(0))}
<h3>Files</h3>${srow('Downloads from it go to', '', `<span class="b-path">~/Downloads/RFE/${s.name}</span>${K.btn('Change…', { k: 'sm' })}`)}${srow('Show hidden files', 'Files whose names start with a dot', K.sw(0))}${srow('Ask before replacing a file', '', K.sw(1))}
<h3>Speed</h3>${srow('Use at most', 'Leaves room for other things on a slow connection', seg2(['No limit', '10 MB/s', '2 MB/s'], 'No limit'))}${srow('Transfers at the same time', '', seg2(['1', '2', '4'], '2'))}
<h3>Waking it up</h3>${srow('Wake it when it is asleep', 'Works on the home network if the PC allows it', K.sw(s.id === 'studio' ? 1 : 0))}
<h3>Signed in</h3>${srow('As sam', 'With the account password', K.btn('Sign out', { k: 'sm' }))}
</div>${B.tech([['Address', s.addr], ['Prefer', s.route === 'LAN' ? 'Home network, then Tailscale' : s.route], ['Key', s.tag + ' …']])}`, K.btn('Done', { k: 'pri', a: 'close' }), 600); };

/* ---------- search menu: one per design ---------- */
let srEl;
B.srOpen = () => {
  if (!B.design.search) return;
  if (!srEl) { srEl = document.createElement('div'); srEl.className = 'b-sr'; document.querySelector('.win').appendChild(srEl); }
  srEl.innerHTML = B.design.search(); srEl.classList.add('open'); document.querySelector('.win').classList.add('sr-open');
  const i = srEl.querySelector('input'); if (i) { i.focus(); i.setSelectionRange(i.value.length, i.value.length); }
};
B.srClose = () => { if (srEl) srEl.classList.remove('open'); document.querySelector('.win').classList.remove('sr-open'); };

/* ---------- mount ---------- */
B.mount = (o) => {
  if (window.MK_VIEWER) return;
  B.design = o;
  const side = document.getElementById('side'), rail = document.getElementById('rail');
  /* Collapse the sidebar to icons; the choice is kept on the .rfe frame, so it survives the sidebar being redrawn. */
  /* Theme: System, Light or Dark. The Theme controls in Settings and the account menu change the whole window; ?theme=dark starts dark (the viewer's Dark mode button uses it). */
  const win = document.querySelector('.win'), THEMES = { System: 'system', Light: 'light', Dark: 'dark' };
  let theme = 'System';
  try { theme = { dark: 'Dark', light: 'Light' }[localStorage.getItem('rfe.theme')] || 'System'; } catch (e) {}
  const q = new URLSearchParams(location.search); if (q.get('theme')) theme = q.get('theme') === 'dark' ? 'Dark' : 'Light'; else if (/theme=dark/.test(location.hash)) theme = 'Dark';
  const sysDark = () => !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
  const paintTheme = () => { const dark = theme === 'Dark' || (theme === 'System' && sysDark()); win.dataset.theme = dark ? 'dark' : 'light'; document.querySelectorAll('.k-seg').forEach((sg) => { const bs = [...sg.querySelectorAll('button')]; if (bs.map((x) => x.textContent).join() === 'System,Light,Dark') bs.forEach((x) => x.classList.toggle('on', x.textContent === theme)); }); };
  paintTheme();
  new MutationObserver(paintTheme).observe(win, { childList: true, subtree: true });
  document.addEventListener('click', (e) => { const bt = e.target.closest('.k-seg button'); if (!bt || !THEMES[bt.textContent] || [...bt.parentNode.querySelectorAll('button')].map((x) => x.textContent).join() !== 'System,Light,Dark') return; theme = bt.textContent; try { localStorage.setItem('rfe.theme', THEMES[theme]); } catch (x) {} paintTheme(); }, true);
  const frame = side.parentElement;
  try { if (localStorage.getItem('rfe.sbmin') === '1' && !/still/.test(location.hash)) frame.classList.add('sbmin'); } catch (e) {}
  if (/sbmin/.test(location.hash)) frame.classList.add('sbmin');
  document.addEventListener('click', (e) => { if (!e.target.closest('.sb-t')) return; const on = frame.classList.toggle('sbmin'); document.querySelectorAll('.sb-t').forEach((b) => { b.setAttribute('aria-label', on ? 'Expand navigation' : 'Collapse navigation'); b.title = (on ? 'Expand navigation' : 'Collapse navigation') + ' (Ctrl+\\)'; }); try { localStorage.setItem('rfe.sbmin', on ? '1' : '0'); } catch (x) {} });
  document.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && e.key === '\\') { e.preventDefault(); const b = document.querySelector('.sb-t'); if (b) b.click(); } });
  const views = Object.assign({ home: B.home, files: B.outline, servers: LIST.servers, devices: LIST.devices, transfers: LIST.transfers, history: B.history }, o.views || {});
  const goK = K.go; K.go = (v) => (v === 'search' ? B.srOpen() : (B.srClose(), goK(v)));
  const dlgK = K.openDlg; K.openDlg = (nm, a) => (nm === 'palette' ? B.srOpen() : (B.srClose(), dlgK(nm, a)));
  /* the search menu's own listeners come first, so a fault later in the mount cannot leave it stuck open */
  document.querySelector('.win').addEventListener('click', (e) => {
    if (!srEl || !srEl.classList.contains('open')) return;
    const t = e.target.closest('[data-k]');
    if ((e.target.closest('.b-sr') && e.target !== srEl) || e.target.closest('.k-menu') || (t && /^(go:search|dlg:palette|sr:|menu:sfilt)/.test(t.dataset.k))) return;
    B.srClose();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') B.srClose();
    else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k' && e.target.closest && e.target.closest('input,textarea,[contenteditable]')) { e.preventDefault(); B.srOpen(); }
  });
  /* History filters: each one narrows the list */
  const hf = (e) => { const f = e.target.closest && e.target.closest('[data-hf]'); if (!f || !f.closest('#st')) return; HF[f.dataset.hf] = f.value; const pos = f.selectionStart; K.render();
    if (f.tagName === 'INPUT') { const n = document.querySelector('#st [data-hf="q"]'); if (n) { n.focus(); n.setSelectionRange(pos, pos); } } };
  document.addEventListener('change', (e) => { if (e.target.tagName === 'SELECT') hf(e); if (o.change) o.change(e); });
  document.addEventListener('input', (e) => { if (e.target.tagName === 'INPUT') hf(e); if (o.change) o.change(e); });
  K.mount({ stage: document.getElementById('st'), state: Object.assign({ view: 'home' }, o.state || {}), views,
    on: (a, b, t) => {
      if (a === 'pick') { const p = t.dataset.k.split(':'); pick[p[1]] = p[1] === 'history' ? +p[2] : p.slice(2).join(':'); if (K.state.view !== p[1]) K.go(p[1]); else K.render(); }
      else if (a === 'sr') { if (b === 'close') B.srClose(); else if (b === 'go') { B.srClose(); goK(t.dataset.k.split(':')[2]); } else if (t.dataset.pc) { const off = B.S.off; off.has(t.dataset.pc) ? off.delete(t.dataset.pc) : off.add(t.dataset.pc); B.srOpen(); } else { const g = t.parentElement; if (t.dataset.multi === undefined) g.querySelectorAll('.on').forEach((x) => x !== t && x.classList.remove('on')); t.classList.toggle('on'); } }
      else if (o.on) o.on(a, b, t);
    },
    onGo: (v) => { side.innerHTML = B.side(v === 'devices' ? 'servers' : v); rail.innerHTML = B.rail(v); if (v === 'devices') { const c = document.querySelector('.win .stage .cdv'), sg = document.querySelector('.win .stage'); if (c) c.open = true; /* the shipped window clamps the scroll so the last computer row stays visible above the card */ if (c && sg) sg.scrollTop += c.getBoundingClientRect().top - sg.getBoundingClientRect().top - 92; } if (o.onGo) o.onGo(v); } });
  if (new URLSearchParams(location.hash.slice(1)).has('sr')) B.srOpen();
};
B.sk = (k) => `<kbd class="b-kbd">${k}</kbd>`;
})();
