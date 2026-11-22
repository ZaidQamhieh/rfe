'use strict';
// Builds every phone screen of the mockup from small parts. Static data, no behaviour.

const P = {
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  more: '<circle cx="12" cy="5" r="1.1" fill="currentColor"/><circle cx="12" cy="12" r="1.1" fill="currentColor"/><circle cx="12" cy="19" r="1.1" fill="currentColor"/>',
  down: '<path d="m6 9 6 6 6-6"/>',
  right: '<path d="m9 6 6 6-6 6"/>',
  back: '<path d="M19 12H5M12 19l-7-7 7-7"/>',
  x: '<path d="M18 6 6 18M6 6l12 12"/>',
  check: '<path d="M20 6 9 17l-5-5"/>',
  folder: '<path d="M3 7.5V18a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-7l-2-3H5a2 2 0 0 0-2 2z"/>',
  home: '<path d="M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z"/>',
  drive: '<path d="M22 12H2M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><path d="M6 16h.01M10 16h.01"/>',
  phone: '<rect x="6" y="2.5" width="12" height="19" rx="2"/><path d="M11 18h2"/>',
  updown: '<path d="m21 16-4 4-4-4M17 20V4M3 8l4-4 4 4M7 4v16"/>',
  save: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>',
  up: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M17 8l-5-5-5 5M12 3v12"/>',
  link: '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
  move: '<path d="M2 9V5a2 2 0 0 1 2-2h3.93a2 2 0 0 1 1.66.9l.82 1.2a2 2 0 0 0 1.66.9H20a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H2"/><path d="M2 13h10M9 16l3-3-3-3"/>',
  trash: '<path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>',
  pause: '<path d="M9 5v14M15 5v14"/>',
  play: '<path d="M7 4.5v15l12-7.5z"/>',
  retry: '<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  monitor: '<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>',
  laptop: '<path d="M4 16V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v10M2 20h20"/>',
  power: '<path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.77.04"/>',
  grid: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  key: '<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6M15.5 7.5l3 3L22 7l-3-3"/>',
  open: '<path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>',
  pencil: '<path d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/>',
  image: '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.1-3.1a2 2 0 0 0-2.8 0L6 21"/>',
  inbox: '<path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>',
  apps: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M7 6.5h.01M10 6.5h.01"/>',
  sliders: '<path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 16v-4M12 8h.01"/>',
  wifi: '<path d="M5 12.55a11 11 0 0 1 14.08 0M1.42 9a16 16 0 0 1 21.16 0M8.53 16.11a6 6 0 0 1 6.95 0M12 20h.01"/>',
  qr: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 14h3v3h-3zM20 14v.01M14 20h.01M17 20h4v-3"/>',
};
const DUO = new Set(['search', 'folder', 'home', 'drive', 'phone', 'save', 'up', 'trash', 'pause', 'monitor', 'grid', 'key', 'image', 'inbox', 'apps', 'info', 'pencil', 'move', 'qr']);
// Things (files, folders, computers, phones, places) get Microsoft Fluent Emoji Flat, as on the desktop; actions keep line icons.
const EM = { folder: 'file_folder', openfolder: 'open_file_folder', home: 'house', drive: 'computer_disk', monitor: 'desktop_computer', laptop: 'laptop', phone: 'mobile_phone', image: 'framed_picture', trash: 'wastebasket', apps: 'toolbox', inbox: 'inbox_tray', pin: 'pushpin', lock: 'locked_with_key', info: 'information', gear: 'gear', link: 'link', key: 'key' };
const em = (n, s = 28, off = false) => `<img class="e${off ? ' off' : ''}" src="emoji/${EM[n] || n}.svg" width="${s}" height="${s}" alt="" draggable="false">`;
const ic = (n, s = 24, w = 1.5) => `<svg class="i${DUO.has(n) ? ' duo' : ''}" width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${w}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${P[n]}</svg>`;

