/* A stand-in for the Rust core, for developing and testing the window without a real agent. It serves
   the mockup's sample data (the old simulated engine, ui-dev/sim-engine.js) through the same Tauri
   commands the real core has, so the window can be compared screen by screen with the mockup.
   Dev and test use only; it is never loaded by the app. */
(function (root) {
  'use strict';
  const Sim = root.SimEngine;
  const IDS = { 'nas': '192.168.1.20:7443', 'build': '100.101.4.7:7443', 'fra': '203.0.113.9:7443', 'mac': '100.88.12.3:7443', 'pi': '192.168.1.44:7443', 'office': '100.77.2.15:7443' };
  const SIM_OF = {}; for (const k in IDS) SIM_OF[IDS[k]] = k;
  const hex = (s) => s.fp.replace(/:/g, '').toLowerCase().padEnd(64, '0');
  const iso = (ms) => new Date(ms).toISOString();
  const entry = (n, path) => ({ name: n.n, path, isDir: n.t === 'dir', size: n.t === 'dir' ? 0 : Math.round(n.b), mimeType: '', mode: n.perm, modified: iso(n.mod), created: iso(n.mod), isSymlink: false, symlinkTarget: '', childCount: n.t === 'dir' ? Sim.fs.itemCount(n) : null });
  const J = (p, n) => (p === '/' ? '' : p) + '/' + n;
  const FAST = /[?&]fast=1/.test(location.search);
  const delay = (v, ms) => (FAST ? Promise.resolve(v) : new Promise((r) => setTimeout(() => r(v), ms || 0)));
  const err = (msg) => Promise.reject(msg);
  const state = { rid: 0, xfers: [], trash: [], shares: [], online: {}, prefs: { parallel: 2, limitMbps: 0, onConflict: 'keep', verify: true } };

  function simHost(h) { if (h == null || h === '') return 'nas'; return SIM_OF[h] || h; }
  function node(host, path) { return Sim.fs.get(host, path); }

  const cmds = {
    list_hosts() {
      return Sim.servers.map((s, i) => ({ key: IDS[s.id], name: s.name, host: IDS[s.id], fingerprint: hex(s), username: 'zaid', signedIn: ['online', 'lost', 'offline'].includes(s.state) && s.id !== 'mac', active: i === 0 }));
    },
    agent_health({ host }) {
      const s = Sim.server(simHost(host));
      if (!s || s.state === 'lost') return err('The agent did not answer within 10 seconds');
      if (s.state === 'offline') return err('connect error: network unreachable');
      if (s.state === 'disconnected' && s.id !== 'nas' && s.id !== 'build') return err('not signed in');
      return delay({ host: IDS[s.id], fingerprint: hex(s), health: { status: 'ok', name: s.name, version: s.agent, os: s.os, address: s.host, tailscaleAddress: '', macAddress: 'aa:bb:cc:dd:ee:0' + s.id.length }, status: { version: s.agent, uptimeSeconds: 86400 * 12, platform: 'linux', freeBytes: (s.disk[1] - s.disk[0]) * 1e9, totalBytes: s.disk[1] * 1e9 }, statusNote: '', metrics: null, metricsForbidden: false, metricsNote: '' }, 4 + (s.base || 4));
    },
    files_roots({ host }) {
      const h = simHost(host);
      return { locations: [{ path: '/srv', label: 'srv', totalBytes: 0, freeBytes: 0, isOs: false }, { path: '/home/zaid', label: 'zaid', totalBytes: 0, freeBytes: 0, isOs: false }], source: 'roots', readOnly: false, accessDenied: false, caps: { browse: true, download: true, upload: true, modify: true, delete: true, share: true } };
    },
    files_list({ host, path }) {
      const h = simHost(host); const n = node(h, path);
      if (!n || n.t !== 'dir') return err('The agent says: not found (NOT_FOUND)');
      if (!Sim.fs.canRead(n)) return err('The agent says: permission denied (FORBIDDEN)');
      const kids = Sim.fs.kidsOf(n);
      return delay({ path, crumbs: [], entries: kids.map((k) => entry(k, J(path, k.n))), nextCursor: null }, 6);
    },
    files_meta({ host, path }) { const n = node(simHost(host), path); return n ? entry(n, path) : err('The agent says: not found (NOT_FOUND)'); },
    files_create_folder({ host, parent, name }) {
      try { Sim.fs.mkdir(simHost(host), parent, name); } catch (e) { return err('The agent says: ' + e.message + ' (' + (e.code === 'EACCES' ? 'FORBIDDEN' : 'CONFLICT') + ')'); }
      return delay(entry(node(simHost(host), J(parent, name)), J(parent, name)), 5);
    },
    files_create_file({ host, parent, name }) { try { Sim.fs.add(simHost(host), parent, Sim.fs.mkfile(name, 0)); } catch (e) { return err('The agent says: ' + e.message + ' (FORBIDDEN)'); } return delay(null, 5); },
    files_rename({ host, path, newName }) {
      const dir = path.slice(0, path.lastIndexOf('/')) || '/'; const old = path.slice(path.lastIndexOf('/') + 1);
      try { Sim.fs.rename(simHost(host), dir, old, newName); } catch (e) { return err('The agent says: ' + e.message + ' (' + (e.code === 'EACCES' ? 'FORBIDDEN' : 'CONFLICT') + ')'); }
      return delay(entry(node(simHost(host), J(dir, newName)), J(dir, newName)), 5);
    },
    files_trash({ host, path }) {
      const dir = path.slice(0, path.lastIndexOf('/')) || '/'; const nm = path.slice(path.lastIndexOf('/') + 1);
      try { const rem = Sim.fs.remove(simHost(host), dir, [nm]); for (const r of rem) state.trash.push({ id: 'tr' + (++state.rid), name: r.n, originalPath: path, deletedAt: iso(Date.now()), size: r.b, isDir: r.t === 'dir', _node: r, _host: simHost(host), _dir: dir }); } catch (e) { return err('The agent says: ' + e.message + ' (FORBIDDEN)'); }
      return delay(null, 5);
    },
    trash_items() { return delay(state.trash.map((t) => ({ id: t.id, name: t.name, originalPath: t.originalPath, deletedAt: t.deletedAt, size: t.size, isDir: t.isDir })), 4); },
    trash_restore_items({ ids }) { for (const id of ids) { const i = state.trash.findIndex((t) => t.id === id); if (i >= 0) { const t = state.trash.splice(i, 1)[0]; Sim.fs.restore(t._host, t._dir, [t._node]); } } return delay(null, 4); },
    trash_empty_items({ ids }) { state.trash = ids.length ? state.trash.filter((t) => !ids.includes(t.id)) : []; return delay(null, 4); },
    files_copy({ host, sources, destDir }) { const h = simHost(host); for (const s of sources) { const d = s.slice(0, s.lastIndexOf('/')) || '/'; try { Sim.fs.copy(h, d, [s.slice(s.lastIndexOf('/') + 1)], destDir, false); } catch (e) { return err('The agent says: ' + e.message + ' (FORBIDDEN)'); } } return delay(null, 6); },
    files_move({ host, sources, destDir }) { const h = simHost(host); for (const s of sources) { const d = s.slice(0, s.lastIndexOf('/')) || '/'; try { Sim.fs.copy(h, d, [s.slice(s.lastIndexOf('/') + 1)], destDir, true); } catch (e) { return err('The agent says: ' + e.message + ' (FORBIDDEN)'); } } return delay(null, 6); },
    files_search({ host, options }) {
      const h = simHost(host); const out = []; const q = options.query.toLowerCase(); const stack = [['/', Sim.fs.get(h, '/')]];
      while (stack.length && out.length < (options.limit || 100)) { const [p, n] = stack.pop(); if (!n) continue; if (p !== '/' && n.n.toLowerCase().includes(q)) out.push(entry(n, p)); if (n.t === 'dir') for (const k of Sim.fs.kidsOf(n)) stack.push([J(p, k.n), k]); }
      return delay(out, 30);
    },
    local_search({ query, limit }) {
      const out = []; const q = query.toLowerCase(); const stack = [['/', Sim.fs.get('local', '/')]];
      while (stack.length && out.length < (limit || 100)) { const [p, n] = stack.pop(); if (!n) continue; if (p !== '/' && n.n.toLowerCase().includes(q)) out.push(entry(n, p)); if (n.t === 'dir') for (const k of Sim.fs.kidsOf(n)) stack.push([J(p, k.n), k]); }
      return delay(out, 10);
    },
    files_recent() { return delay([{ name: 'notes.txt', path: '/srv/notes.txt', isDir: false, size: 10, modified: iso(Date.now() - 600e3), mode: '-rw-r--r--' }], 5); },
    files_read_text({ host, path }) { const n = node(simHost(host), path); if (!n || n.t === 'dir') return err('this file is not text, or is too large to open here'); return delay({ path, text: n.content == null ? '' : n.content, modified: iso(n.mod), size: (n.content || '').length }, 5); },
    files_write_text({ host, path, text }) { const n = node(simHost(host), path); if (n) { n.content = text; n.b = text.length; n.mod = Date.now(); } return delay(entry(n, path), 5); },
    files_thumb() { return err('no thumbnail'); },
    files_checksum({ path, algo }) { return delay({ path, algorithm: algo, checksum: '9f2c4e0a7b3d1c9e5a8f6b21d41e83aa'.repeat(2) }, 20); },
    files_checksums({ paths }) { return delay(paths.map((p) => ({ path: p, hash: '9f2c4e0a7b3d1c9e5a8f6b21d41e83aa'.repeat(2), error: '' })), 20); },
    files_chmod({ host, path, mode }) { const n = node(simHost(host), path); return delay(entry(n, path), 5); },
    files_compress() { return delay(null, 10); },
    files_extract({ archive }) { return delay({ name: 'x', path: archive, isDir: true }, 10); },
    files_archive_list() { return delay([], 5); },
    share_mint({ path, expiresInSeconds }) { const t = 'tok' + (++state.rid); const h = (t + '0'.repeat(64)).slice(0, 64); state.shares.push({ tokenHash: h, path, expiresAt: Math.floor(Date.now() / 1000) + expiresInSeconds }); return delay({ token: t, tokenHash: h, expiresAt: Math.floor(Date.now() / 1000) + expiresInSeconds, url: 'https://192.168.1.20:7443/v1/share/' + t }, 10); },
    share_links() { return delay(state.shares.slice(), 5); },
    share_revoke_link({ tokenHash }) { state.shares = state.shares.filter((s) => s.tokenHash !== tokenHash); return delay(null, 5); },
    wake_computer() { return delay(null, 5); },
    probe_agent({ host }) { const s = Sim.server(simHost(host)); if (!s) return delay({ fingerprint: 'ab'.repeat(32), previous: '', changed: false }, 80); if (!s.reachable) return err('connect error: no answer'); return delay({ fingerprint: hex(s), previous: s.pinned ? hex(s) : '', changed: false }, 80); },
    login() { return err('The agent says: wrong username or password (UNAUTHORIZED)'); },
    pair_with_code() { return err('The agent says: that code is not valid (BAD_REQUEST)'); },
    transfer_folder() { return '/home/zaid/Downloads/RFE Desktop'; },
    transfer_upload_tree({ host, localPath, remoteDir }) { return startX(host, 'upload', localPath, remoteDir, false); },
    transfer_download_tree({ host, remotePath, isDir }) { return startX(host, 'download', remotePath, '', isDir); },
    transfer_list() { stepX(); return state.xfers.map((x) => ({ id: x.id, direction: x.direction, state: x.state, name: x.name, remotePath: x.remotePath, localPath: x.localPath, done: Math.round(x.done), total: x.total, error: x.error, verified: x.state === 'done', host: x.host, conflict: x.conflict || null })); },
    desktop_set_prefs(p) { state.desktop = Object.assign({}, p); return null; }, desktop_has_tray() { return true; }, desktop_set_scale({ zoom }) { return zoom; }, desktop_autostart_state() { return !!state.autostart; },
    desktop_set_autostart({ on }) { state.autostart = !!on; return state.autostart; }, desktop_notify(n) { (state.notified = state.notified || []).push(n); return true; },
    update_check({ channel }) { return delay({ current: '1.0.0', latest: channel === 'beta' ? '1.2.0-rc.1' : '1.1.0', available: true, prerelease: channel === 'beta', notes: ['Faster folders', 'A fix'], page: 'https://github.com/x/y/releases', package: { name: 'RFE-Desktop_1.1.0_amd64.deb', size: 8800000 } }, 20); },
    update_download({ version, name }) { return delay('/home/zaid/Downloads/RFE Desktop/' + name, 20); },
    transfer_set_prefs(p) { state.prefs = Object.assign({}, p); return null; },
    transfer_resolve({ id, how }) { const x = state.xfers.find((q) => q.id === id); if (!x || x.state !== 'conflict') return err('that transfer is not waiting for an answer'); x.conflict = null; if (how === 'skip') { x.state = 'cancelled'; x.error = 'Skipped: a file with this name already exists'; } else x.state = 'queued'; return null; },
    transfer_cancel({ id }) { const x = state.xfers.find((q) => q.id === id); if (x) x.state = 'cancelled'; return null; },
    transfer_retry({ id }) { const x = state.xfers.find((q) => q.id === id); if (x && (x.state === 'failed' || x.state === 'paused' || x.state === 'cancelled')) { x.state = 'queued'; x.error = ''; } return null; },
    transfer_clear_finished() { state.xfers = state.xfers.filter((x) => x.state === 'queued' || x.state === 'running'); return 0; },
    local_places() { return [{ label: 'Home', path: '/home/zaid', kind: 'home' }, { label: 'Desktop', path: '/home/zaid/Desktop', kind: 'folder' }, { label: 'Documents', path: '/home/zaid/Documents', kind: 'folder' }, { label: 'Downloads', path: '/home/zaid/Downloads', kind: 'folder' }, { label: 'Pictures', path: '/home/zaid/Pictures', kind: 'folder' }, { label: 'Computer', path: '/', kind: 'drive' }]; },
    local_list({ path }) { const n = node('local', path); if (!n || n.t !== 'dir') return err('cannot open ' + path + ': No such file or directory'); return delay({ path, crumbs: [], entries: Sim.fs.kidsOf(n).map((k) => entry(k, J(path, k.n))), nextCursor: null }, 4); },
    local_create_folder({ parent, name }) { try { Sim.fs.mkdir('local', parent, name); } catch (e) { return err(e.message); } return delay(entry(node('local', J(parent, name)), J(parent, name)), 3); },
    local_rename({ path, newName }) { const dir = path.slice(0, path.lastIndexOf('/')) || '/'; try { Sim.fs.rename('local', dir, path.slice(path.lastIndexOf('/') + 1), newName); } catch (e) { return err(e.message); } return delay(entry(node('local', J(dir, newName)), J(dir, newName)), 3); },
    local_delete({ path }) { const dir = path.slice(0, path.lastIndexOf('/')) || '/'; try { Sim.fs.remove('local', dir, [path.slice(path.lastIndexOf('/') + 1)]); } catch (e) { return err(e.message); } return delay(null, 3); },
    rename_host() { return null; }, remove_host() { return { wasActive: false }; }, switch_host() { return {}; }, sign_out() { return {}; },
    generate_pairing_code() { return delay({ status: 'ok', code: '482913', expiresInSeconds: 600, qr: JSON.stringify({ address: '192.168.1.20:7443', certFingerprint: 'AB', code: '482913' }) }, 20); },
    list_devices() { return delay([], 5); }, list_pair_requests() { return delay({ requests: [] }, 5); },
    list_host_apps() { return delay({ apps: [] }, 5); },
    discover_agents() { return delay([], 400); },
    device_access_all() { return delay([], 5); },
    answer_pair_request() { return delay({ ok: true }, 5); },
    set_device_access() { return delay(null, 5); }, revoke_device() { return delay({ signedOut: false }, 5); }, remove_device() { return delay(null, 5); },
    audit_page() { return delay({ forbidden: false, entries: [{ at: iso(Date.now() - 3600e3), action: 'login', target: 'zaid', detail: 'password', actor: '' }, { at: iso(Date.now() - 7200e3), action: 'device pair', target: 'Pixel 8', detail: '', actor: '' }] }, 10); },
    launch_host_app() { return delay(null, 5); },
    request_pairing() { return delay(null, 5); }, poll_pairing() { return delay({ status: 'pending', saved: null }, 5); }, cancel_pairing() { return null; },
    diagnostics() { return 'RFE Desktop diagnostics\napp version: dev (stub)'; },
    choose_download_folder() { return delay('/home/zaid/Downloads/Chosen', 5); }, reset_download_folder() { return '/home/zaid/Downloads/RFE Desktop'; },
    pick_files() { return delay(['/home/zaid/Documents/notes.txt'], 5); }, pick_folder() { return delay('/home/zaid/Documents', 5); },
    local_read_text({ path }) { const n = node('local', path); return n && n.t !== 'dir' ? delay({ path, text: n.content == null ? '' : n.content, size: n.b }, 4) : err('this file is not text, or is too large to open here'); },
    local_read_image() { return err('not an image'); }, local_open() { return null; },
    local_save_text({ dir, name }) { return delay(dir + '/' + name, 4); },
    transfer_pause({ id }) { const x = state.xfers.find((q) => q.id === id); if (x && (x.state === 'running' || x.state === 'queued')) x.state = 'paused'; return null; },
    set_log_level({ level }) { return level; }, check_keystore() { return delay(null, 20); }, reset_device_key() { return delay(null, 20); },
    agent_log() { return delay({ forbidden: false, lines: [{ ts: '2026-10-05T10:00:00Z', message: 'agent started' }, { ts: '2026-10-05T10:01:00Z', message: 'login ok' }] }, 10); },
    app_settings() { return { logLevel: 'info', version: '1.0.0' }; }
  };
  function startX(host, dir, path, other, isDir) {
    const sim = simHost(host); const name = path.slice(path.lastIndexOf('/') + 1);
    const srcHost = dir === 'upload' ? 'local' : sim; const n = Sim.fs.get(srcHost, path); const bytes = n ? Math.max(1, Sim.fs.total(n).bytes) : 1000;
    const id = 'x' + (++state.rid);
    /* a name already taken at the destination, as the core asks about it */
    const dstHost = dir === 'upload' ? sim : 'local'; const dstDir = dir === 'upload' ? other : '/home/zaid/Downloads'; const dn = Sim.fs.get(dstHost, dstDir); const taken = dn && Sim.fs.kidsOf(dn).find((k) => k.n === name);
    const askNow = taken && state.prefs.onConflict === 'ask';
    state.xfers.push({ id, direction: dir, state: askNow ? 'conflict' : 'queued', conflict: askNow ? { isDir: taken.t === 'dir', size: taken.b || 0, modifiedMs: taken.mod || null, modifiedText: '' } : null, name, remotePath: dir === 'upload' ? J(other, name) : path, localPath: dir === 'upload' ? path : '', done: 0, total: bytes, error: '', host: IDS[sim] || host, speed: 38e6 + Math.random() * 20e6, last: Date.now() });
    return [id];
  }
  function stepX() {
    const now = Date.now(); let running = state.xfers.filter((x) => x.state === 'running').length;
    for (const x of state.xfers) {
      if (x.state === 'queued' && running < 2) { x.state = 'running'; running++; x.last = now; }
      if (x.state === 'running') { x.done = Math.min(x.total, x.done + x.speed * (now - x.last) / 1000 * (window.__SPEED || 1)); x.last = now; if (x.done >= x.total) x.state = 'done'; }
    }
  }
  root.StubBackend = {
    invoke(cmd, args) { const f = cmds[cmd]; if (!f) return Promise.reject('unknown command ' + cmd); try { return Promise.resolve(f(args || {})); } catch (e) { return Promise.reject(String(e && e.message || e)); } },
    cmds, state
  };
  root.__TAURI__ = { core: { invoke: (c, a) => root.StubBackend.invoke(c, a) } };
})(typeof window !== 'undefined' ? window : globalThis);
