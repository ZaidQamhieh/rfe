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

/* ---------- data ---------- */
B.HIST = [
  { d: 'Today', t: '10:46', cat: 'Devices', em: 'mobile_phone', tx: 'Pixel 9 asked to pair with studio-pc', pc: 'studio-pc', dev: 'Pixel 9', out: 'Waiting', src: 'Agent audit', ip: '192.168.1.40', more: 'Match code 4829 1307 · expires 10:48' },
  { d: 'Today', t: '10:44', cat: 'Transfers', em: 'outbox_tray', tx: 'Uploading walkthrough-full.mov to studio-pc', pc: 'studio-pc', dev: 'desk-linux', out: 'Running', src: 'This app', more: '4.1 GB · 71% · /home/sam/Projects/garden-plan' },
  { d: 'Today', t: '10:41', cat: 'Transfers', em: 'inbox_tray', tx: 'Downloaded north-bed-before.jpg, checksum verified', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'This app', more: '4.1 MB · ~/Downloads/RFE Desktop' },
  { d: 'Today', t: '10:02', cat: 'Transfers', em: 'warning', tx: 'bed-layout.pdf waits: the name exists on work-laptop', pc: 'work-laptop', dev: 'desk-linux', out: 'Waiting', src: 'This app', more: 'Upload to /Users/sam/Documents' },
  { d: 'Today', t: '09:01', cat: 'Security', em: 'locked', tx: 'Signed in to studio-pc as sam', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'Agent audit', ip: '192.168.1.12', more: 'Account sign-in, proof v2' },
  { d: 'Today', t: '08:58', cat: 'Connections', em: 'antenna_bars', tx: 'Connected to studio-pc over LAN · 12 ms', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'This app' },
  { d: 'Today', t: '08:58', cat: 'Security', em: 'key', tx: 'nas presented a different key than the one you trusted', pc: 'nas', dev: 'desk-linux', out: 'Failed', src: 'This app', more: 'Pinned 41DE 0B93 · RFE will not connect until you compare' },
  { d: 'Today', t: '08:14', cat: 'Connections', em: 'antenna_bars', tx: 'media-mini stopped answering; its transfers are paused', pc: 'media-mini', dev: 'desk-linux', out: 'Failed', src: 'This app' },
  { d: 'Today', t: '07:55', cat: 'Security', em: 'locked', tx: 'Failed sign-in for account "admin"', pc: 'studio-pc', dev: 'unknown', out: 'Failed', src: 'Agent audit', ip: '10.0.0.9', more: 'Wrong password · 3rd try in 10 min' },
  { d: 'Yesterday', t: '18:20', cat: 'Shares', em: 'link', tx: 'Share link made for north-bed-before.jpg, 24 hours', pc: 'studio-pc', dev: 'Pixel 8', out: 'Succeeded', src: 'Agent audit' },
  { d: 'Yesterday', t: '16:03', cat: 'Devices', em: 'mobile_phone', tx: 'old-phone was revoked', pc: 'studio-pc', dev: 'desk-linux', out: 'Succeeded', src: 'Agent audit' },
  { d: 'Yesterday', t: '09:30', cat: 'Connections', em: 'globe_with_meridians', tx: 'Agent restarted on studio-pc, version 1.3.0', pc: 'studio-pc', dev: '—', out: 'Succeeded', src: 'Agent audit' },
];
const res = D.search.res.map((r) => ({ n: r[0], k: r[1], path: r[2], pc: r[3], date: r[4], size: r[5] }));
B.S = { q: 'bed', res, recent: ['invoice 2025', 'walkthrough', 'thesis draft'], saved: D.search.saved, line: '6 results · 48,210 items on studio-pc and work-laptop · media-mini skipped (offline)',
  types: [['Images', 'img', 2], ['Documents', 'doc', 2], ['Folders', 'dir', 1], ['Text', 'txt', 1]], pcs: [['studio-pc', 4], ['work-laptop', 2], ['media-mini', 0, 'offline'], ['nas', 0, 'key changed']] };
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
B.side = (v) => `<div class="lg">${I('key', 16)}RFE</div><button class="k-navi${v === 'home' ? ' on' : ''}" data-k="go:home">${I('grid', 18)}<span>Overview</span></button>${K.C.nav(v)}<h4>Computers</h4>${D.servers.map((s) => `<button class="pc" data-k="${s.dot === 'ok' ? 'go:files' : s.dot === 'bad' ? 'dlg:keychanged' : 'pick:servers:' + s.id}">${K.dot(s.dot)}${s.name}<small>${s.ms ? s.ms + ' ms' : s.dot === 'bad' ? 'key' : 'off'}</small></button>`).join('')}<button class="pc" data-k="dlg:newconn">${I('plus', 14)}Add a computer</button><span class="gap"></span>`;