// Photo placeholders: blurred colour fields standing in for real thumbnails.
const SCENES = {
  sea: ['#b8c6cd', [['rect', 0, 52, 100, 48, '#58778a'], ['rect', 0, 80, 100, 20, '#d6c6a8'], ['circle', 74, 26, 9, '#eae3d2']]],
  forest: ['#c5ccbd', [['rect', 0, 40, 100, 60, '#56643f'], ['circle', 22, 44, 22, '#3d4a2f'], ['circle', 70, 50, 26, '#46552f']]],
  room: ['#c69f78', [['rect', 0, 62, 100, 38, '#6b4f3a'], ['rect', 58, 12, 30, 34, '#efdcbf'], ['circle', 28, 58, 12, '#8e3f2c']]],
  night: ['#222a38', [['rect', 0, 64, 100, 36, '#373f4f'], ['circle', 20, 52, 3, '#e6b65c'], ['circle', 46, 48, 2.5, '#f0d08a'], ['circle', 64, 56, 3, '#e6b65c'], ['circle', 84, 50, 2, '#cfd8e8']]],
  sunset: ['#e3a46c', [['rect', 0, 0, 100, 30, '#c97e5e'], ['rect', 0, 66, 100, 34, '#2e2a33'], ['circle', 50, 62, 11, '#f6d49a']]],
  snow: ['#e6ebee', [['path', 'M0 70 L30 34 L52 58 L72 30 L100 64 L100 100 L0 100Z', '#94a3ae'], ['rect', 0, 82, 100, 18, '#f4f6f7']]],
  face: ['#a9a296', [['circle', 50, 42, 18, '#d8b597'], ['rect', 22, 66, 56, 40, '#44505e'], ['circle', 50, 30, 18, '#3b3430']]],
  cake: ['#d9cbb8', [['rect', 24, 46, 52, 34, '#f2e6dc'], ['rect', 24, 56, 52, 6, '#c8706a'], ['rect', 0, 80, 100, 20, '#8d6a50'], ['circle', 50, 36, 3, '#f5c45d']]],
  doc: ['#ece6d8', [['rect', 14, 16, 72, 6, '#9a8f7a'], ['rect', 14, 30, 50, 4, '#b5ab97'], ['rect', 14, 40, 60, 4, '#b5ab97'], ['rect', 60, 56, 26, 30, '#c9bda5']]],
  street: ['#9aa3a8', [['rect', 0, 58, 100, 42, '#5b5f61'], ['rect', 8, 18, 22, 46, '#c7b89c'], ['rect', 64, 10, 28, 54, '#7d6f62'], ['rect', 46, 70, 8, 30, '#d9d4c4']]],
};
let blurId = 0;
const photo = (scene, w = 40, h = w, blur = 1) => {
  const [bg, shapes] = SCENES[scene];
  const id = `b${blurId++}`;
  const parts = shapes.map(s => s[0] === 'rect' ? `<rect x="${s[1]}" y="${s[2]}" width="${s[3]}" height="${s[4]}" fill="${s[5]}"/>`
    : s[0] === 'circle' ? `<circle cx="${s[1]}" cy="${s[2]}" r="${s[3]}" fill="${s[4]}"/>` : `<path d="${s[1]}" fill="${s[2]}"/>`).join('');
  return `<svg width="${w}" height="${h}" viewBox="0 0 100 100" preserveAspectRatio="xMidYMid slice"><defs><filter id="${id}" x="-10%" y="-10%" width="120%" height="120%"><feGaussianBlur stdDeviation="${blur}"/></filter></defs><rect width="100" height="100" fill="${bg}"/><g filter="url(#${id})">${parts}</g></svg>`;
};

// Middle truncation keeps the extension and the version at the end of a name visible.
const mid = (name, max = 32) => {
  if (name.length <= max) return name;
  const dot = name.lastIndexOf('.');
  const tail = Math.min(name.length - 8, Math.max(12, name.length - dot + 14));
  return name.slice(0, max - tail - 1).trimEnd() + '…' + name.slice(name.length - tail).trimStart();
};

const n = v => String(v).replace(/\d+/g, m => `<span class="mono">${m}</span>`);
const KIND = { xlsx: 'bar_chart', csv: 'bar_chart', pdf: 'page_facing_up', docx: 'page_facing_up', eml: 'page_facing_up', dxf: 'page_facing_up', vcf: 'bust_in_silhouette', wav: 'musical_notes', mp4: 'film_frames', zip: 'card_file_box', '': 'memo' };
const tag = ext => em(KIND[ext] || 'page_facing_up');
const THING = new Set(['folder', 'home', 'drive', 'monitor', 'laptop', 'phone', 'image']);
const lead = l => typeof l === 'string' && THING.has(l) ? `<span class="lead">${em(l)}</span>` : typeof l === 'string' && P[l] ? `<span class="lead">${ic(l, 24)}</span>` : `<span class="lead">${l}</span>`;
const row = ({ l, t1, t2, num = '', end = '', sel = false, cls = '', b = false }) =>
  `<div class="row ${sel ? 'sel' : ''} ${cls}">${l ? lead(l) : ''}<div class="txt"><div class="t1${b ? ' b' : ''}">${t1}</div>${t2 ? `<div class="t2">${t2}</div>` : ''}</div>${num ? `<span class="num">${n(num)}</span>` : ''}${end}</div>`;
const sec = (t, a = '', cls = '') => `<div class="sec ${cls}"><span>${t}</span>${a ? `<a>${a}</a>` : ''}</div>`;
const btn = (t, cls = 'sec', i = '') => `<a class="btn ${cls}">${i ? ic(i, 18) : ''}${t}</a>`;

const statusBar = (time = '17:24') => `<div class="sb"><span>${time}</span><span>${ic('wifi', 16, 2)}<svg width="24" height="14" viewBox="0 0 24 14" aria-hidden="true"><rect x=".75" y=".75" width="20" height="12.5" rx="3" fill="none" stroke="currentColor" stroke-width="1.5"/><rect x="3" y="3" width="13" height="8" rx="1.3" fill="currentColor"/><rect x="22" y="4.5" width="1.6" height="5" rx=".8" fill="currentColor"/></svg></span></div>`;
const screen = (inner, dark = false) => `<div class="dev"><div class="scr${dark ? ' dark' : ''}">${statusBar()}${inner}<div class="gest"><i></i></div></div></div>`;

const nav = (on, moving = 0) => `<nav class="nav">${[
  ['files', 'folder', 'Computers'], ['phone', 'phone', 'This phone'], ['moves', 'updown', 'Transfers'],
].map(([k, i, t]) => `<a class="${k === on ? 'on' : ''}"><span class="ic">${ic(i)}</span><span>${t}${k === 'moves' && moving ? `<span class="n">${moving}</span>` : ''}</span></a>`).join('')}</nav>`;

const plate = (name, lamp, state, right = `<a class="ib">${ic('search')}</a><a class="ib">${ic('more')}</a>`) =>
  `<div class="plate"><div class="who"><div class="name">${em('monitor', 28, lamp === 'off')}${name}${ic('down', 20, 2)}</div><div class="state" style="padding-left:34px"><i class="lamp ${lamp}"></i>${state}</div></div>${right}</div>`;

