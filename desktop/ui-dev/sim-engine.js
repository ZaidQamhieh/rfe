/* RFE Tonal - simulation engine. No DOM. Virtual filesystems, servers, transfers.
   Time is stepped by Engine.tick(ms), so the UI drives it with a timer and tests can drive it by hand. */
(function (root, factory) {
  if (typeof module === 'object' && module.exports) module.exports = factory();
  else root.Engine = factory();
})(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /* ---------- helpers ---------- */
  const USER = 'zaid';
  const T0 = Date.UTC(2026, 9, 4, 14, 7);
  let clockMs = T0;
  const now = () => clockMs;
  const ts = (m, d, h, mi) => Date.UTC(2026, m - 1, d, h || 0, mi || 0);
  let seed = 1234;
  const rnd = () => { seed |= 0; seed = (seed + 0x6D2B79F5) | 0; let t = Math.imul(seed ^ (seed >>> 15), 1 | seed); t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t; return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
  const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const p2 = (n) => String(n).padStart(2, '0');
  const fmtDate = (ms) => { const d = new Date(ms); const base = MON[d.getUTCMonth()] + ' ' + d.getUTCDate(); return d.getUTCFullYear() === 2026 ? base + ', ' + p2(d.getUTCHours()) + ':' + p2(d.getUTCMinutes()) : base + ', ' + d.getUTCFullYear(); };
  const fmtTime = (ms) => { const d = new Date(ms); return p2(d.getUTCHours()) + ':' + p2(d.getUTCMinutes()) + ':' + p2(d.getUTCSeconds()); };
  const fmtBytes = (b) => {
    if (b < 1000) return b + ' B';
    const u = ['KB', 'MB', 'GB', 'TB']; let i = -1; let v = b;
    do { v /= 1000; i++; } while (v >= 1000 && i < 3);
    return (v >= 100 || i === 0 ? v.toFixed(i === 0 ? 0 : 0) : v >= 10 ? v.toFixed(1) : v.toFixed(2)) + ' ' + u[i];
  };
  const fmtDur = (s) => { s = Math.max(0, Math.round(s)); if (!isFinite(s)) return '—'; if (s >= 3600) return Math.floor(s / 3600) + 'h ' + p2(Math.floor((s % 3600) / 60)) + 'm'; if (s >= 60) return Math.floor(s / 60) + 'm ' + p2(s % 60) + 's'; return s + 's'; };
  const fmtSpeed = (bps) => (bps / 1e6).toFixed(1) + ' MB/s';
  let uid = 0;
  const nid = (p) => p + (++uid);

  /* ---------- paths ---------- */
  const norm = (p) => { const out = []; for (const s of String(p).split('/')) { if (!s || s === '.') continue; if (s === '..') out.pop(); else out.push(s); } return '/' + out.join('/'); };
  const join = (a, b) => norm(a + '/' + b);
  const parentOf = (p) => { const n = norm(p); const i = n.lastIndexOf('/'); return i <= 0 ? '/' : n.slice(0, i); };
  const baseOf = (p) => norm(p).split('/').pop();
  const segs = (p) => norm(p).split('/').filter(Boolean);

  /* ---------- file kinds ---------- */
  const EXT = { apk: 'pkg', aab: 'pkg', deb: 'pkg', appimage: 'pkg', rpm: 'pkg', mp4: 'video', mov: 'video', mkv: 'video', png: 'image', jpg: 'image', jpeg: 'image', exr: 'image', zip: 'archive', zst: 'archive', gz: 'archive', tar: 'archive', md: 'text', txt: 'text', log: 'text', json: 'code', yml: 'code', yaml: 'code', pdf: 'doc', jks: 'key', keystore: 'key', pem: 'key', sh: 'exec', bin: 'file' };
  const KINDS = { dir: 'Folder', pkg: 'Package', video: 'Video', image: 'Image', archive: 'Archive', text: 'Text', code: 'Data', doc: 'PDF document', key: 'Keystore', exec: 'Executable', file: 'File' };
  const kindOf = (n) => { if (n.t === 'dir') return 'dir'; if (n.exec) return 'exec'; const m = /\.([a-z0-9]+)$/i.exec(n.n); return (m && EXT[m[1].toLowerCase()]) || 'file'; };
  const FILTERS = {
    all: () => true, folders: (n) => n.t === 'dir', packages: (n) => kindOf(n) === 'pkg',
    media: (n) => ['video', 'image'].includes(kindOf(n)), archives: (n) => kindOf(n) === 'archive', big: (n) => n.t === 'file' && n.b >= 100e6
  };

  /* ---------- nodes ---------- */
  const F = (n, b, mod, o) => Object.assign({ n, t: 'file', b, mod, perm: '-rw-r--r--', own: USER }, o);
  const D = (n, kids, mod, o) => Object.assign({ n, t: 'dir', b: 0, mod, perm: 'drwxr-xr-x', own: USER, kids: kids || [] }, o);
  const kidsOf = (d) => { if (d.gen) { const g = d.gen; d.gen = null; d.kids = g(); } return d.kids; };
  const sizeOf = (n) => (n.t === 'dir' ? 0 : n.b);
  const clone = (n) => { const c = Object.assign({}, n); if (n.t === 'dir') { c.kids = kidsOf(n).map(clone); c.gen = null; } return c; };
  const walk = (n, fn) => { fn(n); if (n.t === 'dir') for (const k of kidsOf(n)) walk(k, fn); };
  const total = (n) => { let bytes = 0, files = 0, dirs = 0; walk(n, (x) => { if (x.t === 'dir') dirs++; else { files++; bytes += x.b; } }); return { bytes, files, dirs: dirs - (n.t === 'dir' ? 1 : 0) }; };
  const itemCount = (n) => (n.t === 'dir' ? (n.gen ? n.count : n.kids.length) : 0);
  const canRead = (n) => n.own === USER || n.perm[7] === 'r' || (n.perm[4] === 'r' && n.own === 'backup');
  const canWrite = (d) => d.own === USER || d.perm[8] === 'w';
  const ROOT_PERM = (o) => Object.assign({ own: 'root', perm: '-rw-------' }, o || {});

  /* generated filler */
  const filler = (prefix, count, exts, m, o) => () => { const out = []; for (let i = 1; i <= count; i++) { const e = exts[i % exts.length]; out.push(F(prefix + String(i).padStart(String(count).length < 3 ? 3 : String(count).length, '0') + '.' + e, Math.floor(2e3 + rnd() * (o && o.max || 4e6)), ts(m[0], m[1], 8 + (i % 9), i % 60))); } return out; };

  const LOG = ['> Task :app:preBuild UP-TO-DATE', '> Task :app:compileReleaseKotlin', '> Task :app:minifyReleaseWithR8', 'R8: 14 warnings, 0 errors', '> Task :app:validateSigningRelease', 'signing with keystore.jks (alias atlas-release)', '> Task :app:packageRelease', '> Task :app:assembleRelease', 'BUILD SUCCESSFUL in 4m 18s', '212 actionable tasks: 41 executed, 171 up-to-date'];
  const buildLog = () => { const out = []; for (let i = 0; i < 120; i++) out.push(LOG[i % LOG.length]); return out.join('\n'); };
  const NOTES = '# Atlas 2026.10\n\n- Android: atlas-2.0.0 (apk, aab)\n- Desktop: rfe-desktop 1.0.0 for Linux (deb, AppImage)\n- Agent 1.43.0, proof v2\n\n## Fixes\n- Resume partial uploads after connection loss\n- Permission errors now name the owner and mode\n- Faster listing of folders with 100k+ entries\n';
  const PROOF = '{\n  "version": 2,\n  "agent": "1.43.0",\n  "files": 14,\n  "sha256": "9f2c4e0a7b3d…d41e83",\n  "signed": true,\n  "created": "2026-10-04T09:14:00Z"\n}';
  const SUMS = '9f2c4e0a7b3d1c9e5a8f6b21d41e83aa  rfe-desktop_1.0.0_amd64.deb\n1b77c0d9e83a4f5566be90a1cd02f7e3  rfe-desktop-1.0.0.AppImage\n7c3a99e0d18b2f4466a5e017bb9c4d52  rfe-linux-amd64\n';

  /* ---------- filesystems ---------- */
  function buildFS() {
    const rel = D('2026.10', [
      D('android', [F('app-release.apk', 84.2e6, ts(10, 3, 18, 40)), F('mapping.txt', 14.8e6, ts(10, 3, 18, 39), { content: 'com.atlas.app.MainActivity -> a.a:\n    void onCreate(android.os.Bundle) -> onCreate\n'.repeat(40) })], ts(10, 3, 18, 42)),
      D('checksums', [F('SHA256SUMS.txt', 1.1e3, ts(10, 4, 9, 20), { content: SUMS }), F('SHA256SUMS.sig', 0.5e3, ts(10, 4, 9, 20)), F('amd64.sha256', 0.1e3, ts(10, 4, 9, 20)), F('arm64.sha256', 0.1e3, ts(10, 4, 9, 20)), F('apk.sha256', 0.1e3, ts(10, 3, 18, 41)), F('aab.sha256', 0.1e3, ts(10, 3, 18, 41))], ts(10, 4, 9, 20)),
      D('desktop', [F('rfe-desktop_1.0.0_amd64.deb', 38.7e6, ts(10, 4, 9, 12)), F('rfe-desktop-1.0.0.AppImage', 96.3e6, ts(10, 4, 9, 12), { exec: 1, perm: '-rwxr-xr-x' })], ts(10, 4, 9, 12)),
      D('symbols', null, ts(10, 1, 22, 5), { gen: filler('sym-', 212, ['sym', 'dbg', 'pdb'], [10, 1], { max: 2e6 }), count: 212 }),
      F('atlas-2.0.0-release.apk', 84.2e6, ts(10, 3, 18, 40)), F('atlas-2.0.0-release.aab', 61.8e6, ts(10, 3, 18, 40)),
      F('backup-2026-10-01.tar.zst', 3.8e9, ts(10, 1, 3, 0), { perm: '-rw-r-----', own: 'backup' }),
      F('build.log', 2.3e6, ts(10, 4, 8, 57), { content: buildLog() }),
      F('demo-walkthrough.mp4', 412e6, ts(10, 2, 14, 31)), F('hero.png', 2.4e6, ts(10, 2, 11, 8)),
      F('keystore.jks', 2.7e3, ts(9, 12, 10, 14), ROOT_PERM({ content: null })),
      F('mapping.txt', 14.8e6, ts(10, 3, 18, 39), { content: 'com.atlas.app.MainActivity -> a.a:\n'.repeat(60) }),
      F('proof-v2.json', 18.2e3, ts(10, 4, 9, 14), { content: PROOF }),
      F('release-notes.md', 6.4e3, ts(10, 4, 9, 30), { content: NOTES }),
      F('rfe-linux-amd64', 22.4e6, ts(10, 4, 9, 10), { exec: 1, perm: '-rwxr-xr-x' }), F('rfe-linux-arm64', 21.9e6, ts(10, 4, 9, 10), { exec: 1, perm: '-rwxr-xr-x' }),
      F('rfe-desktop_1.0.0_amd64.deb', 38.7e6, ts(10, 4, 9, 12)), F('rfe-desktop-1.0.0.AppImage', 96.3e6, ts(10, 4, 9, 12), { exec: 1, perm: '-rwxr-xr-x' }),
      F('screenshots-a53.zip', 148e6, ts(10, 2, 16, 22)), F('SHA256SUMS.txt', 1.1e3, ts(10, 4, 9, 20), { content: SUMS }), F('store-listing.jpg', 940e3, ts(10, 2, 11, 10))
    ], ts(10, 4, 9, 20));
    const atlas = D('atlas', [D('releases', [D('2026.08', [F('atlas-1.9.0.apk', 79e6, ts(8, 30, 18, 0))], ts(8, 30)), D('2026.09', [F('atlas-1.9.5.apk', 82e6, ts(9, 28, 17, 40)), F('notes.md', 3e3, ts(9, 28), { content: '# Atlas 2026.09\n' })], ts(9, 28)), rel, D('staging', [], ts(10, 2, 10, 0))], ts(10, 4, 9, 20)),
      D('docs', [F('signing-keystore-howto.md', 6.1e3, ts(8, 3, 10, 0), { content: '# Signing\nKeep keystore.jks outside the repo.\n' }), F('architecture.pdf', 2.2e6, ts(7, 14))], ts(8, 3)),
      D('android', [F('keystore.properties', 412, ts(10, 2, 9, 0), { content: 'storeFile=keystore.jks\nkeyAlias=atlas-release\n' })], ts(10, 2))], ts(10, 4, 9, 20));
    const backups = D('backups', [D('2026-09', [F('app-release.keystore.bak', 3.4e3, ts(9, 30, 3, 0), { perm: '-rw-r-----', own: 'backup' })], ts(9, 30)), F('backup-2026-09-24.tar.zst', 3.61e9, ts(9, 24, 3, 0), { perm: '-rw-r-----', own: 'backup' }), F('backup-2026-09-17.tar.zst', 3.58e9, ts(9, 17, 3, 0), { perm: '-rw-r-----', own: 'backup' })], ts(10, 1, 3, 0), { own: 'root', perm: 'drwxr-xr-x' });
    const media = D('media', [D('frames', null, ts(10, 3, 22, 0), { gen: filler('frame_', 12000, ['exr', 'png'], [10, 3], { max: 3e6 }), count: 12000 }), F('hero-loop.mov', 2.2e9, ts(10, 2, 12, 0)), F('walkthrough-raw-0412.mov', 3.1e9, ts(9, 20, 12, 0)), F('demo-walkthrough.mp4', 412e6, ts(10, 2, 14, 31))], ts(10, 3, 22, 0));
    const nas = D('', [D('srv', [D('projects', [atlas], ts(10, 4)), backups, media], ts(10, 1)), D('var', [D('log', [F('syslog', 8.2e6, ts(10, 4, 14, 0), { own: 'root', perm: '-rw-r-----' }), F('nginx-access.log', 41e6, ts(10, 4, 14, 0), { perm: '-rw-r--r--' })], ts(10, 4, 14, 0))], ts(9, 1)), D('home', [D('zaid', [F('notes.txt', 1.2e3, ts(10, 1), { content: 'todo: rotate keys\n' })], ts(10, 1))], ts(9, 1))], ts(9, 1));

    const localDist = D('dist', [
      D('assets', null, ts(10, 4, 8, 40), { gen: filler('asset-', 31, ['png', 'jpg'], [10, 4], { max: 2e6 }), count: 31 }),
      D('build', null, ts(10, 4, 9, 2), { gen: filler('obj-', 1204, ['bin', 'log'], [10, 4], { max: 9e5 }), count: 1204 }),
      D('logs', null, ts(10, 4, 9, 41), { gen: filler('run-', 58, ['log'], [10, 4], { max: 6e5 }), count: 58 }),
      F('atlas-footage-raw.mov', 4.2e9, ts(10, 4, 7, 55)), F('demo-walkthrough-v2.mp4', 618e6, ts(10, 4, 8, 12)), F('hero@2x.png', 3.1e6, ts(10, 4, 8, 41)),
      F('release-notes.md', 7.9e3, ts(10, 4, 9, 44), { content: NOTES + '- Local edit: added desktop section\n' }), F('rfe-desktop_1.0.1_amd64.deb', 39.0e6, ts(10, 4, 9, 38)), F('symbols-batch.zip', 212e6, ts(10, 4, 9, 30))
    ], ts(10, 4, 9, 44));
    const local = D('', [D('home', [D('zaid', [
      D('Projects', [D('atlas', [localDist, D('src', [F('main.go', 12e3, ts(10, 3), { content: 'package main\n\nfunc main() {}\n' })], ts(10, 3))], ts(10, 4))], ts(10, 4)),
      D('Downloads', [F('old-notes.txt', 2.2e3, ts(9, 2), { content: 'old notes\n' }), F('receipt-sep.pdf', 184e3, ts(9, 30)), F('ubuntu-24.04.iso', 5.9e9, ts(8, 12, 9, 0))], ts(9, 30)),
      D('Documents', [F('plan.md', 4.1e3, ts(9, 5), { content: '# Plan\n' }), F('budget.pdf', 320e3, ts(8, 20))], ts(9, 5)),
      D('Pictures', [F('a53-preview-01.png', 2.3e6, ts(10, 2)), F('a53-preview-02.png', 2.1e6, ts(10, 2)), F('a53-settings-07.png', 2.4e6, ts(10, 2))], ts(10, 2)),
      D('Desktop', [], ts(9, 1))], ts(10, 4))], ts(9, 1))], ts(9, 1));

    const gen = (name, kids) => D('', kids, ts(9, 1));
    const build = gen('', [D('var', [D('lib', [D('ci', [D('cache', [D('blobs', null, ts(10, 4, 14, 2), { gen: filler('blob-', 300, ['bin'], [10, 4], { max: 6e8 }), count: 300 }), D('index', [F('index.json', 1.4e6, ts(10, 4, 13, 58), { content: '{}' })], ts(10, 4, 13, 58)), D('tmp', [], ts(10, 4, 14, 6)), D('ci-secrets', [F('token.env', 120, ts(9, 12), ROOT_PERM())], ts(9, 12), { own: 'root', perm: 'drwx------' }), F('layer-b85cc8aa.json', 23.1e6, ts(10, 4, 13, 49)), F('pkg-0a7ec18e.tgz', 187.8e6, ts(10, 4, 13, 41))], ts(10, 4, 14, 2))], ts(10, 4))], ts(10, 4))], ts(10, 4))]);
    const fra = gen('', [D('var', [D('crash', [D('symbols', [F('symbols-batch.zip.part', 142e6, ts(10, 4, 14, 0)), F('app-crash-0412.dmp', 8e6, ts(10, 3))], ts(10, 4, 14, 0))], ts(10, 4)), D('www', [F('index.html', 4e3, ts(9, 1), { content: '<h1>fra1</h1>' })], ts(9, 1))], ts(10, 4))]);
    const mac = gen('', [D('Users', [D('zaid', [D('Movies', [F('studio-cut-v3.mov', 6.4e9, ts(10, 3, 20, 0))], ts(10, 3)), D('Documents', [F('contract.pdf', 410e3, ts(9, 3))], ts(9, 3)), D('Desktop', [], ts(9, 1))], ts(10, 3))], ts(9, 1))]);
    const pi = gen('', [D('media', [D('music', [F('album-01.mp4', 90e6, ts(8, 3))], ts(8, 3))], ts(8, 3))]);
    const office = gen('', [D('home', [D('office', [D('Reports', [F('q3-report.pdf', 2.1e6, ts(10, 1)), F('q3-data.json', 90e3, ts(10, 1), { content: '{}' })], ts(10, 1))], ts(10, 1))], ts(9, 1))]);
    return { local, nas, build, fra, mac, pi, office };
  }

  /* ---------- state ---------- */
  const settings = { parallel: 2, limit: 0, onConflict: 'ask', verify: true, autoReconnect: true, downloadDir: '/home/zaid/Downloads', theme: 'light', density: 'comfortable', notifyDone: true, notifyErrors: true, simSpeed: 1 };
  let fs, servers, tasks, history, notes, listeners;
  const START = { nas: '/srv/projects/atlas/releases/2026.10', build: '/var/lib/ci/cache', fra: '/var/crash/symbols', mac: '/Users/zaid', pi: '/media', office: '/home/office', local: '/home/zaid/Projects/atlas/dist' };
  const LINK = { nas: 90e6, build: 40e6, fra: 6.5e6, mac: 55e6, pi: 9e6, office: 30e6 };

  const emit = (type, data) => { for (const l of listeners.slice()) { try { l(type, data); } catch (e) { if (typeof console !== 'undefined') console.error(e); } } };
  const note = (kind, text) => { const n = { id: nid('n'), at: now(), kind, text, read: false }; notes.unshift(n); if (notes.length > 60) notes.pop(); emit('notify', n); };
  const log = (kind, text, extra) => { history.unshift(Object.assign({ id: nid('h'), at: now(), kind, text }, extra)); if (history.length > 300) history.pop(); emit('history'); };

  function reset() {
    uid = 0; clockMs = T0; seed = 1234; listeners = listeners || [];
    fs = buildFS();
    const S = (id, name, host, via, os, st, o) => Object.assign({ id, name, host, port: 7443, via, os, agent: '1.43.0', pinned: true, fp: '9F:2C:4E:0A:7B:3D:1C:9E:5A:8F:6B:21:D4:1E:83:AA', state: st, latency: 0, base: 0, disk: [0, 1], reachable: true, hist: [], retryIn: 0, attempts: 0, connectLeft: 0, lostAt: 0, path: START[id], saved: true }, o);
    servers = [
      S('nas', 'atlas-nas', '192.168.1.20', 'LAN', 'Debian 12', 'online', { base: 4, disk: [3.1, 8.0] }),
      S('build', 'build-box', '100.101.4.7', 'Tailscale', 'Ubuntu 24.04', 'online', { base: 38, disk: [214, 480], agent: '1.42.2' }),
      S('fra', 'vps-fra1', '203.0.113.9', 'Direct HTTPS', 'Debian 12', 'lost', { base: 22, disk: [41, 80], lostAt: now() - 14000, retryIn: 8, attempts: 1 }),
      S('mac', 'studio-mac', '100.88.12.3', 'Tailscale', 'macOS 15', 'disconnected', { base: 16, disk: [380, 500] }),
      S('pi', 'media-pi', '192.168.1.44', 'LAN', 'Raspberry Pi OS', 'offline', { base: 9, disk: [1.4, 4.0], reachable: false, lastSeen: now() - 7200e3 }),
      S('office', 'office-pc', '100.77.2.15', 'Tailscale', 'Windows 11', 'disconnected', { base: 27, disk: [220, 512] })
    ];
    for (const s of servers) for (let i = 0; i < 30; i++) s.hist.push(s.state === 'online' ? s.base + Math.round(rnd() * 3) : null);
    for (const s of servers) if (s.state === 'online') s.latency = s.base;
    const RT = { nas: [['LAN', '192.168.1.20', 4], ['Tailscale', '100.101.4.20', 18]], build: [['Tailscale', '100.101.4.7', 38], ['LAN', '192.168.1.30', 6]], fra: [['Direct HTTPS', '203.0.113.9', 22], ['Tailscale', '100.88.44.9', 41]], mac: [['Tailscale', '100.88.12.3', 16]], pi: [['LAN', '192.168.1.44', 9]], office: [['Tailscale', '100.77.2.15', 27], ['LAN', '192.168.1.77', 5]] };
    for (const s of servers) { s.routes = (RT[s.id] || []).map((r) => ({ via: r[0], host: r[1], base: r[2] })); s.roots = [{ path: '/srv', rw: true }, { path: '/home/' + USER, rw: true }, { path: '/mnt/backup', rw: false }]; }
    tasks = []; history = []; notes = [];
    note('error', 'vps-fra1 connection lost');
    note('info', 'build-box: agent 1.43.0 is available');
  }

  /* ---------- fs api ---------- */
  const hostOf = (id) => fs[id];
  function getNode(host, path) {
    let n = hostOf(host); if (!n) return null;
    for (const s of segs(path)) { if (n.t !== 'dir') return null; const k = kidsOf(n).find((x) => x.n === s); if (!k) return null; n = k; }
    return n;
  }
  const list = (host, path) => { const n = getNode(host, path); return n && n.t === 'dir' ? kidsOf(n) : null; };
  const exists = (host, dir, name) => !!(list(host, dir) || []).find((x) => x.n === name);
  const eacces = (msg) => Object.assign(new Error(msg), { code: 'EACCES' });
  const bad = (msg, code) => Object.assign(new Error(msg), { code: code || 'EINVAL' });
  const validName = (name) => { if (!name || !name.trim()) throw bad('Name can’t be empty'); if (/[\/\0]/.test(name)) throw bad('Names can’t contain “/”'); if (name === '.' || name === '..') throw bad('Reserved name'); if (name.length > 255) throw bad('Name too long'); };
  function writableDir(host, dir) { const d = getNode(host, dir); if (!d || d.t !== 'dir') throw bad('Folder not found', 'ENOENT'); if (!canWrite(d)) throw eacces('Permission denied: ' + dir + ' is owned by ' + d.own + ' (' + d.perm + ')'); return d; }
  function mkdir(host, dir, name) {
    validName(name); const d = writableDir(host, dir);
    if (exists(host, dir, name)) throw bad('“' + name + '” already exists', 'EEXIST');
    kidsOf(d).push(D(name, [], now())); d.mod = now(); emit('fs', { host, dir }); return join(dir, name);
  }
  function rename(host, dir, oldName, newName) {
    validName(newName); const d = writableDir(host, dir); const k = kidsOf(d).find((x) => x.n === oldName);
    if (!k) throw bad('Item not found', 'ENOENT');
    if (newName !== oldName && exists(host, dir, newName)) throw bad('“' + newName + '” already exists', 'EEXIST');
    k.n = newName; k.mod = now(); emit('fs', { host, dir }); return newName;
  }
  function remove(host, dir, names) {
    const d = writableDir(host, dir); const removed = [];
    for (const nm of names) { const i = kidsOf(d).findIndex((x) => x.n === nm); if (i >= 0) removed.push(kidsOf(d).splice(i, 1)[0]); }
    d.mod = now(); emit('fs', { host, dir }); return removed;
  }
  function restore(host, dir, nodes) { const d = getNode(host, dir); if (!d) return; for (const n of nodes) if (!exists(host, dir, n.n)) kidsOf(d).push(n); emit('fs', { host, dir }); }
  const mkfile = (name, bytes, o) => F(name, bytes, now(), o);
  function add(host, dir, node) {
    const d = writableDir(host, dir); if (exists(host, dir, node.n)) node.n = uniqueName(host, dir, node.n);
    kidsOf(d).push(node); d.mod = now(); emit('fs', { host, dir }); return node;
  }
  function copy(host, dir, names, dstDir, move) {
    const d = writableDir(host, dstDir); const src = getNode(host, dir); if (!src) throw bad('Folder not found', 'ENOENT'); const done = [];
    for (const nm of names) {
      const k = kidsOf(src).find((x) => x.n === nm); if (!k) continue;
      if (!canRead(k)) throw eacces('Permission denied: ' + nm + ' is owned by ' + k.own + ' (' + k.perm + ')');
      const from = join(dir, nm); if (k.t === 'dir' && (dstDir === from || dstDir.startsWith(from + '/'))) throw bad('Can’t put a folder inside itself');
      if (move && dstDir === dir) continue;
      const c = clone(k); if (exists(host, dstDir, c.n)) c.n = uniqueName(host, dstDir, c.n); c.mod = now();
      kidsOf(d).push(c); done.push(c.n);
      if (move) { writableDir(host, dir); kidsOf(src).splice(kidsOf(src).indexOf(k), 1); }
    }
    d.mod = now(); if (move) src.mod = now(); emit('fs', { host, dir: dstDir }); if (move) emit('fs', { host, dir }); return done;
  }
  const info = (host, dir) => { const n = getNode(host, dir); return n ? { items: itemCount(n), node: n } : null; };
  const uniqueName = (host, dir, name) => {
    const m = /^(.*?)(\.[^.]*)?$/.exec(name); let i = 1, c;
    do { c = m[1] + ' (' + i + ')' + (m[2] || ''); i++; } while (exists(host, dir, c)); return c;
  };

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

  /* search: incremental so the UI can show a live scan */
  function makeSearch(hostIds, q, o) {
    o = o || {}; q = q.toLowerCase(); const stack = hostIds.map((h) => ({ h, n: hostOf(h), p: '/' })); const res = []; let scanned = 0;
    const f = FILTERS[o.filter || 'all'] || FILTERS.all;
    return {
      results: res, get scanned() { return scanned; },
      step(budget) {
        let b = budget || 4000;
        while (stack.length && b > 0) {
          const { h, n, p } = stack.pop(); scanned++; b--;
          if (p !== '/' && n.n.toLowerCase().includes(q) && f(n) && (!o.minSize || n.b >= o.minSize)) res.push({ host: h, dir: parentOf(p), node: n, path: p });
          if (n.t === 'dir') { if (h !== 'local' || true) { const ks = kidsOf(n); for (let i = ks.length - 1; i >= 0; i--) stack.push({ h, n: ks[i], p: join(p, ks[i].n) }); } }
          if (res.length >= (o.max || 500)) { stack.length = 0; break; }
        }
        return !stack.length;
      }
    };
  }

  /* ---------- servers ---------- */
  const server = (id) => servers.find((s) => s.id === id);
  const connected = (id) => id === 'local' || (server(id) && server(id).state === 'online');
  function setState(s, st, msg) { if (s.state === st) return; s.state = st; if (msg) s.msg = msg; emit('servers', s); }
  function connect(id) {
    const s = server(id); if (!s || s.state === 'connecting' || s.state === 'online') return;
    s.attempts = 0; s.error = null;
    s.connectLeft = 1800; setState(s, 'connecting'); log('conn', 'Connecting to ' + s.name);
  }
  function trust(id) { const s = server(id); if (!s || s.state !== 'trust') return; s.pinned = true; s.connectLeft = 700; setState(s, 'connecting'); }
  function cancelConnect(id) { const s = server(id); if (s && (s.state === 'connecting' || s.state === 'trust')) { s.connectLeft = 0; setState(s, 'disconnected'); } }
  function disconnect(id) {
    const s = server(id); if (!s || s.state === 'disconnected') return;
    setState(s, 'disconnected'); log('conn', 'Disconnected from ' + s.name); suspend(id, 'Disconnected from ' + s.name);
  }
  function simulateLoss(id) {
    const s = server(id); if (!s || s.state !== 'online') return;
    s.lostAt = now(); s.attempts = 0; s.retryIn = 8; setState(s, 'lost'); note('error', s.name + ' connection lost'); log('conn', s.name + ' connection lost', { error: true });
    suspend(id, 'Connection lost');
  }
  function retryNow(id) { const s = server(id); if (!s) return; if (s.state === 'lost') { s.retryIn = 0; s.attempts++; s.connectLeft = 1200; setState(s, 'connecting'); } else if (s.state === 'offline') connect(id); }
  function certChanged(id) {
    const s = server(id); if (!s || s.state !== 'online') return false;
    s.oldFp = s.fp; s.newFp = '7E:D1:09:BC:44:A2:F3:58:91:0A:6C:E7:3B:D4:25:C8'; s.lostAt = now(); setState(s, 'refused');
    note('error', s.name + ': certificate changed, connection refused'); log('conn', s.name + ' presented a different certificate (refused)', { error: true }); suspend(id, 'Certificate changed'); return true;
  }
  function reverify(id) {
    const s = server(id); if (!s || s.state !== 'refused') return; s.fp = s.newFp; s.oldFp = null; s.newFp = null; s.pinned = true; s.connectLeft = 900; setState(s, 'connecting'); log('conn', 'Pinned the new certificate of ' + s.name);
  }
  function switchRoute(id, via) {
    const s = server(id); if (!s || !s.routes) return false; const r = s.routes.find((x) => x.via === via); if (!r) return false;
    s.via = r.via; s.host = r.host; s.base = r.base; if (s.state === 'online') { s.latency = r.base; } log('conn', s.name + ': using ' + r.via + ' (' + r.host + ')'); emit('servers', s); return true;
  }
  function restoreServer(s, root) { if (!s || server(s.id)) return; servers.push(s); fs[s.id] = root; emit('servers', s); }
  function addServer(o) {
    const id = 's' + nid('x'); const s = Object.assign({ id, name: o.name, host: o.host, port: o.port || 7443, via: o.via || 'LAN', os: 'Linux', agent: '1.43.0', pinned: false, fp: '3B:A0:6D:1C:F2:84:5E:9A:70:C3:2B:E8:14:D7:9F:60', state: 'disconnected', latency: 0, base: 14 + Math.floor(rnd() * 30), disk: [Math.round(20 + rnd() * 100), 256], reachable: !/^(10\.0\.0\.|0\.0\.0\.0|unreachable)/.test(o.host), hist: [], retryIn: 0, attempts: 0, connectLeft: 0, path: '/home/' + USER, saved: true }, {});
    fs[id] = D('', [D('home', [D(USER, [D('Documents', [F('readme.txt', 220, ts(10, 1), { content: 'Welcome to ' + o.name + '\n' })], ts(10, 1)), D('Backups', [], ts(10, 1))], ts(10, 1))], ts(10, 1))], ts(9, 1));
    LINK[id] = 25e6; servers.push(s); emit('servers', s); return s;
  }
  function updateServer(id, o) { const s = server(id); if (!s) return; Object.assign(s, o); if (o.host) { s.pinned = false; s.reachable = !/^(10\.0\.0\.|0\.0\.0\.0|unreachable)/.test(o.host); } emit('servers', s); }
  function forget(id) {
    const i = servers.findIndex((s) => s.id === id); if (i < 0) return;
    for (const t of tasks.slice()) if (t.host === id && ['queued', 'running', 'paused', 'waiting', 'conflict'].includes(t.state)) cancel(t.id);
    servers.splice(i, 1); emit('servers'); emit('transfers');
  }

  function serverTick(dt) {
    for (const s of servers) {
      if (s.state === 'connecting') {
        s.connectLeft -= dt;
        if (s.connectLeft <= 0) {
          if (!s.reachable) { s.error = 'The PC did not answer on ' + s.host + ':' + s.port; setState(s, 'offline', s.error); s.lastSeen = s.lastSeen || now(); log('conn', 'Could not reach ' + s.name, { error: true }); note('error', s.name + ' is unreachable'); }
          else if (!s.pinned) { setState(s, 'trust'); }
          else { s.latency = s.base; s.hist.push(s.base); setState(s, 'online'); s.attempts = 0; log('conn', 'Connected to ' + s.name + ' · ' + s.latency + ' ms'); if (settings.notifyDone) note('ok', 'Connected to ' + s.name); resume(s.id); }
        }
      } else if (s.state === 'lost' && settings.autoReconnect) {
        s.retryIn -= dt / 1000;
        if (s.retryIn <= 0) {
          s.attempts++;
          if (s.attempts >= 3 && s.reachable) { s.connectLeft = 900; setState(s, 'connecting'); } else s.retryIn = 8;
        }
      }
    }
  }
  let latAcc = 0;
  function latencyTick(dt) {
    latAcc += dt; if (latAcc < 1500) return; latAcc = 0;
    for (const s of servers) {
      if (s.state === 'online') { s.latency = Math.max(1, Math.round(s.base + (rnd() - 0.4) * Math.max(2, s.base * 0.25))); s.hist.push(s.latency); } else s.hist.push(null);
      if (s.hist.length > 40) s.hist.shift();
    }
    emit('latency');
  }

  /* ---------- transfers ---------- */
  const ACTIVE = ['queued', 'running', 'paused', 'waiting', 'conflict'];
  function enqueue(o) {
    // o: {dir:'up'|'down', host, srcDir, dstDir, names:[], resolution?}
    const remote = o.host;
    if (!connected(remote)) return { error: (server(remote) ? server(remote).name : remote) + ' is not connected' };
    const srcHost = o.dir === 'up' ? 'local' : remote, dstHost = o.dir === 'up' ? remote : 'local';
    const out = [];
    for (const name of o.names) {
      const n = getNode(srcHost, join(o.srcDir, name)); if (!n) continue;
      const tot = total(n);
      const t = { id: nid('t'), dir: o.dir, host: remote, name, isDir: n.t === 'dir', bytes: Math.max(1, tot.bytes), files: tot.files, srcHost, srcDir: norm(o.srcDir), dstHost, dstDir: norm(o.dstDir), state: 'queued', done: 0, speed: 0, queuedAt: now(), startedAt: 0, finishedAt: 0, msg: '', resolution: o.resolution || null, elevated: !!o.elevated, kind: kindOf(n) };
      tasks.push(t); out.push(t);
    }
    if (out.length) { log('queue', (o.dir === 'up' ? 'Queued upload: ' : 'Queued download: ') + out.map((t) => t.name).join(', ')); emit('transfers'); }
    return { tasks: out };
  }
  const taskById = (id) => tasks.find((t) => t.id === id);
  const running = () => tasks.filter((t) => t.state === 'running');
  function preflight(t) {
    const src = getNode(t.srcHost, join(t.srcDir, t.name));
    if (!src) return { fail: 'Source no longer exists' };
    if (!t.elevated) {
      let denied = 0, first = null; walk(src, (x) => { if (!canRead(x)) { denied++; first = first || x; } });
      if (denied) return { fail: 'Permission denied: ' + (first.n) + ' is owned by ' + first.own + ' (mode ' + permOctal(first.perm) + ')', fix: 'access' };
    }
    const dst = getNode(t.dstHost, t.dstDir);
    if (!dst || dst.t !== 'dir') return { fail: 'Destination folder no longer exists' };
    if (!t.elevated && !canWrite(dst)) return { fail: 'Permission denied: ' + t.dstDir + ' is owned by ' + dst.own + ' (' + dst.perm + ')', fix: 'access' };
    return {};
  }
  const permOctal = (p) => { const v = (s) => (s[0] === 'r' ? 4 : 0) + (s[1] === 'w' ? 2 : 0) + (s[2] === 'x' ? 1 : 0); return '0' + v(p.slice(1, 4)) + v(p.slice(4, 7)) + v(p.slice(7, 10)); };
  function finish(t) {
    const src = getNode(t.srcHost, join(t.srcDir, t.name)); const dst = getNode(t.dstHost, t.dstDir);
    if (!src || !dst) { t.state = 'failed'; t.msg = 'Source or destination disappeared'; return; }
    const ks = kidsOf(dst); const ex = ks.findIndex((x) => x.n === t.name);
    const c = clone(src); c.mod = now(); c.own = USER; if (c.t === 'file' && c.perm.startsWith('-rw-------') && src.own !== USER) { c.perm = '-rw-r--r--'; }
    if (ex >= 0) { if (t.resolution === 'replace') ks.splice(ex, 1); else if (t.resolution === 'keep') c.n = uniqueName(t.dstHost, t.dstDir, t.name); }
    ks.push(c); dst.mod = now(); t.state = 'done'; t.done = t.bytes; t.speed = 0; t.finishedAt = now(); t.savedAs = c.n;
    log('transfer', (t.dir === 'up' ? 'Uploaded ' : 'Downloaded ') + t.name + ' · ' + fmtBytes(t.bytes) + ' in ' + fmtDur((t.finishedAt - t.startedAt) / 1000), { tid: t.id, ok: true, dir: t.dir, host: t.host });
    if (settings.notifyDone) note('ok', t.name + ' ' + (t.dir === 'up' ? 'uploaded' : 'downloaded'));
    emit('fs', { host: t.dstHost, dir: t.dstDir }); emit('transfers');
  }
  function fail(t, msg, fix) { t.state = 'failed'; t.msg = msg; t.fix = fix || null; t.speed = 0; t.finishedAt = now(); log('transfer', t.name + ' failed: ' + msg, { tid: t.id, error: true, dir: t.dir, host: t.host }); if (settings.notifyErrors) note('error', t.name + ': ' + msg); emit('transfers'); }
  function suspend(hostId, why) {
    let any = false;
    for (const t of tasks) if (t.host === hostId && (t.state === 'running' || t.state === 'queued')) { t.state = 'waiting'; t.speed = 0; t.msg = why + (t.done ? ' at ' + Math.floor(t.done / t.bytes * 100) + '%' : ''); any = true; }
    if (any) emit('transfers');
  }
  function resume(hostId) { let any = false; for (const t of tasks) if (t.host === hostId && t.state === 'waiting') { t.state = 'queued'; t.msg = ''; any = true; } if (any) emit('transfers'); }
  function pause(id) { const t = taskById(id); if (t && (t.state === 'running' || t.state === 'queued')) { t.state = 'paused'; t.speed = 0; emit('transfers'); } }
  function resumeTask(id) { const t = taskById(id); if (t && t.state === 'paused') { t.state = 'queued'; emit('transfers'); } }
  function cancel(id) { const t = taskById(id); if (!t) return; if (ACTIVE.includes(t.state) || t.state === 'failed') { const was = t.state; t.state = 'cancelled'; t.speed = 0; if (was !== 'failed') log('transfer', 'Cancelled ' + t.name, { tid: t.id, dir: t.dir, host: t.host }); tasks.splice(tasks.indexOf(t), 1); emit('transfers'); } }
  function dismiss(id) { const i = tasks.findIndex((t) => t.id === id); if (i >= 0 && !ACTIVE.includes(tasks[i].state)) { tasks.splice(i, 1); emit('transfers'); } }
  function retry(id, o) { const t = taskById(id); if (!t || t.state !== 'failed') return; t.state = 'queued'; t.msg = ''; t.fix = null; if (o && o.elevate) t.elevated = true; emit('transfers'); }
  function resolve(id, how, all) {
    const t = taskById(id); if (!t || t.state !== 'conflict') return;
    const apply = (x) => { if (how === 'skip') { x.state = 'cancelled'; tasks.splice(tasks.indexOf(x), 1); log('transfer', 'Skipped ' + x.name + ' (already exists)', { tid: x.id, dir: x.dir, host: x.host }); } else { x.resolution = how; x.state = 'queued'; x.msg = ''; } };
    apply(t); if (all) for (const x of tasks.slice()) if (x.state === 'conflict') apply(x);
    emit('transfers');
  }
  function pauseAll() { for (const t of tasks) if (t.state === 'running' || t.state === 'queued') { t.state = 'paused'; t.speed = 0; } emit('transfers'); }
  function resumeAll() { for (const t of tasks) if (t.state === 'paused') t.state = 'queued'; emit('transfers'); }
  function clearDone() { const n = tasks.length; for (let i = tasks.length - 1; i >= 0; i--) if (tasks[i].state === 'done') tasks.splice(i, 1); if (tasks.length !== n) emit('transfers'); }
  function retryFailed() { for (const t of tasks) if (t.state === 'failed') { t.state = 'queued'; t.msg = ''; } emit('transfers'); }

  function transferTick(dt) {
    let changed = false;
    // start queued tasks
    for (const t of tasks) {
      if (t.state !== 'queued') continue;
      if (!connected(t.host)) { t.state = 'waiting'; t.msg = 'Waiting for ' + (server(t.host) ? server(t.host).name : t.host); changed = true; continue; }
      const pf = preflight(t); if (pf.fail) { fail(t, pf.fail, pf.fix); changed = true; continue; }
      const dst = getNode(t.dstHost, t.dstDir);
      if (!t.resolution && kidsOf(dst).some((x) => x.n === t.name)) {
        const pol = settings.onConflict;
        if (pol === 'ask') { t.state = 'conflict'; const ex = kidsOf(dst).find((x) => x.n === t.name); t.conflict = { size: ex.b, mod: ex.mod, dir: ex.t === 'dir' }; t.msg = 'A ' + (ex.t === 'dir' ? 'folder' : 'file') + ' with this name already exists'; changed = true; continue; }
        if (pol === 'skip') { t.state = 'cancelled'; tasks.splice(tasks.indexOf(t), 1); log('transfer', 'Skipped ' + t.name + ' (already exists)', { dir: t.dir, host: t.host }); changed = true; continue; }
        t.resolution = pol;
      }
      if (running().length < settings.parallel) { t.state = 'running'; t.startedAt = t.startedAt || now(); changed = true; }
    }
    // run
    const byHost = {}; for (const t of running()) byHost[t.host] = (byHost[t.host] || 0) + 1;
    const sim = settings.simSpeed || 1;
    for (const t of running().slice()) {
      const share = (LINK[t.host] || 30e6) / byHost[t.host];
      const cap = settings.limit > 0 ? (settings.limit * 1e6) / Math.max(1, running().length) : Infinity;
      const target = Math.min(share, cap) * (0.94 + rnd() * 0.12);
      t.speed = t.speed ? t.speed * 0.7 + target * 0.3 : target;
      t.done = Math.min(t.bytes, t.done + t.speed * (dt / 1000) * sim);
      if (t.done >= t.bytes) {
        const dst = getNode(t.dstHost, t.dstDir);
        if (!dst) fail(t, 'Destination folder no longer exists'); else finish(t);
        changed = true;
      }
    }
    return changed;
  }
  const eta = (t) => (t.speed > 0 ? (t.bytes - t.done) / (t.speed * (settings.simSpeed || 1)) : Infinity);

  function tick(dt) {
    clockMs += dt;
    serverTick(dt); latencyTick(dt);
    const ch = transferTick(dt);
    if (ch) emit('transfers');
    emit('tick');
  }

  /* ---------- public ---------- */
  const api = {
    USER, settings, reset, now, tick, on(fn) { listeners.push(fn); return () => { listeners.splice(listeners.indexOf(fn), 1); }; },
    get servers() { return servers; }, get tasks() { return tasks; }, get history() { return history; }, get notes() { return notes; },
    server, connected, connect, trust, cancelConnect, disconnect, simulateLoss, retryNow, addServer, updateServer, forget, certChanged, reverify, switchRoute, restoreServer, start: (id) => START[id] || '/',
    fs: { add, copy, mkfile, get: getNode, list, exists, mkdir, rename, remove, restore, info, total, view, uniqueName, writableDir, canRead, canWrite, kidsOf, itemCount },
    makeSearch, enqueue, pause, resumeTask, cancel, dismiss, retry, resolve, pauseAll, resumeAll, clearDone, retryFailed, task: taskById, eta, ACTIVE,
    totalSpeed: () => running().reduce((a, t) => a + t.speed * (settings.simSpeed || 1), 0),
    note, log, emit,
    util: { fmtBytes, fmtDate, fmtTime, fmtDur, fmtSpeed, norm, join, parentOf, baseOf, segs, kindOf, KINDS, permOctal, sizeOf }
  };
  listeners = [];
  reset();
  return api;
});
