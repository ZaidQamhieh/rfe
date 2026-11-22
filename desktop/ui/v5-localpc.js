/* RFE - this PC as a computer for the Devices page.
   When no computer is online but the agent runs on this PC, the Devices page reads that agent directly through its admin
   commands (no sign-in, the same ones `rfe devices` uses). To the rest of the app it looks like one more online computer,
   id "this-pc": the calls the page already makes (list_devices, device_access_all, set_device_access, revoke_device,
   remove_device, list_pair_requests, answer_pair_request, audit_page, generate_pairing_code) are answered here. */
rfeModule('v5-localpc', ['features'], function () {
  'use strict';
  const A = window.A; if (!A) return;
  const E = A.E, st = A.state;
  const ID = 'this-pc';
  const pc = { id: ID, name: 'this PC', state: 'online', pinned: true, fp: '', host: '', caps: null };
  let ready = false;
  const realOnline = () => E.servers.some((s) => s.state === 'online');
  const active = () => ready && !realOnline();
  A.localPC = { id: ID, srv: () => (active() ? pc : null), on: active };

  const admin = (args) => E.call('local_agent_admin', { args });
  const json = (out) => { try { return JSON.parse(out || '[]'); } catch (e) { return []; } };
  const devices = () => admin(['devices', '-json']).then(json);
  const word = (x) => (x ? 'on' : 'off');

  const accessOf = (d) => ({ id: d.id, label: d.label || 'Phone', revoked: !!d.revoked, current: false, viaLogin: false, jailRoot: d.jailRoot || '', readOnly: !!d.readOnly, viewApps: !!d.viewApps, launchApps: !!d.launchApps, browse: !!d.canBrowse, download: !!d.canDownload, upload: !!d.canUpload, modify: !!d.canModify, delete: !!d.canDelete, share: !!d.canShare });
  const deviceOf = (d) => ({ id: d.id, label: d.label || 'Phone', created: d.created || 0, lastSeen: d.lastSeen || 0, revoked: !!d.revoked, current: false, lastAddress: d.lastAddress || '', lastVersion: '', viaLogin: false });
  const FILE = ['browse', 'download', 'upload', 'modify', 'delete', 'share', 'wake'];

  async function setAccess(id, patch) {
    const all = await devices(); const cur = all.find((d) => d.id === id); if (!cur) throw new Error('That phone is no longer paired with this PC.');
    const a = accessOf(cur);
    if (FILE.some((k) => k in patch)) { const next = Object.assign({}, a, patch); const on = FILE.filter((k) => next[k]); await admin(['allow', id, on.length ? on.join(',') : 'none']); }
    if ('readOnly' in patch) await admin(['readonly', id, word(patch.readOnly)]);
    if ('viewApps' in patch || 'launchApps' in patch) { const n = Object.assign({}, a, patch); await admin(['apps', id, word(n.viewApps), word(n.launchApps)]); }
    if ('jailRoot' in patch) await admin(['jail', id, patch.jailRoot || '']);
    const after = (await devices()).find((d) => d.id === id);
    return after ? accessOf(after) : a;
  }

  const handlers = {
    list_devices: () => devices().then((l) => l.map(deviceOf)),
    device_access_all: () => devices().then((l) => l.map(accessOf)),
    set_device_access: (a) => setAccess(a.id, a.patch || {}),
    revoke_device: (a) => admin(['revoke', a.id]).then(() => ({ signedOut: false })),
    remove_device: (a) => admin(['remove', a.id]).then(() => ({ signedOut: false })),
    list_pair_requests: () => admin(['pair', 'requests', '-json']).then((out) => ({ forbidden: false, requests: json(out).map((r) => ({ id: r.id, label: r.label, matchCode: r.matchCode, address: r.remoteIp, replaces: r.replaces || '', ageSeconds: 0, expiresInSeconds: Math.max(0, Math.round((Date.parse(r.expiresAt) - Date.now()) / 1000)) || 0 })), limit: 0, ttlSeconds: 0 })),
    answer_pair_request: (a) => admin(['pair', a.approve ? 'accept' : 'reject', a.id]).then(() => 'done', (e) => { if (/waiting requests match/i.test((e && e.message) || '')) return 'gone'; throw e; }),
    audit_page: () => admin(['audit', '-json', '-n', '50']).then((out) => ({ forbidden: false, entries: json(out).map((e) => ({ id: e.id, at: e.at, action: e.action, actor: e.actor || '', target: e.target || '', detail: e.detail || '' })), nextBefore: null })),
    generate_pairing_code: (a) => E.call('generate_local_pairing_code', a),
  };

  const call0 = E.call;
  E.call = function (cmd, args) {
    if (args && args.host === ID && handlers[cmd]) return Promise.resolve().then(() => handlers[cmd](args));
    return call0.apply(this, arguments);
  };
  const server0 = E.server;
  E.server = function (id) { return id === ID ? (active() ? pc : undefined) : server0.apply(this, arguments); };

  /* Is there an agent on this PC to read? Asked once, then again while no computer is online, so installing or starting it shows up. */
  let tick = 0;
  function probe() {
    if (realOnline() && !ready) return;
    if (ready && tick++ % 4) return; /* found: look again only now and then, to notice it going away */
    devices().then(() => true, () => false).then((ok) => {
      if (ok === ready) return; ready = ok;
      if (st.view === 'devices' && A.pageDevices) A.pageDevices();
      if (ready && A.loadInbox) A.loadInbox();
    });
  }
  setTimeout(probe, 1500);
  setInterval(probe, 5000);
});