const moving = (pct, t1, t2) => `<div class="moving"><i class="p" style="width:${pct}%"></i><div class="txt"><div class="t1">${t1}</div><div class="t2">${t2}</div></div><a class="ib" aria-label="Pause">${ic('pause')}</a></div>`;

const pathLine = segs => `<div class="path">${segs.map((s, i) => `${i ? ic('right', 14, 2) : ''}<a class="${i === segs.length - 1 ? 'cur' : ''}">${s}</a>`).join('')}</div>`;

// ---------- data ----------
const DOCS = [
  { l: 'folder', t1: 'Taxes', t2: '14 items', num: '' },
  { l: 'folder', t1: 'School – Year 11', t2: '38 items' },
  { l: 'folder', t1: 'Lease and house', t2: '9 items' },
  { l: tag('xlsx'), t1: 'Budget 2026.xlsx', t2: 'Today 09:14', num: '48 KB' },
  { l: tag('docx'), t1: mid('Thesis – chapter 3 – literature review – draft v7.docx', 36), t2: 'Yesterday 22:51', num: '2.3 MB' },
  { l: tag('pdf'), t1: '<bdi dir="rtl">عقد الإيجار ٢٠٢٦</bdi>.pdf', t2: '3 Nov', num: '812 KB' },
  { l: tag('pdf'), t1: 'Invoice 0412.pdf', t2: '28 Oct', num: '96 KB' },
  { l: tag('pdf'), t1: 'Invoice 0412 (1).pdf', t2: '28 Oct', num: '96 KB' },
  { l: `<span class="th">${photo('doc')}</span>`, t1: 'Passport scan.jpg', t2: '12 Sep', num: '1.9 MB' },
  { l: tag('vcf'), t1: 'Phone contacts backup.vcf', t2: '30 Aug', num: '31 KB' },
  { l: tag(''), t1: 'notes', t2: '2 Jun 2024', num: '0 B' },
];

const S = {};

