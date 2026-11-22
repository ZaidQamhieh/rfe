/* DEV HARNESS ONLY. Never linked from desktop/ui/index.html and never shipped.
   Loaded by stub-backend.js when the page is opened with ?seed=v5 (or ?seed=v5-empty). It maps the v5 mockup's
   own sample data (K.D from mockups/v5/kit.js, B.HIST from mockups/v5/base.js; both are loaded as plain scripts just
   before this one) into the shapes the stub backend returns, so the real window can be compared with the mockup
   screen by screen. The data is read from the mockup files and is never copied here, so the two cannot drift.
   ?seed=v5-empty maps the mockup's first-run state: no computer, device, transfer or event. */
(function (root) {
  'use strict';
  const SB = root.StubBackend, K = root.K, B = root.B;
  if (!SB || !K || !K.D) { console.error('v5-seed: the mockup data did not load'); return; }
  const EMPTY = /[?&]seed=v5-empty/.test(location.search);
  const D = K.D, cmds = SB.cmds;
  const NOW = Date.parse('2026-11-01T10:46:00');
  const MON = { Jan: 0, Feb: 1, Mar: 2, Apr: 3, May: 4, Jun: 5, Jul: 6, Aug: 7, Sep: 8, Oct: 9, Nov: 10, Dec: 11 };
  const DAY = { Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6 };

  /* Turns the mockup's loose date words ('1 Nov 09:12', '1 Nov', '09:01', 'Tue', '12 min ago', 'Yesterday') into epoch ms. */
  function when(t) {
    t = String(t || '').trim(); let m;
    if ((m = /^(\d+) (\w{3})(?: (\d\d):(\d\d))?$/.exec(t))) return new Date(2026, MON[m[2]], +m[1], +(m[3] || 0), +(m[4] || 0)).getTime();
    if ((m = /^(\d\d):(\d\d)$/.exec(t))) return new Date(2026, 10, 1, +m[1], +m[2]).getTime();
    if ((m = /^(\d+) (min|h|d) ago$/.exec(t))) return NOW - +m[1] * { min: 6e4, h: 36e5, d: 864e5 }[m[2]];
    if (t === 'Yesterday') return NOW - 864e5;
    if (t in DAY) { const back = (new Date(NOW).getDay() - DAY[t] + 7) % 7 || 7; return NOW - back * 864e5; }
    return NOW;
  }
  const iso = (ms) => new Date(ms).toISOString();
  const bytes = (f) => { if (f.b != null) return Math.round(f.b * 1048576); const m = /([\d.]+) (KB|MB|GB)/.exec(f.s || ''); return m ? Math.round(+m[1] * { KB: 1024, MB: 1048576, GB: 1073741824 }[m[2]]) : 0; };
  const hex = (s) => String(s.fp || s.tag || '').replace(/\s+/g, '').toLowerCase().padEnd(64, '0');
  const secs = (u) => { const m = /(?:(\d+) d)? ?(?:(\d+) h)? ?(?:(\d+) min)?/.exec(u || ''); return m ? (+m[1] || 0) * 86400 + (+m[2] || 0) * 3600 + (+m[3] || 0) * 60 : 0; };
  const servers = EMPTY ? [] : D.servers;
  const byAddr = (h) => servers.find((s) => s.addr === h || s.name === h || s.id === h);
  const joinP = (a, n) => (a === '/' ? '' : a) + '/' + n;
  const curDir = '/' + D.path.slice(1).join('/');
  const entry = (f, dir) => ({ name: f.n, path: joinP(dir, f.n), isDir: f.k === 'dir', size: f.k === 'dir' ? 0 : bytes(f), mimeType: '', mode: f.pm || 'rw-r--r--', modified: iso(when(f.m)), created: iso(when(f.m)), isSymlink: false, symlinkTarget: '', childCount: f.k === 'dir' ? f.c : null });
  /* "3.8 MB" -> 3800000: the window counts a megabyte as a million bytes, so a search result prints the same size words as the mockup. */
  const decimalBytes = (x) => { const m = /([\d.]+) (KB|MB|GB)/.exec(x || ''); return m ? Math.round(+m[1] * { KB: 1e3, MB: 1e6, GB: 1e9 }[m[2]]) : 0; };
  const searchEntry = (r) => ({ name: r[0], path: r[2] + '/' + r[0], isDir: r[1] === 'dir', size: r[1] === 'dir' ? 0 : decimalBytes(r[5]), mimeType: '', mode: 'rw-r--r--', modified: iso(when(r[4])), created: iso(when(r[4])), isSymlink: false, symlinkTarget: '', childCount: r[1] === 'dir' ? (parseInt(r[5], 10) || 0) : null });

  /* The mockup's studio-pc has its own downloads folder (the "Settings for" box): the window keeps that per computer in its own storage. */
  try { const k = 'rfe-v5-pcprefs-v1', all = JSON.parse(localStorage.getItem(k) || '{}'), st = servers.find((x) => x.name === 'studio-pc'); if (st && !all[st.addr]) { all[st.addr] = { dir: '~/Downloads/RFE/studio-pc' }; localStorage.setItem(k, JSON.stringify(all)); } } catch (e) { /* no storage */ }

  const orig = { files_list: cmds.files_list, local_list: cmds.local_list, local_search: cmds.local_search };

  cmds.list_hosts = () => servers.map((s, i) => ({ key: s.addr, name: s.name, host: s.addr, fingerprint: hex(s), username: D.me.account, signedIn: s.dot !== 'bad', active: i === 0 }));
  /* "Find your PC" in the Add a computer box: the two PCs the mockup offers (its own list is private to base.js, so they are written here, dev only) plus the computer already saved. */
  /* First run: no computer and no agent on this PC either (the stub would otherwise answer as a local agent with its own phones, which the mockup's first-run screen does not have). */
  if (EMPTY) cmds.local_agent_admin = () => Promise.reject(new Error('no agent on this PC'));
  cmds.discover_agents = () => Promise.resolve(EMPTY ? [] : [{ name: 'garage-pi', hostport: '192.168.1.44:8765', version: '1.3.0', os: 'Raspberry Pi' }, { name: 'den-desktop', hostport: '192.168.1.45:8765', version: '1.3.0', os: 'Windows' }].concat(servers.slice(0, 1).map((s) => ({ name: s.name, hostport: s.addr, version: s.ver, os: s.sys }))));
  cmds.agent_health =({ host }) => {
    const s = byAddr(host);
    if (!s || s.dot === 'off') return Promise.reject('connect error: network unreachable');
    if (s.dot === 'bad') return Promise.reject('the certificate fingerprint does not match the one pinned for this agent');
    return Promise.resolve({ host: s.addr, fingerprint: hex(s), health: { status: 'ok', name: s.name, version: s.ver, os: s.sys, address: s.addr, tailscaleAddress: s.tail || '', macAddress: s.mac || '' },
      status: { version: s.ver, uptimeSeconds: secs(s.up), platform: /Windows/.test(s.sys) ? 'windows' : 'linux', freeBytes: (s.total - s.used) * 1e9, totalBytes: s.total * 1e9 }, statusNote: '', metrics: null, metricsForbidden: false, metricsNote: '' });
  };
  cmds.probe_agent = ({ host }) => { const s = byAddr(host); return Promise.resolve(s ? { fingerprint: hex(s), previous: s.dot === 'bad' ? 'ab'.repeat(32) : hex(s), changed: s.dot === 'bad' } : { fingerprint: 'ab'.repeat(32), previous: '', changed: false }); };
  /* "812 GB used" / "1.1 TB used" -> a disk size and free space that make the tree print the same words. The mockup's tree lists only the first two. */
  const usedGB = (note) => { const m = /([\d.]+) (GB|TB)/.exec(note || ''); return m ? +m[1] * (m[2] === 'TB' ? 1000 : 1) : 0; };
  const rootDisk = (r) => { const u = usedGB(r.note) * 1e9; return { totalBytes: u ? 2e12 : 0, freeBytes: u ? 2e12 - u : 0 }; };
  cmds.files_roots = () => ({ locations: (EMPTY ? [] : D.roots.slice(0, 2)).map((r) => Object.assign({ path: r.n, label: r.n.split('/').pop() || r.n, isOs: false }, rootDisk(r))), source: 'roots', readOnly: false, accessDenied: false, caps: { browse: true, download: true, upload: true, modify: true, delete: true, share: true } });
  /* the mockup's folder lists its files; each folder above it lists just the next one down the mockup's path */
  cmds.files_list = (a) => {
    if (EMPTY) return orig.files_list(a);
    if (a.path === curDir) return Promise.resolve({ path: a.path, crumbs: [], entries: D.files.map((f) => entry(f, curDir)), nextCursor: null });
    /* the folders above the mockup's one list it with the sibling the mockup draws beside it, with the mockup's item counts and no date */
    const above = { '/home/sam': [['Documents', 38], ['Projects', 9]], '/home/sam/Projects': [['garden-plan', 11]] }[a.path];
    if (above) return Promise.resolve({ path: a.path, crumbs: [], entries: above.map(([n, c]) => Object.assign(entry({ n, k: 'dir', c, m: '1 Nov' }, a.path), { modified: iso(0), created: iso(0) })), nextCursor: null });
    if ((curDir + '/').startsWith((a.path === '/' ? '' : a.path) + '/')) { const next = curDir.slice(a.path === '/' ? 1 : a.path.length + 1).split('/')[0]; return Promise.resolve({ path: a.path, crumbs: [], entries: [entry({ n: next, k: 'dir', c: 1, m: '1 Nov 08:40' }, a.path)], nextCursor: null }); }
    return orig.files_list(a);
  };
  cmds.local_list = (a) => { if (a.path === '/home/zaid/Downloads' || a.path === '/home/zaid/Downloads/RFE Desktop') return Promise.resolve({ path: a.path, crumbs: [], entries: D.local.map((f) => entry(f, a.path)), nextCursor: null }); return orig.local_list(a); };
  cmds.files_recent = () => Promise.resolve(D.recent.map((r) => { const s = servers.find((x) => x.name === r[1]); return { name: r[0], path: '/home/sam/Projects/' + r[2] + '/' + r[0], isDir: false, size: 0, modified: iso(when(r[3])), mode: 'rw-r--r--', host: s ? s.addr : '' }; }));
  cmds.files_search = ({ host, options }) => { const s = byAddr(host), q = (options.query || '').toLowerCase(); return Promise.resolve(D.search.res.filter((r) => s && r[3] === s.name && r[0].toLowerCase().includes(q)).map(searchEntry)); };
  cmds.local_search = (a) => (EMPTY ? orig.local_search(a) : Promise.resolve(D.local.filter((f) => f.n.toLowerCase().includes((a.query || '').toLowerCase())).map((f) => entry(f, '/home/zaid/Downloads'))));
  cmds.list_devices = () => Promise.resolve(D.devices.map((d, i) => ({ id: d.id || 'dev' + i, label: d.n, created: Math.round(when(d.seen) / 1000) - 86400 * 30, lastSeen: Math.round(when(d.seen) / 1000), revoked: !!d.revoked, current: !!d.me, lastAddress: d.addr, lastVersion: d.ver, viaLogin: /Account/.test(d.how) })));
  /* each device's rights, read from the mockup's own words about what it may do (the same reading as the mockup's rights()) */
  cmds.device_access_all = () => Promise.resolve(D.devices.map((d, i) => {
    const m = d.may.toLowerCase(), own = /owner/.test(m), ro = /read-only/.test(m), has = (re) => !d.revoked && (own || re.test(m));
    return { id: 'dev' + i, download: has(/download/), upload: !ro && has(/upload/), modify: !ro && has(/change/), delete: !ro && has(/delete/), share: has(/share/), viewApps: has(/see apps/), launchApps: has(/start apps/), readOnly: ro, jailRoot: d.lim || '' };
  }));
  /* the mockup's History events, pushed into the window's own history once the app is up (the day words and times become real moments) */
  const KIND = { Transfers: 'transfer', Connections: 'conn', Security: 'audit', Devices: 'audit', Shares: 'audit' };
  const putHistory = () => {
    const A = root.A; if (!A || !A.ready || !A.E || !A.E.servers.length && !EMPTY) return false;
    const E = A.E; if (E.history.some((h) => h.v5)) return true;
    const FIRST = [{ d: 'Today', t: '10:40', cat: 'Connections', tx: 'RFE Desktop started for the first time', pc: '—', dev: 'desk-linux', out: 'Succeeded', src: 'This app', more: 'Version 1.1.0 · nothing added yet' }];
    (EMPTY ? FIRST : B && B.HIST || []).forEach((h, i) => {
      const day = h.d === 'Today' ? NOW : NOW - 864e5, [hh, mm] = h.t.split(':').map(Number), at = new Date(day).setHours(hh, mm, 0, 0);
      const sv = E.servers.find((s) => s.name === h.pc);
      E.history.push({ id: 'v5h' + i, v5: 1, at, kind: KIND[h.cat] || 'conn', text: h.tx, host: sv ? sv.id : '', ok: h.out === 'Succeeded', error: h.out === 'Failed', cat: h.cat, out: h.out, dev: h.dev, src: h.src, ip: h.ip, more: h.more, agent: h.src === 'The PC' });
    });
    E.history.sort((a, b) => b.at - a.at); E.emit('history'); return true;
  };
  /* The mockup's nas has a changed key (dot 'bad'): the window only learns that by connecting, so the seed marks it the way a refused connection would. */
  const markKeyChanged = () => {
    const A = root.A; if (!A || !A.ready || !A.E || !A.E.servers.length) return false;
    servers.filter((x) => x.dot === 'bad').forEach((x) => { const sv = A.E.servers.find((y) => y.name === x.name); if (sv && sv.state !== 'refused') { sv.oldFp = sv.fp; sv.newFp = sv.fp; sv.state = 'refused'; A.E.emit('servers', sv); } });
    return true;
  };
  const poll = setInterval(() => { if (putHistory() && markKeyChanged()) clearInterval(poll); }, 25);
  cmds.list_pair_requests = (a) => Promise.resolve({ requests: (a && a.host && (byAddr(a.host) || {}).name !== 'studio-pc' ? [] : D.requests).map((r, i) => ({ id: 'rq' + i, label: r.n, name: r.n, code: r.code, matchCode: r.code, replaces: r.over || '', remote: r.from, address: r.from, createdAt: iso(NOW - 18e3), expiresInSeconds: 102 - i * 33 })) });
  cmds.audit_page = () => Promise.resolve({ forbidden: false, entries: D.events.map((e) => ({ at: iso(when(e[0])), action: e[1], target: e[2], detail: '', actor: '' })) });
  cmds.list_host_apps = () => Promise.resolve({ launchAllowed: true, apps: D.apps.map((a, i) => ({ id: 'app' + i, name: a[0], description: a[1], category: a[2], launchable: !!a[3] })) });
  /* The Tools pages: the mockup's Trash and Share links, for studio-pc only (the mockup's "Trash on studio-pc"; its share links name the computer they are on). */
  const bytesOf = (t) => { const m = /^([\d.]+) (KB|MB|GB)/.exec(t); return m ? Math.round(+m[1] * { KB: 1e3, MB: 1e6, GB: 1e9 }[m[2]]) : 0; };
  const agoMs = (t) => { const m = /^(\d+) (day|week)s? ago/.exec(t); return m ? +m[1] * (m[2] === 'week' ? 7 : 1) * 86400e3 : 0; };
  cmds.trash_items = ({ host }) => { const s = byAddr(host); return Promise.resolve(s && s.name === 'studio-pc' ? D.trash.map((t, i) => ({ id: 'tr' + i, name: t[0], originalPath: t[2] + '/' + t[0], deletedAt: iso(NOW - agoMs(t[3])), size: bytesOf(t[4]), isDir: t[1] === 'dir' })) : []); };
  cmds.share_links = ({ host }) => { const s = byAddr(host); const endsIn = (t) => { const m = /(\d+) (min|h)/.exec(t); return m ? +m[1] * (m[2] === 'h' ? 3600 : 60) : 3600; }; return Promise.resolve(s ? D.shares.filter((x) => x[3] === s.name).map((x, i) => ({ tokenHash: 'h' + s.name + i, path: '/home/sam/Projects/garden-plan/' + x[0], expiresAt: Math.round(NOW / 1000) + endsIn(x[2]) })) : []); };
  cmds.generate_pairing_code = () => Promise.resolve({ status: 'ok', code: D.pair.code.replace(/\s/g, ''), expiresInSeconds: 252, qr: JSON.stringify({ address: '192.168.1.20:8765', certFingerprint: 'AB', code: D.pair.code.replace(/\s/g, '') }) });
  /* the mockup's transfers, as static rows (no ticking), grouped the way the mockup groups them */
  const T0 = Date.now();
  cmds.transfer_list = () => {
    const rows = []; const X = D.xfer, STATE = { conflict: 'conflict', paused: 'paused', failed: 'failed' };
    /* active rows move in real time (the first at the mockup's 52.4 MB/s, the others slower), so the speed numbers and the Overview chart have something to measure */
    const live = (g, x, i) => (g !== 'active' ? (x.pct || 0) * 10 : Math.min(3.99e9, (x.pct || 0) / 100 * 4e9 + (Date.now() - T0) * (i === 0 ? 52400 : 8000)));
    const add = (g, x, i, st) => { const pc = (x.where.split(' · ')[0]); const sv = servers.find((s) => s.name === pc); rows.push({ id: 'v5' + g + i, direction: x.d === 'up' ? 'upload' : 'download', state: st || STATE[x.st] || 'running', name: x.n, remotePath: x.where.split(' · ')[1] || '', localPath: '', done: live(g, x, i), total: g === 'active' ? 4e9 : 1000, error: x.st ? x.msg : '', verified: g === 'done', host: sv ? sv.addr : '', conflict: x.st === 'conflict' ? { isDir: false, size: 0, modifiedMs: null, modifiedText: '' } : null }); };
    ['attention', 'active', 'queued', 'done'].forEach((g) => X[g].forEach((x, i) => add(g, x, i, g === 'queued' ? 'queued' : g === 'done' ? 'done' : g === 'active' && x.pct === 100 ? 'running' : undefined)));
    return rows;
  };
})(window);