/* ---------- Files (v2 11) ---------- */
const ia = (a) => `<span class="ia">${a.map((x) => K.ib(...x)).join('')}</span>`;
const fA = ia([['download', 'Download (Ctrl D)', 'toast:Downloading to ~/Downloads/RFE Desktop'], ['link', 'Share link…', 'dlg:share'], ['more', 'Rename, move, delete and more', 'menu:file']]);
const n = (d, tw, l, m, cls = '', act = '', row = '') => `<div class="n ${cls}" style="padding-left:${8 + d * 24}px" ${row ? `data-row="${row}"` : ''}><span class="l"><span class="tw">${tw ? I(tw === 'o' ? 'chevd' : 'chevr', 14) : ''}</span>${l}</span><span class="m">${m}</span>${act}</div>`;
function outlineFull() {
  let h = '';
  h += n(0, 'o', `${K.dot('ok')}${E('server', 18)}studio-pc ${K.tag('studio')}<small>Fedora 41 · 12 ms LAN</small>`, '<span>812 GB of 2 TB</span>', 'pc', ia([['upload', 'Upload to studio-pc', 'dlg:upload'], ['more', 'Status, diagnostics, roots', 'menu:server']]));
  h += n(1, 'o', `${E('home', 16)}/home/sam`, '<span>812 GB used</span>');
  h += n(2, 'c', `${K.fic('dir')}Documents`, '<span>38 items</span>');
  h += n(2, 'o', `${K.fic('dir')}Projects`, '<span>9 items</span>');
  h += n(3, 'o', `${K.fic('dir')}garden-plan`, '<span>11 items</span>', '', ia([['upload', 'Upload into garden-plan', 'dlg:upload'], ['folder-plus', 'New folder (Ctrl+Shift+N)', 'dlg:newfolder']]));
  D.files.forEach((f) => {
    h += n(4, f.k === 'dir' ? 'c' : '', `${K.fic(f.k)}${f.n}${f.k === 'dir' ? `<small>${f.c} items</small>` : ''}`, `<span>${f.s || ''}</span><span>${f.m}</span>`, f.n === D.sel ? 'sel' : '', f.k === 'dir' ? ia([['download', 'Download as .zip'], ['more', 'More', 'menu:file']]) : fA, f.n);
    if (f.n === 'walkthrough.mp4') h += n(4, '', `${K.fic('vid')}walkthrough-full.mov <small>uploading</small>`, `${K.bar(71)}<b class="mono" style="color:var(--t2)">71%</b>`, '', ia([['pause', 'Pause'], ['x', 'Cancel']]));
  });
  h += n(1, 'c', `${E('drive', 16)}/srv/media`, '<span>1.1 TB used</span>') + n(1, 'c', `${E('wastebasket', 16)}Trash`, '<span>5 items</span>', '', ia([['trash', 'Empty Trash', 'dlg:emptytrash']])) + n(1, 'c', `${E('link', 16)}Share links`, '<span>2 live</span>') + n(1, 'c', `${E('toolbox', 16)}Apps`, '<span>6</span>');
  h += n(0, 'c', `${K.dot('ok')}${E('laptop', 18)}work-laptop ${K.tag('work')}<small>Windows 11 · 48 ms Tailscale</small>`, '<span>402 GB of 512 GB</span>', 'pc', ia([['upload', 'Upload here', 'dlg:upload'], ['more', 'More', 'menu:server']]));
  h += n(0, '', `${K.dot('bad')}${E('server', 18)}nas ${K.tag('nas')}<small>key changed</small>`, '<span class="bad">Not trusted</span>', 'pc', `<span style="display:flex">${K.btn('Compare keys…', { k: 'sm', a: 'dlg:keychanged' })}</span>`);
  h += n(0, '', `${K.dot('off')}${E('server', 18)}media-mini ${K.tag('mini')}<small>offline · last seen 08:14</small>`, '<span>1 transfer paused</span>', 'pc', `<span style="display:flex">${K.btn('Wake computer', { k: 'sm', i: 'power' })}</span>`);
  h += n(0, '', `${I('plus', 14)}Add a computer`, '', 'pc" data-k="dlg:newconn" style="cursor:pointer;color:var(--t2)');
  const f = D.files.find((x) => x.n === D.sel);
  return `<div class="ol"><div class="tr">${h}</div><aside class="olside"><div data-k="dlg:preview" style="cursor:zoom-in">${K.photo(2, 280, 200, 8)}</div><h3>${E('img', 16)}${f.n}</h3><p class="k-sub">3.8 MB · 4032 × 3024 · ${f.m}</p><dl class="k-kv" style="margin-top:12px"><dt>On</dt><dd>studio-pc</dd><dt>Path</dt><dd class="mono">…/Projects/garden-plan</dd><dt>Mode</dt><dd class="mono">rw-r--r--</dd></dl><div class="k-dact" style="margin-top:12px">${K.btn('Download', { i: 'download', k: 'pri', kc: 'Ctrl D' })}<div class="k-row">${K.btn('Share link…', { i: 'link', a: 'dlg:share' })}${K.ib('more', 'Offline, properties, checksum, copy path', 'menu:detmore')}</div></div></aside></div>`;
}
B.outline = () => EM ? `<div class="ol"><div class="tr"><div class="b-empty left">${E('open_file_folder', 40)}<b>Your computers’ files appear here as one tree</b><p>Each computer you add becomes a branch: its folders, and the files in them, with the actions on each row.</p><div class="k-row">${add}</div></div></div><aside class="olside"><h3>Set up a PC</h3><ol class="b-steps"><li>Install RFE on the PC (the .deb, .rpm, or the Windows installer).</li><li>On the PC, run <code>rfe setup</code>. It makes the RFE Files folder and shows a pairing code.</li><li>Here, choose <b>Add a computer</b> and type the code, or pick the PC from the network.</li><li>Compare the key RFE shows with <code>rfe status</code> on the PC.</li></ol></aside></div>` : outlineFull();

