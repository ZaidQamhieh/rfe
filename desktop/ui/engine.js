/* RFE Tonal - the engine behind the window. No DOM. It keeps the window's picture of the saved
   servers, the folders it has listed, the transfers and the activity log, and talks to the Rust core
   through Tauri commands (every network call and every secret stays in Rust). The window reads the
   picture synchronously and is told when it changes (`on`); every change that needs the network is
   a promise. Time moves with `tick(ms)`, which the window calls from a timer. */
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.Engine = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';
  const NOOP = () => {}; // the one empty handler for a failure that is safe to ignore
  /* Search defaults: the row cap when a caller gives none, the size floor of the "big" filter, the work time per step the window gives a search. */
  const SEARCH = { max: 200, bigMin: 100e6, slice: 7000 };
  /* Folder listing: the agent returns a folder in pages of PAGE_LIMIT entries, and the window reads at most MAX_PAGES of them. */
  const PAGE_LIMIT = 1000, MAX_PAGES = 60;
  /* Latency per computer: the running average keeps LATENCY_KEEP of the old value and takes LATENCY_NEW of the new sample; LATENCY_HISTORY samples are kept. */
  const LATENCY_KEEP = 0.7, LATENCY_NEW = 0.3, LATENCY_HISTORY = 40;
  const BYTES_PER_GB = 1e9;
  /* Seconds before the next reconnect attempt to a computer that was lost. */
  const RETRY_SECONDS = 8;
  /* The defaults of two settings a person can change: Trash keeps deleted items for this many days; a pairing code works for this many minutes. */
  const TRASH_DAYS_DEFAULT = 30, PAIR_LIFE_DEFAULT = 10;
  /* The codes of the window's own refusals (bad()), and the shapes of a core error text: the code in brackets at its end, and the "The agent says: " lead-in. */
  const EINVAL = 'EINVAL', EEXIST = 'EEXIST';
  const CODE_AT_END_RE = /\(([A-Z][A-Z_]+)\)$/, CODE_SUFFIX_RE = / \([A-Z][A-Z_]+\)$/, AGENT_SAYS_RE = /^The agent says: /;
  /* The fallback limits until the core answers desktop_limits; the same numbers as the core's (src-tauri/src/fileops.rs desktop_limits). */
  const DEFAULT_LIMITS = Object.freeze({ maxVideoBytes: 256 * 1024 * 1024, maxBatch: 1000, maxTextBytes: 1 << 20, maxName: 255, maxImageBytes: 8 << 20 });

  /* ---------- the bridge to the Rust core ---------- */
  let invokeImpl = null;
  const tauri = () => (typeof window !== 'undefined' && window.__TAURI__ && window.__TAURI__.core) || null;
  const rawInvoke = (cmd, args) => {
    if (invokeImpl) return Promise.resolve().then(() => invokeImpl(cmd, args || {}));
    const t = tauri();
    return t ? t.invoke(cmd, args || {}) : Promise.reject('The app’s backend is not available.');
  };
  /* The core answers failures with text. A refusal the agent worded itself ends in its code. */
  function toError(e) {
    const raw = typeof e === 'string' ? e : (e && e.message) || String(e);
    const m = CODE_AT_END_RE.exec(raw);
    const err = new Error(raw.replace(AGENT_SAYS_RE, '').replace(CODE_SUFFIX_RE, ''));
    err.code = m ? m[1] : /too old/i.test(raw) ? 'INCOMPATIBLE' : /not signed in|no longer accepts this login/i.test(raw) ? 'UNAUTHORIZED' : /certificate|fingerprint|pinned/i.test(raw) ? 'CERT' : /did not answer|timed out|could not connect|cannot connect|cannot reach|connection (?:refused|reset|closed|failed)|connect error|network|refused|unreachable|dns/i.test(raw) ? 'NETWORK' : 'ERROR';
    err.raw = raw;
    return err;
  }
  const call = (cmd, args) => rawInvoke(cmd, args).catch((e) => { throw toError(e); });

  /* ---------- helpers ---------- */
  const now = () => Date.now();
  const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const p2 = (n) => String(n).padStart(2, '0');
  /* The language of the window (set with the language choice); dates and numbers follow it. English keeps its short fixed form. */
  const lang = () => (typeof document !== 'undefined' && document.documentElement.lang) || 'en';
  const fmtDate = (ms) => { if (!ms) return '—'; const d = new Date(ms); if (!/^en/.test(lang())) { const y = d.getFullYear() === new Date().getFullYear(); try { return new Intl.DateTimeFormat(lang(), y ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' } : { month: 'short', day: 'numeric', year: 'numeric' }).format(d); } catch (x) { /* fall through to the English form */ } } const base = MON[d.getMonth()] + ' ' + d.getDate(); return d.getFullYear() === new Date().getFullYear() ? base + ', ' + p2(d.getHours()) + ':' + p2(d.getMinutes()) : base + ', ' + d.getFullYear(); };
  const fmtTime = (ms) => { const d = new Date(ms); return p2(d.getHours()) + ':' + p2(d.getMinutes()) + ':' + p2(d.getSeconds()); };
  const fmtBytes = (b) => {
    if (!isFinite(b) || b < 0) return '—';
    if (b < 1000) return b + ' B';
    const u = ['KB', 'MB', 'GB', 'TB']; let i = -1; let v = b;
    do { v /= 1000; i++; } while (v >= 1000 && i < 3);
    /* Digits stay 0-9 in every language, as in the rest of the UI; only the decimal mark follows the language (an engine may otherwise pick Arabic-Indic digits for 'ar'). */
    const nf = (x, d) => (/^en/.test(lang()) ? x.toFixed(d) : x.toLocaleString(lang() + '-u-nu-latn', { minimumFractionDigits: d, maximumFractionDigits: d }));
    const f = (x) => (x >= 100 ? nf(x, 0) : x >= 10 ? nf(x, 1) : nf(x, 2));
    if (parseFloat((x => x >= 100 ? x.toFixed(0) : x >= 10 ? x.toFixed(1) : x.toFixed(2))(v)) >= 1000 && i < 3) { v /= 1000; i++; }
    return f(v) + ' ' + u[i];
  };
  /* Same style as the ages ("5 min ago"): a number, a space, the unit. */
  const fmtDur = (s) => { s = Math.max(0, Math.round(s)); if (!isFinite(s)) return '—'; if (s >= 3600) return Math.floor(s / 3600) + ' h ' + Math.floor((s % 3600) / 60) + ' min'; if (s >= 60) return Math.floor(s / 60) + ' min ' + (s % 60) + ' s'; return s + ' s'; };
  const fmtSpeed = (bps) => (bps >= 1e6 ? (/^en/.test(lang()) ? (bps / 1e6).toFixed(1) : (bps / 1e6).toLocaleString(lang() + '-u-nu-latn', { minimumFractionDigits: 1, maximumFractionDigits: 1 })) + ' MB/s' : bps >= 1e3 ? Math.round(bps / 1e3) + ' KB/s' : Math.round(bps) + ' B/s');
  let uid = 0;
  const nid = (p) => p + (++uid);

  /* ---------- paths: POSIX (/a/b), drive (C:\a\b) and share (\\host\share\a) ---------- */
  function parsePath(p) {
    p = String(p == null ? '' : p);
    let root, rest, sep;
    if (/^[A-Za-z]:([\\/]|$)/.test(p)) { root = p.slice(0, 2).toUpperCase() + '\\'; rest = p.slice(2); sep = '\\'; }
    else if (/^\\\\[^\\/]+[\\/][^\\/]+/.test(p)) { const m = /^\\\\([^\\/]+)[\\/]([^\\/]+)(.*)$/.exec(p); root = '\\\\' + m[1] + '\\' + m[2] + '\\'; rest = m[3]; sep = '\\'; }
    else { root = '/'; rest = p; sep = '/'; }
    const steps = [];
    /* a backslash separates only on Windows paths; on a POSIX path it is an ordinary file-name character */
    for (const s of rest.split(sep === '\\' ? /[\\/]/ : /\//)) { if (!s || s === '.') continue; if (s === '..') steps.pop(); else steps.push(s); }
    return { root, sep, steps };
  }
  const build = (r) => r.root + r.steps.join(r.sep);
  const norm = (p) => build(parsePath(p));
  const segs = (p) => parsePath(p).steps;
  const join = (a, b) => { const r = parsePath(a); const add = String(b).split(r.sep === '\\' ? /[\\/]/ : /\//); return build(parsePath(r.root + r.steps.concat(add).join(r.sep))); };
  const parentOf = (p) => { const r = parsePath(p); if (!r.steps.length) return r.root; r.steps.pop(); return build(r); };
  const baseOf = (p) => { const r = parsePath(p); return r.steps.length ? r.steps[r.steps.length - 1] : r.root; };
  /* the path made of the first n steps, for breadcrumbs */
  const upTo = (p, n) => { const r = parsePath(p); r.steps = r.steps.slice(0, n); return build(r); };
  const isRoot = (p) => !parsePath(p).steps.length;

  /* ---------- file kinds ---------- */
  const EXT = { apk: 'pkg', aab: 'pkg', deb: 'pkg', appimage: 'pkg', rpm: 'pkg', msi: 'pkg', dmg: 'pkg', pkg: 'pkg', snap: 'pkg', flatpak: 'pkg', mp4: 'video', mov: 'video', mkv: 'video', avi: 'video', webm: 'video', m4v: 'video', mp3: 'audio', flac: 'audio', wav: 'audio', ogg: 'audio', m4a: 'audio', opus: 'audio', png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image', bmp: 'image', svg: 'image', exr: 'image', heic: 'image', tif: 'image', tiff: 'image', zip: 'archive', zst: 'archive', gz: 'archive', tgz: 'archive', tar: 'archive', xz: 'archive', bz2: 'archive', '7z': 'archive', rar: 'archive', md: 'text', txt: 'text', log: 'text', rtf: 'text', csv: 'text', json: 'code', yml: 'code', yaml: 'code', toml: 'code', xml: 'code', html: 'code', css: 'code', js: 'code', ts: 'code', go: 'code', rs: 'code', py: 'code', sh: 'code', java: 'code', kt: 'code', c: 'code', h: 'code', cpp: 'code', sql: 'code', pdf: 'doc', doc: 'doc', docx: 'doc', odt: 'doc', xls: 'doc', xlsx: 'doc', ppt: 'doc', pptx: 'doc', jks: 'key', keystore: 'key', pem: 'key', key: 'key', pub: 'key', gpg: 'key' };
  const KINDS = { dir: 'Folder', pkg: 'Package', video: 'Video', audio: 'Audio', image: 'Image', archive: 'Archive', text: 'Text', code: 'Data', doc: 'Document', key: 'Key', exec: 'Executable', file: 'File' };
  const isExec = (n) => n.t !== 'dir' && /^-..[xs]/.test(n.perm || '') && !EXT[((/\.([a-z0-9]+)$/i.exec(n.n) || [])[1] || '').toLowerCase()];
  const kindOf = (n) => { if (n.t === 'dir') return 'dir'; const m = /\.([a-z0-9]+)$/i.exec(n.n); const k = m && EXT[m[1].toLowerCase()]; if (k) return k; return isExec(n) ? 'exec' : 'file'; };
  const FILTERS = {
    all: () => true, folders: (n) => n.t === 'dir', packages: (n) => kindOf(n) === 'pkg',
    media: (n) => ['video', 'image', 'audio'].includes(kindOf(n)), archives: (n) => kindOf(n) === 'archive', big: (n) => n.t === 'file' && n.b >= SEARCH.bigMin
  };
  const permOctal = (p) => { p = p || ''; if (p.length < 10) return '0644'; const v = (s) => (s[0] === 'r' ? 4 : 0) + (s[1] === 'w' ? 2 : 0) + (s[2] === 'x' ? 1 : 0); return '0' + v(p.slice(1, 4)) + v(p.slice(4, 7)) + v(p.slice(7, 10)); };
  const sizeOf = (n) => (n.t === 'dir' ? 0 : n.b);

  /* ---------- state ---------- */
  const settings = { parallel: 2, limit: 0, onConflict: 'ask', verify: true, autoReconnect: true, downloadDir: '', theme: 'system', density: 'comfortable', openMode: 'single', uiSize: 100, railWide: false, sideR: 21, sideHide: false, notifyDone: true, notifyErrors: true, requireApproval: true, trash: true, trashDays: TRASH_DAYS_DEFAULT, pairLife: PAIR_LIFE_DEFAULT };
  let servers = [], tasks = [], history = [], notes = [], listeners = [];
  const emit = (type, data) => { for (const l of listeners.slice()) { try { l(type, data); } catch (e) { if (typeof console !== 'undefined') console.error(e); } } };
  const note = (kind, text) => { const n = { id: nid('n'), at: now(), kind, text, read: false }; notes.unshift(n); if (notes.length > 60) notes.pop(); emit('notify', n); };
  const log = (kind, text, extra) => { history.unshift(Object.assign({ id: nid('h'), at: now(), kind, text }, extra)); if (history.length > 300) history.pop(); emit('history'); };

  /* ---------- folder cache ---------- */
  /* FS[host][dirPath] = { items: [node], state: 'loading'|'loaded'|'error', err, at } */
  const FS = {};
  const dirsOf = (host) => (FS[host] = FS[host] || {});
  const LOCAL = 'local';
  const entryNode = (e) => {
    const dir = !!e.isDir;
    return { n: e.name, t: dir ? 'dir' : 'file', b: e.size || 0, mod: e.modified ? Date.parse(e.modified) || 0 : 0, perm: e.mode || (dir ? 'drwxr-xr-x' : '-rw-r--r--'), own: '', path: e.path, mime: e.mimeType || '', link: !!e.isSymlink, to: e.symlinkTarget || '', cc: e.childCount == null ? null : e.childCount };
  };
  const dirNode = (host, path, items) => ({ n: baseOf(path) || '/', t: 'dir', b: 0, mod: 0, perm: 'drwxr-xr-x', own: '', path, kids: items, cc: items ? items.length : null });
  const cached = (host, path) => dirsOf(host)[norm(path)] || null;
  const kidsOf = (d) => d.kids || [];
  // With hidden files off the list omits dotfiles, so a loaded folder's count must too (audit F-36).
  let dotsHidden = () => false;
  const itemCount = (n) => {
    if (n.t !== 'dir') return 0;
    const base = n.cc != null ? n.cc : n.kids ? n.kids.length : 0;
    return n.kids && dotsHidden() ? base - n.kids.filter((k) => k.n.charAt(0) === '.').length : base;
  };
  // The agent counts at most 1000 children of a folder it has not listed; that figure is a floor, not the count (audit F-35).
  const itemsLabel = (n, fmt) => { const c = itemCount(n); const capped = !n.kids && n.cc != null && n.cc >= 1000; return (capped ? '1000+' : fmt ? fmt(c) : String(c)) + (c === 1 && !capped ? ' item' : ' items'); };

  function getNode(host, path) {
    path = norm(path);
    if (isRoot(path)) { const c = cached(host, path); return dirNode(host, path, c && c.state === 'loaded' ? c.items : null); }
    const c = cached(host, path);
    if (c && c.state === 'loaded') {
      const par = cached(host, parentOf(path)); const own = par && par.items ? par.items.find((x) => x.n === baseOf(path)) : null;
      return Object.assign(own ? Object.assign({}, own) : dirNode(host, path), { kids: c.items, cc: c.items.length });
    }
    const par = cached(host, parentOf(path));
    if (par && par.items) { const own = par.items.find((x) => x.n === baseOf(path)); if (own) return own; }
    return null;
  }
  /* the cached listing of a folder, or null while it is not here. Asking starts loading it. */
  function list(host, path) {
    const c = cached(host, path);
    if (c && (c.state === 'loaded' || (c.state === 'loading' && c.keep))) return c.items; /* a folder being read again keeps showing what it had */
    /* A folder that failed is not asked again here: a failed load announces itself, and reading it again would loop. It is read again on Refresh, on opening it, or when the server is back. */
    if (!c) load(host, path).catch(NOOP);
    return null;
  }
  const listState = (host, path) => { const c = cached(host, path); return c ? c.state : 'none'; };
  const listError = (host, path) => { const c = cached(host, path); return c && c.state === 'error' ? c.err : null; };
  const exists = (host, dir, name) => !!(list(host, dir) || []).find((x) => x.n === name);

  /* Whether the agent says this device may not wake computers (its wake grant is false); unknown or not yet read is not a refusal. */
  const wakeDenied = (s) => !!(s && s.caps && s.caps.wake === false);
  function capsOf(host) { const s = host === LOCAL ? null : server(host); return (s && s.caps) || null; }
  const canRead = (n) => !(n.perm && n.perm.length >= 10 && n.perm.slice(1).indexOf('r') < 0);
  const canWrite = (d, host) => { const c = host ? capsOf(host) : null; return !(host && host !== LOCAL && ((server(host) || {}).readOnly || (c && c.modify === false))); };

  /* Loads every page of a folder. `force` reloads one that is already here. */
  function load(host, path, force) {
    path = norm(path);
    const dirs = dirsOf(host); const old = dirs[path];
    if (old && old.state === 'loading' && old.p) return old.p;
    if (old && old.state === 'loaded' && !force) return Promise.resolve(old.items);
    const keep = !!(old && old.state === 'loaded'); /* read again while its listing stays on show: no 'fs' unless the answer differs */
    const rec = { items: old && old.items ? old.items : [], state: 'loading', err: null, at: now(), keep };
    dirs[path] = rec; /* before the work starts: a listing built without waiting must not be overwritten by 'loading' */
    const p = (async () => {
      let items; let more = false;
      if (host === LOCAL) {
        if (isRoot(path) && path === '/' ) {
          const places = await call('local_places'); items = places.map((pl) => ({ n: pl.label, t: 'dir', b: 0, mod: 0, perm: 'drwxr-xr-x', own: '', path: pl.path, mime: '', link: false, to: '', cc: null, place: pl.kind }));
          /* Where "/" is a real folder (Linux, macOS) its own entries follow the places, so "Computer" is not a row that opens itself. */
          if (places.some((p2) => p2.path === '/')) {
            items = items.filter((it) => it.path !== '/');
            const page = await call('local_list', { path: '/' }).catch(() => ({ entries: [] }));
            items = items.concat(page.entries.map(entryNode));
          }
        } else { const page = await call('local_list', { path }); items = page.entries.map(entryNode); }
      } else {
        const s = server(host);
        /* "/" means the server's list of places or a real folder, depending on its roots: they are known before it is read. */
        if (s && path === '/' && !s.roots.length) await loadRoots(s).catch(NOOP);
        if (s && s.virtualRoot && path === s.virtualRoot) {
          items = (s.roots || []).map((r) => ({ n: r.label || baseOf(r.path), t: 'dir', b: 0, mod: 0, perm: 'drwxr-xr-x', own: '', path: r.path, mime: '', link: false, to: '', cc: null, root: true, total: r.totalBytes, free: r.freeBytes }));
        } else {
          items = []; let cursor = null; let guard = 0; more = false;
          do {
            const page = await call('files_list', { host, path, cursor, limit: PAGE_LIMIT });
            for (const e of page.entries) items.push(entryNode(e));
            cursor = page.nextCursor; guard++;
          } while (cursor && guard < MAX_PAGES);
          more = !!cursor; /* the agent has more than the MAX_PAGES pages read */
        }
      }
      const same = keep && sameListing(rec.items, items);
      dirs[path] = { items: same ? rec.items : items, state: 'loaded', err: null, at: now(), more };
      if (!same) emit('fs', { host, dir: path });
      return dirs[path].items;
    })().catch((e) => { dirs[path] = { items: rec.items, state: 'error', err: e, at: now() }; emit('fs', { host, dir: path }); throw e; });
    rec.p = p;
    if (dirs[path] === rec && !keep) emit('fs', { host, dir: path });
    return p;
  }
  /* True when two folder listings hold the same entries in the same order. */
  const sameListing = (a, b) => a.length === b.length && JSON.stringify(a) === JSON.stringify(b);
  /* Forgets the cached listing of `path`; with `deep`, also those of every folder below it (matched at a path separator, so "/a/b" never takes "/a/bc"). */
  const drop = (host, path, deep) => {
    const dirs = dirsOf(host); const p = norm(path); delete dirs[p];
    if (!deep) return;
    const pre = p.endsWith(parsePath(p).sep) ? p : p + parsePath(p).sep;
    for (const k of Object.keys(dirs)) if (k.startsWith(pre)) delete dirs[k];
  };
  const dropHost = (host) => { delete FS[host]; };
  const refresh = (host, path) => load(host, path, true);

  function insert(host, dir, node) {
    const c = cached(host, dir); if (!c || !c.items) return;
    const i = c.items.findIndex((x) => x.n === node.n); if (i >= 0) c.items[i] = node; else c.items.push(node);
    emit('fs', { host, dir: norm(dir) });
  }
  function takeOut(host, dir, names) {
    const c = cached(host, dir); const out = []; if (!c || !c.items) return out;
    for (const nm of names) { const i = c.items.findIndex((x) => x.n === nm); if (i >= 0) out.push(c.items.splice(i, 1)[0]); }
    emit('fs', { host, dir: norm(dir) }); return out;
  }
  const bad = (msg, code) => Object.assign(new Error(msg), { code: code || EINVAL });
  const validName = (name) => { if (!name || !name.trim()) throw bad('Name can’t be empty'); if (/[\/\\\0]/.test(name)) throw bad('A name can’t contain / or \\'); if (name === '.' || name === '..') throw bad('“.” and “..” are taken; choose another name'); if (name.length > api.limits.maxName) throw bad('A name can be at most ' + api.limits.maxName + ' characters'); if (name.trim() !== name) throw bad('A name can’t start or end with a space'); };
  const uniqueName = (host, dir, name) => {
    const m = /^(.*?)(\.[^.]*)?$/.exec(name); let i = 1, c;
    do { c = m[1] + ' (' + i + ')' + (m[2] || ''); i++; } while (exists(host, dir, c)); return c;
  };

  async function mkdir(host, dir, name) {
    validName(name); if (exists(host, dir, name)) throw bad('“' + name + '” already exists', EEXIST);
    const e = await call(host === LOCAL ? 'local_create_folder' : 'files_create_folder', host === LOCAL ? { parent: dir, name } : { host, parent: dir, name });
    insert(host, dir, entryNode(e)); return join(dir, name);
  }
  async function mkfile(host, dir, name) {
    validName(name); if (exists(host, dir, name)) throw bad('“' + name + '” already exists', EEXIST);
    if (host === LOCAL) throw bad('New files are made on computers; use a folder here.', EINVAL);
    await call('files_create_file', { host, parent: dir, name });
    await refresh(host, dir); return join(dir, name);
  }
  async function rename(host, dir, oldName, newName) {
    validName(newName); if (newName !== oldName && exists(host, dir, newName)) throw bad('“' + newName + '” already exists', EEXIST);
    const old = (list(host, dir) || []).find((x) => x.n === oldName); const path = old && old.path ? old.path : join(dir, oldName);
    const e = await call(host === LOCAL ? 'local_rename' : 'files_rename', host === LOCAL ? { path, newName } : { host, path, newName });
    takeOut(host, dir, [oldName]); insert(host, dir, entryNode(e));
    if (old && old.t === 'dir') drop(host, join(dir, oldName), true);
    return newName;
  }
  /* Moves items to the server's Trash (or deletes them from this computer). Returns the removed nodes. */
  async function remove(host, dir, names) {
    const items = list(host, dir) || []; const removed = [];
    for (const nm of names) {
      const it = items.find((x) => x.n === nm); if (!it) continue;
      if (host === LOCAL) await call('local_delete', { path: it.path }); else await call('files_trash', { host, path: it.path, permanent: settings.trash === false });
      removed.push(takeOut(host, dir, [nm])[0]); if (it.t === 'dir') drop(host, join(dir, nm), true);
    }
    return removed;
  }
  /* Puts back what `remove` took out of the server's Trash. */
  async function restore(host, dir, nodes) {
    if (host === LOCAL) return;
    const bin = await call('trash_items', { host });
    const want = nodes.map((n) => join(dir, n.n)); const ids = [];
    for (const w of want) { const hit = bin.filter((b) => norm(b.originalPath) === w).sort((a, b) => (b.deletedAt || '').localeCompare(a.deletedAt || ''))[0]; if (hit) ids.push(hit.id); }
    if (ids.length) await call('trash_restore_items', { host, ids });
    await refresh(host, dir);
  }
  async function copy(host, dir, names, dstDir, move) {
    const items = list(host, dir) || []; const srcs = names.map((nm) => (items.find((x) => x.n === nm) || {}).path || join(dir, nm));
    await call(move ? 'files_move' : 'files_copy', { host, sources: srcs, destDir: dstDir, duplicate: true, overwrite: false });
    await refresh(host, dstDir); if (move) await refresh(host, dir);
    return names;
  }
  const info = (host, dir) => { const n = getNode(host, dir); return n ? { items: itemCount(n), node: n } : null; };
  const total = (n) => ({ bytes: n.t === 'dir' ? 0 : n.b, files: n.t === 'dir' ? 0 : 1, dirs: 0 });
  const writableDir = (host, dir) => { if (!canWrite(null, host)) throw bad('This computer is read-only for this device.', 'EACCES'); return getNode(host, dir); };

  /* sorting / filtering */
  function view(items, o) {
    o = o || {}; const f = FILTERS[o.filter || 'all'] || FILTERS.all; const q = (o.q || '').toLowerCase(); const dir = o.dir === -1 ? -1 : 1;
    const key = o.key || 'name';
    const out = items.filter((n) => n.t === 'dir' || f(n)).filter((n) => !q || n.n.toLowerCase().includes(q));
    const cmp = {
      name: (a, b) => a.n.localeCompare(b.n, undefined, { numeric: true, sensitivity: 'base' }),
      size: (a, b) => (a.t === 'dir' ? itemCount(a) : a.b) - (b.t === 'dir' ? itemCount(b) : b.b),
      mod: (a, b) => a.mod - b.mod,
      kind: (a, b) => KINDS[kindOf(a)].localeCompare(KINDS[kindOf(b)])
    }[key] || (() => 0);
    return out.sort((a, b) => { if ((a.t === 'dir') !== (b.t === 'dir')) return a.t === 'dir' ? -1 : 1; return cmp(a, b) * dir || a.n.localeCompare(b.n); });
  }

  /* search: the agent searches; the window polls `step` until it is done */
  function makeSearch(hostIds, q, o) {
    o = o || {}; const res = []; let pending = 0; let scanned = 0; let err = null;
    const types = o.types || (o.filter === 'folders' ? ['folder'] : o.filter === 'media' ? ['image', 'video', 'audio'] : o.filter === 'archives' ? ['archive'] : undefined);
    for (const h of hostIds) {
      pending++;
      if (h === LOCAL) {
        call('local_search', { query: q, limit: o.max || SEARCH.max, types: types || [], minSize: o.minSize || (o.filter === 'big' ? SEARCH.bigMin : null), root: o.root || null })
          .then((rows) => { for (const e of rows) { const node = entryNode(e); res.push({ host: h, dir: parentOf(e.path), node, path: e.path }); } scanned += rows.length; })
          .catch((e) => { err = e; }).then(() => { pending--; emit('search'); });
        continue;
      }
      call('files_search', { host: h, options: { query: q, limit: o.max || SEARCH.max, types: types || [], minSize: o.minSize || (o.filter === 'big' ? SEARCH.bigMin : null), root: o.root || null } })
        .then((rows) => { for (const e of rows) { const node = entryNode(e); res.push({ host: h, dir: parentOf(e.path), node, path: e.path }); } scanned += rows.length; })
        .catch((e) => { err = e; }).then(() => { pending--; emit('search'); });
    }
    return { results: res, get scanned() { return scanned; }, get error() { return err; }, step() { return pending === 0; } };
  }

  /* ---------- servers ---------- */
  const server = (id) => servers.find((s) => s.id === id);
  const connected = (id) => id === LOCAL || (server(id) && server(id).state === 'online');
  const DEFAULT_PORT = 8765; /* the agent's port when a saved address names none; the window reads it as U.DEFAULT_PORT */
  /* Splits a saved or typed address into [host, port]. An IPv6 host keeps its brackets ("[::1]:8765" and "[::1]" are read as written; a bare "::1" gets them); with no port in the text, `dp` (default 8765) is used. It does not check the pieces: see parseAddr. */
  const splitAddr = (a, dp) => {
    a = String(a); dp = dp || DEFAULT_PORT;
    let m = /^(\[[^\]]*\])(?::(\d+))?$/.exec(a); if (m) return [m[1], m[2] ? +m[2] : dp];
    if ((a.match(/:/g) || []).length > 1) return ['[' + a + ']', dp];
    m = /^(.*):(\d+)$/.exec(a); return m ? [m[1], +m[2]] : [a, dp];
  };
  const ADDR_FORM = 'agent address must be host:port (put an IPv6 address in brackets, like [::1]:8765)';
  const ADDR_ZONE = 'An address with a zone name (like %eth0) is not supported. Use the PC\u2019s address without the zone, or its host name.';
  const ADDR_PORT = 'Use a port from 1 to 65535';
  /* Reads an address a person typed (host name, IPv4 or IPv6, with or without a port) the way the core will: returns { host, port, addr } with an IPv6 host in brackets, or { error } in words for the person. `port` is used when the text names none. */
  function parseAddr(text, port) {
    const t = String(text == null ? '' : text).trim();
    if (!t) return { error: 'Enter its address' };
    if (/\s/.test(t)) return { error: 'An address has no spaces' };
    if (t.includes('%')) return { error: ADDR_ZONE };
    const [host, p] = splitAddr(t, port);
    const v6 = host.startsWith('[');
    if (v6) { const inner = host.slice(1, -1); let ok = host.endsWith(']') && /^[0-9a-fA-F:.]+$/.test(inner); if (ok) { try { new URL('http://[' + inner + ']/'); } catch (e) { ok = false; } } if (!ok) return { error: ADDR_FORM }; }
    else if (/[:[\]]/.test(host)) return { error: ADDR_FORM };
    if (!Number.isInteger(p) || p < 1 || p > 65535) return { error: ADDR_PORT };
    return { host, port: p, addr: host + ':' + p };
  }
  const viaOf = (addr) => { const h = splitAddr(addr)[0]; if (/^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(h) || /\.ts\.net$/i.test(h)) return 'Tailscale'; if (/^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|127\.|169\.254\.)/.test(h) || /^localhost$/i.test(h) || /\.local$/i.test(h)) return 'LAN'; return 'Direct HTTPS'; };
  const fmtFp = (fp) => String(fp || '').replace(/[^0-9a-fA-F]/g, '').toUpperCase().replace(/(..)(?=.)/g, '$1:');
  const rawFp = (fp) => String(fp || '').replace(/[^0-9a-fA-F]/g, '').toLowerCase();
  const keyOf = (addr) => String(addr).trim().toLowerCase();
  function setState(s, st, msg) { if (s.state === st && !msg) return; const was = s.state; s.state = st; s.msg = msg || ''; if (st === 'online') { s.attempts = 0; s.error = null; } emit('servers', s); return was; }
  /* The last disk use each computer reported, kept so an offline computer still shows it and a reconnect refreshes it. */
  const DISK_KEY = 'rfe.diskUse';
  const diskCache = () => { try { return JSON.parse(localStorage.getItem(DISK_KEY)) || {}; } catch (e) { return {}; } };
  const lastDisk = (key) => { const d = diskCache()[key]; return Array.isArray(d) && d[1] > 0 ? d : [0, 0]; };
  const keepDisk = (key, d) => { try { const all = diskCache(); all[key] = d; localStorage.setItem(DISK_KEY, JSON.stringify(all)); } catch (e) { /* storage unavailable */ } };
  /* The MAC address of each computer, kept between sessions: a sleeping computer cannot be asked for it, and waking it needs it. */
  const MAC_KEY = 'rfe.macAddress';
  const macCache = () => { try { return JSON.parse(localStorage.getItem(MAC_KEY)) || {}; } catch (e) { return {}; } };
  const lastMac = (key) => { const m = macCache()[key]; return typeof m === 'string' ? m : ''; };
  const keepMac = (key, mac) => { try { const all = macCache(); if (mac) all[key] = mac; else delete all[key]; localStorage.setItem(MAC_KEY, JSON.stringify(all)); } catch (e) { /* storage unavailable */ } };
  function mkServer(h) {
    const [addr, port] = splitAddr(h.host);
    return { id: h.key, key: h.key, name: h.name || h.host, host: addr, port, addr: h.host, via: viaOf(h.host), os: '', agent: '', pinned: !!h.fingerprint, fp: fmtFp(h.fingerprint), signedIn: !!h.signedIn, user: h.username || '', active: !!h.active, state: h.signedIn ? 'connecting' : 'disconnected', latency: 0, base: 0, disk: lastDisk(h.key), hist: [], reachable: true, retryIn: 0, attempts: 0, lostAt: 0, lastSeen: 0, routes: [], roots: [], caps: null, readOnly: false, saved: true, userOff: false, mac: lastMac(h.key), version: '' };
  }
  /* Brings the list in line with the saved hosts. Servers added in the window and not yet trusted stay. */
  async function loadServers() {
    const hs = await call('list_hosts');
    const seen = new Set();
    for (const h of hs) {
      seen.add(h.key); let s = server(h.key);
      if (!s) { s = mkServer(h); servers.push(s); }
      else { s.name = h.name || s.name; s.pinned = !!h.fingerprint; s.fp = fmtFp(h.fingerprint) || s.fp; s.signedIn = !!h.signedIn; s.user = h.username || s.user; s.active = !!h.active; s.saved = true; if (!s.signedIn && s.state === 'online') setState(s, 'disconnected'); }
    }
    servers = servers.filter((s) => seen.has(s.id) || !s.saved);
    emit('servers');
    return servers;
  }

  /* One health read: the state, the latency, the disk and what the agent says about itself. */
  async function check(s) {
    if (!s || s.checking) return;
    s.checking = true; const t0 = (typeof performance !== 'undefined' ? performance.now() : Date.now());
    try {
      const snap = await call('agent_health', { host: s.id });
      if (s.userOff) return; /* disconnected while the answer was on its way: it is not a reconnect */
      const ms = Math.max(1, Math.round((typeof performance !== 'undefined' ? performance.now() : Date.now()) - t0));
      s.latency = ms; s.base = s.base ? Math.round(s.base * LATENCY_KEEP + ms * LATENCY_NEW) : ms; s.hist.push(ms); if (s.hist.length > LATENCY_HISTORY) s.hist.shift();
      const h = snap.health || {}; if (h.os) s.os = h.os; if (h.version) s.agent = h.version; if (h.name) s.agentName = h.name; if (h.macAddress) { s.mac = h.macAddress; keepMac(s.id, s.mac); }
      if (h.address) s.routeAddr = h.address; if (h.tailscaleAddress) s.tsAddr = h.tailscaleAddress; s.readOnly = !!h.readOnly;
      if (snap.status && snap.status.totalBytes) { const t = snap.status.totalBytes / BYTES_PER_GB; const f = (snap.status.freeBytes || 0) / BYTES_PER_GB; s.disk = [Math.max(0, t - f), t]; keepDisk(s.id, s.disk); }
      if (snap.status) { s.uptime = snap.status.uptimeSeconds || 0; s.platform = snap.status.platform || ''; }
      s.metrics = snap.metrics || null; s.metricsForbidden = !!snap.metricsForbidden; s.statusNote = snap.statusNote || ''; s.metricsNote = snap.metricsNote || '';
      s.lastSeen = now(); s.reachable = true; s.signedIn = true;
      const was = s.state; s.incompatible = false; setState(s, 'online');
      if (was !== 'online') { const dirs = dirsOf(s.id); for (const k of Object.keys(dirs)) if (dirs[k].state === 'error') delete dirs[k]; } /* folders that failed while it was away are read again */
      if (was !== 'online') { log('conn', 'Connected to ' + s.name + ' · ' + s.latency + ' ms'); if (settings.notifyDone && was !== 'connecting' ) note('ok', s.name + ' is back'); resumeHost(s.id); if (!s.roots.length) loadRoots(s).catch(NOOP); }
    } catch (e) {
      if (s.userOff) return;
      s.error = e.message; s.incompatible = e.code === 'INCOMPATIBLE'; /* an agent older than this app needs: shown as such, not as a dead address */
      if (e.code === 'UNAUTHORIZED') { s.signedIn = false; setState(s, 'login', e.message); }
      else if (e.code === 'CERT') { await flagCert(s).catch(NOOP); }
      else if (s.state === 'online' || s.state === 'lost') {
        if (s.state === 'online') { s.lostAt = now(); s.attempts = 0; s.retryIn = RETRY_SECONDS; setState(s, 'lost', e.message); note('error', s.name + ' connection lost'); log('conn', s.name + ' connection lost', { error: true }); suspendHost(s.id, 'Connection lost'); }
      } else { s.reachable = false; setState(s, 'offline', e.message); if (!s.lastSeenLogged) { s.lastSeenLogged = true; log('conn', 'Could not reach ' + s.name, { error: true }); } }
    } finally { s.checking = false; }
  }
  async function flagCert(s) {
    const p = await call('probe_agent', { host: s.addr });
    if (p.changed) { s.oldFp = s.fp; s.newFp = fmtFp(p.fingerprint); s.lostAt = now(); setState(s, 'refused'); note('error', s.name + ': certificate changed, connection refused'); log('conn', s.name + ' presented a different certificate (refused)', { error: true }); suspendHost(s.id, 'Certificate changed'); }
    else { setState(s, 'lost', 'The connection failed'); }
  }
  async function loadRoots(s) {
    const r = await call('files_roots', { host: s.id });
    s.roots = r.locations || []; s.caps = r.caps || null; s.readOnly = s.readOnly || !!r.readOnly; s.accessDenied = !!r.accessDenied;
    // With roots set on the agent, or on a drive-lettered system, "/" is a list of those places.
    const first = s.roots[0] && s.roots[0].path;
    s.virtualRoot = s.roots.length && (r.source === 'roots' || (first && parsePath(first).sep === '\\') || s.roots.length > 1 && !s.roots.some((x) => x.path === '/')) ? '/' : null;
    /* A folder asked for before the places were known (the first "/" of a server that shows only its allowed folders) failed; it is read again now. */
    const dirs = dirsOf(s.id); for (const k of Object.keys(dirs)) if (dirs[k].state === 'error') { delete dirs[k]; emit('fs', { host: s.id, dir: k }); }
    emit('servers', s);
  }
  const start = (id) => { if (id === LOCAL) return LOCAL_START.path || '/'; const s = server(id); if (!s) return '/'; if (s.last) return s.last; const r = s.roots && s.roots[0]; if (s.virtualRoot) return s.virtualRoot; return r ? r.path : '/'; };
  const LOCAL_START = { path: '' };

  /* Connect: a signed-in server is checked; a saved one with no login asks the user to sign in; a new
     address is asked for its certificate first, which the user must compare before anything is sent. */
  async function connect(id) {
    const s = server(id); if (!s || s.state === 'connecting' || s.state === 'online' || s.state === 'trust') return;
    s.userOff = false; s.error = null; s.attempts = 0; setState(s, 'connecting'); log('conn', 'Connecting to ' + s.name);
    try {
      if (s.signedIn && s.pinned) { await check(s); if (s.state === 'connecting') setState(s, 'offline'); return; }
      const p = await call('probe_agent', { host: s.addr });
      s.reachable = true; s.lastSeen = now();
      if (p.changed) { s.oldFp = s.fp; s.newFp = fmtFp(p.fingerprint); setState(s, 'refused'); return; }
      s.pendingFp = rawFp(p.fingerprint);
      if (s.pinned && rawFp(s.fp) === s.pendingFp) { setState(s, 'login'); return; }
      s.fp = fmtFp(p.fingerprint); setState(s, 'trust');
    } catch (e) {
      s.error = e.message; s.reachable = false; s.lastSeen = s.lastSeen || 0; setState(s, 'offline', e.message); log('conn', 'Could not reach ' + s.name, { error: true }); note('error', s.name + ' is unreachable');
    }
  }
  /* The user compared the fingerprint and accepts it: the sign-in follows; the trust is saved with the login. */
  function trust(id) { const s = server(id); if (!s || s.state !== 'trust') return; s.pendingFp = rawFp(s.fp); setState(s, 'login'); }
  function cancelConnect(id) { const s = server(id); if (s && ['connecting', 'trust', 'login'].includes(s.state)) { s.userOff = true; setState(s, 'disconnected'); } }
  function disconnect(id) { const s = server(id); if (!s || s.state === 'disconnected') return; s.userOff = true; setState(s, 'disconnected'); log('conn', 'Disconnected from ' + s.name); suspendHost(id, 'Disconnected from ' + s.name); }
  function retryNow(id) { const s = server(id); if (!s) return; s.userOff = false; if (s.state === 'lost') { s.attempts++; setState(s, 'connecting'); check(s); } else if (s.state === 'offline' || s.state === 'disconnected') connect(id); }
  /* After the user trusted the new key of a refused server: sign in again (the old login belonged to the old key). */
  function reverify(id) { const s = server(id); if (!s || s.state !== 'refused') return; s.fp = s.newFp; s.pendingFp = rawFp(s.newFp); s.oldFp = null; s.newFp = null; s.signedIn = false; setState(s, 'login'); log('conn', 'Reviewing the new certificate of ' + s.name); }
  function switchRoute(id, via) { const s = server(id); if (!s || !s.routes) return false; const r = s.routes.find((x) => x.via === via); if (!r) return false; s.via = r.via; emit('servers', s); return true; }
  function restoreServer(s) { if (!s || server(s.id)) return; servers.push(s); emit('servers', s); }
  function addServer(o) {
    /* The dialogs refuse an address that does not parse before they get here; one that still arrives (a saved one) is kept as written. */
    const raw = String(o.host || '').trim(), p = parseAddr(raw, o.port);
    const addr = p.error ? raw + (o.port && !/:\d+$/.test(raw) ? ':' + o.port : '') : p.addr; const key = keyOf(addr);
    const have = server(key); if (have) return have;
    const [h, port] = p.error ? splitAddr(addr) : [p.host, p.port];
    const s = { id: key, key, name: o.name || h, host: h, port, addr, via: o.via || viaOf(addr), os: '', agent: '', pinned: false, fp: '', signedIn: false, user: '', active: false, state: 'disconnected', latency: 0, base: 0, disk: lastDisk(key), hist: [], reachable: true, retryIn: 0, attempts: 0, lostAt: 0, lastSeen: 0, routes: [], roots: [], caps: null, readOnly: false, saved: false, userOff: false, mac: '' };
    servers.push(s); emit('servers', s); return s;
  }
  async function updateServer(id, o) {
    const s = server(id); if (!s) return;
    if (o.name && o.name !== s.name) { s.name = o.name; if (s.saved) await call('rename_host', { key: s.id, name: o.name }); }
    emit('servers', s);
  }
  async function forget(id) {
    const s = server(id); if (!s) return;
    for (const t of tasks.slice()) if (t.host === id && ACTIVE.includes(t.state)) await cancel(t.id).catch(NOOP);
    if (s.saved) await call('remove_host', { key: s.id });
    const at = servers.indexOf(s); if (at >= 0) servers.splice(at, 1); /* a second forget that waited on the first finds it gone */
    dropHost(id); keepMac(id, ''); emit('servers'); emit('transfers');
  }

  /* Sign-in: three ways, all ending with the server saved and trusted at the fingerprint the user compared. */
  async function afterSignIn(s, saved) {
    s.signedIn = true; s.pinned = true; s.saved = true; s.user = saved.username || s.user; s.fp = fmtFp(saved.fingerprint) || s.fp; s.pendingFp = null; s.error = null;
    log('conn', 'Signed in to ' + s.name + (s.user ? ' as ' + s.user : ''));
    /* The core saves the host under its address; the name typed in Add a computer is the app's. */
    const want = s.name; setState(s, 'connecting'); await loadServers().catch(NOOP);
    if (want && s.name !== want) { s.name = want; await call('rename_host', { key: s.id, name: want }).catch(NOOP); emit('servers', s); }
    await check(s);
    return s;
  }
  async function signIn(id, username, password) {
    const s = server(id); if (!s) throw bad('Unknown computer', 'ENOENT');
    const saved = await call('login', { host: s.addr, fingerprint: s.pendingFp || rawFp(s.fp), username, password });
    return afterSignIn(s, saved);
  }
  async function signInWithCode(id, code) {
    const s = server(id); if (!s) throw bad('Unknown computer', 'ENOENT');
    const saved = await call('pair_with_code', { host: s.addr, fingerprint: s.pendingFp || rawFp(s.fp), code });
    return afterSignIn(s, saved);
  }
  /* "Ask the PC to approve this computer": returns the match code to show; `pollApproval` follows. */
  async function requestApproval(id) { const s = server(id); return call('request_pairing', { host: s.addr, fingerprint: s.pendingFp || rawFp(s.fp) }); }
  async function pollApproval(id) { const r = await call('poll_pairing'); if (r && r.status === 'approved') { const s = server(id); await afterSignIn(s, r.saved || { username: '', fingerprint: s.pendingFp || '' }); } return r; }
  const cancelApproval = () => call('cancel_pairing');
  async function signOut(id) {
    const s = server(id); if (!s) return null;
    const r = await call('switch_host', { key: s.id }).then(() => call('sign_out'));
    s.signedIn = false; s.userOff = false; setState(s, 'disconnected'); suspendHost(id, 'Signed out of ' + s.name); await loadServers().catch(NOOP);
    return r;
  }

  /* ---------- transfers ---------- */
  const ACTIVE = ['queued', 'running', 'paused', 'waiting', 'conflict'];
  const taskById = (id) => tasks.find((t) => t.id === id);
  const running = () => tasks.filter((t) => t.state === 'running');
  const byRemote = {}; // rust id -> task
  /* Queues transfers. o: {dir:'up'|'down', host, srcDir, dstDir, names:[]}. Uploads read this computer's
     listing (`local`), downloads the server's; a folder goes up or down with everything in it. */
  function enqueue(o) {
    const remote = o.host;
    if (!connected(remote)) return { error: (server(remote) ? server(remote).name : remote) + ' is not connected' };
    const up = o.dir === 'up'; const srcHost = up ? LOCAL : remote; const out = [];
    for (const name of o.names) {
      const n = (list(srcHost, o.srcDir) || []).find((x) => x.n === name); if (!n) continue;
      const t = { id: nid('t'), dir: o.dir, host: remote, name, isDir: n.t === 'dir', bytes: Math.max(1, n.b || 0), files: 1, srcHost, srcDir: norm(o.srcDir), dstHost: up ? remote : LOCAL, dstDir: up ? norm(o.dstDir) : ((api.destFor && api.destFor(remote)) || settings.downloadDir || ''), state: 'queued', done: 0, speed: 0, queuedAt: now(), startedAt: 0, finishedAt: 0, msg: '', resolution: o.resolution || null, askWhere: !!o.askWhere, kind: kindOf(n), rids: [], srcPath: n.path || join(o.srcDir, name), lastDone: 0, lastAt: 0 };
      tasks.push(t); out.push(t); startTask(t);
    }
    if (out.length) { log('queue', (up ? 'Queued upload: ' : 'Queued download: ') + out.map((t) => t.name).join(', ')); emit('transfers'); }
    return { tasks: out };
  }
  async function startTask(t) {
    t.starting = true;
    try {
      const ids = t.dir === 'up'
        ? await call('transfer_upload_tree', { host: t.host, localPath: t.srcPath, remoteDir: t.dstDir })
        : await call('transfer_download_tree', { host: t.host, remotePath: t.srcPath, isDir: t.isDir, askWhere: !!t.askWhere, destDir: (api.destFor && api.destFor(t.host)) || null });
      if (t.state === 'cancelled') { for (const id of ids) call('transfer_cancel', { id }).catch(NOOP); return; } /* cancelled while it was being set up */
      if (!ids.length) { /* a folder with nothing in it: the folders were made, there is nothing to send */
        t.state = 'done'; t.done = t.bytes; t.speed = 0; t.finishedAt = now(); t.started = true; log('transfer', (t.dir === 'up' ? 'Uploaded ' : 'Downloaded ') + t.name + ' (empty folder)', { tid: t.id, ok: true, dir: t.dir, host: t.host }); emit('transfers'); return;
      }
      t.rids = ids; t.files = ids.length; for (const id of ids) byRemote[id] = t; t.started = true;
      if (t.held) for (const id of ids) call('transfer_pause', { id }).catch(NOOP); /* the server went away while it was being set up */
      pollTransfers();
    } catch (e) {
      if (t.held && t.state === 'waiting') { t.msg = 'Waiting for ' + (server(t.host) ? server(t.host).name : 'the computer'); emit('transfers'); } /* it starts again when the server is back */
      else failTask(t, e.message);
    } finally { t.starting = false; }
  }
  function failTask(t, msg, fix) { if (t.state === 'cancelled' || tasks.indexOf(t) < 0) return; /* cancelled or removed while it was being set up: nothing to fail */
    t.state = 'failed'; t.msg = msg; t.fix = fix || null; t.speed = 0; t.finishedAt = now(); log('transfer', t.name + ' failed: ' + msg, { tid: t.id, error: true, dir: t.dir, host: t.host }); if (settings.notifyErrors) note('error', t.name + ': ' + msg); emit('transfers'); }
  let polling = false;
  async function pollTransfers() {
    if (polling) return; polling = true;
    try {
      const views = await call('transfer_list'); const groups = new Map();
      for (const v of views) { const t = byRemote[v.id]; if (!t) continue; if (!groups.has(t)) groups.set(t, []); groups.get(t).push(v); }
      let changed = false;
      for (const [t, vs] of groups) {
        if (t.state === 'cancelled') continue;
        const done = vs.reduce((a, v) => a + (v.done || 0), 0); const tot = vs.reduce((a, v) => a + (v.total || 0), 0);
        const cv = vs.find((v) => v.state === 'conflict'); t.conflictRids = vs.filter((v) => v.state === 'conflict').map((v) => v.id);
        let st = cv ? 'conflict' : vs.some((v) => v.state === 'running') ? 'running' : vs.some((v) => v.state === 'queued') ? 'queued' : vs.some((v) => v.state === 'paused') ? 'paused' : vs.some((v) => v.state === 'failed') ? 'failed' : vs.every((v) => v.state === 'cancelled') ? 'cancelled' : 'done';
        const nowMs = now(); const was = t.state;
        if (was === 'paused' && (st === 'running' || st === 'queued') && nowMs - (t.pauseAt || 0) < 3000) st = 'paused'; /* the core is still stopping it */
        if (was === 'waiting' && (st === 'running' || st === 'queued' || st === 'paused' || (st === 'failed' && t.held))) st = 'waiting'; /* the server is away: it is held until it is back */
        /* A transfer that stopped because the link dropped is held, not failed: the core often reports the drop before the next health check does. The server is checked now; when it answers the transfer is started again (a few times at most), otherwise it waits for the reconnect like the others. */
        if (st === 'failed' && was !== 'failed' && (t.netRetries || 0) < 5) {
          const fv = vs.find((v) => v.state === 'failed');
          if (fv && toError(fv.error || '').code === 'NETWORK') {
            t.netRetries = (t.netRetries || 0) + 1; t.held = true; st = 'waiting'; t.speed = 0; t.msg = 'Connection lost' + (done && tot ? ' at ' + Math.floor(done / tot * 100) + '%' : '');
            const sv = server(t.host);
            if (sv) check(sv).then(() => { if (sv.state === 'online' && t.state === 'waiting' && t.held) { t.held = false; t.resumedAt = now(); retryRust(t); } });
          }
        }
        /* The core finished stopping a transfer that the window has already asked to start again (a reconnect that came while it was stopping): start it again. Only for a short while after a reconnect, so a Pause all from the tray stays paused. */
        if (st === 'paused' && (was === 'queued' || was === 'running') && nowMs - (t.resumedAt || 0) < 20000 && nowMs - (t.autoRetryAt || 0) > 1500) { t.autoRetryAt = nowMs; retryRust(t); st = 'queued'; }
        if (t.lastAt) { const dt = (nowMs - t.lastAt) / 1000; if (dt > 0.2) { const inst = Math.max(0, (done - t.lastDone) / dt); t.speed = t.speed ? t.speed * 0.6 + inst * 0.4 : inst; t.lastDone = done; t.lastAt = nowMs; } } else { t.lastDone = done; t.lastAt = nowMs; }
        t.done = done; if (tot) t.bytes = Math.max(tot, done, 1); t.files = vs.length; t.verified = vs.every((v) => v.verified);
        if (st === 'running' && !t.startedAt) t.startedAt = nowMs;
        if (vs.every((v) => v.state === 'cancelled' && /^Skipped:/.test(v.error || ''))) {
          for (const v of vs) delete byRemote[v.id]; const i = tasks.indexOf(t); if (i >= 0) tasks.splice(i, 1);
          log('transfer', 'Skipped ' + t.name + ' (already exists)', { tid: t.id, dir: t.dir, host: t.host }); changed = true; continue;
        }
        if (st === 'conflict') { const c = cv.conflict || {}; t.state = 'conflict'; t.speed = 0; t.conflict = { size: c.size || 0, mod: c.modifiedMs || Date.parse(c.modifiedText || '') || 0, dir: !!c.isDir, name: cv.name || '', mine: cv.total || 0, more: Math.max(0, vs.filter((v) => v.state === 'conflict').length - 1) }; t.msg = 'A ' + (c.isDir ? 'folder' : 'file') + ' with this name already exists'; }
        else if (st === 'failed') { const f = vs.find((v) => v.state === 'failed'); const nf = vs.filter((v) => v.state === 'failed').length; t.state = 'failed'; t.msg = (nf > 1 ? nf + ' of ' + vs.length + ' files failed. First: ' : '') + (f.error || 'Failed'); t.speed = 0; if (was !== 'failed') { t.finishedAt = nowMs; log('transfer', t.name + ' failed: ' + t.msg, { tid: t.id, error: true, dir: t.dir, host: t.host }); if (settings.notifyErrors) note('error', t.name + ': ' + t.msg); } }
        else if (st === 'done') { t.state = 'done'; t.done = t.bytes; t.speed = 0; if (was !== 'done') { t.finishedAt = nowMs; t.savedAs = vs.length === 1 ? vs[0].name : t.name; if (t.dir === 'down') { const lp = (vs.find((v) => v.localPath) || {}).localPath; if (lp && vs.length === 1) { t.savedPath = lp; t.dstDir = parentOf(lp); } } log('transfer', (t.dir === 'up' ? 'Uploaded ' : 'Downloaded ') + t.name + ' · ' + fmtBytes(t.bytes) + ' in ' + fmtDur((t.finishedAt - (t.startedAt || t.queuedAt)) / 1000), { tid: t.id, ok: true, dir: t.dir, host: t.host }); if (settings.notifyDone) note('ok', t.name + ' ' + (t.dir === 'up' ? 'uploaded' : 'downloaded')); if (t.dir === 'up') refresh(t.host, t.dstDir).catch(NOOP); else refresh(LOCAL, t.dstDir).catch(NOOP); } }
        else if (st === 'cancelled') t.state = 'cancelled';
        else t.state = st;
        if (t.state !== was) changed = true; /* progress is drawn by the tick, not by a rebuild of the page on every poll */
      }
      if (changed) emit('transfers');
    } catch (e) { /* the next poll tries again */ } finally { polling = false; }
  }
  function suspendHost(hostId, why) {
    let any = false;
    for (const t of tasks) {
      if (t.host !== hostId || !['running', 'queued', 'conflict'].includes(t.state)) continue;
      t.held = true; /* stopped in the core, so it is started again when the server is back */
      if (t.state !== 'conflict') { t.state = 'waiting'; t.speed = 0; t.msg = why + (t.done && t.bytes ? ' at ' + Math.floor(t.done / t.bytes * 100) + '%' : ''); any = true; }
      /* Stop sending, keep what is done. A folder in conflict may still have children running. */
      for (const rid of t.rids) call('transfer_pause', { id: rid }).catch(NOOP);
    }
    if (any) emit('transfers');
  }
  function resumeHost(hostId) {
    let any = false;
    for (const t of tasks) if (t.host === hostId && (t.state === 'waiting' || t.held)) { t.held = false; if (t.rids.length) { t.resumedAt = now(); retryRust(t); } else if (t.state === 'waiting') { t.state = 'queued'; t.msg = ''; if (!t.starting) startTask(t); } any = true; }
    if (any) emit('transfers');
  }
  async function retryRust(t) { t.held = false; t.state = 'queued'; t.msg = ''; t.fix = null; emit('transfers'); await Promise.all(t.rids.map(async (id) => { try { await call('transfer_retry', { id }); } catch (e) { /* done or gone */ } })); pollTransfers(); }
  async function pause(id) {
    const t = taskById(id); if (!t || !['running', 'queued'].includes(t.state)) return;
    if (!t.rids.length) throw bad('Wait until the transfer has started, then pause it.', EINVAL);
    t.state = 'paused'; t.speed = 0; t.pauseAt = now(); emit('transfers');
    await Promise.all(t.rids.map(async (rid) => { try { await call('transfer_pause', { id: rid }); } catch (e) { /* finished meanwhile */ } })); /* all files at once, so the button does not flip while it waits */
    log('transfer', 'Paused ' + t.name, { tid: t.id, dir: t.dir, host: t.host }); pollTransfers();
  }
  function resumeTask(id) { const t = taskById(id); if (t) retryRust(t); }
  async function cancel(id) {
    const t = taskById(id); if (!t) return;
    const was = t.state; t.state = 'cancelled'; t.speed = 0;
    /* The card goes at once; the core is told about every file in parallel, not one after the other. */
    const rids = t.rids.slice(); const i = tasks.indexOf(t); if (i >= 0) tasks.splice(i, 1); emit('transfers');
    await Promise.all(rids.map(async (rid) => { try { await call('transfer_cancel', { id: rid }); } catch (e) { /* already finished */ } delete byRemote[rid]; }));
    if (was !== 'failed') log('transfer', 'Cancelled ' + t.name, { tid: t.id, dir: t.dir, host: t.host });
  }
  function dismiss(id) { const i = tasks.findIndex((t) => t.id === id); if (i >= 0 && !ACTIVE.includes(tasks[i].state)) { for (const rid of tasks[i].rids) delete byRemote[rid]; tasks.splice(i, 1); emit('transfers'); } }
  function retry(id) { const t = taskById(id); if (!t || (t.state !== 'failed' && t.state !== 'paused')) return; if (!t.rids.length) { t.held = false; t.state = 'queued'; t.msg = ''; if (!t.starting) startTask(t); emit('transfers'); } else retryRust(t); } /* a task the core is still setting up is not started a second time; it is no longer held, so it is not paused when the core answers */
  /* The answer to a name that is taken: how is 'replace', 'keep' or 'skip'. `all` answers every waiting transfer. */
  async function resolve(id, how, all) {
    const wait = all ? tasks.filter((x) => x.state === 'conflict') : [taskById(id)].filter(Boolean);
    for (const t of wait) { if (t.state !== 'conflict') continue; if (!connected(t.host)) { note('error', 'Connect to ' + (server(t.host) ? server(t.host).name : 'the computer') + ' first, then answer.'); continue; } t.state = 'queued'; t.msg = ''; for (const rid of t.conflictRids || []) { try { await call('transfer_resolve', { id: rid, how }); } catch (e) { /* answered or gone */ } } }
    emit('transfers'); pollTransfers();
  }
  /* Tells the core the settings that change how transfers run. */
  async function pushPrefs() { try { await call('transfer_set_prefs', { parallel: +settings.parallel || 2, limitMbps: +settings.limit || 0, onConflict: settings.onConflict || 'keep', verify: settings.verify !== false }); } catch (e) { /* the core says nothing here; the next change tries again */ } }
  function pauseAll() { for (const t of tasks) if (t.state === 'running' || t.state === 'queued') { if (t.rids.length) pause(t.id); else { t.held = true; t.state = 'paused'; t.pauseAt = now(); emit('transfers'); } } } /* a task not started yet is held and paused when it starts */
  function resumeAll() { for (const t of tasks) if (t.state === 'waiting' || t.state === 'paused') retry(t.id); }
  async function clearDone() { const n = tasks.length; for (let i = tasks.length - 1; i >= 0; i--) if (tasks[i].state === 'done' || tasks[i].state === 'cancelled') { for (const rid of tasks[i].rids) delete byRemote[rid]; tasks.splice(i, 1); } if (tasks.length !== n) { emit('transfers'); try { await call('transfer_clear_finished'); } catch (e) { /* ignore */ } } }
  function retryFailed() { for (const t of tasks) if (t.state === 'failed') retry(t.id); }
  const eta = (t) => (t.speed > 0 ? (t.bytes - t.done) / t.speed : Infinity);

  /* ---------- the clock ---------- */
  let accHealth = 0, accXfer = 0, ticking = false;
  function tick(dt) {
    accXfer += dt; accHealth += dt;
    /* The reconnect countdown the pages show runs on the clock itself, so "Retrying in 8 s" really is 8 s. */
    for (const s of servers) {
      if (s.userOff || !s.signedIn || s.state !== 'lost' || !settings.autoReconnect) continue;
      s.retryIn -= dt / 1000; if (s.retryIn <= 0) { s.attempts++; s.retryIn = RETRY_SECONDS; check(s); }
    }
    const active = tasks.some((t) => ACTIVE.includes(t.state) && t.started);
    if (accXfer >= (active ? 700 : 4000)) { accXfer = 0; if (tasks.length) pollTransfers(); }
    if (accHealth >= 6000) {
      accHealth = 0;
      for (const s of servers) {
        if (s.userOff || !s.signedIn) continue;
        if (s.state === 'online' || s.state === 'connecting') check(s);
        else if (s.state === 'offline' && settings.autoReconnect) check(s);
      }
      emit('latency');
    }
    emit('tick');
  }

  async function init() {
    pushPrefs();
    try { const l = await call('desktop_limits'); for (const k of Object.keys(api.limits)) if (Number.isFinite(l && l[k])) api.limits[k] = l[k]; } catch (e) { /* the defaults stay */ }
    try { const f = await call('transfer_folder'); settings.downloadDir = f; } catch (e) { /* the core says why when a transfer starts */ }
    try { const pl = await call('local_places'); const home = pl.find((p) => p.kind === 'home') || pl[0]; LOCAL_START.path = (pl.find((p) => /Downloads$/.test(p.path)) || home || { path: '/' }).path; LOCAL_START.home = home && home.path; LOCAL_START.places = pl; } catch (e) { LOCAL_START.path = '/'; }
    await loadServers();
    /* A signed-in computer is read at once, unless the window's hook says it must not connect on start: that one stays "disconnected" until the user connects it. */
    for (const s of servers) if (s.signedIn) { if (api.hooks.autoConnect(s) !== false) check(s); else { s.userOff = true; setState(s, 'disconnected'); } }
    return servers;
  }

  /* ---------- the refresh schedule ---------- */
  /* What the window re-reads while a page is open: the pages that show it, and how often (ms) when idle and while a transfer is active (omitted: the same). */
  const LIVE_JOBS = {
    transfers: { views: ['transfers', 'home'], ms: 5000, activeMs: 1000 },
    history: { views: ['history'], ms: 5000, activeMs: 2000, pushMs: 60000 },
    devices: { views: ['devices', 'servers'], ms: 5000, pushMs: 60000 },
    files: { views: ['files'], ms: 5000, pushMs: 60000 },
    tools: { views: ['tools'], ms: 10000 }
  };
  /* The jobs one pushed event kind (from a computer's /v1/events stream) refreshes, and on which pages. "hello" is not listed: a fresh stream refreshes every job the open page has. */
  const LIVE_EVENTS = {
    audit: { history: ['history', 'devices', 'servers'] },
    devices: { devices: ['devices', 'servers'] },
    files: { files: ['files'] },
    settings: { tools: ['tools'] }
  };
  /* How often a job runs: the pushed interval while every computer's stream is connected (push) and the job has one, else the shorter interval while a transfer is active, if it has one. */
  function liveMs(job, active, push) { const j = LIVE_JOBS[job]; return push && j.pushMs ? j.pushMs : active && j.activeMs ? j.activeMs : j.ms; }
  /* Pure: the jobs to run now for an event of `kind` while page `view` is open: only jobs that page shows, never the window's own transfer list. */
  function liveEventJobs(kind, view) {
    if (kind === 'hello') {
      const out = new Set(Object.keys(LIVE_JOBS).filter((k) => k !== 'transfers' && LIVE_JOBS[k].views.includes(view)));
      for (const m of Object.values(LIVE_EVENTS)) for (const k of Object.keys(m)) if (m[k].includes(view)) out.add(k);
      return [...out];
    }
    const m = LIVE_EVENTS[kind]; if (!m) return [];
    return Object.keys(m).filter((k) => m[k].includes(view));
  }
  /* Pure: given {view, visible, active, push, now, last} (push: every computer's stream is connected; last maps a job to the time it last ran) it names the jobs that are due now. A hidden window has none; a job that never ran is due; only jobs of the open page are considered. */
  function liveDue(s) {
    if (!s.visible) return [];
    return Object.keys(LIVE_JOBS).filter((k) => LIVE_JOBS[k].views.includes(s.view) && (s.last[k] === undefined || s.now - s.last[k] >= liveMs(k, s.active, s.push)));
  }

  /* One CSV cell as text in double quotes. A value that starts with = + - @ tab or CR gets a single quote in front, so a spreadsheet shows it as text instead of running it as a formula. */
  function csvCell(v) {
    let s = String(v == null ? '' : v);
    if (/^[=+\-@\t\r]/.test(s)) s = "'" + s;
    return '"' + s.replace(/"/g, '""') + '"';
  }
  /* True when `text` holds the whole device `name` (not a piece of a longer word) at least once outside a longer device name from `names`, so "Pixel" does not match a line about "Pixel 7". An empty name matches nothing. */
  function mentionsDevice(text, name, names) {
    const t = String(text == null ? '' : text), n = String(name == null ? '' : name).trim();
    if (!n) return false;
    const wordCh = /[\p{L}\p{N}_]/u;
    const longer = (names || []).map(String).filter((m) => m.length > n.length && m.includes(n));
    const insideLonger = (at) => longer.some((m) => { for (let s = m.indexOf(n); s !== -1; s = m.indexOf(n, s + 1)) if (at - s >= 0 && t.startsWith(m, at - s)) return true; return false; });
    for (let i = t.indexOf(n); i !== -1; i = t.indexOf(n, i + 1)) {
      const end = i + n.length;
      if ((i > 0 && wordCh.test(t[i - 1])) || (end < t.length && wordCh.test(t[end]))) continue;
      if (!insideLonger(i)) return true;
    }
    return false;
  }

  /* ---------- recent files and per-computer scoping ---------- */

  /* True for a path nobody wants in a "recent files" list: a hidden dotfile or dot-folder anywhere in it, an operating-system
     folder (/proc, /sys, /dev, the Windows folder), a user's AppData, or a node_modules folder. Both slash styles work. */
  const SYSTEM_TOP = new Set(['proc', 'sys', 'dev']);
  function isNoisePath(p) {
    const parts = String(p || '').replace(/\\/g, '/').split('/').filter(Boolean);
    if (parts.length && /^[a-z]:$/i.test(parts[0])) parts.shift();
    else if (parts.length && SYSTEM_TOP.has(parts[0].toLowerCase())) return true;
    if (parts.length && parts[0].toLowerCase() === 'windows' && /^[a-z]:/i.test(String(p))) return true;
    return parts.some((s) => (s[0] === '.' && s !== '.' && s !== '..') || /^(appdata|node_modules)$/i.test(s));
  }

  /* The files moved with one computer, newest first, built only from finished transfers (so a file made on this PC, or moved
     with another computer, never shows). An upload sits in the folder it was sent to; a download is where it is on that
     computer. Each file appears once (its latest transfer); hidden and system paths are left out. o.limit caps the list. */
  function recentFromTasks(list, hostId, o) {
    const limit = (o && o.limit) || 50, seen = new Map();
    for (const t of list || []) {
      if (!t || t.state !== 'done' || t.host !== hostId || !t.name) continue;
      const path = t.dir === 'up' ? (t.dstDir ? join(t.dstDir, t.name) : '') : (t.srcPath || '');
      if (!path || isNoisePath(path)) continue;
      const at = t.finishedAt || 0, prev = seen.get(path);
      if (!prev || at > prev.at) seen.set(path, { at, e: { host: hostId, path, name: t.name, isDir: !!t.isDir, dir: t.dir, modified: new Date(at).toISOString() } });
    }
    return [...seen.values()].sort((a, b) => b.at - a.at).slice(0, limit).map((x) => x.e);
  }

  /* The computer a Tools page shows: the remembered one while it is still connected, else the one open in Files, else the
     first connected one; '' when none is connected. */
  function pickToolHost(onlineIds, remembered, filesHost) {
    if (onlineIds.includes(remembered)) return remembered;
    if (onlineIds.includes(filesHost)) return filesHost;
    return onlineIds[0] || '';
  }

  /* The items of one computer (hostOf gives an item's computer), or a copy of all of them when showAll is on or no computer is selected. */
  function scopeToHost(items, hostOf, hostId, showAll) {
    return showAll || !hostId ? items.slice() : items.filter((x) => hostOf(x) === hostId);
  }

  /* ---------- public ---------- */
  const api = {
    recentFiles: (hostId, o) => recentFromTasks(tasks, hostId, o),
    liveDue, liveMs, liveEventJobs, csvCell, mentionsDevice,
    USER: '', limits: Object.assign({}, DEFAULT_LIMITS) /* the defaults until the core answers desktop_limits */, settings, TRASH_DAYS_DEFAULT, PAIR_LIFE_DEFAULT, now, ONLINE_MS: 120000, isOnline: (d) => !d.revoked && d.last && api.now() - d.last < api.ONLINE_MS, tick, init, hooks: { autoConnect: () => true }, call, setInvoke(fn) { invokeImpl = fn; }, on(fn) { listeners.push(fn); let live = true; return () => { if (!live) return; live = false; const i = listeners.indexOf(fn); if (i >= 0) listeners.splice(i, 1); }; },
    get servers() { return servers; }, get tasks() { return tasks; }, get history() { return history; }, get notes() { return notes; },
    server, connected, connect, trust, cancelConnect, disconnect, retryNow, addServer, updateServer, forget, reverify, switchRoute, restoreServer, start, loadServers, check, loadRoots, wakeDenied,
    signIn, signInWithCode, requestApproval, pollApproval, cancelApproval, signOut,
    local: LOCAL_START,
    fs: { setDotsHidden: (f) => { dotsHidden = f; }, itemsLabel, add: insert, copy, mkfile, get: getNode, list, state: listState, error: listError, load, refresh, drop, exists, mkdir, rename, remove, restore, info, total, view, uniqueName, writableDir, canRead, canWrite, kidsOf, itemCount, cached },
    makeSearch, SEARCH, noop: NOOP, enqueue, pause, resumeTask, cancel, dismiss, retry, resolve, pushPrefs, pauseAll, resumeAll, clearDone, retryFailed, task: taskById, eta, ACTIVE, pollTransfers,
    totalSpeed: () => running().reduce((a, t) => a + t.speed, 0),
    note, log, emit,
    util: { isNoisePath, recentFromTasks, pickToolHost, scopeToHost, fmtBytes, lang, fmtDate, fmtTime, fmtDur, fmtSpeed, norm, join, parentOf, baseOf, segs, upTo, isRoot, kindOf, KINDS, permOctal, sizeOf, fmtFp, viaOf, splitAddr, parseAddr, parsePath, DEFAULT_PORT }
  };
  return api;
});
