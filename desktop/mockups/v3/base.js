/* v3 base: the three fixed parts every v3 design shares, unchanged between designs:
   - the left navigation of v2 design 12 (Overview, pages, Computers),
   - the file system of v2 design 11 (one outline tree across every computer, actions on the row),
   - the Settings page of v2 design 15 (the kit's sectioned Settings).
   Each page shows twice: full of data, and empty (first run, no computer added yet). */
(function () {
'use strict';
const K = window.K, I = K.I, D = K.D, B = {};
const q = new URLSearchParams(location.search);
B.empty = q.get('v') === 'empty';
B.framed = q.has('v');
window.B = B;

/* ---------- empty data: a first run, nothing added yet ---------- */
if (B.empty) {
  Object.assign(D, { servers: [], files: [], roots: [], requests: [], devices: [], events: [], history: [], favs: [], recent: [], offline: [], big: [], dupes: [], trash: [], shares: [], rules: [], apps: [], notes: [] });
  D.search = { q: '', saved: [], line: '', res: [] };
  D.xfer = { stats: [['0 B/s', 'current speed'], ['0', 'active'], ['0', 'queued'], ['0', 'need attention'], ['0', 'completed'], ['0 B', 'moved this session']], attention: [], active: [], queued: [], done: [] };
  K.C.navItems.forEach((n) => (n.length = 3));
  const a = K.C.actions; K.C.actions = () => a().replace('<i class="k-nb"></i>', '').replace('Notifications: 3 new', 'Notifications');
  const W = (icon, t, s, acts = '') => `<div class="b-empty">${K.E(icon, 40)}<b>${t}</b><p>${s}</p>${acts ? `<div class="k-row">${acts}</div>` : ''}</div>`;
  const add = K.btn('Add a computer', { i: 'plus', k: 'pri', a: 'dlg:newconn' }) + K.btn('Find on network', { i: 'radar', a: 'dlg:newconn:Find on network' });
  B.W = W; B.addActs = add;
  K.V.servers = () => K.head('Servers', 'None saved yet') + W('desktop_computer', 'No computers yet', 'Install RFE on a PC, run <code>rfe setup</code> there, then add it here by its address, from the network, or with a pairing code.', add);
  K.V.devices = () => K.head('Devices', 'Phones and computers allowed on a PC') + W('mobile_phone', 'No computer to pair with', 'Devices belong to a PC. Add a computer first; then pair phones with it here.', K.btn('Add a computer', { i: 'plus', k: 'pri', a: 'dlg:newconn' }));
  K.V.transfers = () => K.head('Transfers', 'Everything moving between this computer and your servers') + W('updown_arrow', 'Nothing moving', 'Uploads and downloads appear here. They resume on their own after a lost connection or a restart.');
  K.V.search = () => K.head('Search', '', `<label class="k-in">${I('search', 14)}<input placeholder="Search file names" aria-label="Search"></label>`) + W('magnifying_glass_tilted_left', 'Nothing to search yet', 'Search looks through the computers you add, and this computer’s Downloads folder.');
  const TE = { fav: ['star', 'No favorites', 'Star a folder in Files to keep it here.'], store: ['file_cabinet', 'No storage to show', 'Disk use appears once a computer is connected.'], trash: ['wastebasket', 'Trash is empty', 'Deleted files wait here before they go for good.'], share: ['link', 'No share links', 'Make one from a file: Share link…'], sync: ['counterclockwise_arrows_button', 'No sync rules', 'Pair a folder here with one on a computer.', K.btn('New rule', { i: 'plus', a: 'dlg:newrule' })], apps: ['toolbox', 'No apps to show', 'A connected computer lists the apps it lets you start.'] };
  Object.keys(TE).forEach((t) => (K.V.tool[t] = () => W(...TE[t])));
  K.V.history = () => K.head('History', 'This app’s activity and, under Security, the agent’s audit log') + W('scroll', 'Nothing has happened yet', 'Connections, transfers and sign-ins are recorded here as they happen.');
}

/* ---------- fixed part 1: navigation (v2 design 12) ---------- */
B.side = (v) => `<div class="lg">${I('key', 16)}RFE</div><button class="k-navi${v === 'home' ? ' on' : ''}" data-k="go:home">${I('grid', 18)}<span>Overview</span></button>${K.C.nav(v)}<h4>Computers</h4>${D.servers.map((s) => `<button class="pc" data-k="${s.dot === 'ok' ? 'go:files' : s.dot === 'bad' ? 'dlg:keychanged' : 'menu:server'}">${K.dot(s.dot)}${s.name}<small>${s.ms ? s.ms + ' ms' : s.dot === 'bad' ? 'key' : 'off'}</small></button>`).join('')}<button class="pc" data-k="dlg:newconn">${I('plus', 14)}Add a computer</button><span class="gap"></span>`;

/* ---------- fixed part 2: file system (v2 design 11) ---------- */
const ia=(a)=>`<span class="ia">${a.map(x=>K.ib(...x)).join('')}</span>`;
const fA=ia([['download','Download (Ctrl D)','toast:Downloading to ~/Downloads/RFE Desktop'],['link','Share link…','dlg:share'],['more','Rename, move, delete and more','menu:file']]);
const n=(d,tw,l,m,cls='',act='',row='')=>`<div class="n ${cls}" style="padding-left:${8+d*24}px" ${row?`data-row="${row}"`:''}><span class="l"><span class="tw">${tw?I(tw==='o'?'chevd':'chevr',14):''}</span>${l}</span><span class="m">${m}</span>${act}</div>`;
function outlineFull(){const s=D.servers;let h='';
 h+=n(0,'o',`${K.dot('ok')}${K.E('server',18)}studio-pc ${K.tag('studio')}<small>Fedora 41 · 12 ms LAN</small>`,`<span>812 GB of 2 TB</span>`,'pc',ia([['upload','Upload to studio-pc','dlg:upload'],['more','Status, diagnostics, roots','menu:server']]));
 h+=n(1,'',`${K.E('phone',16)}Pixel 9 asks to join · match <b class="mono">4829 1307</b>`,`<span>1:42 left</span>`,'note',`<span style="display:flex;gap:4px">${K.btn('Accept…',{k:'sm pri',a:'dlg:accept'})}${K.btn('Reject',{k:'sm ghost'})}</span>`);
 h+=n(1,'o',`${K.E('home',16)}/home/sam`,`<span>812 GB used</span>`);
 h+=n(2,'c',`${K.fic('dir')}Documents`,`<span>38 items</span>`);
 h+=n(2,'o',`${K.fic('dir')}Projects`,`<span>9 items</span>`);
 h+=n(3,'o',`${K.fic('dir')}garden-plan`,`<span>11 items</span>`,'',ia([['upload','Upload into garden-plan','dlg:upload'],['folder-plus','New folder (Ctrl+Shift+N)','dlg:newfolder']]));
 D.files.forEach(f=>{h+=n(4,f.k==='dir'?'c':'',`${K.fic(f.k)}${f.n}${f.k==='dir'?`<small>${f.c} items</small>`:''}`,`<span>${f.s||''}</span><span>${f.m}</span>`,f.n===D.sel?'sel':'',f.k==='dir'?ia([['download','Download as .zip'],['more','More','menu:file']]):fA,f.n);
  if(f.n==='walkthrough.mp4')h+=n(4,'',`${K.fic('vid')}walkthrough-full.mov <small>uploading</small>`,`${K.bar(71)}<b class="mono" style="color:var(--t2)">71%</b>`,'',ia([['pause','Pause'],['x','Cancel']]));});
 h+=n(1,'c',`${K.E('drive',16)}/srv/media`,`<span>1.1 TB used</span>`)+n(1,'c',`${K.E('wastebasket',16)}Trash`,`<span>5 items</span>`,'',ia([['trash','Empty Trash','dlg:emptytrash']]))+n(1,'c',`${K.E('link',16)}Share links`,`<span>2 live</span>`)+n(1,'c',`${K.E('toolbox',16)}Apps`,`<span>6</span>`);
 h+=n(0,'c',`${K.dot('ok')}${K.E('laptop',18)}work-laptop ${K.tag('work')}<small>Windows 11 · 48 ms Tailscale</small>`,`<span>402 GB of 512 GB</span>`,'pc',ia([['upload','Upload here','dlg:upload'],['more','More','menu:server']]));
 h+=n(1,'',`${K.E('laptop',16)}sam-thinkpad wants to take over work-laptop · match <b class="mono">0316 8842</b>`,`<span>1:09 left</span>`,'note',`<span style="display:flex;gap:4px">${K.btn('Review…',{k:'sm',a:'dlg:accept'})}</span>`);
 h+=n(0,'',`${K.dot('bad')}${K.E('server',18)}nas ${K.tag('nas')}<small>key changed</small>`,`<span class="bad">Not trusted</span>`,'pc',`<span style="display:flex">${K.btn('Compare keys…',{k:'sm',a:'dlg:keychanged'})}</span>`);
 h+=n(0,'',`${K.dot('off')}${K.E('server',18)}media-mini ${K.tag('mini')}<small>offline · last seen 08:14</small>`,`<span>1 transfer paused</span>`,'pc',`<span style="display:flex">${K.btn('Wake computer',{k:'sm',i:'power'})}</span>`);
 h+=n(0,'',`${I('plus',14)}Add a computer`,'','pc" data-k="dlg:newconn" style="cursor:pointer;color:var(--t2)');
 const f=D.files.find(x=>x.n===D.sel);
 return `<div class="ol"><div class="tr">${h}</div><aside class="olside"><div data-k="dlg:preview" style="cursor:zoom-in">${K.photo(2,280,200,8)}</div><h3>${K.E('img',16)}${f.n}</h3><p class="k-sub">3.8 MB · 4032 × 3024 · ${f.m}</p><dl class="k-kv" style="margin-top:12px"><dt>On</dt><dd>studio-pc</dd><dt>Path</dt><dd class="mono">…/Projects/garden-plan</dd><dt>Mode</dt><dd class="mono">rw-r--r--</dd></dl><div class="k-dact" style="margin-top:12px">${K.btn('Download',{i:'download',k:'pri',kc:'Ctrl D'})}<div class="k-row">${K.btn('Share link…',{i:'link',a:'dlg:share'})}${K.ib('more','Offline, properties, checksum, copy path','menu:detmore')}</div></div></aside></div>`;}
B.outline = () => B.empty ? `<div class="ol"><div class="tr"><div class="b-empty left">${K.E('open_file_folder', 40)}<b>Your computers’ files appear here as one tree</b><p>Each computer you add becomes a branch: its folders, and the files in them, with the actions on each row.</p><div class="k-row">${B.addActs}</div></div></div><aside class="olside"><h3>Set up a PC</h3><ol class="b-steps"><li>Install RFE on the PC (the .deb, .rpm, or the Windows installer).</li><li>On the PC, run <code>rfe setup</code>. It makes the RFE Files folder and shows a pairing code.</li><li>Here, choose <b>Add a computer</b> and type the code, or pick the PC from the network.</li><li>Compare the key RFE shows with <code>rfe status</code> on the PC.</li></ol></aside></div>` : outlineFull();

/* ---------- mount: a design supplies its own chrome and views; files and settings stay fixed ---------- */
B.mount = (o) => {
  if (window.MK_VIEWER) return;
  const side = document.getElementById('side');
  const views = Object.assign({}, o.views || {}, { files: B.outline, outline: B.outline });
  delete views.settings;
  K.mount({ stage: document.getElementById('st'), state: Object.assign({ view: 'home' }, o.state || {}), views, on: o.on,
    onGo: (v) => { side.innerHTML = B.side(v); if (o.onGo) o.onGo(v); } });
};
})();