// A QR code as RFE on the computer would draw it (pattern is illustrative, not a real code).
const qr = (seed = 7) => {
  const N = 25, cells = [];
  let x = seed;
  const rnd = () => (x = (x * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
  const finder = (r, c) => (r < 7 && c < 7) || (r < 7 && c >= N - 7) || (r >= N - 7 && c < 7);
  for (let r = 0; r < N; r++) for (let c = 0; c < N; c++) if (!finder(r, c) && rnd() > .52) cells.push(`<rect x="${c}" y="${r}" width="1" height="1"/>`);
  const f = (r, c) => `<rect x="${c}" y="${r}" width="7" height="7"/><rect x="${c + 1}" y="${r + 1}" width="5" height="5" fill="#fff"/><rect x="${c + 2}" y="${r + 2}" width="3" height="3"/>`;
  return `<svg viewBox="0 0 ${N} ${N}" width="100%" height="100%" shape-rendering="crispEdges" fill="#1c1d1a">${cells.join('')}${f(0, 0)}${f(0, N - 7)}${f(N - 7, 0)}</svg>`;
};
const keyMark = (s = 14) => `<svg width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><circle cx="8.5" cy="8.5" r="4.5"/><path d="M12 12l8 8M18 18l2-2M15 15l1.5-1.5"/></svg>`;
// RFE's desktop app on the computer, showing the pairing code. The phone points its camera at this.
const pcWindow = () => `<div class="win">
  <div class="tb">${keyMark(12)}RFE<i style="margin-left:auto"></i><i><span>–</span><span>□</span><span>✕</span></i></div>
  <div class="wb"><div class="side">${['Overview', 'Files', 'Computers', 'Devices', 'Transfers'].map((t, i) => `<span class="${i === 3 ? 'on' : ''}">${t}</span>`).join('')}</div>
  <div class="main"><div class="qr">${qr()}</div><div><h6>Pair a phone</h6><p>Scan this, or type the code on the phone</p><div class="code">7KQ2 9MXD</div><p style="margin-top:4px">Works for 9:41</p></div></div></div>
</div>`;

// Previews of real file contents for the home screen.
const sheetPreview = () => {
  const rows = [['', 'Jan', 'Feb', 'Mar'], ['Rent', '650', '650', '650'], ['Food', '312', '287', '341'], ['Power', '58', '71', '64'], ['Car', '120', '95', '240'], ['Phone', '25', '25', '25'], ['Total', '1165', '1128', '1320']];
  let y = 0, out = '';
  rows.forEach((r, i) => {
    out += `<rect x="0" y="${y}" width="120" height="16" fill="${i === 0 ? '#eeeeea' : i === rows.length - 1 ? '#f4f4f0' : '#fff'}"/>`;
    r.forEach((v, j) => { out += `<text x="${j === 0 ? 4 : 30 + j * 30 - 4}" y="${y + 11}" text-anchor="${j === 0 ? 'start' : 'end'}" font-size="7.5" font-family="JetBrains Mono" fill="${i === 0 || j === 0 ? '#5a5c55' : '#1d1e1b'}" font-weight="${i === rows.length - 1 ? 600 : 400}">${v}</text>`; });
    out += `<line x1="0" x2="120" y1="${y + 16}" y2="${y + 16}" stroke="#e1e1db"/>`; y += 16;
  });
  [30, 60, 90].forEach(xx => { out += `<line x1="${xx}" x2="${xx}" y1="0" y2="${y}" stroke="#e1e1db"/>`; });
  return `<svg width="120" height="152" viewBox="0 0 120 152"><rect width="120" height="152" fill="#fff"/>${out}</svg>`;
};
const wavePreview = () => {
  let bars = '', x = 6;
  for (let i = 0; i < 27; i++) { const h = 6 + Math.round(Math.abs(Math.sin(i * .9) * 22 + Math.sin(i * 2.3) * 12)); bars += `<rect x="${x}" y="${76 - h / 2}" width="2" height="${h}" rx="1" fill="${i < 10 ? '#1d1e1b' : '#b8b9b2'}"/>`; x += 4; }
  return `<svg width="120" height="152" viewBox="0 0 120 152"><rect width="120" height="152" fill="#f4f3ef"/>${bars}<text x="6" y="140" font-size="10" font-family="JetBrains Mono" fill="#5a5c55">1:12 / 3:42</text></svg>`;
};
// A real-looking photo for the preview screen: sunset over water with a jetty.
const bigPhoto = (w = 412, h = 340) => `<svg width="${w}" height="${h}" viewBox="0 0 400 300" preserveAspectRatio="xMidYMid slice">
  <defs><linearGradient id="sky" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#9c6a63"/><stop offset=".55" stop-color="#e09a6a"/><stop offset="1" stop-color="#f4c98c"/></linearGradient>
  <linearGradient id="sea" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#6f5a63"/><stop offset="1" stop-color="#2a2630"/></linearGradient>
  <filter id="soft"><feGaussianBlur stdDeviation="1.2"/></filter><filter id="glow"><feGaussianBlur stdDeviation="6"/></filter></defs>
  <rect width="400" height="176" fill="url(#sky)"/>
  <g fill="#c98068" opacity=".55" filter="url(#soft)"><ellipse cx="90" cy="60" rx="70" ry="5"/><ellipse cx="300" cy="44" rx="90" ry="4"/><ellipse cx="200" cy="96" rx="110" ry="3.5"/></g>
  <circle cx="262" cy="160" r="30" fill="#f8deaa" opacity=".6" filter="url(#glow)"/><circle cx="262" cy="160" r="15" fill="#fbe8bf"/>
  <path d="M0 170 Q60 150 120 166 T240 168 T400 164 V176 H0Z" fill="#5a4450"/>
  <rect y="176" width="400" height="124" fill="url(#sea)"/>
  <g fill="#f6cf92" opacity=".7">${[184, 192, 201, 212, 225, 240].map((y, i) => `<rect x="${250 - 14 + i * 2 - (i % 2) * 6}" y="${y}" width="${24 - i * 2 + (i % 2) * 10}" height="${1.6 + i * .3}" rx="1"/>`).join('')}</g>
  <g fill="#1c1a20"><rect x="0" y="246" width="190" height="6"/>${[14, 52, 90, 128, 166].map(x => `<rect x="${x}" y="246" width="5" height="54"/>`).join('')}<rect x="150" y="226" width="3" height="20"/><circle cx="151.5" cy="222" r="4"/><rect x="146" y="230" width="11" height="14" rx="3"/></g>
</svg>`;


// ---------- getting in ----------
S.firstRun = screen(`
  <div class="body">
    <div class="empty" style="padding-top:24px">
      <div style="margin-bottom:32px">${pcWindow()}<div class="t2" style="margin-top:8px">RFE on a computer, ready to pair</div></div>
      <h5 style="font-size:24px;line-height:32px">Open the files on your own computers</h5>
      <p>Install RFE on a Windows or Linux computer, then add it here. Files go straight between this phone and that computer. Nothing is stored anywhere else.</p>
      <div class="btns" style="flex-direction:column;align-items:stretch;max-width:100%">
        ${btn('Add a computer', 'pri full', 'plus')}
        ${btn('Get RFE for a computer', 'ter', 'open')}
      </div>

    </div>
  </div>`);

S.add = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3>Add a computer</h3></div>
  <div class="body">
    <p class="pad note" style="color:var(--ink)">On the computer, open RFE and choose <b style="font-weight:500">Pair a phone</b>. Point this camera at the code it shows.</p>
    <div class="vf">
      ${[['top:24px;left:24px', '2px 0 0 2px'], ['top:24px;right:24px', '2px 2px 0 0'], ['bottom:24px;left:24px', '0 0 2px 2px'], ['bottom:24px;right:24px', '0 2px 2px 0']].map(([pos, bw]) => `<i class="c" style="${pos};border-width:${bw.split(' ').map(v => v === '2px' ? '3px' : '0').join(' ')};z-index:1"></i>`).join('')}
      <div style="position:absolute;left:50%;top:36px;width:340px;margin-left:-170px;transform:perspective(800px) rotateY(-8deg) rotateX(4deg) scale(.78);transform-origin:50% 0;filter:brightness(.9)">${pcWindow()}</div>
      <div style="position:absolute;left:0;right:0;bottom:12px;text-align:center;color:#ecebe5;font-size:14px">Hold still</div>
    </div>
    <div class="pad" style="margin-top:12px">${btn('Type the code instead', 'sec full')}</div>
    ${sec('On this Wi-Fi')}
    ${row({ l: 'monitor', t1: 'DESKTOP-7F3K2', t2: 'RFE is running', end: btn('Connect', 'sec s') })}
    ${row({ l: 'laptop', t1: 'leila-thinkpad', t2: 'RFE is running', end: btn('Connect', 'sec s') })}
    <div class="disc"><span>Use an address instead</span>${ic('right', 18)}</div>
  </div>`);

S.waiting = screen(`
  <div class="bar"><a class="ib">${ic('x')}</a><h3></h3></div>
  <div class="body">
    <div class="empty" style="padding-top:24px">
      <div class="state" style="margin-bottom:14px"><i class="lamp wait"></i>Waiting</div>
      <h5>Allow this phone on DESKTOP-7F3K2</h5>
      <p>The computer now shows a request from <span style="color:var(--ink)">Zaid’s Pixel 8</span>. Allow it only if it shows this number:</p>
      <div class="sas">4829 1307</div>
      <p class="t2" style="margin-bottom:32px">The request waits for 2 minutes</p>
      <p style="margin-bottom:12px">Not near the computer?</p>
      <div class="btns">${btn('Sign in instead', 'sec')}${btn('Cancel', 'ter')}</div>
    </div>
    <div style="position:absolute;left:0;right:0;bottom:0">
      <div class="disc"><span>Technical details</span>${ic('down', 18)}</div>
    </div>
  </div>`);

// ---------- computers and files ----------
const homeBody = `
  ${sec('Places')}
  ${row({ l: 'folder', t1: 'RFE Files', t2: 'Shared folder · 212 items' })}
  ${row({ l: 'home', t1: 'Home', t2: 'zaid' })}
  ${row({ l: 'drive', t1: 'Media drive', t2: '1.8 TB free of 4 TB' })}
  ${sec('Pinned', 'Edit')}
  ${row({ l: 'folder', t1: 'Taxes', t2: 'Home › Documents' })}
  ${row({ l: 'folder', t1: 'Band recordings', t2: 'Media drive › Audio' })}
  ${sec('Changed lately', 'See all')}
  <div class="prev">
    <figure><div class="pv">${sheetPreview()}</div><figcaption><div class="t1">Budget 2026.xlsx</div><div class="t2">Today 09:14</div></figcaption></figure>
    <figure><div class="pv">${wavePreview()}</div><figcaption><div class="t1">mix_final_v3…wav</div><div class="t2">Yesterday</div></figcaption></figure>
    <figure><div class="pv">${bigPhoto(120, 152)}</div><figcaption><div class="t1">IMG_4418.HEIC</div><div class="t2">8 Nov</div></figcaption></figure>
  </div>`;

S.home = screen(`
  ${plate('Studio PC', '', 'Online · on this Wi-Fi · yours')}
  <div class="body">${homeBody}</div>
  ${moving(37, 'Sending 2 files to Studio PC', `${n('312 of 840 MB')} · 3 min left`)}
  ${nav('files', 3)}`);

S.switcher = screen(`
  ${plate('Studio PC', '', 'Online · on this Wi-Fi · yours')}
  <div class="body">${homeBody}</div>
  ${nav('files', 3)}
  <div class="scrim"></div>
  <div class="sheet">
    <div class="handle"></div>
    <h4>Computers</h4>
    ${row({ l: 'monitor', t1: 'Studio PC', t2: '<span class="go">Online</span> · on this Wi-Fi', sel: true, end: `<span style="color:var(--ac-ink)">${ic('check', 20, 2)}</span>` })}
    ${row({ l: em('monitor', 28, true), t1: 'Workshop PC', t2: 'Asleep · seen 22:41 yesterday', end: btn('Wake', 'sec s', 'power') })}
    ${row({ l: em('laptop', 28, true), t1: 'Mum’s laptop', t2: 'Not reachable · Mum lets you see and save' })}
    ${row({ l: 'laptop', t1: 'Old ThinkPad', t2: '<span class="stop">Its identity changed</span>', end: btn('Review', 'sec s') })}
    <div class="rule"></div>
    <div class="btns pad" style="margin-top:12px">${btn('Add a computer', 'sec', 'plus')}${btn('Manage', 'ter')}</div>
  </div>`);

S.menu = screen(`
  ${plate('Studio PC', '', 'Online · on this Wi-Fi · yours')}
  <div class="body">${homeBody}</div>
  ${nav('files')}
  <div class="menu">
    <a>${em('trash', 24)}Trash</a>
    <a>${em('apps', 24)}Apps on Studio PC</a>
    <a>${em('phone', 24)}Phones with access</a>
    <a>${em('info', 24)}About this computer</a>
    <hr>
    <a>${em('gear', 24)}Settings</a>
  </div>`);

S.docs = (dark = false) => screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3>Documents</h3><a class="ib">${ic('search')}</a><a class="ib">${ic('more')}</a></div>
  ${pathLine(['Studio PC', 'Home', 'Documents'])}
  <div class="tools"><span class="grow">11 items · 5.3 MB</span><a class="tb">Name ${ic('down', 16, 2)}</a><a class="ib" aria-label="Show as grid">${ic('grid', 20)}</a></div>
  <div class="body" style="padding-top:4px">${DOCS.map(d => row(d)).join('')}</div>
  ${nav('files')}`, dark);

const PH = [
  ['day', '8 November', '6'], ['sunset', 1], ['sunset', 0], ['face', 0], ['cake', 1], ['cake', 1, '0:42'], ['street', 0],
  ['day', '2 November', '9'], ['sea', 1], ['sea', 0], ['forest', 0], ['snow', 0], ['night', 0, '1:08'], ['room', 0], ['forest', 0], ['street', 0], ['night', 0],
];
S.photos = (() => {
  let out = '', cells = [];
  const flush = () => { if (cells.length) { out += `<div class="grid3">${cells.join('')}</div>`; cells = []; } };
  for (const p of PH) {
    if (p[0] === 'day') { flush(); out += `<div class="day"><span>${p[1]}</span><span>${n(p[2])}</span></div>`; continue; }
    const [sc, on, dur] = p;
    cells.push(`<div class="cell${on ? ' sel' : ''}">${(sc === 'sunset' ? bigPhoto(136, 136) : photo(sc, 136, 136, 1))}<span class="cb${on ? ' on' : ''}">${on ? ic('check', 16, 2.5) : ''}</span>${dur ? `<span class="dur">${dur}</span>` : ''}</div>`);
  }
  flush();
  return screen(`
    <div class="bar ctx"><a class="ib" aria-label="Clear selection">${ic('x')}</a><h3>3 selected<small>${n('7.9 MB')}</small></h3><a class="btn ter s">Select all</a></div>
    ${pathLine(['Studio PC', 'Pictures', 'Phone', 'Photos 2026'])}
    <div class="body">${out}</div>
    <div class="acts"><a>${ic('save')}Save to phone</a><a>${ic('link')}Share link</a><a>${ic('move')}Move</a><a>${ic('more')}More</a></div>`);
})();

S.preview = (() => screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3 style="font-size:17px">IMG_4418.HEIC<small>Studio PC › Pictures › Photos 2026</small></h3><a class="ib">${ic('more')}</a></div>
  <div class="body">
    <div style="height:340px;background:#121311;display:grid;place-items:center">${bigPhoto()}</div>
    <div style="padding:16px 16px 4px" class="btns">${btn('Save to phone', 'pri', 'save')}${btn('Share link', 'sec', 'link')}${btn('Open with', 'ter', 'open')}</div>
    <dl class="facts" style="margin-top:10px">
      <dt>Size</dt><dd>${n('3.2 MB')} · ${n('4032 × 3024')}</dd>
      <dt>Taken</dt><dd>8 Nov 2026, 17:02</dd>
      <dt>Changed</dt><dd>8 Nov 2026, 19:40</dd>
    </dl>
    <div style="margin-top:14px">
      ${row({ l: 'pencil', t1: 'Rename', cls: 's1' })}
      ${row({ l: 'move', t1: 'Move or copy', cls: 's1' })}
      ${row({ l: `<span style="color:var(--stop)">${ic('trash', 24)}</span>`, t1: '<span style="color:var(--stop)">Delete</span>', cls: 's1' })}
    </div>
  </div>`, true))();

S.share = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3>Documents</h3><a class="ib">${ic('search')}</a><a class="ib">${ic('more')}</a></div>
  ${pathLine(['Studio PC', 'Home', 'Documents'])}
  <div class="tools"><span class="grow">11 items · 5.3 MB</span><a class="tb">Name ${ic('down', 16, 2)}</a><a class="ib">${ic('grid', 20)}</a></div>
  <div class="body" style="padding-top:4px">${DOCS.map(d => row(d)).join('')}</div>
  ${nav('files')}
  <div class="scrim"></div>
  <div class="sheet">
    <div class="handle"></div>
    <h4>Share link</h4>
    <p class="lede">Anyone with the link can download <b style="color:var(--ink);font-weight:600">Budget 2026.xlsx</b> until it stops working. Studio PC has to be on.</p>
    <div class="pad" style="margin:8px 0 6px;font:500 14px/20px var(--sans);color:var(--ink-2)">Works for</div>
    <div class="pad"><div class="seg"><a>1 hour</a><a class="on">1 day</a><a>1 week</a></div></div>
    <div class="pad" style="margin-top:8px">${btn('Create link', 'pri full')}</div>
  </div>`);

S.search = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><div style="flex:1;padding-right:8px"><div class="field focus">${ic('search', 20)}<span>invoice</span><i class="caret"></i></div></div></div>
  <div class="chips"><a class="chip on">Studio PC</a><a class="chip">All computers</a><a class="chip">Documents ${ic('down', 16, 2)}</a><a class="chip">Any size</a></div>
  <div class="body">
    <div class="sec"><span>7 found on Studio PC</span></div>
    ${row({ l: tag('pdf'), t1: '<b style="font-weight:600">Invoice</b> 0412.pdf', t2: 'Home › Documents', num: '96 KB' })}
    ${row({ l: tag('pdf'), t1: '<b style="font-weight:600">Invoice</b> 0412 (1).pdf', t2: 'Home › Documents', num: '96 KB' })}
    ${row({ l: tag('pdf'), t1: '<b style="font-weight:600">Invoice</b>s paid 2025.pdf', t2: 'Home › Documents › Taxes', num: '1.1 MB' })}
    ${row({ l: tag('xlsx'), t1: 'Studio <b style="font-weight:600">invoice</b> template.xlsx', t2: 'RFE Files › Work', num: '22 KB' })}
    ${row({ l: 'folder', t1: 'Old <b style="font-weight:600">invoice</b>s', t2: 'Media drive › Archive 2019', num: '' })}
    ${row({ l: tag('eml'), t1: 'Re: <b style="font-weight:600">invoice</b> for October.eml', t2: 'Home › Downloads', num: '18 KB' })}
    ${row({ l: tag('pdf'), t1: 'Electricity invoice – Oct 2026.pdf'.replace('invoice', '<b style="font-weight:600">invoice</b>'), t2: 'Home › Downloads', num: '240 KB' })}
  </div>`);

// ---------- transfers ----------
const trow = ({ l, t1, to, pg, numL, numR, st, end, warn }) =>
  `<div class="row" style="align-items:flex-start;padding-top:12px;padding-bottom:12px">${lead(l)}<div class="txt"><div class="t1">${t1}</div><div class="t2">${to}</div>${pg != null ? `<div class="pg${warn ? ' wait' : ''}"><i style="width:${pg}%"></i></div><div class="t2" style="display:flex;justify-content:space-between"><span>${n(numL)}</span><span>${numR}</span></div>` : ''}${st ? `<div class="t2">${st}</div>` : ''}</div>${end || ''}</div>`;

S.transfers = (dark = false) => screen(`
  <div class="bar"><h3 style="padding-left:12px">Transfers</h3><a class="btn ter s">Pause all</a><a class="ib">${ic('more')}</a></div>
  <div class="body">
    ${sec('Needs you', '', 'wait')}
    <div class="row" style="align-items:flex-start;padding-top:10px">${lead(tag('xlsx'))}<div class="txt"><div class="t1">Budget 2026.xlsx</div><div class="t2" style="white-space:normal">Documents on Studio PC already has a file with this name</div>
      <div class="cmp"><div><div class="t2">Sending</div><div class="t1" style="font-size:15px">${n('51 KB')}</div><div class="t2">Today ${n('14:20')}</div></div><div><div class="t2">On Studio PC</div><div class="t1" style="font-size:15px">${n('48 KB')}</div><div class="t2">Today ${n('09:14')}</div></div></div>
      <div class="btns" style="margin-top:12px">${btn('Keep both', 'pri s')}${btn('Replace', 'sec s')}${btn('Skip', 'ter s')}</div></div></div>
    ${sec('Moving')}
    ${trow({ l: `<span class="th">${photo('cake')}</span>`, t1: 'VID_20261108_142233.mp4', to: 'To Studio PC › Pictures › Phone', pg: 37, numL: '312 / 840 MB', numR: '3 min left', end: `<a class="ib" aria-label="Pause">${ic('pause')}</a>` })}
    ${trow({ l: tag('wav'), t1: 'mix_final_v3 (louder).wav', to: 'From Studio PC to Downloads', pg: 64, numL: '39 / 61 MB', numR: 'Paused', warn: true, end: `<a class="ib" aria-label="Resume">${ic('play')}</a>` })}
    ${sec('Couldn’t finish', '', 'stop')}
    ${trow({ l: tag('zip'), t1: 'Holiday photos 2025.zip', to: 'From Workshop PC to Downloads', st: 'Workshop PC went to sleep at 81%', end: btn('Retry', 'sec s') })}
    ${sec('Done today', 'Clear')}
    ${trow({ l: tag('pdf'), t1: 'Invoice 0412.pdf', to: 'Saved to Downloads · 14:02', end: btn('Open', 'ter s') })}
    ${trow({ l: `<span class="th">${bigPhoto(40, 40)}</span>`, t1: 'IMG_4418.HEIC', to: 'Saved to Pictures › RFE · 13:47', end: btn('Open', 'ter s') })}
  </div>
  ${nav('moves', 3)}`, dark);

// ---------- this phone ----------
S.phone = screen(`
  <div class="bar"><h3 style="padding-left:12px">This phone</h3><a class="ib">${ic('more')}</a></div>
  <div class="body">
    ${sec('Photo backup')}
    ${row({ l: em('image'), t1: 'Backing up to Studio PC', t2: `Up to date 09:12 · ${n('2,184')} copied`, end: '<span class="tg on"></span>' })}
    ${sec('Kept on this phone', `<span style="font-weight:400;color:var(--ink-2)">${n('1.2 GB')}</span>`)}
    ${row({ l: tag('pdf'), t1: 'Lease 2026 signed.pdf', t2: 'From Studio PC · same as the computer', num: '812 KB' })}
    ${row({ l: tag('xlsx'), t1: 'Budget 2026.xlsx', t2: '<span class="wait">Changed on Studio PC</span>', end: btn('Update', 'sec s') })}
    ${row({ l: 'folder', t1: 'Band recordings', t2: 'From Studio PC · 14 files', num: '1.1 GB' })}
    ${sec('Received', 'See all')}
    ${row({ l: `<span class="th">${photo('face')}</span>`, t1: 'IMG-20261109-WA0012.jpg', t2: 'From WhatsApp · not sent yet', end: btn('Send', 'sec s', 'up') })}
    ${row({ l: tag('pdf'), t1: 'Boarding pass AMM–IST.pdf', t2: 'Sent to Studio PC › RFE Files', num: '210 KB' })}
  </div>
  ${nav('phone')}`);

// ---------- owner: phones with access ----------
const perm = (t, on, d = '') => `<div class="sw-r" style="min-height:48px;padding-top:2px;padding-bottom:2px"><div class="txt"><div class="t1" style="font-size:15px">${t}</div>${d ? `<div class="t2">${d}</div>` : ''}</div><span class="tg${on ? ' on' : ''}"></span></div>`;
S.access = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3>Phones with access<small>Studio PC</small></h3></div>
  <div class="body">
    ${sec('Asking to come in', '', 'wait')}
    ${row({ l: 'phone', t1: 'Mum’s Galaxy S23', t2: `Asked 2 min ago · number ${n('6150 2283')}` })}
    <div class="btns" style="padding:0 16px 8px 72px">${btn('Allow', 'pri s')}${btn('Refuse', 'sec s')}</div>
    ${sec('Allowed')}
    ${row({ l: 'phone', t1: 'Zaid’s Pixel 8 <span style="color:var(--ink-2)">(this phone)</span>', t2: 'Owner · everything' })}
    <div style="margin:0 8px;border-radius:8px;box-shadow:inset 0 0 0 1px var(--rule-2);background:var(--fill)">
      ${row({ l: 'phone', t1: 'Leila’s Galaxy A54', t2: 'Last used today 11:20', end: `<span style="color:var(--ink-2)">${ic('down', 20, 2)}</span>` }).replace('class="row', 'style="padding-left:8px;padding-right:8px" class="row')}
      <div style="padding:0 0 4px">
        ${perm('See files', true)}${perm('Save to phone', true)}${perm('Send files', true)}${perm('Rename and move', false)}${perm('Delete', false)}${perm('Share links', false)}
      </div>
      <div class="sw-r"><div class="txt"><div class="t2">Only inside</div><div class="t1" style="font-size:15px">RFE Files › Leila</div></div>${btn('Change', 'ter s')}</div>
      <div class="btns" style="padding:4px 16px 14px">${btn('Block', 'danger sec s')}${btn('Remove', 'danger sec s', 'trash')}</div>
    </div>
  </div>`);