/* ---------- Overview (v3 2) ---------- */
const up = [2, 3, 2, 6, 14, 22, 18, 30, 41, 38, 47, 52, 49, 53, 52, 51], dn = [1, 1, 4, 9, 6, 4, 12, 8, 6, 11, 9, 7, 14, 10, 6, 8];
const X = (i) => 40 + i * (800 / 15), Y = (v) => 200 - v * 3, line = (a) => a.map((v, i) => `${i ? 'L' : 'M'}${X(i).toFixed(1)} ${Y(v)}`).join(' ');
const grid = `${[0, 20, 40, 60].map((v) => `<path d="M40 ${Y(v)}H840" stroke="var(--line)"/><text x="0" y="${Y(v) + 4}">${v}</text>`).join('')}${['08:00', '09:00', '10:00', '11:00'].map((t, i) => `<text x="${40 + i * 266 - 16}" y="226">${t}</text>`).join('')}`;
B.home = () => {
  const stats = EM ? [['Moving now', '—', 'nothing to move yet'], ['Moved today', '0 B', 'since RFE started'], ['Computers up', '0', 'add one to start'], ['Devices', '0', 'pair a phone after']] : [['Moving now', '52.4 MB/s', '2 transfers · 23 s left'], ['Moved today', '1.9 GB', '+640 MB vs yesterday'], ['Computers up', '2 of 4', 'nas key changed · media-mini off'], ['Devices', '4 allowed', '2 asking to pair']];
  return `<div class="db">${stats.map((s) => `<div class="stat"><small>${s[0]}</small><b>${s[1]}</b><span>${s[2]}</span></div>`).join('')}
<div class="chart"><h2>Throughput today <small>MB/s · upload and download</small></h2><svg viewBox="0 0 860 232">${grid}${EM ? '' : `<path d="${line(dn)}" fill="none" stroke="var(--t3)" stroke-width="1.5"/><path d="${line(up)}" fill="none" stroke="var(--ac)" stroke-width="2"/>`}</svg>${EM ? '<div class="ov">The line starts with your first transfer</div>' : ''}</div>
<div class="card"><h2>Waiting for you ${EM ? '' : '<em class="k-badge bad">4</em>'}</h2>${EM ? '<div class="nil">Nothing waits for you.<small>Pairing requests, name conflicts and key changes come here.</small></div>' : [['phone', 'Pixel 9 asks to join studio-pc', 'Match 4829 1307 · 1:42 left', K.btn('Accept…', { k: 'sm pri', a: 'dlg:accept' })], ['laptop', 'sam-thinkpad wants work-laptop’s place', 'Match 0316 8842', K.btn('Review…', { k: 'sm', a: 'dlg:accept' })], ['doc', 'bed-layout.pdf exists on work-laptop', 'Upload to /Users/sam/Documents', K.btn('Keep both', { k: 'sm' })], ['warning', 'notes.txt was damaged while uploading', 'Discarded; try again', K.btn('Retry', { k: 'sm', i: 'refresh' })]].map((x) => `<div class="need">${E(x[0], 20)}<div>${x[1]}<small>${x[2]}</small></div>${x[3]}</div>`).join('')}</div>
<div class="card w"><h2>Computers</h2>${EM ? `<div class="nil">No computers yet.${K.btn('Add a computer', { i: 'plus', k: 'sm pri', a: 'dlg:newconn' })}</div>` : D.servers.map((s) => `<div class="pcr" data-k="pick:servers:${s.id}">${K.dot(s.dot)}<span>${K.mach(s, 16)}${s.name} <small>${s.tag}</small></span>${K.bar(Math.round(s.used / s.total * 100))}<small>${Math.round(s.used / s.total * 100)}% full</small></div>`).join('')}</div>
<div class="card w"><h2>Recent files</h2>${EM ? '<div class="nil">Files you open or move show here.</div>' : `<div class="rc">${[[2, 'north-bed-after.jpg', 'studio-pc · 12 min ago'], [1, 'north-bed-before.jpg', 'downloaded'], [3, 'garden-sketch.png', 'this computer']].map((r) => `<div data-k="dlg:preview" style="cursor:pointer">${K.photo(r[0], 180, 100, 8)}<b>${r[1]}</b>${r[2]}</div>`).join('')}</div>`}</div></div>`;
};

