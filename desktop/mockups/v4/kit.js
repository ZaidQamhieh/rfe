/* RFE Desktop v2 mockups: shared kit. Icons (lucide geometry, 1.5 stroke), plausible data, and every page
   and dialog of the current app as HTML. Each design supplies its own shell, navigation and theme and calls
   K.mount(). Nothing here talks to a network. */
(function () {
  'use strict';
  const p = (d) => d.split(' M').map((s, i) => `<path d="${i ? 'M' + s : s}"/>`).join('');
  const P = {
    folder: p('M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9l-.8-1.2A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z'),
    'folder-plus': p('M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9l-.8-1.2A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z M12 10v6 M9 13h6'),
    file: p('M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z M14 2v4a2 2 0 0 0 2 2h4'),
    text: p('M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z M14 2v4a2 2 0 0 0 2 2h4 M10 9H8 M16 13H8 M16 17H8'),
    image: '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/>' + p('m21 15-3.1-3.1a2 2 0 0 0-2.8 0L6 21'),
    video: '<rect x="2" y="6" width="14" height="12" rx="2"/>' + p('m16 13 5.2 3.5a.5.5 0 0 0 .8-.4V7.9a.5.5 0 0 0-.8-.4L16 11'),
    archive: p('M21 8v13H3V8 M10 12h4') + '<rect x="1" y="3" width="22" height="5" rx="1"/>',
    package: p('M16.5 9.4 7.5 4.2 M21 16V8a2 2 0 0 0-1-1.7l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.7l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z M3.3 7 12 12l8.7-5 M12 22V12'),
    sheet: p('M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z M14 2v4a2 2 0 0 0 2 2h4 M8 13h2 M14 13h2 M8 17h2 M14 17h2'),
    server: '<rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/>' + p('M6 6h.01 M6 18h.01'),
    laptop: p('M20 16V7a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v9m16 0H4m16 0 1.28 2.55a1 1 0 0 1-.9 1.45H3.62a1 1 0 0 1-.9-1.45L4 16'),
    phone: '<rect x="5" y="2" width="14" height="20" rx="2"/>' + p('M12 18h.01'),
    transfer: p('M8 3 4 7l4 4 M4 7h16 M16 21l4-4-4-4 M20 17H4'),
    search: '<circle cx="11" cy="11" r="8"/>' + p('m21 21-4.3-4.3'),
    layers: p('m12 2 10 5-10 5L2 7z M2 17l10 5 10-5 M2 12l10 5 10-5'),
    history: p('M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8 M3 3v5h5 M12 7v5l4 2'),
    sliders: p('M21 4h-7 M10 4H3 M21 12h-9 M8 12H3 M21 20h-5 M12 20H3 M14 2v4 M8 10v4 M16 18v4'),
    plug: p('M12 22v-5 M9 8V2 M15 8V2 M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z'),
    upload: p('M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4 M17 8l-5-5-5 5 M12 3v12'),
    download: p('M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4 M7 10l5 5 5-5 M12 15V3'),
    left: p('m12 19-7-7 7-7 M19 12H5'), right: p('M5 12h14 M12 5l7 7-7 7'), up: p('m5 12 7-7 7 7 M12 19V5'),
    refresh: p('M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8 M21 3v5h-5 M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16 M8 16H3v5'),
    qr: '<rect x="3" y="3" width="5" height="5" rx="1"/><rect x="16" y="3" width="5" height="5" rx="1"/><rect x="3" y="16" width="5" height="5" rx="1"/>' + p('M21 16h-3a2 2 0 0 0-2 2v3 M21 21v.01 M12 7v3a2 2 0 0 1-2 2H7 M3 12h.01 M12 3h.01 M12 16v.01 M16 12h1 M21 12v.01 M12 21v-1'),
    sun: '<circle cx="12" cy="12" r="4"/>' + p('M12 2v2 M12 20v2 M4.93 4.93l1.41 1.41 M17.66 17.66l1.41 1.41 M2 12h2 M20 12h2 M6.34 17.66l-1.41 1.41 M19.07 4.93l-1.41 1.41'),
    moon: p('M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z'),
    command: p('M15 6v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V6a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3'),
    bell: p('M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9 M10.3 21a1.94 1.94 0 0 0 3.4 0'),
    star: p('M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01z'),
    pencil: p('M17 3a2.85 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z'),
    trash: p('M3 6h18 M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6 M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2'),
    link: p('M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71 M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71'),
    copy: '<rect x="8" y="8" width="14" height="14" rx="2"/>' + p('M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2'),
    move: p('M2 9V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H20a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-1 M2 13h10 M9 16l3-3-3-3'),
    shield: p('M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z M9 12l2 2 4-4'),
    key: '<circle cx="7.5" cy="15.5" r="5.5"/>' + p('m21 2-9.6 9.6 M15.5 7.5l3 3L22 7l-3-3'),
    check: p('M20 6 9 17l-5-5'), x: p('M18 6 6 18 M6 6l12 12'),
    pause: '<rect x="14" y="4" width="4" height="16" rx="1"/><rect x="6" y="4" width="4" height="16" rx="1"/>',
    play: p('m6 3 14 9-14 9z'),
    more: '<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>',
    alert: '<circle cx="12" cy="12" r="10"/>' + p('M12 8v4 M12 16h.01'),
    info: '<circle cx="12" cy="12" r="10"/>' + p('M12 16v-4 M12 8h.01'),
    clock: '<circle cx="12" cy="12" r="10"/>' + p('M12 6v6l4 2'),
    pin: p('M12 17v5 M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z'),
    pie: p('M21.21 15.89A10 10 0 1 1 8 2.83 M22 12A10 10 0 0 0 12 2v10z'),
    zap: p('M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z'),
    power: p('M12 2v10 M18.4 6.6a9 9 0 1 1-12.77.04'),
    grid: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/>',
    list: p('M3 12h.01 M3 18h.01 M3 6h.01 M8 12h13 M8 18h13 M8 6h13'),
    filter: p('M22 3H2l8 9.46V19l4 2v-8.54z'),
    sort: p('m21 16-4 4-4-4 M17 20V4 M3 8l4-4 4 4 M7 4v16'),
    eye: p('M2.06 12.35a1 1 0 0 1 0-.7 10.75 10.75 0 0 1 19.88 0 1 1 0 0 1 0 .7 10.75 10.75 0 0 1-19.88 0') + '<circle cx="12" cy="12" r="3"/>',
    user: '<circle cx="12" cy="8" r="5"/>' + p('M20 21a8 8 0 0 0-16 0'),
    logout: p('M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4 M16 17l5-5-5-5 M21 12H9'),
    hash: p('M4 9h16 M4 15h16 M10 3 8 21 M16 3l-2 18'),
    activity: p('M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2'),
    panel: '<rect x="3" y="3" width="18" height="18" rx="2"/>' + p('M15 3v18'),
    lock: '<rect x="3" y="11" width="18" height="11" rx="2"/>' + p('M7 11V7a5 5 0 0 1 10 0v4'),
    music: p('M9 18V5l12-2v13') + '<circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
    code: p('m16 18 6-6-6-6 M8 6l-6 6 6 6'),
    offline: p('M12 2v8 M16 6l-4 4-4-4 M2 14h20v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2z M6 18h.01 M10 18h.01'),
    chevr: p('m9 18 6-6-6-6'), chevd: p('m6 9 6 6 6-6'),
    plus: p('M5 12h14 M12 5v14'),
    keyboard: '<rect x="2" y="4" width="20" height="16" rx="2"/>' + p('M6 8h.01 M10 8h.01 M14 8h.01 M18 8h.01 M8 12h.01 M12 12h.01 M16 12h.01 M7 16h10'),
    help: '<circle cx="12" cy="12" r="10"/>' + p('M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3 M12 17h.01'),
    save: p('M15.2 3a2 2 0 0 1 1.4.6l3.8 3.8a2 2 0 0 1 .6 1.4V19a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z M17 21v-7a1 1 0 0 0-1-1H8a1 1 0 0 0-1 1v7 M7 3v4a1 1 0 0 0 1 1h7'),
    radar: p('M19.07 4.93A10 10 0 0 0 6.99 3.34 M2.29 9.62A10 10 0 1 0 21.31 8.35 M16.24 7.76A6 6 0 1 0 8.23 16.67 M17.99 11.66A6 6 0 0 1 15.77 16.67 M13.41 10.59l5.66-5.66') + '<circle cx="12" cy="12" r="2"/>',
    undo: p('M3 7v6h6 M21 17a9 9 0 0 0-9-9 9 9 0 0 0-6 2.3L3 13'),
    home: p('M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8 M3 10a2 2 0 0 1 .71-1.53l7-6a2 2 0 0 1 2.58 0l7 6A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z'),
    drive: p('M22 12H2 M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z M6 16h.01 M10 16h.01'),
    inbox: p('M22 12h-6l-2 3h-4l-2-3H2 M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z'),
    zoomin: '<circle cx="11" cy="11" r="8"/>' + p('m21 21-4.3-4.3 M11 8v6 M8 11h6'),
    cpu: '<rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6" rx="1"/>' + p('M15 2v2 M15 20v2 M2 15h2 M2 9h2 M20 15h2 M20 9h2 M9 2v2 M9 20v2'),
  };
  /* Duotone, as in ObsidianUI: closed shapes get a second layer of the same color at low opacity.
     Only icons whose outline is closed take it; chevrons and strokes would fill wrongly. */
  const DUO = new Set(['folder', 'folder-plus', 'file', 'text', 'image', 'video', 'archive', 'package', 'sheet', 'server', 'laptop', 'phone', 'search', 'bell', 'star', 'trash', 'copy', 'shield', 'key', 'pause', 'play', 'alert', 'info', 'clock', 'pie', 'zap', 'grid', 'filter', 'eye', 'user', 'lock', 'save', 'home', 'drive', 'inbox', 'cpu', 'qr', 'keyboard', 'help', 'move', 'zoomin', 'sun', 'moon', 'offline', 'panel']);
  const I = (n, s = 16) => `<svg class="i${DUO.has(n) ? ' duo' : ''}" width="${s}" height="${s}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${P[n] || ''}</svg>`;
  /* Emoji: Microsoft Fluent Emoji Flat (MIT, emoji/LICENSE.txt), one consistent set. Emoji mark things
     (file types, computers, phones, sections); line icons mark actions. Never decoration. */
  const EM = { dir: 'file_folder', img: 'framed_picture', doc: 'page_facing_up', txt: 'memo', sheet: 'bar_chart', vid: 'film_frames', arc: 'card_file_box', pkg: 'package', home: 'house', drive: 'computer_disk', music: 'musical_notes', code: 'scroll', server: 'desktop_computer', laptop: 'laptop', phone: 'mobile_phone' };
  const E = (n, s = 18, alt = '') => `<img class="k-e" src="emoji/${EM[n] || n}.svg" width="${s}" height="${s}" alt="${alt}" draggable="false">`;
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);

  /* ---------- data ---------- */
  const D = {
    me: { account: 'sam', device: 'desk-linux', version: '1.1.0' },
    servers: [
      { id: 'studio', name: 'studio-pc', state: 'Connected', dot: 'ok', route: 'LAN', addr: '192.168.1.20:8765', sys: 'Fedora 41', plat: 'Linux x86_64', ver: '1.3.0', ms: 12, used: 812, total: 2000, tag: '9F3A 1EC4', fp: '9F3A 1EC4 77B0 2D19 C5E8 0A41 6B2F 93D7 E810 54CA 2F6E 1B98 D03C 7A15 46E2 B0F9', login: 'Account', up: '6 d 4 h', tail: '100.71.4.2', mac: 'a8:5e:45:21:9c:0e' },
      { id: 'work', name: 'work-laptop', state: 'Connected', dot: 'ok', route: 'Tailscale', addr: '100.71.4.18:8765', sys: 'Windows 11', plat: 'Windows x86_64', ver: '1.3.0', ms: 48, used: 402, total: 512, tag: 'C07B 52A8', login: 'Pairing code', up: '3 h 12 min' },
      { id: 'nas', name: 'nas', state: 'Key changed', dot: 'bad', route: 'LAN', addr: '192.168.1.5:8765', sys: 'Debian 12', plat: 'Linux aarch64', ver: '1.2.4', ms: null, used: 5400, total: 8000, tag: '41DE 0B93', login: 'Account' },
      { id: 'mini', name: 'media-mini', state: 'Offline', dot: 'off', route: 'LAN', addr: '192.168.1.31:8765', sys: 'Ubuntu 24.04', plat: 'Linux x86_64', ver: '1.3.0', ms: null, used: 611, total: 1000, tag: '7A20 E5F1', login: 'Account', last: 'Last seen 08:14' },
    ],
    path: ['/', 'home', 'sam', 'Projects', 'garden-plan'],
    files: [
      { n: 'drawings', k: 'dir', c: 14, m: '1 Nov 08:40', pm: 'rwxr-xr-x' },
      { n: 'photos', k: 'dir', c: 86, m: '31 Oct 18:05', pm: 'rwxr-xr-x' },
      { n: 'quotes', k: 'dir', c: 6, m: '24 Oct 11:20', pm: 'rwxr-xr-x' },
      { n: 'north-bed-after.jpg', k: 'img', s: '3.8 MB', b: 3.8, m: '1 Nov 09:12', pm: 'rw-r--r--', seed: 2 },
      { n: 'north-bed-before.jpg', k: 'img', s: '4.1 MB', b: 4.1, m: '12 Oct 16:44', pm: 'rw-r--r--', seed: 1 },
      { n: 'bed-layout.pdf', k: 'doc', s: '2.4 MB', b: 2.4, m: '29 Oct 21:03', pm: 'rw-r--r--' },
      { n: 'planting-calendar.md', k: 'txt', s: '6 KB', b: 0.006, m: '28 Oct 07:50', pm: 'rw-r--r--' },
      { n: 'soil-test-results.csv', k: 'sheet', s: '18 KB', b: 0.018, m: '20 Oct 14:31', pm: 'rw-r--r--' },
      { n: 'walkthrough.mp4', k: 'vid', s: '412 MB', b: 412, m: '19 Oct 17:22', pm: 'rw-r--r--' },
      { n: 'supplier-invoices.zip', k: 'arc', s: '23.6 MB', b: 23.6, m: '15 Oct 10:02', pm: 'rw-r--r--' },
      { n: 'irrigation-timer.deb', k: 'pkg', s: '1.2 MB', b: 1.2, m: '3 Oct 19:47', pm: 'rw-r--r--' },
    ],
    sel: 'north-bed-after.jpg',
    local: [
      { n: 'RFE Desktop', k: 'dir', c: 12, m: '1 Nov 10:41' }, { n: 'scan-0142.pdf', k: 'doc', s: '880 KB', m: '1 Nov 10:38' },
      { n: 'seed-order.xlsx', k: 'sheet', s: '64 KB', m: '31 Oct 20:15' }, { n: 'garden-sketch.png', k: 'img', s: '2.2 MB', m: '30 Oct 12:09', seed: 3 },
      { n: 'trip-photos.zip', k: 'arc', s: '2.1 GB', m: '27 Oct 09:02' }, { n: 'notes.txt', k: 'txt', s: '3 KB', m: '26 Oct 22:47' },
    ],
    roots: [{ n: '/home/sam', k: 'home', note: '812 GB used' }, { n: '/srv/media', k: 'drive', note: '1.1 TB used' }, { n: '/mnt/backup', k: 'drive', note: '96 GB used' }],
    xfer: {
      stats: [['52.4 MB/s', 'current speed'], ['2', 'active'], ['1', 'queued'], ['3', 'need attention'], ['2', 'completed'], ['1.9 GB', 'moved this session']],
      attention: [
        { d: 'up', n: 'bed-layout.pdf', where: 'work-laptop · /Users/sam/Documents', st: 'conflict', msg: 'A file with this name already exists on the computer' },
        { d: 'down', n: 'album-2019.zip', where: 'media-mini · /srv/media/archive', st: 'paused', pct: 33, msg: 'media-mini is offline. Paused at 33%; it continues when the server is back' },
        { d: 'up', n: 'notes.txt', where: 'studio-pc · /home/sam/Inbox', st: 'failed', msg: 'The file changed or was damaged while it was uploading. The upload was discarded; try again' },
      ],
      active: [
        { d: 'up', n: 'walkthrough-full.mov', where: 'studio-pc · /home/sam/Projects/garden-plan', pct: 71, msg: '2.9 of 4.1 GB · 52.4 MB/s · 23 s left' },
        { d: 'down', n: 'tax-2025.pdf', where: 'work-laptop · /Users/sam/Documents', pct: 100, msg: 'Checking the file' },
      ],
      queued: [{ d: 'up', n: 'seed-order.xlsx', where: 'studio-pc · /home/sam/Inbox', pct: 0, msg: 'Waiting for a free slot' }],
      done: [
        { d: 'down', n: 'north-bed-before.jpg', where: '~/Downloads/RFE Desktop', pct: 100, msg: 'Checksum verified' },
        { d: 'up', n: 'scan-0142.pdf', where: 'studio-pc · /home/sam/Inbox', pct: 100, msg: 'Checked by studio-pc' },
      ],
    },
    pair: { code: 'K7QM 2XPA', left: '4:12', life: '5 minutes' },
    requests: [
      { n: 'Pixel 9', from: '192.168.1.40', wait: '18 s', exp: '1:42', code: '4829 1307' },
      { n: 'sam-thinkpad', from: '100.71.4.22', wait: '51 s', exp: '1:09', code: '0316 8842', over: 'work-laptop' },
    ],
    devices: [
      { n: 'desk-linux', me: true, kind: 'laptop', how: 'Account (sam)', seen: 'Now', may: 'Owner: every file action', lim: '' },
      { n: 'Pixel 8', kind: 'phone', how: 'Pairing code', seen: '2 h ago', may: 'Browse, download, upload, share links', lim: '/home/sam/Photos' },
      { n: 'Galaxy Tab S9', kind: 'phone', how: 'Approved on the PC', seen: 'Tue', may: 'Browse, see apps, start apps · read-only', lim: '' },
      { n: 'work-laptop', kind: 'laptop', how: 'Pairing code', seen: 'Yesterday', may: 'Browse, download, upload, change', lim: '/home/sam/Projects' },
      { n: 'old-phone', kind: 'phone', how: 'Pairing code', seen: '12 Oct', may: 'Revoked', lim: '', revoked: true },
    ],
    events: [
      ['09:01', 'Sign-in', 'desk-linux, account sam', 'ok'], ['07:55', 'Failed sign-in', 'account "admin" from 10.0.0.9', 'bad'],
      ['Tue', 'Share link made', 'north-bed-before.jpg, 24 hours', ''], ['Mon', 'Device revoked', 'old-phone', ''],
      ['Sun', 'App launched', 'GIMP, by Galaxy Tab S9', ''], ['Sat', 'Agent restarted', 'version 1.3.0', ''],
    ],
    history: [
      ['10:44', 'Transfers', 'Uploading walkthrough-full.mov to studio-pc', ''], ['10:41', 'Transfers', 'Downloaded north-bed-before.jpg from studio-pc · checksum verified', 'ok'],
      ['10:02', 'Transfers', 'bed-layout.pdf waits for an answer: the name exists on work-laptop', 'warn'], ['09:01', 'Security', 'Signed in to studio-pc as sam (account)', 'ok'],
      ['08:58', 'Connections', 'Connected to studio-pc over LAN · 12 ms', ''], ['08:58', 'Errors', 'nas: This agent’s certificate is not the one you trusted', 'bad'],
      ['08:14', 'Connections', 'media-mini stopped answering; its transfers are paused', 'warn'], ['07:55', 'Security', 'Failed sign-in on studio-pc for account "admin" from 10.0.0.9', 'bad'],
    ],
    agentlog: ['10:44:02 INFO upload session 7f3c opened by desk-linux (4.1 GB, 4 MiB chunks)', '10:41:57 INFO download north-bed-before.jpg sha256 ok', '09:01:12 INFO auth: device desk-linux signed in (account sam, proof v2)', '07:55:40 WARN auth: failed sign-in for "admin" from 10.0.0.9', '07:12:03 INFO mdns: advertising _rfe._tcp on enp5s0'],
    search: {
      q: 'bed', saved: ['invoices 2025', 'video over 100 MB'], line: '6 results · scanned 48,210 items on studio-pc and work-laptop · skipped media-mini (not connected)',
      res: [['north-bed-after.jpg', 'img', '/home/sam/Projects/garden-plan', 'studio-pc', '1 Nov', '3.8 MB'], ['north-bed-before.jpg', 'img', '/home/sam/Projects/garden-plan', 'studio-pc', '12 Oct', '4.1 MB'], ['bed-layout.pdf', 'doc', '/home/sam/Projects/garden-plan', 'studio-pc', '29 Oct', '2.4 MB'], ['bed-layout-v2.pdf', 'doc', '/Users/sam/Documents/Garden', 'work-laptop', '30 Oct', '2.6 MB'], ['raised-beds', 'dir', '/home/sam/Photos/2025', 'studio-pc', '2 Sep', '38 items'], ['bedroom-paint.txt', 'txt', '/Users/sam/Desktop', 'work-laptop', '14 Aug', '1 KB']],
    },
    favs: [['garden-plan', 'studio-pc', '/home/sam/Projects/garden-plan', 'dir'], ['Thesis', 'work-laptop', '/Users/sam/Documents/Thesis', 'dir'], ['Photos', 'studio-pc', '/home/sam/Photos', 'dir']],
    recent: [['north-bed-after.jpg', 'studio-pc', 'garden-plan', '12 min ago'], ['planting-calendar.md', 'studio-pc', 'garden-plan', '1 h ago'], ['tax-2025.pdf', 'work-laptop', 'Documents', 'Yesterday']],
    offline: [['planting-calendar.md', 'Available offline · 6 KB', 100], ['bed-layout.pdf', 'Downloading 40% of 2.4 MB', 40]],
    big: [['walkthrough-full.mov', '/home/sam/Projects/garden-plan', 18.4], ['vm-fedora.qcow2', '/home/sam/VMs', 14.2], ['lisbon-raw.tar', '/srv/media/archive', 9.8], ['steam-library.tar', '/mnt/backup', 6.1], ['walkthrough.mp4', '/home/sam/Projects/garden-plan', 0.4]],
    dupes: [['north-bed-after.jpg', '2 copies · 7.6 MB', '/home/sam/Projects/garden-plan, /home/sam/Photos/2025'], ['invoice-1042.pdf', '3 copies · 288 KB', '/home/sam/Documents, /home/sam/Inbox, /mnt/backup/docs']],
    trash: [['old-sketches', 'dir', '/home/sam/Projects/garden-plan', '2 days ago', '12 items'], ['bed-layout-draft.pdf', 'doc', '/home/sam/Projects/garden-plan', '5 days ago', '2.1 MB'], ['IMG_2041.jpg', 'img', '/home/sam/Photos/2025', '6 weeks ago', '3.4 MB'], ['quote-oldsupplier.pdf', 'doc', '/home/sam/Projects/garden-plan/quotes', '7 weeks ago', '220 KB']],
    shares: [['north-bed-before.jpg', 'https://192.168.1.20:8765/s/tq4M9wXz', 'Ends in 22 h', 'studio-pc'], ['planting-calendar.md', 'https://192.168.1.20:8765/s/b81LkPae', 'Ends in 41 min', 'studio-pc']],
    rules: [['Garden photos', '~/Pictures/Garden', 'studio-pc:/home/sam/Projects/garden-plan/photos', 'up', 'Last run 2 h ago'], ['Thesis', '~/Documents/Thesis', 'work-laptop:/Users/sam/Documents/Thesis', 'down', 'Never run']],
    apps: [['GIMP', 'Image editor', 'Graphics', 1], ['Firefox', 'Web browser', 'Internet', 1], ['Kdenlive', 'Video editor', 'Video', 1], ['LibreOffice Calc', 'Spreadsheets', 'Office', 1], ['Files', 'File manager', 'System', 1], ['Steam', 'Games (no way to start it)', 'Games', 0]],
    notes: [['Pixel 9 asks to pair with studio-pc', 'Match code 4829 1307 · 1:42 left', 'now'], ['tax-2025.pdf downloaded', 'from work-laptop · checking the file', '2 min'], ['RFE 1.1.1 is available', 'Stable channel · About', 'Today']],
  };
  const S = (id) => D.servers.find((s) => s.id === id) || D.servers[0];
  const kindIcon = { dir: 'folder', img: 'image', doc: 'file', txt: 'text', sheet: 'sheet', vid: 'video', arc: 'archive', pkg: 'package', home: 'home', drive: 'drive', music: 'music', code: 'code' };

  /* A photo thumbnail drawn as content: flat garden scene in muted natural tones, no gradient. */
  const PAL = [['#c8d3d8', '#8a9d7b', '#6a8160', '#9b7f60', '#e8dcc0'], ['#d5d9d2', '#a2a678', '#7d8a55', '#8c6b4b', '#f0e2b6'], ['#cfd6dc', '#7f9a85', '#5c7766', '#a38463', '#efe6cf'], ['#dcd6cc', '#b09b78', '#8b7a5a', '#6e5a43', '#f4ead2']];
  function photo(seed = 0, w = 160, h = 110, r = 0) {
    const c = PAL[seed % PAL.length]; const s = seed * 37;
    return `<svg class="ph" viewBox="0 0 160 110" width="${w}" height="${h}" preserveAspectRatio="xMidYMid slice" style="border-radius:${r}px" aria-hidden="true"><rect width="160" height="110" fill="${c[0]}"/><circle cx="${40 + (s % 80)}" cy="26" r="9" fill="${c[4]}"/><path d="M0 62 Q40 ${44 + (s % 10)} 80 58 T160 ${50 + (s % 12)} V110 H0Z" fill="${c[1]}"/><path d="M0 78 Q50 66 100 76 T160 72 V110 H0Z" fill="${c[2]}"/><rect y="88" width="160" height="22" fill="${c[3]}"/><g fill="${c[2]}"><rect x="${18 + (s % 20)}" y="80" width="30" height="8" rx="2"/><rect x="${70 + (s % 15)}" y="82" width="44" height="6" rx="2"/></g></svg>`;
  }
  /* A QR-like module grid: deterministic, with the three finder squares. */
  function qr(size = 132, fg = 'currentColor') {
    const n = 25, m = size / n; let r = '', x = 7;
    const fin = (a, b) => (a < 7 && b < 7) || (a < 7 && b >= n - 7) || (a >= n - 7 && b < 7);
    for (let i = 0; i < n; i++) for (let j = 0; j < n; j++) { if (fin(i, j)) continue; x = (x * 1103515245 + 12345) & 0x7fffffff; if (x % 7 < 3) r += `<rect x="${j * m}" y="${i * m}" width="${m}" height="${m}"/>`; }
    const f = (a, b) => `<rect x="${b * m}" y="${a * m}" width="${7 * m}" height="${7 * m}" fill="none" stroke="${fg}" stroke-width="${m}" transform="translate(${m / 2} ${m / 2}) scale(${(6 * m) / (7 * m)})"/><rect x="${(b + 2) * m}" y="${(a + 2) * m}" width="${3 * m}" height="${3 * m}"/>`;
    return `<svg class="qr" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}" fill="${fg}" aria-label="Pairing QR code">${r}${f(0, 0)}${f(0, n - 7)}${f(n - 7, 0)}</svg>`;
  }

  /* ---------- small parts ---------- */
  const btn = (label, o = {}) => `<button class="k-btn${o.k ? ' ' + o.k : ''}"${o.a ? ` data-k="${o.a}"` : ''}${o.dis ? ' disabled' : ''}>${o.i ? I(o.i) : ''}${label ? `<span>${label}</span>` : ''}${o.kc ? `<kbd>${o.kc}</kbd>` : ''}</button>`;
  /* Icon buttons carry a styled tooltip (data-tip) instead of the slow native title: label, then shortcut. */
  const ib = (icon, label, a, extra = '') => `<button class="k-ib" aria-label="${label.replace(/ \(.*\)$/, '')}" data-tip="${label}"${a ? ` data-k="${a}"` : ''}>${I(icon)}${extra}</button>`;
  const chip = (label, on, a) => `<button class="k-chip${on ? ' on' : ''}"${a ? ` data-k="${a}"` : ''}>${label}</button>`;
  const sw = (on) => `<span class="k-sw${on ? ' on' : ''}" role="switch" aria-checked="${!!on}" tabindex="0"></span>`;
  const seg = (opts, cur) => `<span class="k-seg">${opts.map((o) => `<button class="${o === cur ? 'on' : ''}">${o}</button>`).join('')}</span>`;
  const tag = (s) => `<span class="k-tag" title="Pinned key">${I('key', 12)}${S(s).tag}</span>`;
  const dot = (k) => `<i class="k-dot ${k}"></i>`;
  const bar = (pct, k = '') => `<span class="k-bar ${k}"><i style="width:${pct}%"></i></span>`;
  const empty = (icon, t, s) => `<div class="k-empty">${EM[icon] || /_/.test(icon) ? E(icon, 32) : I(icon, 20)}<b>${t}</b><small>${s}</small></div>`;
  const head = (t, sub, right = '') => `<header class="k-ph"><div><h1 class="k-title">${t}</h1>${sub ? `<p class="k-sub">${sub}</p>` : ''}</div><div class="k-ph-r">${right}</div></header>`;
  const sec = (t, right = '', body = '', cls = '') => `<section class="k-card ${cls}"><div class="k-ch"><h2>${t}</h2><div>${right}</div></div>${body}</section>`;
  const fic = (k) => `<span class="k-fic k-fic-${k}">${E(EM[k] ? k : 'doc', 18)}</span>`;
  const OS = (s) => (/Windows/.test(s.sys) ? E('window', 14, 'Windows') : E('penguin', 14, 'Linux'));
  const mach = (s, n = 20) => E(s.id === 'work' ? 'laptop' : 'server', n);

  /* ---------- views ---------- */
  const V = {};
  V.files = (o = {}) => {
    const sv = S(o.host || 'studio'); const grid = o.mode === 'grid';
    const trail = (o.panel === false ? D.path : ['…', ...D.path.slice(-2)]).map((s, i, a) => i === a.length - 1 ? `<b>${s}</b>` : `<button data-k="go:files" title="${s === '…' ? '/home/sam' : ''}">${s === '/' ? 'Root' : s}</button>`).join(`<span class="sep">/</span>`);
    const rows = D.files.map((f) => `<div class="k-frow${f.n === D.sel ? ' sel' : ''}" data-row="${esc(f.n)}" tabindex="0" ${f.k === 'img' ? 'data-k="dlg:preview"' : f.k === 'txt' ? 'data-k="dlg:editor"' : ''}><span class="k-ck">${f.n === D.sel ? I('check', 12) : ''}</span>${fic(f.k)}<span class="nm">${f.n}${f.k === 'dir' ? `<small>${f.c} items</small>` : ''}</span><span class="sz">${f.k === 'dir' ? '' : f.s}</span><span class="md">${f.m}</span><span class="pm">${f.pm}</span>${ib('more', 'More for ' + f.n, 'menu:file')}</div>`).join('');
    const tiles = D.files.map((f) => `<div class="k-tile${f.n === D.sel ? ' sel' : ''}" data-row="${esc(f.n)}" tabindex="0">${f.k === 'img' ? photo(f.seed, 160, 104) : `<span class="k-tth">${E(f.k, 44)}</span>`}<b>${f.n}</b><small>${f.k === 'dir' ? f.c + ' items' : f.s}</small></div>`).join('');
    /* Fewer controls: Forward lives on Alt+Right and the palette; the trail itself is the path field
       (click its empty end); Refresh is automatic (F5 still works). */
    const top = `<div class="k-ftop"><span class="k-nav">${ib('left', 'Back (Alt+Left)', 'go:files')}${ib('up', 'Up one folder (Alt+Up)', 'go:locations')}</span><nav class="k-trail k-trail-edit" aria-label="Path" data-tip="Click the empty end to type a path (Ctrl+L)"><button data-k="go:locations" class="host">${dot('ok')}${sv.name}</button><span class="sep">:</span>${trail}</nav>${ib('star', 'Add this folder to favorites')}<span class="sp"></span>${ib('folder-plus', 'New folder (Ctrl+Shift+N)', 'dlg:newfolder')}${btn('Upload', { i: 'upload', k: 'sm pri', a: 'dlg:upload' })}</div>`;
    const tools = `<div class="k-fbar"><label class="k-in sm k-grow">${I('filter', 14)}<input placeholder="Filter this folder" aria-label="Filter this folder"></label><select class="k-sel" aria-label="Show">${['Everything', 'Folders', 'Packages', 'Media', 'Archives', 'Over 100 MB'].map((c) => `<option>${c}</option>`).join('')}</select>${ib(grid ? 'list' : 'grid', grid ? 'Show as a list' : 'Show as tiles', 'view:' + (grid ? 'list' : 'grid'))}</div>`;
    const bulk = o.bulk ? `<div class="k-bulk">${ib('x', 'Clear selection (Esc)')}<b>2 selected · 7.9 MB</b>${btn('Download', { i: 'download', k: 'sm pri', kc: 'Ctrl D' })}${btn('Copy or move…', { i: 'move', k: 'sm', a: 'dlg:copyto' })}${btn('Delete', { i: 'trash', k: 'sm', a: 'dlg:delete' })}${ib('more', 'Rename several, compress to .zip', 'menu:selmore')}</div>` : '';
    const list = grid ? `<div class="k-tiles">${tiles}</div>` : `<div class="k-fhead"><span></span><span></span><button>Name ${I('up', 12)}</button><button>Size</button><button>Modified</button><span>Permissions</span><span></span></div><div class="k-flist" role="grid">${rows}</div>`;
    const foot = `<div class="k-ffoot"><span>11 items · 1 selected</span><span>812 GB free of 2 TB</span><span class="sp"></span><span class="mut">Hidden files are off · Settings, Files</span></div>`;
    const panel = o.panel === false ? '' : `<aside class="k-side">${V.sidepanel(o.panel || 'details')}</aside>`;
    return `<div class="k-files${o.panel === false ? ' nopanel' : ''}"><div class="k-fmain">${o.notop ? '' : top}${o.notools ? '' : tools}${bulk}${list}${foot}</div>${panel}</div>`;
  };
  V.sidepanel = (t = 'details') => {
    const f = D.files.find((x) => x.n === D.sel);
    const tabs = `<div class="k-tabs"><button class="${t === 'details' ? 'on' : ''}" data-k="panel:details">Details</button><button class="${t === 'local' ? 'on' : ''}" data-k="panel:local">Local files</button></div>`;
    if (t === 'local') return tabs + `<div class="k-local"><div class="k-trail sm"><b>~/Downloads</b></div>${D.local.map((l) => `<div class="k-lrow" data-row="${l.n}">${fic(l.k)}<span>${l.n}</span><small>${l.k === 'dir' ? l.c + ' items' : l.s}</small></div>`).join('')}<div class="k-drop">${I('transfer')}<span>Drag files to the server list to upload, or from it to download</span></div>${btn('Upload selected', { i: 'upload', k: 'sm', kc: 'Ctrl U' })}</div>`;
    return tabs + `<div class="k-det"><div class="k-dprev" data-k="dlg:preview" data-tip="Open the preview (Space)">${photo(f.seed, 300, 190, 8)}</div><h3>${E('img', 16)}${f.n}</h3><dl class="k-kv"><dt>Size</dt><dd>${f.s} · 4032 × 3024</dd><dt>Modified</dt><dd>${f.m}</dd><dt>Permissions</dt><dd class="mono">${f.pm}</dd><dt>Where</dt><dd>studio-pc · /home/sam/Projects/garden-plan</dd></dl><div class="k-dact">${btn('Download', { i: 'download', k: 'pri', kc: 'Ctrl D' })}<div class="k-row">${btn('Share link…', { i: 'link', a: 'dlg:share' })}${ib('more', 'Offline, properties, checksum, copy path', 'menu:detmore')}</div></div></div>`;
  };
  V.locations = () => head('studio-pc', `Where this login may start · ${tag('studio')}`) + `<div class="k-list">${D.roots.map((r) => `<div class="k-lrow big" data-k="go:files">${fic(r.k)}<span>${r.n}</span><small>${r.note}</small>${I('chevr')}</div>`).join('')}</div><p class="k-note">${I('info', 14)}The agent confines this login to these folders. Other logins may see different ones.</p>`;
  V.servers = () => {
    const card = (s) => {
      const disk = Math.round((s.used / s.total) * 100); const fmt = (g) => (g >= 1000 ? (g / 1000).toFixed(1) + ' TB' : g + ' GB');
      /* One action per state; the rest sit in the menu. Offline servers are retried on their own. */
      const acts = s.state === 'Connected' ? btn('Open files', { i: 'folder', k: 'pri sm', a: 'go:files' }) : s.state === 'Key changed' ? btn('Review key…', { i: 'alert', k: 'sm warn', a: 'dlg:keychanged' }) : btn('Wake computer', { i: 'power', k: 'sm', a: 'toast:Wake signal sent to media-mini' }) + `<small class="mut">Retried every minute</small>`;
      return `<article class="k-srv st-${s.dot}"><div class="k-srv-h">${mach(s, 28)}<div><b>${s.name}</b><small class="mono">${s.addr}</small></div><span class="k-state">${dot(s.dot)}${s.state}</span></div>${s.state === 'Key changed' ? `<p class="k-warn">${I('alert', 14)}This agent’s certificate is not the one you trusted. Its files and login are not used until you compare again.</p>` : ''}<dl class="k-kv cols"><dt>Route</dt><dd>${s.route}</dd><dt>System</dt><dd class="k-os">${OS(s)}${s.sys}</dd><dt>Agent</dt><dd>${s.ver}</dd><dt>Response</dt><dd class="mono">${s.ms ? s.ms + ' ms' : (s.last || '—')}</dd><dt>Sign-in</dt><dd>${s.login}</dd><dt>Key</dt><dd>${tag(s.id)}</dd></dl><div class="k-disk"><span>Disk</span><span class="mono">${fmt(s.used)} of ${fmt(s.total)}</span></div>${bar(disk)}<div class="k-srv-a">${acts}<span class="sp"></span>${ib('more', 'More for ' + s.name, 'menu:server')}</div></article>`;
    };
    return head('Servers', `${D.servers.length} saved · 2 connected · browsing studio-pc`, btn('New connection', { i: 'plug', k: 'pri', a: 'dlg:newconn' })) + `<div class="k-srvs">${D.servers.map(card).join('')}</div>`;
  };
  V.pairPhone = (compact) => `<div class="k-pair${compact ? ' compact' : ''}"><div class="k-qr">${qr(compact ? 112 : 140)}</div><div class="k-pairtx"><small>Code for studio-pc · works once</small><div class="k-code mono">${D.pair.code}</div><small>${D.pair.left} left · lifetime ${D.pair.life}</small><p>On the phone: RFE for Android, Add computer, Scan QR. It starts with browse access only.</p><div class="k-row">${btn('Copy code', { i: 'copy', k: 'sm' })}${ib('more', 'Copy code data, save QR, new code', 'menu:pair')}</div></div></div>`;
  V.requests = () => `<div class="k-reqs">${D.requests.map((r, i) => `<div class="k-req"><div class="who">${E('phone', 24)}<div><b>${r.n}</b><small>from <span class="mono">${r.from}</span> · waiting ${r.wait} · ${r.exp} left${r.over ? ` · takes over <b>${r.over}</b>` : ''}</small></div></div><div class="k-match mono" title="Match code">${r.code}</div><div class="k-row">${btn('Accept…', { k: 'pri sm', a: 'dlg:accept' })}${btn('Reject', { k: 'sm', a: 'toast:Rejected ' + r.n })}</div></div>`).join('')}<p class="k-note">${I('info', 14)}Accept only if the code matches the one shown on the device asking. The name is whatever it sent. The agent holds at most 3 requests.</p></div>`;
  V.deviceList = () => `<div class="k-tbl k-devs"><div class="th"><span>Device</span><span>Sign-in</span><span>Last seen</span><span>May do</span><span>Folder limit</span><span></span></div>${D.devices.map((d) => `<div class="tr${d.revoked ? ' revoked' : ''}"><span class="dn">${E(d.kind, 20)}<b>${d.n}</b>${d.me ? '<em class="k-pill">This app</em>' : ''}${d.revoked ? '<em class="k-pill bad">Revoked</em>' : ''}</span><span>${d.how}</span><span>${d.seen}</span><span class="t2">${d.may}</span><span class="mono t2">${d.lim || '—'}</span><span class="a">${d.revoked ? btn('Remove', { k: 'sm ghost', a: 'dlg:remove' }) : btn('Permissions…', { k: 'sm', a: 'dlg:perms' }) + ib('more', 'More for ' + d.n, 'menu:device')}</span></div>`).join('')}</div>`;
  V.events = (n = 4) => `<div class="k-evs">${D.events.slice(0, n).map((e) => `<div class="k-ev ${e[3]}"><time class="mono">${e[0]}</time><b>${e[1]}</b><span>${e[2]}</span></div>`).join('')}</div>`;
  V.devices = () => head('Devices', `Phones and computers allowed to open studio-pc’s files`, seg(['studio-pc', 'work-laptop', 'nas'], 'studio-pc')) +
    `<div class="k-g2">${sec('Pair a phone', ib('qr', 'Open in a dialog', 'dlg:pairphone'), V.pairPhone(true))}${sec('Pairing requests <span class="k-count">2</span>', `<small class="mut">refreshes every few seconds</small>`, V.requests())}</div>` +
    sec('Paired devices', btn('Sign out all phones', { k: 'sm ghost', a: 'dlg:signoutall' }), V.deviceList()) + sec('Security events', btn('View all', { k: 'sm ghost', a: 'go:history' }), V.events(4));
  const xcard = (t, grp) => {
    const st = grp === 'attention' ? t.st : grp; const icon = t.d === 'up' ? 'upload' : 'download';
    const act = st === 'conflict' ? `<div class="k-row">${btn('Replace', { k: 'sm' })}${btn('Keep both', { k: 'sm pri' })}${btn('Skip', { k: 'sm' })}<label class="k-cb"><input type="checkbox"> Apply to all</label></div>` : st === 'paused' ? `<div class="k-row">${btn('Resume', { i: 'play', k: 'sm' })}${btn('Cancel', { k: 'sm ghost' })}</div>` : st === 'failed' ? `<div class="k-row">${btn('Retry', { i: 'refresh', k: 'sm' })}${btn('Cancel', { k: 'sm ghost' })}</div>` : grp === 'done' ? `<div class="k-row">${btn('Show in folder', { k: 'sm ghost' })}</div>` : `<div class="k-row">${ib('pause', 'Pause')}${ib('x', 'Cancel')}</div>`;
    return `<div class="k-xc ${st}">${I(icon)}<div class="k-xm"><b>${t.n}</b><small>${t.d === 'up' ? 'to' : 'from'} ${t.where}</small>${grp !== 'done' && st !== 'conflict' && st !== 'failed' ? bar(t.pct, st === 'paused' ? 'off' : '') : ''}<small class="${st === 'failed' ? 'bad' : st === 'conflict' || st === 'paused' ? 'warn' : grp === 'done' ? 'ok' : ''}">${grp === 'done' ? I('shield', 12) : ''}${t.msg}</small></div>${act}</div>`;
  };
  /* Group actions sit on the group they act on, not in the page header. */
  const XG = { attention: () => btn('Retry failed', { i: 'refresh', k: 'sm ghost' }), done: () => btn('Clear', { k: 'sm ghost' }) };
  V.xgroups = (only) => [['attention', 'Needs attention'], ['active', 'Active'], ['queued', 'Queued'], ['done', 'Completed']].filter((g) => !only || only.includes(g[0])).map((g) => `<div class="k-xg"><h3>${g[1]} <span class="mono mut">${D.xfer[g[0]].length}</span><span class="sp"></span>${!only && XG[g[0]] ? XG[g[0]]() : ''}</h3>${D.xfer[g[0]].map((t) => xcard(t, g[0])).join('')}</div>`).join('');
  V.xstats = () => `<div class="k-stats">${D.xfer.stats.map((s) => `<div><b class="mono">${s[0]}</b><small>${s[1]}</small></div>`).join('')}</div>`;
  V.transfers = () => head('Transfers', 'Everything moving between this computer and your servers', btn('Pause all', { i: 'pause', k: 'sm' })) + V.xstats() + V.xgroups() + `<div class="k-journal">${E('scroll', 24)}<div><b>Transfer journal</b><small>1 unfinished upload from the last session: garden-raw.tar to studio-pc</small></div>${btn('Queue again', { k: 'sm' })}</div>`;
  V.search = () => { const s = D.search; return head('Search', '', `<label class="k-in">${I('search', 14)}<input value="${s.q}" aria-label="Search"></label>${ib('star', 'Save this search')}`) + `<div class="k-fbar">${seg(['This server', 'All connected servers', 'This computer'], 'All connected servers')}<select class="k-sel" aria-label="Type">${['Everything', 'Folders', 'Packages', 'Media', 'Archives', 'Over 100 MB'].map((c) => `<option>${c}</option>`).join('')}</select><span class="sp"></span><span class="mut">Saved:</span>${s.saved.map((x) => `<span class="k-saved">${x}<button aria-label="Remove saved search">${I('x', 12)}</button></span>`).join('')}</div><p class="k-prog">${I('check', 14)}${s.line}</p><div class="k-tbl k-res">${s.res.map((r) => `<div class="tr" data-k="go:files">${fic(r[1])}<span><b>${r[0].replace(s.q, `<mark>${s.q}</mark>`)}</b><small class="mono">${r[2]}</small></span><span class="k-hostc">${dot('ok')}${r[3]}</span><span class="t2">${r[4]}</span><span class="t2 mono r">${r[5]}</span></div>`).join('')}</div>`; };
  const TOOLT = [['fav', 'star', 'Favorites & offline'], ['store', 'file_cabinet', 'Storage'], ['trash', 'wastebasket', 'Trash', 4], ['share', 'link', 'Share links', 2], ['sync', 'counterclockwise_arrows_button', 'Backup & sync'], ['apps', 'toolbox', 'Apps']];
  V.toolTabs = (t) => `<div class="k-ttabs" role="tablist">${TOOLT.map((x) => `<button class="k-chip${t === x[0] ? ' on' : ''}" data-k="tool:${x[0]}">${E(x[1], 16)}${x[2]}${x[3] ? ` <span class="mono mut">${x[3]}</span>` : ''}</button>`).join('')}</div>`;
  V.tool = {};
  /* Rows open on click; the one secondary action per row shows on hover. */
  V.tool.fav = () => `<div class="k-g2">${sec('Favorites', btn('Add current folder', { i: 'plus', k: 'sm' }), `<div class="k-list">${D.favs.map((f) => `<div class="k-lrow big" data-k="go:files">${E('dir', 20)}<span><b>${f[0]}</b><small>${f[1]} · ${f[2]}</small></span><span class="k-hov">${ib('x', 'Remove from favorites')}</span></div>`).join('')}</div>`)}${sec('Recent files', '', `<div class="k-list">${D.recent.map((r) => `<div class="k-lrow big" data-k="go:files">${E(/\.jpg$/.test(r[0]) ? 'img' : /\.md$/.test(r[0]) ? 'txt' : 'doc', 20)}<span><b>${r[0]}</b><small>${r[1]} · ${r[2]} · ${r[3]}</small></span>${I('chevr')}</div>`).join('')}</div>`)}</div>` + sec('Available offline', '<small class="mut">Right-click a file and choose Make available offline</small>', `<div class="k-list">${D.offline.map((o) => `<div class="k-lrow">${E(/\.md$/.test(o[0]) ? 'txt' : 'doc', 20)}<span><b>${o[0]}</b><small>${o[1]}</small>${o[2] < 100 ? bar(o[2]) : ''}</span><span class="k-hov">${ib('x', 'Stop keeping offline')}</span></div>`).join('')}</div>`);
  V.tool.store = () => `<div class="k-fbar">${seg(['studio-pc', 'work-laptop'], 'studio-pc')}</div><div class="k-stats">${[['812 GB', 'used on /home/sam'], ['1.1 TB', 'used on /srv/media'], ['3.2 GB', 'uploaded this session'], ['1', 'failed transfer']].map((s) => `<div><b class="mono">${s[0]}</b><small>${s[1]}</small></div>`).join('')}</div>` + sec('Space by folder', '', `<div class="k-stack"><i style="flex:812;background:var(--ac)"></i><i style="flex:1100;background:var(--t2)"></i><i style="flex:96;background:var(--t3)"></i><span style="flex:1992"></span></div><div class="k-legend"><span><i class="k-dot" style="background:var(--ac)"></i>/home/sam 812 GB</span><span><i class="k-dot" style="background:var(--t2)"></i>/srv/media 1.1 TB</span><span><i class="k-dot" style="background:var(--t3)"></i>/mnt/backup 96 GB</span><span class="mut">1.9 TB free of 4 TB</span></div>`) + `<div class="k-g2">${sec('Largest files', btn('Scan again', { i: 'refresh', k: 'sm ghost' }), `<div class="k-list">${D.big.map((b) => `<div class="k-lrow">${fic(b[0].endsWith('qcow2') ? 'drive' : b[0].endsWith('tar') ? 'arc' : 'vid')}<span><b>${b[0]}</b><small class="mono">${b[1]}</small>${bar((b[2] / 18.4) * 100)}</span><span class="mono t2">${b[2]} GB</span></div>`).join('')}</div>`)}${sec('Duplicates', btn('Find duplicates', { k: 'sm ghost' }), `<div class="k-list">${D.dupes.map((d) => `<div class="k-lrow big" data-k="go:files">${E(/\.jpg$/.test(d[0]) ? 'img' : 'doc', 20)}<span><b>${d[0]}</b><small>${d[1]}</small><small class="mono">${d[2]}</small></span>${I('chevr')}</div>`).join('')}</div>`)}</div>`;
  V.tool.trash = () => `<div class="k-banner">${I('clock')}<span><b>Delete old items from Trash?</b> 2 items were deleted more than 30 days ago (Settings, Files).</span>${btn('Delete them', { k: 'sm' })}${btn('Keep them', { k: 'sm ghost' })}</div>` + sec('Trash on studio-pc', `<small class="mut">1 selected</small>` + btn('Restore', { i: 'undo', k: 'sm pri' }) + ib('more', 'Delete forever, empty Trash', 'menu:trash'), `<div class="k-tbl k-trash"><div class="th"><span></span><span>Name</span><span>Deleted from</span><span>Deleted</span><span class="r">Size</span></div>${D.trash.map((t, i) => `<div class="tr${i === 1 ? ' sel' : ''}"><span class="k-ck">${i === 1 ? I('check', 12) : ''}</span><span class="dn">${fic(t[1])}<b>${t[0]}</b></span><span class="mono t2">${t[2]}</span><span class="t2">${t[3]}</span><span class="mono t2 r">${t[4]}</span></div>`).join('')}</div>`);
  V.tool.share = () => sec('Live share links', '<small class="mut">Make one from a file: Share link…</small>', `<div class="k-list">${D.shares.map((s) => `<div class="k-lrow">${E(/\.jpg$/.test(s[0]) ? 'img' : 'txt', 20)}<span><b>${s[0]}</b><small class="mono">${s[1]}</small></span><span class="t2">${s[2]}</span>${btn('Copy link', { i: 'copy', k: 'sm' })}${ib('power', 'Turn this link off', 'dlg:turnoff')}</div>`).join('')}</div>`);
  V.tool.sync = () => sec('Sync rules', btn('New rule', { i: 'plus', k: 'sm pri', a: 'dlg:newrule' }), `<p class="k-sub">One-way folder pairs. Run now copies only files that are new or different in size; nothing is deleted.</p><div class="k-list">${D.rules.map((r) => `<div class="k-lrow">${E(r[3] === 'up' ? 'outbox_tray' : 'inbox_tray', 20)}<span><b>${r[0]}</b><small class="mono">${r[1]} ${r[3] === 'up' ? '→' : '←'} ${r[2]}</small><small>${r[4]}</small></span>${btn('Run now', { i: 'play', k: 'sm' })}<span class="k-hov">${ib('trash', 'Delete this rule')}</span></div>`).join('')}</div>`) + `<p class="k-note">${I('info', 14)}The settings backup lives in Settings, Backup.</p>`;
  /* A whole card launches; there is no separate Launch button or Refresh (the list loads on open). */
  const APPE = { GIMP: 'artist_palette', Firefox: 'globe_with_meridians', Kdenlive: 'film_frames', 'LibreOffice Calc': 'bar_chart', Files: 'file_cabinet', Steam: 'puzzle_piece' };
  V.tool.apps = () => `<div class="k-fbar">${seg(['studio-pc', 'work-laptop'], 'studio-pc')}<span class="sp"></span><label class="k-in sm">${I('filter', 14)}<input placeholder="Filter apps" aria-label="Filter apps"></label></div><div class="k-apps">${D.apps.map((a) => `<button class="k-app${a[3] ? '' : ' dim'}"${a[3] ? ` data-k="dlg:launch" data-tip="Open ${a[0]} on studio-pc’s screen"` : ' disabled'}><span class="k-appic">${E(APPE[a[0]], 24)}</span><div><b>${a[0]}</b><small>${a[1]}</small><small class="mut">${a[2]}</small></div>${a[3] ? `<span class="k-go">${I('play', 14)}Launch</span>` : '<small class="mut">Cannot be launched</small>'}</button>`).join('')}</div><p class="k-note">${I('info', 14)}Apps open on studio-pc’s own screen. This computer has the rights see apps and start apps there.</p>`;
  V.tools = (t = 'fav') => head('Tools', 'Favorites, storage, trash, links, sync and the PC’s apps') + V.toolTabs(t) + `<div class="k-tbody">${V.tool[t]()}</div>`;
  /* Each kind of event has one emoji, used on the filter and on every row, so a row reads at a glance. */
  const HK = { Transfers: 'updown_arrow', Connections: 'antenna_bars', Security: 'locked', Errors: 'warning' };
  V.history = (f = 'All') => head('History', 'This app’s activity and, under Security, the agent’s audit log', btn('Agent log', { i: 'text', k: 'sm ghost', a: 'dlg:agentlog' }) + ib('trash', 'Clear history')) + `<div class="k-fbar">${['All', 'Transfers', 'Connections', 'Security', 'Errors'].map((c) => `<button class="k-chip${c === f ? ' on' : ''}" data-k="hist:${c}">${HK[c] ? E(HK[c], 14) : ''}${c}</button>`).join('')}<span class="sp"></span>${f === 'Security' ? '<select class="k-sel" aria-label="Event"><option>Any event</option><option>Sign-in</option><option>Pairing</option></select>' : ''}<select class="k-sel" aria-label="When"><option>Today</option><option>Last 7 days</option><option>Everything loaded</option></select></div><div class="k-hist">${D.history.filter((h) => f === 'All' || h[1] === f).map((h) => `<div class="k-ev ${h[3]}"><time class="mono" title="2026-11-01 ${h[0]}:00">${h[0]}</time><span class="k-hk">${E(HK[h[1]], 14)}${h[1]}</span><span>${h[2]}</span></div>`).join('')}</div>`;
  const SETS = ['Appearance', 'Language and accessibility', 'Transfers', 'Connection', 'Files', 'Security', 'Desktop', 'Backup', 'Troubleshooting', 'Help', 'About'];
  const row = (t, s, c) => `<div class="k-set"><div><b>${t}</b>${s ? `<small>${s}</small>` : ''}</div><div>${c}</div></div>`;
  const SB = {
    Appearance: () => row('Theme', 'Follows the system unless you choose', seg(['System', 'Light', 'Dark'], 'System')) + row('Row density', '', seg(['Comfortable', 'Compact'], 'Comfortable')) + row('Interface size', 'On top of the scale the window already has', seg(['90%', '100%', '115%', '130%'], '100%')) + row('Open files and folders', 'One click opens folders and shows previewable files', seg(['One click', 'Two clicks'], 'One click')),
    'Language and accessibility': () => row('Language', '', `<select class="k-sel"><option>English</option><option>Deutsch</option><option>Español</option><option>العربية (right to left)</option></select>`) + row('Reduce motion', '', sw(0)) + row('High contrast', '', sw(0)),
    Transfers: () => row('Parallel transfers', 'The others wait for a free slot', seg(['1', '2', '3', '4'], '2')) + row('Speed limit', 'Shared by all transfers', seg(['None', '10 MB/s', '25 MB/s', '50 MB/s'], 'None')) + row('When a name already exists', '', seg(['Ask', 'Replace', 'Keep both', 'Skip'], 'Ask')) + row('Verify checksums', 'Compare a finished download with the server’s SHA-256', sw(1)) + row('Download folder', '<span class="mono">~/Downloads/RFE Desktop</span>', btn('Change…', { k: 'sm' }) + btn('Reset', { k: 'sm ghost' })),
    Connection: () => row('Reconnect automatically', 'Retry a lost server and resume its transfers', sw(1)) + row('Notifications inside the app', '', sw(1)),
    Files: () => row('Move deleted items to Trash', 'Off makes a delete permanent', sw(1)) + row('Keep items in Trash for', 'Asked when you open Trash', seg(['7 days', '30 days', '90 days'], '30 days')) + row('Ask where to save each download', '', sw(0)) + row('Show hidden files', '', sw(0)),
    Security: () => row('Approve new devices here', 'The app never approves by itself', sw(1)) + row('Pairing code lifetime', 'A code also works only once', seg(['2 min', '5 min', '10 min'], '5 min')) + row('Pinned certificates', '4 servers', btn('Show…', { k: 'sm', a: 'dlg:pins' })) + row('Sign out all phones', 'Revokes code and approved devices of studio-pc', btn('Sign out all phones…', { k: 'sm warn', a: 'dlg:signoutall' })),
    Desktop: () => row('Close to the system tray', 'Transfers go on while the window is hidden', sw(1)) + row('Start RFE when I sign in', 'Starts hidden in the tray (Linux)', sw(0)) + row('Desktop notifications', 'Only while the window is not in front', sw(1)) + row('Test notification', '', btn('Test notification', { k: 'sm' })),
    Backup: () => row('Back up settings', 'A password-encrypted file; it holds no password, token or key', btn('Back up…', { k: 'sm', a: 'dlg:backup' })) + row('Restore settings', '', btn('Restore…', { k: 'sm' })),
    Troubleshooting: () => row('Log detail', 'The last 500 events, kept in memory only', seg(['Normal', 'Detailed'], 'Normal')) + row('Agent log', 'Recent lines of studio-pc’s own log', btn('Agent log', { k: 'sm', a: 'dlg:agentlog' })) + row('Check the keystore', 'Saves, reads back and removes a test secret', btn('Check', { k: 'sm' })) + row('Device key', 'Make this computer a new device at the next sign-in', btn('New key…', { k: 'sm warn' })),
    Help: () => `<div class="k-keys">${[['/', 'Search'], ['Ctrl K', 'Command palette'], ['Ctrl L', 'Type a path'], ['Ctrl D', 'Download'], ['Ctrl U', 'Upload'], ['Ctrl Shift N', 'New folder'], ['F2', 'Rename'], ['Delete', 'Move to Trash'], ['F5', 'Refresh'], ['Alt ←', 'Back'], ['Alt →', 'Forward'], ['Alt ↑', 'Up'], ['Ctrl A', 'Select all'], ['Esc', 'Close a dialog or menu']].map((k) => `<div><span>${k[0].split(' ').map((x) => `<kbd>${x}</kbd>`).join('')}</span>${k[1]}</div>`).join('')}</div>`,
    About: () => row('RFE Desktop', `Version ${D.me.version} · Linux x86_64 · AppImage`, btn('Details', { k: 'sm ghost' })) + row('Check for updates', 'Asks GitHub’s public list of releases; the app never installs anything', seg(['Stable', 'Beta'], 'Stable') + btn('Check now', { k: 'sm', a: 'dlg:updates' })) + row('Check automatically', 'Once a day while the app runs', sw(0)) + row('Report a problem', 'Builds a diagnostics report without secrets', btn('Report a problem…', { k: 'sm', a: 'dlg:report' })) + row('Welcome tour', '', btn('Show the welcome screen', { k: 'sm ghost', a: 'dlg:welcome' })),
  };
  const SE = { Appearance: 'artist_palette', 'Language and accessibility': 'globe_with_meridians', Transfers: 'updown_arrow', Connection: 'antenna_bars', Files: 'card_index_dividers', Security: 'locked_with_key', Desktop: 'desktop_computer', Backup: 'floppy_disk', Troubleshooting: 'wrench', Help: 'keyboard', About: 'information' };
  V.settings = (s = 'Appearance') => head('Settings') + `<div class="k-settings"><nav class="k-snav">${SETS.map((x) => `<button class="${x === s ? 'on' : ''}" data-k="set:${x}">${E(SE[x], 16)}${x}</button>`).join('')}</nav><div class="k-sbody"><h2>${E(SE[s], 20)}${s}</h2>${SB[s]()}</div></div>`;
  V.settingsAll = () => SETS.map((x) => `<section class="k-sgroup"><h2>${E(SE[x], 20)}${x}</h2>${SB[x]()}</section>`).join('');

  /* ---------- shell parts a design can place anywhere ---------- */
  const C = {};
  C.navItems = [['files', 'folder', 'Files'], ['servers', 'server', 'Servers'], ['devices', 'phone', 'Devices', 2], ['transfers', 'transfer', 'Transfers', 3], ['search', 'search', 'Search'], ['tools', 'layers', 'Tools'], ['history', 'history', 'History'], ['settings', 'sliders', 'Settings']];
  C.nav = (cur, o = {}) => C.navItems.filter((n) => !o.skip || !o.skip.includes(n[0])).map((n) => `<button class="k-navi${n[0] === cur ? ' on' : ''}" data-k="go:${n[0]}" aria-label="${n[2]}">${I(n[1], o.size || 18)}${o.icons ? '' : `<span>${n[2]}</span>`}${n[3] ? `<em class="k-badge${n[0] === 'devices' ? ' bad' : ''}">${n[3]}</em>` : ''}</button>`).join('');
  C.switcher = (host = 'studio') => { const s = S(host); return `<button class="k-switch" data-k="menu:browse">${dot(s.dot)}<b>${s.name}</b><small class="mono">${s.ms} ms · ${s.route}</small>${I('chevd', 14)}</button>`; };
  /* One field for finding files and running commands: it opens the palette, whose first row searches. */
  C.searchBox = (ph = 'Search files or run a command') => `<button class="k-in k-sbox" data-k="dlg:palette" aria-label="Search files or run a command">${I('search', 14)}<span class="k-sph">${ph}</span><kbd>Ctrl K</kbd></button>`;
  /* Three top actions instead of six: Refresh is automatic (F5), Theme moved to the account menu,
     the palette button merged into the search field. */
  C.actions = () => [btn('Pair a phone', { i: 'qr', k: 'sm ghost', a: 'dlg:pairphone' }), ib('bell', 'Notifications: 3 new', 'menu:notes', '<i class="k-nb"></i>'), `<button class="k-av" data-k="menu:account" aria-label="Account and theme" data-tip="sam · account and theme">S</button>`].join('');
  C.ring = (pct = 64) => `<svg class="k-ring" width="18" height="18" viewBox="0 0 36 36"><circle cx="18" cy="18" r="15" fill="none" stroke="var(--line2)" stroke-width="4"/><circle cx="18" cy="18" r="15" fill="none" stroke="var(--ac)" stroke-width="4" stroke-dasharray="${pct * 0.94} 100" transform="rotate(-90 18 18)" stroke-linecap="round"/></svg>`;
  C.pill = () => `<button class="k-xpill" data-k="dock" data-tip="Open the transfers dock">${C.ring(64)}<span>2 moving</span><small class="warn">3 need you</small></button>`;
  C.dock = () => `<div class="k-dock"><div class="k-tabs">${['Active 2', 'Needs attention 3', 'Queued 1', 'Done 2'].map((t, i) => `<button class="${i === 1 ? 'on' : ''}">${t.replace(/ (\d)$/, ' <span class="mono mut">$1</span>')}</button>`).join('')}<span class="sp"></span>${ib('pause', 'Pause all transfers')}${ib('x', 'Close (Esc)', 'dock')}</div><div class="k-dockb">${V.xgroups(['attention'])}</div></div>`;
  C.tray = () => `<div class="k-menu tray"><button data-k="go:files"><b>Open RFE</b></button><div class="k-mt">2 transfers running</div><button>${I('pause')}Pause all transfers</button><button data-k="dlg:pairphone">${I('qr')}Pair a phone…</button><hr><button>Quit RFE</button></div>`;

  /* ---------- dialogs ---------- */
  const dlg = (title, body, acts = '', w = 520) => `<div class="k-dlg" role="dialog" aria-label="${esc(title.replace(/<[^>]+>/g, ''))}" style="width:${w}px"><div class="k-dh"><h2>${title}</h2>${ib('x', 'Close (Esc)', 'close')}</div><div class="k-db">${body}</div>${acts ? `<div class="k-da">${acts}</div>` : ''}</div>`;
  const field = (l, v, o = {}) => `<label class="k-field"><span>${l}</span><span class="k-in${o.mono ? ' mono' : ''}"><input value="${esc(v)}"${o.ph ? ` placeholder="${o.ph}"` : ''}></span>${o.hint ? `<small>${o.hint}</small>` : ''}</label>`;
  const fp = (s) => { const g = s.split(' '); const rows = []; for (let i = 0; i < g.length; i += 4) rows.push(g.slice(i, i + 4).join(' ')); return `<div class="k-fp mono">${rows.join('<br>')}</div>`; };
  const DLG = {
    newconn: (t = 'Manual') => dlg('New connection', `<div class="k-tabs">${['Manual', 'Find on network', 'Pairing code'].map((x) => `<button class="${x === t ? 'on' : ''}" data-k="dlg:newconn:${x}">${x}</button>`).join('')}</div>` + (t === 'Manual' ? field('Name', 'garage-pi') + `<div class="k-g2 tight">${field('Address', '192.168.1.44', { mono: 1 })}${field('Port', '8765', { mono: 1 })}</div><p class="k-note">${I('info', 14)}Nothing is sent before you compare the key. Use host:port; put an IPv6 address in brackets.</p>` : t === 'Find on network' ? `<div class="k-list">${[['garage-pi', '192.168.1.44:8765'], ['studio-pc', '192.168.1.20:8765 · saved']].map((x) => `<div class="k-lrow">${E('server', 20)}<span><b>${x[0]}</b><small class="mono">${x[1]}</small></span>${btn('Add', { k: 'sm' })}</div>`).join('')}</div><p class="k-note">${I('info', 14)}Anyone on the network can advertise _rfe._tcp. A listed agent is as unverified as one you type.</p>` : field('Name', 'garage-pi') + field('Address', '192.168.1.44:8765', { mono: 1 }) + field('Pairing code', 'K7QM 2XPA', { mono: 1, hint: '8 letters and digits from rfe pair or from Devices on another computer' })), (t === 'Find on network' ? btn('Scan again', { i: 'radar' }) : '') + btn('Cancel', { a: 'close' }) + btn('Connect', { k: 'pri', a: 'dlg:trust' })),
    trust: () => dlg('Trust garage-pi?', `<p>The agent sent this certificate key. On the PC run <code>rfe status</code> and compare every group.</p>${fp('9F3A1EC477B02D19C5E80A416B2F93D7E81054CA2F6E1B98D03C7A1546E2B0F9'.match(/.{4}/g).join(' '))}<p class="k-note">${I('shield', 14)}Cancel stops here; nothing was sent to the server.</p>`, ib('copy', 'Copy the fingerprint') + `<span class="sp"></span>` + btn('Cancel', { a: 'close' }) + btn('All 16 groups match: trust', { i: 'check', k: 'pri', a: 'dlg:signin' })),
    signin: (t = 'Account') => dlg('Sign in to garage-pi', `<div class="k-tabs">${['Account', 'Pairing code', 'Ask the PC'].map((x) => `<button class="${x === t ? 'on' : ''}" data-k="dlg:signin:${x}">${x}</button>`).join('')}</div>` + (t === 'Account' ? field('Username', 'sam') + field('Password', '••••••••••••', { hint: 'Sent once over the pinned connection, never stored' }) + `<p class="k-note">${I('info', 14)}An account may list and manage every device.</p>` : t === 'Pairing code' ? field('Pairing code', '', { ph: 'K7QM 2XPA', mono: 1, hint: 'Lower or upper case, with or without the space' }) : `<p>Someone at the PC approves this computer. Approve only if both codes are identical.</p><div class="k-code big mono">6150 2294</div><p class="k-sub">Waiting for the PC · 4:51 left</p>`) + `<button class="k-link">Create a new device key for this computer</button>`, btn(t === 'Ask the PC' ? 'Not now' : 'Cancel', { a: 'close' }) + (t === 'Ask the PC' ? '' : btn('Sign in', { k: 'pri', a: 'close' }))),
    keychanged: () => dlg('nas has a new key', `<p class="k-warn">${I('alert', 14)}This agent’s certificate is not the one you trusted. Continue only if you know why it changed, for example a reinstall.</p><div class="k-g2 tight"><div><small class="mut">Pinned</small>${fp('41DE 0B93 5C17 A2E0 88F4 1D6B 0C93 77AE')}</div><div><small class="mut">Presented now</small>${fp('E602 7B1C 94DA 3F05 61C8 2BE7 0A4D 9F13')}</div></div>`, btn('Keep it blocked', { a: 'close' }) + btn('Trust new key', { k: 'warn', a: 'close' })),
    status: () => { const s = S('studio'); return dlg('Status of studio-pc', `<dl class="k-kv cols"><dt>Agent</dt><dd>rfe ${s.ver}</dd><dt>System</dt><dd>${s.sys}</dd><dt>Platform</dt><dd>${s.plat} · read-write</dd><dt>Reachable at</dt><dd class="mono">${s.addr} (LAN)<br>${s.tail}:8765 (Tailscale)<br>MAC ${s.mac}</dd><dt>Uptime</dt><dd>${s.up}</dd><dt>Disk</dt><dd class="mono">812 GB of 2 TB</dd></dl><h3>Load</h3><div class="k-stats sm"><div><b class="mono">18%</b><small>processor</small></div><div><b class="mono">41%</b><small>memory</small></div><div><b class="mono">38.2 GB</b><small>received</small></div><div><b class="mono">12.7 GB</b><small>sent</small></div></div><p class="k-note">Shows the last reading only; the agent keeps no history.</p>`, btn('Run diagnostics', { k: 'ghost', a: 'dlg:diag' }) + btn('Refresh', { i: 'refresh' })); },
    diag: () => dlg('Diagnose studio-pc', `<div class="k-list">${[['Address resolves', '192.168.1.20'], ['TLS answers', '12 ms'], ['Key matches the pin', '9F3A 1EC4'], ['Login accepted', 'account sam'], ['Health', 'ok']].map((x) => `<div class="k-lrow">${I('check')}<span><b>${x[0]}</b></span><span class="mono t2">${x[1]}</span></div>`).join('')}</div>`, btn('Copy diagnostics', { i: 'copy', k: 'ghost' }) + btn('Run again', { i: 'refresh' })),
    roots: () => dlg('Allowed roots on studio-pc', `<p>The folders the agent lets logins open. Set on the PC; shown here for checking.</p><div class="k-list">${D.roots.map((r) => `<div class="k-lrow">${E(r.k, 18)}<span class="mono">${r.n}</span></div>`).join('')}</div>`, btn('Done', { a: 'close' })),
    editconn: () => dlg('Edit connection', field('Name', 'studio-pc') + `<p class="k-note">${I('info', 14)}The address belongs to the trusted key; a different address is a new connection.</p>`, btn('Cancel', { a: 'close' }) + btn('Save', { k: 'pri', a: 'close' })),
    forget: () => dlg('Forget media-mini?', `<p>Deletes its saved login and forgets its trusted key. Unfinished transfers to it are cancelled. The device stays on the agent until it is revoked there.</p>`, btn('Cancel', { a: 'close' }) + btn('Forget', { k: 'warn', a: 'close' })),
    pairphone: () => dlg('Pair a phone with studio-pc', V.pairPhone(), btn('Done', { a: 'close' }), 560),
    accept: () => dlg('Accept Pixel 9?', `<p>Accept only if Pixel 9 shows this same code.</p><div class="k-code big mono">4829 1307</div><p class="k-sub">It starts with browse access only. Give it more under Permissions.</p>`, btn('Cancel', { a: 'close' }) + btn('Codes match: accept', { k: 'pri', a: 'close' })),
    perms: () => dlg('Permissions of Pixel 8', `<div class="k-perms">${[['Browse files', 1, 'always on'], ['Download', 1], ['Upload', 1], ['Change (rename, move, new folder)', 0], ['Delete', 0], ['Make share links', 1], ['See the apps on this PC', 0], ['Start approved apps', 0, 'needs See the apps'], ['Read-only', 0]].map((x) => `<div class="k-set"><div><b>${x[0]}</b>${x[2] ? `<small>${x[2]}</small>` : ''}</div>${sw(x[1])}</div>`).join('')}</div>${field('Folder limit', '/home/sam/Photos', { mono: 1, hint: 'An absolute path inside the agent’s folders; empty means no limit' })}`, btn('Cancel', { a: 'close' }) + btn('Save', { k: 'pri', a: 'close' })),
    revoke: () => dlg('Revoke Pixel 8?', `<p>The agent refuses its login from now on. The row stays, marked Revoked. It can only come back by pairing again.</p>`, btn('Cancel', { a: 'close' }) + btn('Revoke access', { k: 'warn', a: 'close' })),
    remove: () => dlg('Remove old-phone from the list?', `<p>Deletes the device’s row for good. An active device is blocked at the same time.</p>`, btn('Cancel', { a: 'close' }) + btn('Remove from list', { k: 'warn', a: 'close' })),
    signoutall: () => dlg('Sign out all phones of studio-pc?', `<p>Revokes every device that signed in with a code or was approved on the PC: Pixel 8, Galaxy Tab S9, work-laptop. Owner logins and this app stay.</p>`, btn('Cancel', { a: 'close' }) + btn('Sign out all phones', { k: 'warn', a: 'close' })),
    share: () => dlg('Share link for north-bed-after.jpg', `<p>Opens the file in a browser without a sign-in, until it ends.</p>${seg(['15 minutes', '1 hour', '24 hours'], '1 hour')}<div class="k-in mono" style="margin-top:12px"><input value="https://192.168.1.20:8765/s/Qe7hW2rN" readonly></div>`, btn('Turn off', { k: 'ghost' }) + btn('Copy link', { i: 'copy', k: 'pri', a: 'close' })),
    props: () => dlg('Properties of north-bed-after.jpg', `<dl class="k-kv cols"><dt>Size</dt><dd>3.8 MB (3,984,211 bytes)</dd><dt>Modified</dt><dd>1 Nov 2026 09:12</dd><dt>Accessed</dt><dd>1 Nov 2026 10:20</dd><dt>Where</dt><dd class="mono">/home/sam/Projects/garden-plan</dd></dl>${field('Permissions', '644', { mono: 1, hint: '3 or 4 octal digits · rw-r--r--' })}<div class="k-set"><div><b>Checksum</b><small class="mono">sha256 · 6c1f…a09e</small></div>${btn('Checksum…', { k: 'sm', a: 'dlg:checksum' })}</div>`, btn('Copy path', { i: 'copy', k: 'ghost' }) + btn('Cancel', { a: 'close' }) + btn('Save', { k: 'pri', a: 'close' })),
    checksum: () => dlg('Checksum', `${seg(['sha256', 'sha1', 'md5'], 'sha256')}<div class="k-fp mono" style="margin-top:12px">6c1f 92ad 07e4 b51c 3f88 2a90 d6e1 77c4<br>0b5e 9a13 f2c7 64d8 e019 5b3a 81fe a09e</div>${field('Compare with', '', { ph: 'Paste a published value', mono: 1 })}`, btn('Copy', { i: 'copy', k: 'ghost' }) + btn('Done', { a: 'close' })),
    newfolder: () => dlg('New folder', field('New folder name', 'seedlings') + `<p class="k-sub">In studio-pc:/home/sam/Projects/garden-plan</p>`, btn('Cancel', { a: 'close' }) + btn('Create', { k: 'pri', a: 'close' })),
    batch: () => dlg('Rename 2 items', field('Pattern', 'north-bed-{n}', { mono: 1, hint: '{n} counts from 1; {name} keeps the old name' }) + `<div class="k-list">${[['north-bed-after.jpg', 'north-bed-1.jpg'], ['north-bed-before.jpg', 'north-bed-2.jpg']].map((x) => `<div class="k-lrow"><span class="mono t2">${x[0]}</span>${I('right', 14)}<span class="mono">${x[1]}</span></div>`).join('')}</div>`, btn('Cancel', { a: 'close' }) + btn('Rename', { k: 'pri', a: 'close' })),
    copyto: () => dlg('Copy or move 2 items', `${seg(['Copy', 'Move'], 'Copy')}<div class="k-list" style="margin-top:12px">${['/home/sam/Photos', '/home/sam/Projects', '/home/sam/Projects/garden-plan/photos', '/srv/media'].map((x, i) => `<div class="k-lrow${i === 2 ? ' sel' : ''}">${E('dir', 18)}<span class="mono">${x}</span></div>`).join('')}</div><p class="k-note">${I('info', 14)}Runs on studio-pc. Copying between two servers isn’t supported: download, then upload.</p>`, btn('Cancel', { a: 'close' }) + btn('Copy to photos', { k: 'pri', a: 'close' })),
    delete: () => dlg('Move 2 items to Trash?', `<p>north-bed-after.jpg and north-bed-before.jpg go to studio-pc’s Trash. Restore them from Tools, Trash.</p>`, btn('Cancel', { a: 'close' }) + btn('Move to Trash', { k: 'warn', a: 'toast:Moved to Trash: 2 items' })),
    emptytrash: () => dlg('Empty Trash?', `<p>Deletes 4 items on studio-pc forever.</p>`, btn('Cancel', { a: 'close' }) + btn('Empty Trash', { k: 'warn', a: 'close' })),
    turnoff: () => dlg('Turn off this link?', `<p>north-bed-before.jpg stops opening at its link at once.</p>`, btn('Cancel', { a: 'close' }) + btn('Turn off', { k: 'warn', a: 'close' })),
    upload: () => dlg('Upload to garden-plan', `<div class="k-drop big">${I('upload', 20)}<b>Drop files here</b><span>or choose them from this computer</span></div><p class="k-sub">Each piece and the whole file are checked by studio-pc before the file appears.</p>`, btn('Cancel', { a: 'close' }) + btn('Upload files…', { k: 'pri', a: 'close' })),
    preview: () => dlg('north-bed-after.jpg', `<div class="k-pv">${photo(2, 680, 440, 6)}</div>`, ib('left', 'Previous file') + ib('right', 'Next file') + ib('zoomin', 'Zoom in') + `<span class="sp"></span>` + btn('Share link…', { k: 'ghost', a: 'dlg:share' }) + btn('Download', { i: 'download', k: 'pri' }), 760),
    editor: () => dlg('planting-calendar.md', `<p class="k-warn">${I('alert', 14)}This file changed on the server after you opened it.</p><pre class="k-ed mono" contenteditable="true"># Planting calendar\n\n| Week | Bed   | What            |\n|------|-------|-----------------|\n| 44   | North | Garlic, onions  |\n| 45   | East  | Broad beans     |\n| 46   | South | Mulch, compost  |</pre><small class="mut">Tab indents; Ctrl+M makes Tab move focus</small>`, btn('Save as copy', { k: 'ghost' }) + btn('Overwrite', { k: 'warn' }) + btn('Save', { i: 'save', k: 'pri', a: 'close' }), 680),
    newrule: () => dlg('New sync rule', field('Name', 'Garden photos') + `<div class="k-set"><div><b>Direction</b></div>${seg(['This computer → server', 'Server → this computer'], 'This computer → server')}</div>` + field('Folder on this computer', '~/Pictures/Garden', { mono: 1 }) + field('Folder on the server', 'studio-pc:/home/sam/Projects/garden-plan/photos', { mono: 1 }), btn('Cancel', { a: 'close' }) + btn('Create rule', { k: 'pri', a: 'close' })),
    launch: () => dlg('Launch “GIMP” on studio-pc?', `<p>It opens on studio-pc’s own screen. Nothing runs on this computer; the launch is recorded in the agent’s security log.</p>`, btn('Cancel', { a: 'close' }) + btn('Launch', { k: 'pri', a: 'toast:Asked the PC to open GIMP. It can take a moment to appear on its screen.' })),
    agentlog: () => dlg('Agent log of studio-pc', `<label class="k-in sm">${I('filter', 14)}<input placeholder="Filter the log"></label><pre class="k-log mono">${D.agentlog.join('\n')}</pre>`, btn('Copy', { i: 'copy', k: 'ghost' }) + btn('Refresh', { i: 'refresh' }), 680),
    backup: () => dlg('Back up settings', field('Password', '••••••••••', { hint: 'Encrypts the file (AES-GCM, key from PBKDF2). It holds no password, token or key.' }) + field('Repeat password', '••••••••••'), btn('Cancel', { a: 'close' }) + btn('Save backup…', { k: 'pri', a: 'close' })),
    pins: () => dlg('Pinned certificates', `<div class="k-list">${D.servers.map((s) => `<div class="k-lrow">${mach(s, 20)}<span><b>${s.name}</b><small class="mono">${s.addr}</small></span>${tag(s.id)}</div>`).join('')}</div>`, btn('Done', { a: 'close' })),
    report: () => dlg('Report a problem', `<label class="k-field"><span>What happened</span><textarea class="k-ta">The upload of walkthrough-full.mov stopped at 71% when the laptop slept.</textarea></label><p class="k-note">${I('shield', 14)}The report holds no password, token or key. It names your servers’ addresses; read it before posting it in public.</p>`, btn('Copy report', { i: 'copy', k: 'pri', a: 'close' })),
    updates: () => dlg('Check for updates', `<div class="k-lrow">${I('download')}<span><b>RFE 1.1.1</b><small>host-v1.1.1 · Stable · .AppImage, 96 MB</small></span></div><p class="k-sub">The package is kept only if its SHA-256 matches the release’s SHA256SUMS. Install it with your package manager or run the AppImage.</p>`, btn('Later', { a: 'close' }) + btn('Download', { k: 'pri', a: 'close' })),
    welcome: () => dlg('Welcome to RFE', `<div class="k-wel">${[['key', 'Pinned keys', 'Each computer is trusted once, by its certificate key. A different key is refused.'], ['phone', 'You approve phones', 'Nothing joins until you compare a code on both screens.'], ['transfer', 'Transfers that resume', 'Pause, lose the network, come back: files continue where they stopped.']].map((x) => `<div>${E({ key: 'key', phone: 'phone', transfer: 'counterclockwise_arrows_button' }[x[0]], 32)}<b>${x[1]}</b><small>${x[2]}</small></div>`).join('')}</div>`, btn('Skip', { a: 'close' }) + btn('Find on network', { a: 'dlg:newconn:Find on network' }) + btn('Add a computer', { k: 'pri', a: 'dlg:newconn' }), 640),
    palette: () => `<div class="k-dlg k-pal" role="dialog" aria-label="Search files or run a command" style="width:600px"><label class="k-in big">${I('search', 16)}<input value="bed" placeholder="Search files or run a command" autofocus></label><div class="k-palr">${[['Search', [['Search all connected servers for “bed”  ·  Enter', 'go:search', 'search'], ['north-bed-after.jpg  ·  studio-pc', 'dlg:preview', 'image'], ['bed-layout.pdf  ·  studio-pc', 'go:files', 'file']]], ['Go', [['Files', 'go:files', 'folder'], ['Devices', 'go:devices', 'phone'], ['Transfers', 'go:transfers', 'transfer']]], ['Actions', [['New connection', 'dlg:newconn', 'plug'], ['Pair a phone', 'dlg:pairphone', 'qr'], ['Status of studio-pc', 'dlg:status', 'activity'], ['Pause all transfers', 'close', 'pause'], ['Show hidden files', 'close', 'eye'], ['Check for updates', 'dlg:updates', 'download'], ['Show the welcome screen', 'dlg:welcome', 'info']]], ['Recent files', [['north-bed-after.jpg', 'dlg:preview', 'image'], ['planting-calendar.md', 'dlg:editor', 'text']]]].map((g) => `<h3>${g[0]}</h3>${g[1].map((c, i) => `<button class="${g[0] === 'Search' && i === 0 ? 'on' : ''}" data-k="${c[1]}">${{ image: E('img', 16), file: E('doc', 16), text: E('txt', 16) }[c[2]] || I(c[2])}<span>${c[0]}</span></button>`).join('')}`).join('')}</div></div>`,
  };
  const MENU = {
    /* 14 rows instead of 18: Open and Preview are one row for a file, Copy and Move share a dialog,
       Rename covers one or several, Extract here appears only on archives. */
    file: [['Preview', 'dlg:preview', 'eye', 'Space'], ['Download', 'close', 'download', 'Ctrl D'], ['Share link…', 'dlg:share', 'link'], ['Make available offline', 'close', 'offline'], ['Add to favorites', 'close', 'star'], '-', ['Copy or move…', 'dlg:copyto', 'move'], ['Rename…', 'dlg:batch', 'pencil', 'F2'], ['Compress to .zip', 'close', 'archive'], '-', ['Checksum…', 'dlg:checksum', 'hash'], ['Properties…', 'dlg:props', 'info'], ['Copy path', 'close', 'copy'], '-', ['Move to Trash', 'dlg:delete', 'trash', 'Delete']],
    server: [['Browse files', 'go:files', 'folder'], ['Disconnect', 'close', 'plug'], ['Edit connection', 'dlg:editconn', 'pencil'], ['Status…', 'dlg:status', 'activity'], ['Run diagnostics', 'dlg:diag', 'cpu'], ['Allowed roots…', 'dlg:roots', 'drive'], ['Wake computer', 'toast:Wake signal sent to media-mini', 'power'], ['Sign in…', 'dlg:signin', 'user'], '-', ['Forget server', 'dlg:forget', 'trash']],
    device: [['Permissions…', 'dlg:perms', 'lock'], ['Activity', 'go:history', 'history'], '-', ['Revoke access', 'dlg:revoke', 'x'], ['Remove from list', 'dlg:remove', 'trash']],
    browse: () => [['Browse on', null], ...D.servers.map((s) => [s.name + (s.ms ? `  ·  ${s.ms} ms ${s.route}` : `  ·  ${s.state}`), s.state === 'Connected' ? 'go:files' : 'go:servers', 'server']), ['This computer', 'panel:local', 'laptop'], '-', ['New connection', 'dlg:newconn', 'plug']],
    account: () => [['sam on studio-pc', null], [`<div class="k-mrow">${I('sun')}<span>Theme</span>${seg(['System', 'Light', 'Dark'], 'System')}</div>`, 'raw'], '-', ['Accounts', 'dlg:accounts', 'user'], ['Settings', 'go:settings', 'sliders'], '-', ['Sign out of studio-pc', 'close', 'logout']],
    notes: () => [['Notifications', null], ...D.notes.map((n) => [`${n[0]}<small>${n[1]} · ${n[2]}</small>`, n[0].startsWith('Pixel') ? 'go:devices' : n[0].startsWith('RFE') ? 'dlg:updates' : 'go:transfers', n[0].startsWith('Pixel') ? '@phone' : n[0].startsWith('RFE') ? '@package' : '@inbox_tray'])],
    pair: [['Copy code data', 'toast:Copied the code data', 'copy'], ['Save QR as image', 'toast:Saved pairing-qr.png to Downloads', 'download'], '-', ['New code', 'toast:New code made; the old one stops working', 'refresh']],
    selmore: [['Rename several…', 'dlg:batch', 'pencil'], ['Compress to .zip', 'toast:Compressing 2 items to garden-plan.zip', 'archive']],
    detmore: [['Make available offline', 'toast:north-bed-after.jpg will stay available offline', 'offline'], ['Add to favorites', 'close', 'star'], '-', ['Properties…', 'dlg:props', 'info'], ['Checksum…', 'dlg:checksum', 'hash'], ['Copy path', 'toast:Copied the path', 'copy']],
    trash: [['Delete forever', 'toast:Deleted bed-layout-draft.pdf forever', 'x'], '-', ['Empty Trash…', 'dlg:emptytrash', 'trash']],
  };
  DLG.accounts = () => dlg('Accounts', `<div class="k-list">${D.servers.map((s) => `<div class="k-lrow">${mach(s, 20)}<span><b>${s.name}</b><small>${s.state === 'Connected' ? (s.login === 'Account' ? 'Account sam · device desk-linux' : 'Pairing code · device desk-linux') : s.state}</small></span>${s.state === 'Connected' ? btn('Sign out', { k: 'sm' }) : btn('Sign in…', { k: 'sm', a: 'dlg:signin' })}</div>`).join('')}</div>`, btn('Done', { a: 'close' }));

  /* ---------- runtime ---------- */
  const K = { I, E, EM, OS, mach, D, V, C, DLG, MENU, photo, qr, btn, ib, chip, sw, seg, tag, dot, bar, head, sec, fic, esc, empty, S };
  let root, stage, state = { view: 'files', tool: 'fav', set: 'Appearance', panel: 'details', hist: 'All' };
  K.state = state;
  K.render = () => {
    const v = state.view; let h;
    if (K.views && K.views[v]) h = K.views[v](state);
    else if (v === 'files') h = V.files({ panel: state.panel, mode: state.mode });
    else if (v === 'tools') h = V.tools(state.tool);
    else if (v === 'settings') h = V.settings(state.set);
    else if (v === 'history') h = V.history(state.hist);
    else h = V[v] ? V[v]() : '';
    stage.innerHTML = h; stage.scrollTop = 0;
    root.querySelectorAll('[data-k^="go:"]').forEach((b) => b.classList.toggle('on', b.dataset.k === 'go:' + v && b.classList.contains('k-navi')));
    if (K.onGo) K.onGo(v);
  };
  K.go = (v) => { state.view = v; closeAll(); K.render(); };
  const layer = () => { let l = root.querySelector('.k-layer'); if (!l) { l = document.createElement('div'); l.className = 'k-layer'; root.appendChild(l); } return l; };
  function closeAll() { const l = root.querySelector('.k-layer'); if (l) l.remove(); }
  K.openDlg = (name, arg) => { if (!DLG[name]) return; const l = layer(); l.className = 'k-layer k-ov'; l.innerHTML = DLG[name](arg); const i = l.querySelector('input'); if (i) i.focus(); };
  K.toast = (t) => { const old = root.querySelector('.k-toast'); if (old) old.remove(); const d = document.createElement('div'); d.className = 'k-toast'; d.innerHTML = `<span>${t}</span><button data-k="close">${I('x', 14)}</button>`; root.appendChild(d); setTimeout(() => d.remove(), 4200); };
  K.menu = (name, x, y) => {
    let items = MENU[name]; if (typeof items === 'function') items = items(); const l = layer(); l.className = 'k-layer';
    const r = root.getBoundingClientRect();
    l.innerHTML = `<div class="k-menu" style="left:${Math.min(x - r.left, r.width - 280)}px;top:${Math.min(y - r.top, r.height - items.length * 32 - 20)}px">${items.map((m) => (m === '-' ? '<hr>' : m[1] === null ? `<div class="k-mt">${m[0]}</div>` : m[1] === 'raw' ? m[0] : `<button data-k="${m[1]}">${m[2] ? (m[2][0] === '@' ? E(m[2].slice(1), 18) : I(m[2])) : ''}<span>${m[0]}</span>${m[3] ? `<kbd>${m[3]}</kbd>` : ''}</button>`)).join('')}</div>`;
  };
  K.mount = (o) => {
    root = o.root || document.querySelector('.win'); stage = o.stage; Object.assign(state, o.state || {}); K.views = o.views || {}; K.onGo = o.onGo;
    root.addEventListener('click', (e) => {
      const t = e.target.closest('[data-k]'); if (!t || !root.contains(t)) { if (e.target.classList.contains('k-ov') || (root.querySelector('.k-layer:not(.k-ov)') && !e.target.closest('.k-menu'))) closeAll(); return; }
      const [a, b, c] = t.dataset.k.split(':'); const rest = t.dataset.k.slice(a.length + 1);
      if (a === 'go') K.go(b); else if (a === 'dlg') K.openDlg(b, c); else if (a === 'close') { closeAll(); const tt = t.closest('.k-toast'); if (tt) tt.remove(); }
      else if (a === 'menu') { e.stopPropagation(); const r = t.getBoundingClientRect(); K.menu(b, r.left, r.bottom + 4); }
      else if (a === 'tool') { state.tool = b; state.view = 'tools'; K.render(); } else if (a === 'set') { state.set = rest; state.view = 'settings'; K.render(); }
      else if (a === 'panel') { state.panel = b; if (state.view !== 'files') state.view = 'files'; closeAll(); K.render(); } else if (a === 'hist') { state.hist = b; K.render(); }
      else if (a === 'view') { state.mode = b; K.render(); }
      else if (a === 'toast') { closeAll(); K.toast(rest); } else if (a === 'dock') { const d = root.querySelector('.k-dockwrap'); if (d) d.classList.toggle('open'); }
      else if (o.on) o.on(a, b, t);
    });
    root.addEventListener('contextmenu', (e) => { const r = e.target.closest('[data-row]'); if (r) { e.preventDefault(); K.menu('file', e.clientX, e.clientY); } });
    document.addEventListener('keydown', (e) => {
      if (e.target.closest && e.target.closest('input,textarea,[contenteditable]') && e.key !== 'Escape') return;
      if (e.key === 'Escape') closeAll(); else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); K.openDlg('palette'); } else if (e.key === '/') { e.preventDefault(); K.go('search'); }
    });
    const h = new URLSearchParams(location.hash.slice(1)); if (h.has('still')) document.documentElement.classList.add('still');
    if (h.get('tool')) state.tool = h.get('tool'); if (h.get('set')) state.set = h.get('set'); if (h.get('go')) state.view = h.get('go');
    K.render(); if (h.get('dlg')) K.openDlg(h.get('dlg'), h.get('tab') || undefined); if (h.get('menu')) K.menu(h.get('menu'), 400, 200); if (h.get('dock')) { const d = root.querySelector('.k-dockwrap'); if (d) d.classList.add('open'); }
  };
  window.K = K;
})();