// ---------- empty, offline, error, loading ----------
S.emptyFolder = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3>Scans</h3><a class="ib">${ic('search')}</a><a class="ib">${ic('more')}</a></div>
  ${pathLine(['Studio PC', 'Home', 'Documents', 'Scans'])}
  <div class="body" style="border-top:1px solid var(--rule)"><div class="empty"><div class="obj">${em('openfolder', 48)}</div><h5>This folder is empty</h5><p>Send files here from your phone, or make a folder.</p><div class="btns">${btn('Send files here', 'pri', 'up')}${btn('New folder', 'sec')}</div></div></div>
  ${nav('files')}`);

S.asleep = screen(`
  ${plate('Workshop PC', 'off', 'Asleep · seen 22:41 yesterday', `<a class="ib">${ic('more')}</a>`)}
  <div class="body">
    <div class="empty" style="padding-top:28px">
      <div class="obj">${em('monitor', 48, true)}</div><h5>Workshop PC is asleep</h5>
      <p>RFE can wake it through Studio PC, which is on the same network. It usually takes under a minute.</p>
      <div class="btns">${btn('Wake it', 'pri', 'power')}${btn('Try again', 'sec')}</div>
    </div>
    ${sec('Kept on this phone from Workshop PC')}
    ${row({ l: tag('dxf'), t1: 'Shelf bracket rev C.dxf', t2: 'Opens without the computer', num: '420 KB' })}
    ${row({ l: tag('pdf'), t1: 'Router manual.pdf', t2: 'Opens without the computer', num: '6.8 MB' })}
  </div>
  ${nav('files')}`);

S.noResults = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><div style="flex:1;padding-right:8px"><div class="field focus">${ic('search', 20)}<span>reciept</span><i class="caret"></i></div></div></div>
  <div class="chips"><a class="chip on">Studio PC</a><a class="chip">All computers</a><a class="chip">Any type ${ic('down', 16, 2)}</a></div>
  <div class="body"><div class="empty" style="padding-top:24px"><h5>Nothing called “reciept” on Studio PC</h5><p>Check the spelling, or look on every computer.</p><div class="btns">${btn('Search all computers', 'sec')}${btn('Search “receipt”', 'ter')}</div></div></div>`);