/* ---------- Servers, Devices, Transfers: a list and an inspector (v3 4) ---------- */
const pick = { servers: 'studio', devices: 'Pixel 8', transfers: 'walkthrough-full.mov', history: 4 };
B.pick = pick;
const ro = (page, id, icon, t, s, st) => `<div class="ro${String(pick[page]) === String(id) ? ' sel' : ''}" data-k="pick:${page}:${id}">${icon}<span><b>${t}</b><small>${s}</small></span><span class="st">${st}</span></div>`;
const allX = () => [...D.xfer.attention, ...D.xfer.active, ...D.xfer.queued, ...D.xfer.done];
const OUT = { Succeeded: 'ok', Failed: 'bad', Waiting: 'warn', Running: '' };
const INSP = {
  servers: () => { const s = K.S(pick.servers); return `${K.mach(s, 40)}<h2>${s.name}</h2><p class="k-sub">${K.dot(s.dot)} ${s.state} · ${s.route}</p><dl class="k-kv"><dt>Address</dt><dd class="mono">${s.addr}</dd><dt>System</dt><dd>${s.sys}</dd><dt>Agent</dt><dd>${s.ver}</dd><dt>Key</dt><dd>${K.tag(s.id)}</dd><dt>Disk</dt><dd>${s.used} GB of ${s.total} GB${K.bar(s.used / s.total * 100)}</dd></dl><div class="k-dact">${s.state === 'Connected' ? K.btn('Open files', { i: 'folder', k: 'pri', a: 'go:files' }) : s.state === 'Key changed' ? K.btn('Review key…', { k: 'warn', a: 'dlg:keychanged' }) : K.btn('Wake computer', { i: 'power', k: 'pri' })}${K.btn('Status…', { i: 'activity', a: 'dlg:status' })}${K.btn('Run diagnostics', { i: 'radar', a: 'dlg:diag' })}${K.btn('Allowed roots…', { i: 'drive', a: 'dlg:roots' })}${K.btn('Forget…', { k: 'ghost', a: 'dlg:forget' })}</div>`; },
  devices: () => { const d = D.devices.find((x) => x.n === pick.devices); return `${E(d.kind, 40)}<h2>${d.n}</h2><p class="k-sub">${d.how} · seen ${d.seen}</p><div class="k-perms">${[['Browse', 1], ['Download', 1], ['Upload', 1], ['Change', 0], ['Delete', 0], ['Share links', 1], ['See apps', 0], ['Start apps', 0], ['Read-only', 0]].map((x) => `<div class="k-set"><div><b>${x[0]}</b></div>${K.sw(x[1])}</div>`).join('')}</div><dl class="k-kv" style="margin-top:12px"><dt>Folder limit</dt><dd class="mono">${d.lim || 'none'}</dd></dl><div class="k-dact">${K.btn('Activity', { i: 'history', a: 'go:history' })}${K.btn('Revoke access…', { k: 'warn', a: 'dlg:revoke' })}</div>`; },
  transfers: () => `${E('outbox_tray', 40)}<h2>walkthrough-full.mov</h2><p class="k-sub">Upload to studio-pc · /home/sam/Projects/garden-plan</p>${K.bar(71)}<dl class="k-kv" style="margin-top:12px"><dt>Done</dt><dd>2.9 of 4.1 GB · 71%</dd><dt>Speed</dt><dd>52.4 MB/s · 23 s left</dd><dt>Checks</dt><dd>Each 4 MiB piece and the whole file, by SHA-256</dd></dl><div class="k-dact">${K.btn('Pause', { i: 'pause', k: 'pri' })}${K.btn('Show in Files', { i: 'folder', a: 'go:files' })}${K.btn('Cancel', { k: 'ghost' })}</div>`,
  history: () => { const h = B.HIST[pick.history] || B.HIST[0]; const acts = { Transfers: K.btn('Show in Files', { i: 'folder', k: 'pri', a: 'go:files' }), Devices: K.btn('Open in Devices', { i: 'phone', k: 'pri', a: 'go:devices' }), Shares: K.btn('Revoke link…', { i: 'link', k: 'pri' }), Connections: K.btn('Open in Servers', { i: 'server', k: 'pri', a: 'go:servers' }), Security: K.btn('Open in Servers', { i: 'server', k: 'pri', a: 'go:servers' }) }; return `${E(h.em, 40)}<h2>${h.cat}</h2><p class="k-sub"><span class="b-out ${OUT[h.out]}">${h.out}</span> · ${h.d}, ${h.t}</p><p style="margin:0 0 16px">${h.tx}</p><dl class="k-kv"><dt>Computer</dt><dd>${h.pc}</dd><dt>Device</dt><dd>${h.dev}</dd>${h.ip ? `<dt>From</dt><dd class="mono">${h.ip}</dd>` : ''}<dt>Recorded by</dt><dd>${h.src}</dd>${h.more ? `<dt>Details</dt><dd>${h.more}</dd>` : ''}</dl><div class="k-dact">${acts[h.cat] || ''}${K.btn('Copy details', { i: 'copy' })}${h.src === 'Agent audit' ? K.btn('Agent log', { i: 'text', k: 'ghost', a: 'dlg:agentlog' }) : ''}</div>`; },
};
const LIST = {
  servers: () => K.head('Servers', `${D.servers.length} saved`, K.btn('New connection', { i: 'plug', k: 'pri', a: 'dlg:newconn' })) + `<div class="li">${D.servers.map((s) => ro('servers', s.id, K.mach(s, 24), s.name, s.addr + ' · ' + s.route, K.dot(s.dot) + s.state)).join('')}</div>`,
  devices: () => K.head('Devices', 'Allowed on studio-pc', K.btn('Pair a phone', { i: 'qr', a: 'dlg:pairphone' })) + K.sec('Asking to join <span class="k-count">2</span>', '', K.V.requests()) + `<div class="li">${D.devices.map((d) => ro('devices', d.n, E(d.kind, 24), d.n, d.how + ' · ' + d.seen, d.revoked ? '<span class="bad">Revoked</span>' : d.lim || 'every folder')).join('')}</div>`,
  transfers: () => K.head('Transfers', '52.4 MB/s · 2 moving · 3 need you', K.btn('Pause all', { i: 'pause', k: 'sm' })) + `<div class="li">${allX().map((t) => ro('transfers', t.n, E(t.d === 'up' ? 'outbox_tray' : 'inbox_tray', 24), t.n, t.where, t.st ? `<span class="warn">${t.st}</span>` : t.pct + '%')).join('')}</div>`,
};
if (EM) {
  LIST.servers = () => K.head('Servers', 'None saved yet') + W('desktop_computer', 'No computers yet', 'Install RFE on a PC, run <code>rfe setup</code> there, then add it here by its address, from the network, or with a pairing code.', add);
  LIST.devices = () => K.head('Devices', 'Phones and computers allowed on a PC') + W('mobile_phone', 'No computer to pair with', 'Devices belong to a PC. Add a computer first; then pair phones with it here.', K.btn('Add a computer', { i: 'plus', k: 'pri', a: 'dlg:newconn' }));
  LIST.transfers = () => K.head('Transfers', 'Nothing moving') + W('updown_arrow', 'Nothing moving', 'Uploads and downloads appear here. They resume on their own after a lost connection or a restart.');
}

