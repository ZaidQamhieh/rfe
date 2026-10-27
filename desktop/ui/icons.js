/* RFE design kit: icons, one shared dataset, tiny behaviours.
   Every concept renders the same realistic session so layouts are comparable. */
(function () {
  const P = {
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
    moon: '<path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z"/>',
    qr: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 14h3v3h-3zM20 14v3M14 20h3M20 20h1"/>',
    phone: '<rect x="7" y="2.5" width="10" height="19" rx="2.2"/><path d="M11 18.5h2"/>',
    folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    'folder-open': '<path d="M3 8V7a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v1"/><path d="M3.5 19h14.3a2 2 0 0 0 1.9-1.4l1.8-6a1.5 1.5 0 0 0-1.4-1.9H6.6a2 2 0 0 0-1.9 1.4L3 17z"/>',
    file: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/>',
    text: '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M9 13h6M9 17h6M9 9h2"/>',
    image: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="m21 16-5-5-9 9"/>',
    video: '<rect x="3" y="5" width="14" height="14" rx="2"/><path d="m17 10 4-2.5v9L17 14"/>',
    music: '<path d="M9 18V5l11-2v13"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="17.5" cy="16" r="2.5"/>',
    archive: '<rect x="3" y="4" width="18" height="4" rx="1"/><path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8M10 12h4"/>',
    code: '<path d="m8 8-5 4 5 4M16 8l5 4-5 4M14 5l-4 14"/>',
    terminal: '<path d="m4 7 5 5-5 5M12 18h8"/>',
    database: '<ellipse cx="12" cy="5.5" rx="8" ry="3"/><path d="M4 5.5v13c0 1.7 3.6 3 8 3s8-1.3 8-3v-13M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
    package: '<path d="m12 2.5 9 4.5v10l-9 4.5-9-4.5V7z"/><path d="m3 7 9 4.5L21 7M12 11.5v10"/>',
    key: '<circle cx="8" cy="15" r="4"/><path d="m11 12 9-9M16 7l3 3"/>',
    exec: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 10 3 2-3 2M12 15h5"/>',
    drive: '<rect x="3" y="13" width="18" height="7" rx="2"/><path d="m5 13 2.5-8h9L19 13M7 16.5h.01M11 16.5h.01"/>',
    server: '<rect x="3" y="3.5" width="18" height="7" rx="2"/><rect x="3" y="13.5" width="18" height="7" rx="2"/><path d="M7 7h.01M7 17h.01"/>',
    monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
    laptop: '<rect x="5" y="5" width="14" height="10" rx="1.5"/><path d="M2.5 19h19l-1.5-4H4z"/>',
    home: '<path d="m3 11 9-8 9 8M5 9.5V20h5v-6h4v6h5V9.5"/>',
    upload: '<path d="M12 16V4M7 9l5-5 5 5M4 20h16"/>',
    download: '<path d="M12 4v12M7 11l5 5 5-5M4 20h16"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
    refresh: '<path d="M3 12a9 9 0 0 1 15.5-6.2L21 8M21 3v5h-5M21 12a9 9 0 0 1-15.5 6.2L3 16M3 21v-5h5"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    minus: '<path d="M5 12h14"/>',
    x: '<path d="M18 6 6 18M6 6l12 12"/>',
    check: '<path d="m5 12.5 4.5 4.5L19 7"/>',
    trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
    edit: '<path d="M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4"/>',
    copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V6a2 2 0 0 1 2-2h8"/>',
    scissors: '<circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M20 4 8.1 15.9M14.5 14.5 20 20M8.1 8.1 12 12"/>',
    'folder-plus': '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM12 10v6M9 13h6"/>',
    'arrow-right': '<path d="M5 12h14M13 6l6 6-6 6"/>',
    'arrow-left': '<path d="M19 12H5M11 6l-6 6 6 6"/>',
    'arrow-up': '<path d="M12 19V5M5 12l7-7 7 7"/>',
    'arrow-down': '<path d="M12 5v14M19 12l-7 7-7-7"/>',
    swap: '<path d="M7 4 3 8l4 4M3 8h14M17 20l4-4-4-4M21 16H7"/>',
    'chevron-right': '<path d="m9 6 6 6-6 6"/>',
    'chevron-left': '<path d="m15 6-6 6 6 6"/>',
    'chevron-down': '<path d="m6 9 6 6 6-6"/>',
    'chevron-up': '<path d="m6 15 6-6 6 6"/>',
    'chevrons-ud': '<path d="m7 15 5 5 5-5M7 9l5-5 5 5"/>',
    gear: '<path d="M12.2 2h-.4a2 2 0 0 0-2 2v.2a2 2 0 0 1-1 1.7l-.4.3a2 2 0 0 1-2 0l-.2-.1a2 2 0 0 0-2.7.7l-.2.4a2 2 0 0 0 .7 2.7l.2.1a2 2 0 0 1 1 1.7v.5a2 2 0 0 1-1 1.7l-.2.1a2 2 0 0 0-.7 2.7l.2.4a2 2 0 0 0 2.7.7l.2-.1a2 2 0 0 1 2 0l.4.3a2 2 0 0 1 1 1.7V20a2 2 0 0 0 2 2h.4a2 2 0 0 0 2-2v-.2a2 2 0 0 1 1-1.7l.4-.3a2 2 0 0 1 2 0l.2.1a2 2 0 0 0 2.7-.7l.2-.4a2 2 0 0 0-.7-2.7l-.2-.1a2 2 0 0 1-1-1.7v-.5a2 2 0 0 1 1-1.7l.2-.1a2 2 0 0 0 .7-2.7l-.2-.4a2 2 0 0 0-2.7-.7l-.2.1a2 2 0 0 1-2 0l-.4-.3a2 2 0 0 1-1-1.7V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>',
    sliders: '<path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1"/><circle cx="15" cy="6" r="2"/><circle cx="9" cy="12" r="2"/><circle cx="17" cy="18" r="2"/>',
    wifi: '<path d="M2 9a15 15 0 0 1 20 0M5.5 12.5a10 10 0 0 1 13 0M9 16a5 5 0 0 1 6 0M12 20h.01"/>',
    'wifi-off': '<path d="M2 9a15 15 0 0 1 4-2.7M10 5.2A15 15 0 0 1 22 9M5.5 12.5a10 10 0 0 1 3-1.8M14 11a10 10 0 0 1 4.5 1.5M9 16a5 5 0 0 1 6 0M12 20h.01M3 3l18 18"/>',
    lock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
    unlock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 7.5-2"/>',
    alert: '<path d="M12 3 2 20h20z"/><path d="M12 10v4M12 17h.01"/>',
    'alert-circle': '<circle cx="12" cy="12" r="9"/><path d="M12 7.5v5M12 16h.01"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
    'check-circle': '<circle cx="12" cy="12" r="9"/><path d="m8 12.5 3 3 5-6"/>',
    'x-circle': '<circle cx="12" cy="12" r="9"/><path d="m9 9 6 6M15 9l-6 6"/>',
    grid: '<rect x="3" y="3" width="7.5" height="7.5" rx="1.5"/><rect x="13.5" y="3" width="7.5" height="7.5" rx="1.5"/><rect x="3" y="13.5" width="7.5" height="7.5" rx="1.5"/><rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.5"/>',
    menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
    panel: '<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M15 4v16"/>',
    list: '<path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/>',
    columns: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16M15 4v16"/>',
    sidebar: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>',
    'panel-bottom': '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 15h18"/>',
    'panel-right': '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>',
    filter: '<path d="M3 5h18l-7 8v6l-4 2v-8z"/>',
    sort: '<path d="M7 4v16M3 16l4 4 4-4M17 20V4M13 8l4-4 4 4"/>',
    more: '<circle cx="5" cy="12" r="1.2" fill="currentColor"/><circle cx="12" cy="12" r="1.2" fill="currentColor"/><circle cx="19" cy="12" r="1.2" fill="currentColor"/>',
    'more-v': '<circle cx="12" cy="5" r="1.2" fill="currentColor"/><circle cx="12" cy="12" r="1.2" fill="currentColor"/><circle cx="12" cy="19" r="1.2" fill="currentColor"/>',
    command: '<path d="m5 8 5 4-5 4"/><path d="M13 17h6"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    pause: '<path d="M8 5v14M16 5v14"/>',
    play: '<path d="M7 4.5v15l12-7.5z"/>',
    retry: '<path d="M3 12a9 9 0 1 0 3-6.7L3 8M3 3v5h5"/>',
    star: '<path d="m12 3 2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1L3.2 9.5l6.1-.9z"/>',
    pin: '<path d="M12 17v5M8 3h8l-1 6 3 4H6l3-4z"/>',
    tag: '<path d="M3 12V4h8l10 10-8 8z"/><circle cx="7.5" cy="8.5" r="1.2"/>',
    share: '<circle cx="6" cy="12" r="3"/><circle cx="18" cy="6" r="3"/><circle cx="18" cy="18" r="3"/><path d="m8.7 10.6 6.6-3.2M8.7 13.4l6.6 3.2"/>',
    eye: '<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
    link: '<path d="M10 14a4.5 4.5 0 0 0 6.4 0l3-3a4.5 4.5 0 0 0-6.4-6.4l-1 1M14 10a4.5 4.5 0 0 0-6.4 0l-3 3a4.5 4.5 0 0 0 6.4 6.4l1-1"/>',
    cloud: '<path d="M7 18a5 5 0 1 1 .9-9.9A6 6 0 0 1 19.500 10 4 4 0 0 1 18 18z"/>',
    cpu: '<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9.5" y="9.5" width="5" height="5"/><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/>',
    activity: '<path d="M3 12h4l3-8 4 16 3-8h4"/>',
    layers: '<path d="m12 3 9 5-9 5-9-5zM3 13l9 5 9-5M3 17.500l9 5 9-5"/>',
    bell: '<path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4zM10 21h4"/>',
    user: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
    shield: '<path d="M12 3 4 6v6c0 5 3.400 8 8 9 4.600-1 8-4 8-9V6z"/>',
    'shield-check': '<path d="M12 3 4 6v6c0 5 3.4 8 8 9 4.6-1 8-4 8-9V6z"/><path d="m9 12 2 2 4-4"/>',
    globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18"/>',
    zap: '<path d="M13 2 4 14h7l-1 8 9-12h-7z"/>',
    plug: '<path d="M9 2v5M15 2v5M6 7h12v4a6 6 0 0 1-12 0zM12 17v5"/>',
    power: '<path d="M12 3v9M6.3 6.3a8 8 0 1 0 11.400 0"/>',
    history: '<path d="M3 12a9 9 0 1 0 3-6.700L3 8M3 3v5h5M12 7v5l3 2"/>',
    bookmark: '<path d="M6 3h12v18l-6-4-6 4z"/>',
    external: '<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>',
    maximize: '<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>',
    square: '<rect x="5" y="5" width="14" height="14" rx="1"/>',
    stop: '<rect x="6" y="6" width="12" height="12" rx="1.5" fill="currentColor"/>',
    hash: '<path d="M5 9h15M4 15h15M10 3 8 21M16 3l-2 18"/>',
    mouse: '<rect x="6" y="3" width="12" height="18" rx="6"/><path d="M12 7v4"/>',
    move: '<path d="M12 3v18M3 12h18M8 7l4-4 4 4M8 17l4 4 4-4M7 8l-4 4 4 4M17 8l4 4-4 4"/>',
    pie: '<path d="M12 3a9 9 0 1 0 9 9h-9z"/><path d="M15 3.500A9 9 0 0 1 20.500 9H15z"/>',
    tree: '<path d="M5 4v14a2 2 0 0 0 2 2h3M5 10h5M5 4h5"/><rect x="10" y="2" width="8" height="4" rx="1"/><rect x="10" y="8" width="8" height="4" rx="1"/><rect x="10" y="18" width="8" height="4" rx="1"/>',
    inbox: '<path d="M3 13h5l1 3h6l1-3h5M5 5h14l2 8v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6z"/>',
    send: '<path d="m21 3-9 18-2.500-7.500L2 11zM21 3 9.500 13.500"/>',
    dot: '<circle cx="12" cy="12" r="4" fill="currentColor"/>',
    spinner: '<path d="M12 3a9 9 0 1 0 9 9"/>',
    gauge: '<path d="M4 18a9 9 0 1 1 16 0"/><path d="m12 14 4-5"/><circle cx="12" cy="14" r="1"/>',
    undo: '<path d="M9 14 4 9l5-5M4 9h10a6 6 0 0 1 0 12h-3"/>',
    keyboard: '<rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10"/>',
    palette: '<circle cx="12" cy="12" r="9"/><circle cx="8" cy="10" r="1" fill="currentColor"/><circle cx="12" cy="7.500" r="1" fill="currentColor"/><circle cx="16" cy="10" r="1" fill="currentColor"/>',
    'file-plus': '<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M12 11v6M9 14h6"/>',
    'panel-left-close': '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16m6-9-2 2 2 2"/>'
  };

  const icon = (name, o) => {
    o = o || {};
    const sw = o.sw || 1.7;
    const cls = 'ic' + (o.cls ? ' ' + o.cls : '');
    const st = o.size ? ` style="width:${o.size / 16}rem;height:${o.size / 16}rem"` : '';
    return `<svg class="${cls}" data-i="${name}"${st} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${P[name] || P.file}</svg>`;
  };

  const KINDS = {
    dir: ['folder', 'Folder'], pkg: ['package', 'Package'], video: ['video', 'Video'], image: ['image', 'Image'],
    archive: ['archive', 'Archive'], text: ['text', 'Text'], code: ['code', 'Data'], doc: ['text', 'PDF document'],
    key: ['key', 'Keystore'], exec: ['exec', 'Executable'], file: ['file', 'File']
  };
  const EXT = {
    apk: 'pkg', aab: 'pkg', deb: 'pkg', appimage: 'pkg', mp4: 'video', mov: 'video', png: 'image', jpg: 'image',
    zip: 'archive', zst: 'archive', gz: 'archive', md: 'text', txt: 'text', log: 'text', json: 'code', yml: 'code',
    pdf: 'doc', jks: 'key', sh: 'exec'
  };
  const kindOf = (it) => {
    if (it.t === 'dir') return 'dir';
    if (it.exec) return 'exec';
    const m = /\.([a-z0-9]+)$/i.exec(it.n);
    return (m && EXT[m[1].toLowerCase()]) || 'file';
  };
  const fileIcon = (it, o) => {
    const k = kindOf(it);
    return icon(KINDS[k][0], Object.assign({ cls: 'fi fi-' + k }, o || {}));
  };

  const RFE = { icon, fileIcon, kindOf, KINDS };
  RFE.kindLabel = (it) => KINDS[kindOf(it)][1];
  RFE.sort = (items) => items.slice().sort((a, b) => (a.t === b.t ? 0 : a.t === 'dir' ? -1 : 1));

  /* icons from data-ic="name" (optional data-size) so markup stays readable */
  RFE.hydrate = (root) => {
    (root || document).querySelectorAll('[data-ic]').forEach((el) => {
      el.insertAdjacentHTML('afterbegin', icon(el.dataset.ic, { size: el.dataset.size, sw: el.dataset.sw }));
      el.removeAttribute('data-ic');
    });
  };

  /* context menu: show `menu` at the pointer for right-clicks inside `scope` */
  RFE.contextMenu = (scope, menu, onShow) => {
    const hide = () => menu.classList.remove('open');
    scope.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      if (onShow) onShow(e);
      menu.classList.add('open');
      const r = menu.getBoundingClientRect();
      const x = Math.min(e.clientX, innerWidth - r.width - 8);
      const y = Math.min(e.clientY, innerHeight - r.height - 8);
      menu.style.left = x + 'px';
      menu.style.top = y + 'px';
    });
    addEventListener('click', hide);
    addEventListener('keydown', (e) => e.key === 'Escape' && hide());
  };

  /* click / ctrl-click row selection */
  RFE.selectable = (scope, rowSel, cls) => {
    cls = cls || 'sel';
    scope.addEventListener('click', (e) => {
      const row = e.target.closest(rowSel);
      if (!row || !scope.contains(row)) return;
      if (!(e.ctrlKey || e.metaKey)) scope.querySelectorAll(rowSel + '.' + cls).forEach((r) => r !== row && r.classList.remove(cls));
      row.classList.toggle(cls, e.ctrlKey || e.metaKey ? undefined : true);
    });
  };

  /* toggle `cls` on `el` with a key combo, Esc closes */
  RFE.toggleOn = (el, keyFn, cls) => {
    cls = cls || 'open';
    addEventListener('keydown', (e) => {
      if (keyFn(e)) { e.preventDefault(); el.classList.toggle(cls); }
      else if (e.key === 'Escape') el.classList.remove(cls);
    });
  };

  /* live progress: any [data-xf="t1"] gets --p and [data-f=pct|done|speed|eta] updated each second */
  const GB = 1e9;
  RFE.live = () => {
    const thumb = /[?&]thumb=1/.test(location.search);
    const state = {
      t1: { pct: 37, speed: 38.2 }, t2: { pct: 82, speed: 61.4 }
    };
    const fmt = (b) => (b >= GB ? (b / GB).toFixed(2) + ' GB' : (b / 1e6).toFixed(0) + ' MB');
    const eta = (s) => (s >= 60 ? (() => { const r = Math.round(s); return Math.floor(r / 60) + 'm ' + String(r % 60).padStart(2, '0') + 's'; })() : Math.round(s) + 's');
    const paint = () => {
      for (const id in state) {
        const t = RFE.xfer(id), s = state[id];
        const done = (s.pct / 100) * t.bytes;
        const rem = t.bytes - done;
        document.querySelectorAll(`[data-xf="${id}"]`).forEach((root) => {
          root.style.setProperty('--p', s.pct.toFixed(1));
          const set = (f, v) => root.querySelectorAll(`[data-f="${f}"]`).forEach((n) => (n.textContent = v));
          set('pct', Math.floor(s.pct) + '%');
          set('done', fmt(done));
          set('speed', s.speed.toFixed(1) + ' MB/s');
          set('eta', s.pct >= 100 ? 'done' : eta(rem / (s.speed * 1e6)));
        });
      }
    };
    paint();
    if (thumb || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    setInterval(() => {
      for (const id in state) {
        const s = state[id], t = RFE.xfer(id);
        if (s.pct >= 97) { s.pct = id === 't2' ? 82 : 37; }
        s.pct = Math.min(100, s.pct + (s.speed * 1e6 / t.bytes) * 100);
        s.speed = Math.max(20, s.speed + (Math.random() - 0.5) * 3);
      }
      paint();
    }, 1000);
  };

  window.RFE = RFE;
  document.addEventListener('DOMContentLoaded', () => { RFE.hydrate(); });
})();