S.movesEmpty = screen(`
  <div class="bar"><h3 style="padding-left:12px">Transfers</h3></div>
  <div class="body"><div class="empty" style="padding-top:16px"><h5>Nothing is moving</h5><p>Files you save to this phone or send to a computer show here until you clear them. They carry on if you leave the app.</p></div></div>
  ${nav('moves')}`);

S.loading = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3>Media drive</h3><a class="ib">${ic('search')}</a><a class="ib">${ic('more')}</a></div>
  ${pathLine(['Studio PC', 'Media drive'])}
  <div class="tools" style="position:relative"><span class="grow">Reading folder…</span><i style="position:absolute;left:0;bottom:-1px;height:2px;width:30%;background:var(--ink)"></i></div>
  <div class="body" style="padding-top:4px">
    ${row({ l: 'folder', t1: 'Audio', t2: '' })}${row({ l: 'folder', t1: 'Films', t2: '' })}${row({ l: 'folder', t1: 'Archive 2019', t2: '' })}
    ${[70, 52, 84, 60].map(w => `<div class="row"><span class="lead"><span class="skel" style="width:32px;height:40px"></span></span><div class="txt"><div class="skel" style="width:${w}%"></div><div class="skel" style="width:30%;margin-top:10px;height:10px"></div></div></div>`).join('')}
  </div>
  ${nav('files')}`);

S.lost = screen(`
  ${plate('Studio PC', 'wait', 'Connection lost · trying again')}
  <div class="body">
    <div style="padding:12px 16px 14px;border-bottom:1px solid var(--rule)">
      <div class="t1" style="white-space:normal">Studio PC stopped answering 40 seconds ago.</div>
      <div class="t2" style="white-space:normal;margin-top:2px">Your transfer is paused and will carry on by itself when it is back.</div>
      <div class="btns" style="margin-top:12px">${btn('Try now', 'sec s', 'retry')}${btn('Check connection', 'ter s')}</div>
    </div>
    <div style="opacity:.5">${homeBody}</div>
  </div>
  ${nav('files', 3)}`);

S.identity = screen(`
  <div class="bar"><a class="ib">${ic('back')}</a><h3></h3></div>
  <div class="body">
    <div class="empty" style="padding-top:8px">
      <div class="state" style="margin-bottom:14px"><i class="lamp stop"></i><span style="color:var(--stop)">Not connected</span></div>
      <div class="obj">${em('lock', 48)}</div><h5>Old ThinkPad answered with a different identity</h5>
      <p>This happens after RFE is reinstalled on it. It can also mean another computer is pretending to be it. RFE stopped before sending anything.</p>
      <p style="margin-bottom:24px">If you reinstalled RFE yourself, add the computer again. Otherwise leave it until you have checked.</p>
      <div class="btns" style="flex-direction:column;align-items:stretch">${btn('Add it again', 'pri full')}${btn('Forget Old ThinkPad', 'danger sec full')}${btn('Not now', 'ter full')}</div>
    </div>
    <div style="position:absolute;left:0;right:0;bottom:0"><div class="disc"><span>Technical details</span>${ic('down', 18)}</div></div>
  </div>`);