/* ---------- History: filters on every field an event has, and an inspector ---------- */
const CATS = [['All', ''], ['Transfers', 'updown_arrow'], ['Connections', 'antenna_bars'], ['Security', 'locked'], ['Devices', 'mobile_phone'], ['Shares', 'link']];
const sel = (label, opts) => `<label class="b-fs" title="${label}"><select class="k-sel" aria-label="${label}">${opts.map((o) => `<option>${o}</option>`).join('')}</select></label>`;
B.history = (st) => {
  const f = st.hist || 'All', rows = B.HIST.map((h, i) => [h, i]).filter(([h]) => f === 'All' || h.cat === f);
  const cnt = (c) => (c === 'All' ? B.HIST.length : B.HIST.filter((h) => h.cat === c).length);
  let day = '', list = '';
  rows.forEach(([h, i]) => { if (h.d !== day) { day = h.d; list += `<h3 class="b-day">${day}</h3>`; } list += `<div class="b-ev${pick.history === i ? ' sel' : ''}" data-k="pick:history:${i}"><time class="mono">${h.t}</time>${E(h.em, 18)}<span class="tx">${h.tx}<small>${h.pc} · ${h.dev}${h.ip ? ' · ' + h.ip : ''}</small></span><span class="b-out ${OUT[h.out]}">${h.out}</span></div>`; });
  return K.head('History', 'What this app did, and the agents’ audit log', K.btn('Agent log', { i: 'text', k: 'sm ghost', a: 'dlg:agentlog' }) + K.ib('download', 'Export as CSV') + K.ib('trash', 'Clear this app’s history')) +
    `<div class="b-filt"><label class="k-in sm b-fq">${I('filter', 14)}<input placeholder="Filter" aria-label="Filter by words, a file or an address"></label>${sel('Kind', ['Kind', ...CATS.slice(1).map((c) => c[0] + ' · ' + cnt(c[0])), 'Agent audit log only'])}${sel('When', ['Today', 'Yesterday', 'Last 7 days', 'Last 30 days', 'Everything', 'Pick dates…'])}${sel('Computer', ['Computer', 'studio-pc', 'work-laptop', 'nas', 'media-mini'])}${sel('Device', ['Device', 'desk-linux (this one)', 'Pixel 8', 'Pixel 9', 'Galaxy Tab S9', 'Unknown'])}${sel('Outcome', ['Outcome', 'Succeeded', 'Failed', 'Waiting', 'Running'])}</div>
<p class="b-fsum">${rows.length} of ${B.HIST.length} events${f !== 'All' ? ' · ' + f : ''} · newest first${f !== 'All' ? K.btn('Clear filters', { k: 'sm ghost', a: 'hist:All' }) : ''}</p><div class="b-hist">${list}</div>${EM ? '<p class="mut" style="margin-top:16px">Connections, transfers, sign-ins and pairing are recorded here as they happen.</p>' : ''}`;
};

/* ---------- the rail: an inspector for the selected row ---------- */
B.rail = (v) => {
  if (EM) return '';
  if (v === 'history' || (INSP[v] && LIST[v])) return INSP[v]();
  return '';
};

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
  const views = Object.assign({ home: B.home, files: B.outline, servers: LIST.servers, devices: LIST.devices, transfers: LIST.transfers, history: B.history }, o.views || {});
  const goK = K.go; K.go = (v) => (v === 'search' ? B.srOpen() : (B.srClose(), goK(v)));
  const dlgK = K.openDlg; K.openDlg = (nm, a) => (nm === 'palette' ? B.srOpen() : (B.srClose(), dlgK(nm, a)));
  K.mount({ stage: document.getElementById('st'), state: Object.assign({ view: 'home' }, o.state || {}), views,
    on: (a, b, t) => {
      if (a === 'pick') { const p = t.dataset.k.split(':'); pick[p[1]] = p[1] === 'history' ? +p[2] : p.slice(2).join(':'); if (K.state.view !== p[1]) K.go(p[1]); else K.render(); }
      else if (a === 'sr') { if (b === 'close') B.srClose(); else if (b === 'go') { B.srClose(); goK(t.dataset.k.split(':')[2]); } else { const g = t.parentElement; if (t.dataset.multi === undefined) g.querySelectorAll('.on').forEach((x) => x !== t && x.classList.remove('on')); t.classList.toggle('on'); } }
      else if (o.on) o.on(a, b, t);
    },
    onGo: (v) => { side.innerHTML = B.side(v); rail.innerHTML = B.rail(v); if (o.onGo) o.onGo(v); } });
  document.querySelector('.win').addEventListener('click', (e) => {
    if (!srEl || !srEl.classList.contains('open')) return;
    const t = e.target.closest('[data-k]');
    if ((e.target.closest('.b-sr') && e.target !== srEl) || (t && /^(go:search|dlg:palette|sr:)/.test(t.dataset.k))) return;
    B.srClose();
  });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') B.srClose(); });
  if (new URLSearchParams(location.hash.slice(1)).has('sr')) B.srOpen();
};
B.sk = (k) => `<kbd class="b-kbd">${k}</kbd>`;
})();
