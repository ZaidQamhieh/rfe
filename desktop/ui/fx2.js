/* RFE Tonal - part 2: onboarding, adding computers (pairing code, discovery), trust and sign-in, certificate change, diagnostics, roots. */
(function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, st = A.state, X = A.X, F = A.fx, H = A.fxHandlers;
  const audit = F.audit;
  const X2 = A.X2 = { auto: {}, code: {}, found: null };
  A.fx2 = {};
  const THUMB = /[?&](thumb|selftest)=1/.test(location.search);
  const fld = (id, label, val, ph, o) => { o = o || {}; return '<div class="fld"' + (o.style ? ' style="' + o.style + '"' : '') + '><label for="' + id + '">' + label + '</label><input id="' + id + '" type="' + (o.type || 'text') + '" value="' + esc(val || '') + '" placeholder="' + esc(ph || '') + '" spellcheck="false" autocomplete="off"' + (o.max ? ' maxlength="' + o.max + '"' : '') + (o.mode ? ' inputmode="' + o.mode + '"' : '') + '><span class="err" id="' + id + 'e"></span></div>'; };
  const setErr = (ctl, id, m) => { const e = $('#' + id + 'e', ctl.el); if (e) e.textContent = m || ''; const i = $('#' + id, ctl.el); if (i) i.style.borderColor = m ? 'var(--err)' : ''; return !m; };
  const fail = (e) => A.snack((e && e.message) || String(e), { error: true });

  /* ================= adding a computer ================= */
  A.connTabs = (cur) => '<div class="seg fxtabs" role="tablist" aria-label="How to add a computer">' + [['manual', 'Manual'], ['code', 'Pairing code'], ['disc', 'Find on network']].map((t) => '<button role="tab" aria-selected="' + (cur === t[0]) + '" data-fx="conn.tab|' + t[0] + '" class="' + (cur === t[0] ? 'on' : '') + '">' + t[1] + '</button>').join('') + '</div>';
  H['conn.tab'] = (tab, a2, b) => { if (b.classList.contains('on')) return; const sc = b.closest('.scrim'); A.dialogReuse = sc && sc._ctl ? sc._ctl : null; (tab === 'manual' ? A.serverDialog() : tab === 'code' ? codeDialog() : discDialog()); A.dialogReuse = null; };
  /* The trust and sign-in dialogs follow a connection the user started here, one step after the other. */
  A.watchConnect = (id) => { X2.auto[id] = true; };
  const addAndOpen = (o, code) => {
    const s = E.addServer(o); if (code) X2.code[s.id] = code; A.watchConnect(s.id); A.setMainHost(s.id); A.go('files'); E.connect(s.id); A.renderAll(); return s;
  };
  A.addAndOpen = addAndOpen;
  const lastState = {};
  E.on((t, d) => {
    if (t !== 'servers' || !d || !d.id) return; const was = lastState[d.id]; lastState[d.id] = d.state; if (was === d.state || !X2.auto[d.id]) return;
    if (d.state === 'trust') setTimeout(() => A.trustDialog(d), 150);
    else if (d.state === 'login') { delete X2.auto[d.id]; const code = X2.code[d.id]; delete X2.code[d.id]; if (code) pairNow(d, code); else setTimeout(() => A.signInDialog(d), 150); }
    else if (d.state === 'offline' || d.state === 'refused' || d.state === 'disconnected' || d.state === 'online') delete X2.auto[d.id];
  });
  async function pairNow(s, code) {
    A.snack('Pairing with ' + s.name + '…');
    try { await E.signInWithCode(s.id, code); audit('Paired with ' + s.name + ' using a code'); A.snack('Paired with ' + s.name); A.renderAll(); }
    catch (e) { A.signInDialog(s, { mode: 'code', error: e.message }); }
  }
  function codeDialog() {
    A.dialog({
      icon: 'plug', cls: 'conn', title: 'New connection', width: 520,
      body: A.connTabs('code') + '<p class="fxp">On the other computer open <b>Devices → Pair a phone</b> in RFE, or run <span class="mono">rfe pair</span>. Enter the address it shows and the 8-character code.</p>' + fld('pcn', 'Name', '', 'e.g. render-farm') + fld('pch', 'Address', '', '100.64.0.12:8765 or host.example.com', {}) + fld('pcc', 'Pairing code', '', 'ABCD 2345', { max: 9 }) + '<div class="hint" style="margin:0">' + ic('shield-check') + '<span>You still compare the server’s certificate fingerprint before the code is sent. The code works once and expires after a few minutes.</span></div>',
      enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Continue', kind: 'f', icon: 'plug', cb: (c) => {
        const g = (i) => $('#' + i, c.el).value.trim(); const name = g('pcn'), host = g('pch'), code = g('pcc').replace(/\s/g, '').toUpperCase(); let ok = true;
        ok = setErr(c, 'pcn', !name ? 'Give it a name' : E.servers.some((s) => s.name.toLowerCase() === name.toLowerCase()) ? 'You already have “' + name + '”' : '') && ok;
        ok = setErr(c, 'pch', !host ? 'Enter its address' : /\s/.test(host) ? 'No spaces in an address' : '') && ok;
        ok = setErr(c, 'pcc', !/^[2-9A-HJ-NP-Z]{8}$/.test(code.toUpperCase()) ? 'The code is 8 letters and digits' : '') && ok; if (!ok) return false;
        addAndOpen({ name, host }, code);
      } }],
      onOpen: (c) => { $('#pcc', c.el).addEventListener('input', (e) => { const v = e.target.value.toUpperCase().replace(/[^2-9A-HJ-NP-Z]/g, '').slice(0, 8); e.target.value = v.length > 4 ? v.slice(0, 4) + ' ' + v.slice(4) : v; }); $('#pcn', c.el).focus(); }
    });
  }
  function discDialog() {
    let found = null, err = '';
    const body = (c) => {
      const el = $('.fxdisc', c.el); if (!el) return;
      if (found === null) { el.innerHTML = '<div class="fxwait"><span class="spin">' + ic('spinner') + '</span><b>Looking for computers…</b><small>Listening for rfe announcements on this network</small></div>'; return; }
      if (err) { el.innerHTML = '<div class="hint bad">' + ic('alert-circle') + '<span>' + esc(err) + '</span></div>'; return; }
      el.innerHTML = found.length ? '<div class="fxsub" style="margin-bottom:.375rem">Found ' + found.length + (found.length === 1 ? ' computer' : ' computers') + ' running rfe on this network.</div>' + found.map((f, i) => { const saved = E.servers.some((s) => s.addr === f.hostport); return '<div class="lrow"><div class="gi">' + ic('server') + '</div><div class="dm"><b>' + esc(f.name) + '</b><small>' + esc(f.hostport) + (f.version ? ' · agent ' + esc(f.version) : '') + '</small></div>' + (saved ? '<span class="tag">Saved</span>' : '<button class="btn sm f" data-fx="disc.add|' + i + '">Add</button>') + '</div>'; }).join('') : empty('search', 'Nothing found', 'No computer announced rfe. It must be on the same Wi-Fi or cable network. For Tailscale machines use a pairing code or the address.');
    };
    let scanId = 0;
    const scan = (c) => {
      const mine = ++scanId; found = null; err = ''; body(c);
      const btn = c.btn && c.btn.scan; if (btn) btn.disabled = true;
      /* An older scan that answers late must not overwrite a newer one; no answer for 10 s is reported. */
      const slow = new Promise((_, no) => setTimeout(() => no(new Error('No answer after 10 seconds. Check that the computer is on this network, or use a pairing code or an address.')), 10000));
      Promise.race([E.call('discover_agents'), slow]).then((l) => { if (mine === scanId) found = l; }).catch((e) => { if (mine === scanId) { found = []; err = e.message; } }).then(() => { if (mine !== scanId) return; if (btn) btn.disabled = false; if (c.el.isConnected) body(c); });
    };
    A.dialog({ icon: 'plug', cls: 'conn', title: 'New connection', width: 520, body: A.connTabs('disc') + '<div class="fxdisc"></div><p class="fxp" style="margin-top:.75rem">Only computers on the same Wi-Fi or cable network appear. Listing a computer does not trust it: you still compare its certificate.</p>', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'scan', label: 'Scan again', kind: 't', icon: 'refresh', cb: (c) => { scan(c); return false; } }], noFocus: true,
      onOpen: (c) => { scan(c); c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-fx^="disc.add"]'); if (!b) return; const f = found[+b.dataset.fx.split('|')[1]]; c.close(); addAndOpen({ name: f.name, host: f.hostport }); }); } });
  }
  const empty = (icn, t, s) => '<div class="fxempty">' + ic(icn, { size: 40 }) + '<b>' + t + '</b><span>' + s + '</span></div>';
  A.fx2.codeDialog = codeDialog; A.fx2.discDialog = discDialog;

  /* ================= trust and sign-in ================= */
  /* The fingerprint is shown whole, in groups, so it can be compared with the one the server prints. */
  /* Grouped by 8 bytes: it may wrap between groups, never inside a byte. The text itself is unchanged. */
  const fpBlock = (fp) => { const b = String(fp || '').split(':'); const g = []; for (let i = 0; i < b.length; i += 8) g.push('<span class="fpg">' + esc(b.slice(i, i + 8).join(':') + (i + 8 < b.length ? ':' : '')) + '</span>'); return '<code class="fpfull">' + g.join('') + '</code>'; };
  A.trustDialog = (s) => {
    A.dialog({ icon: 'shield', title: 'Trust ' + s.name + '?', width: 540, modal: true, enter: 'go',
      body: '<p class="fxp" style="margin-top:0">This is the first time RFE connects to <b>' + esc(s.name) + '</b> (<span class="mono">' + esc(s.addr) + '</span>). Its certificate has this SHA-256 fingerprint:</p><div class="fpdiff one"><div class="new"><small>Presented by the server</small>' + fpBlock(s.fp) + '</div></div><p class="fxp">On that computer run <span class="mono">rfe status</span> and compare it with the fingerprint it prints. If they differ, someone may be intercepting the connection. Nothing is sent to the server until you trust it.</p><label class="fxck"><input type="checkbox" id="tfm"> I compared this fingerprint with the one on that computer and they match</label>',
      actions: [{ label: 'Cancel', kind: 'tx', cb: () => { E.cancelConnect(s.id); delete X2.auto[s.id]; } }, { id: 'go', label: 'They match: trust', kind: 'f', icon: 'shield-check', cb: () => { const box = document.getElementById('tfm'); if (!box || !box.checked) { const d = document.querySelector('.dlg .db'); let er = d && d.querySelector('#tfe'); if (d && !er) { er = document.createElement('div'); er.id = 'tfe'; er.className = 'hint bad'; er.setAttribute('role', 'alert'); d.appendChild(er); } if (er) er.textContent = 'Tick the box to confirm you compared the fingerprints.'; A.snack('Tick the box to confirm you compared the fingerprints.', { error: true }); return false; } X2.auto[s.id] = true; E.trust(s.id); } }] });
  };
  A.signInDialog = (s, o) => {
    o = o || {}; let mode = o.mode || 'in'; let timer = null; let wait = null; let busy = false; let msg = o.error || ''; let typedUser = null;
    const stop = () => { if (timer) { clearInterval(timer); timer = null; } if (wait) { wait = null; E.cancelApproval().catch(() => {}); } };
    const done = (c, how) => { stop(); A.snack('Signed in to ' + s.name + (s.user ? ' as ' + s.user : '')); c.close(); if (A.mainPane.host == null || A.mainPane.host === 'local') A.setMainHost(s.id); A.renderAll(); };
    const draw = (c) => {
      const tabs = '<div class="seg fxtabs" role="tablist" aria-label="Sign-in method">' + [['in', 'Account'], ['code', 'Pairing code'], ['ask', 'Ask the PC']].map((t) => '<button role="tab" aria-selected="' + (mode === t[0]) + '" data-m="' + t[0] + '" class="' + (mode === t[0] ? 'on' : '') + '">' + t[1] + '</button>').join('') + '</div>';
      const err = msg ? '<div class="hint bad" style="margin:.625rem 0 0">' + ic('alert-circle') + '<span>' + esc(msg) + '</span></div>' : '';
      let b;
      if (mode === 'in') b = '<p class="fxp">Sign in to <b>' + esc(s.name) + '</b> with the account made on that computer (<span class="mono">rfe adduser</span>).</p>' + fld('siu', 'Username', typedUser != null ? typedUser : s.user || '', '') + fld('sip', 'Password', '', '', { type: 'password' }) + '<div class="fxsub">The password goes to the server only over the connection pinned to the fingerprint you compared.</div>';
      else if (mode === 'code') b = '<p class="fxp">Enter the 8-character pairing code from <b>Devices → Pair a phone</b> on ' + esc(s.name) + ', or from <span class="mono">rfe pair</span>.</p>' + fld('sic', 'Pairing code', o.code || '', 'ABCD 2345', { max: 9 });
      else if (wait) b = '<div class="fxwait"><span class="spin">' + ic('spinner') + '</span><b>Waiting for approval…</b><small>On ' + esc(s.name) + ' open <b>Devices</b> and approve this computer. It must show this code:</small></div><div class="pcode" style="margin:.625rem 0"><span>' + esc(wait.matchCode) + '</span></div><div><button type="button" class="btn sm tx" data-pcopy="1">' + ic('copy') + 'Copy code</button></div>';
      else b = '<p class="fxp">No password needed: ' + esc(s.name) + ' shows a request, and someone at that computer approves it. A new device starts with browse access only.</p>';
      $('.db', c.el).innerHTML = tabs + b + err;
      const go = $('[data-id="go"]', c.el); if (go) { go.style.display = mode === 'ask' && wait ? 'none' : ''; go.lastChild && (go.lastChild.textContent = mode === 'ask' ? 'Ask for approval' : mode === 'code' ? 'Pair' : 'Sign in'); go.disabled = busy; }
      const ci = $('#sic', c.el); if (ci) ci.addEventListener('input', (e) => { const v = e.target.value.toUpperCase().replace(/[^2-9A-HJ-NP-Z]/g, '').slice(0, 8); e.target.value = v.length > 4 ? v.slice(0, 4) + ' ' + v.slice(4) : v; });
    };
    A.dialog({ icon: 'user', title: 'Sign in to ' + s.name, width: 480, body: '', enter: 'go', modal: true, onClose: () => stop(),
      actions: [{ label: 'Not now', kind: 'tx', cb: () => { stop(); } },
        { id: 'go', label: 'Sign in', kind: 'f', icon: 'user', cb: (c) => {
          msg = ''; busy = true;
          const finish = (p) => p.then(() => done(c, mode === 'code' ? 'pairing code' : 'password')).catch((e) => { msg = e.message; busy = false; if (c.el.isConnected) draw(c); });
          if (mode === 'in') { const u = $('#siu', c.el).value.trim(), p = $('#sip', c.el).value; typedUser = u; let ok = setErr(c, 'siu', u ? '' : 'Enter your username'); ok = setErr(c, 'sip', p ? '' : 'Enter your password') && ok; if (!ok) { busy = false; return false; } finish(E.signIn(s.id, u, p)); }
          else if (mode === 'code') { const code = $('#sic', c.el).value.replace(/\s/g, '').toUpperCase(); if (!setErr(c, 'sic', /^[2-9A-HJ-NP-Z]{8}$/.test(code.toUpperCase()) ? '' : 'The code is 8 letters and digits')) { busy = false; return false; } finish(E.signInWithCode(s.id, code)); }
          else {
            E.requestApproval(s.id).then((w) => {
              wait = w; busy = false; let polls = 0; draw(c);
              timer = setInterval(() => { E.pollApproval(s.id).then((r) => {
                if (!wait) return; if (r.status === 'approved') { wait = null; done(c, 'approved on the PC'); } else if (r.status === 'rejected' || r.status === 'expired') { clearInterval(timer); timer = null; wait = null; msg = r.status === 'rejected' ? 'The request was rejected on the PC.' : 'The request expired or was already used. Ask again.'; draw(c); }
                else if (++polls > 150) { clearInterval(timer); timer = null; wait = null; E.cancelApproval().catch(() => {}); msg = 'The request timed out. Ask again.'; draw(c); }
              }).catch((e) => { clearInterval(timer); timer = null; wait = null; msg = e.message; draw(c); }); }, 2000);
            }).catch((e) => { msg = e.message; busy = false; draw(c); });
          }
          draw(c); return false;
        } }],
      onOpen: (c) => { draw(c); c.el.addEventListener('click', (e) => { const cp = e.target.closest('[data-pcopy]'); if (cp && wait) { A.copy(wait.matchCode, 'Code copied'); return; } const m = e.target.closest('[data-m]'); if (m && !busy) { stop(); mode = m.dataset.m; msg = ''; draw(c); const nf = mode === 'ask' ? $('[data-id="go"]', c.el) : $(mode === 'code' ? '#sic' : '#siu', c.el); if (nf) nf.focus(); } }); const i = $(mode === 'code' ? '#sic' : '#siu', c.el); if (i) i.focus(); } });
  };
  A.accountsDialog = () => {
    const draw = (c) => { $('.db', c.el).innerHTML = E.servers.map((s) => '<div class="kv2"><div><b>' + esc(s.name) + '</b><small>' + (s.signedIn ? 'Signed in as <b>' + esc(s.user || 'this device') + '</b>' : 'Not signed in') + '</small></div>' + (s.signedIn ? '<button class="btn sm" data-so="' + esc(s.id) + '">Sign out</button>' : '<button class="btn sm f" data-si="' + esc(s.id) + '">Sign in…</button>') + '</div>').join('') || '<div class="fxempty"><span>No servers yet.</span></div>'; };
    A.dialog({ icon: 'user', title: 'Accounts', width: 480, body: '', actions: [{ label: 'Done', kind: 'f' }], noFocus: true, onOpen: (c) => { draw(c); c.el.addEventListener('click', (e) => { const so = e.target.closest('[data-so]'), si = e.target.closest('[data-si]'); if (so) { const s = E.server(so.dataset.so); E.signOut(s.id).then(() => { audit('Signed out of ' + s.name); A.snack('Signed out of ' + s.name); draw(c); A.renderAll(); }).catch(fail); } else if (si) { const s = E.server(si.dataset.si); c.close(); if (s.state === 'trust') A.trustDialog(s); else { if (!s.pinned) { X2.auto[s.id] = true; E.connect(s.id); } else A.signInDialog(s); } } }); } });
  };

  /* ================= certificate change ================= */
  A.stateExtra = (s, st0, head, wrap, B) => {
    if (st0 !== 'refused') return null;
    return head + wrap('bad', 'shield', 'Certificate changed. Connection refused', '<b>' + esc(s.name) + '</b> presented a different certificate from the one you trusted. Someone may be intercepting the connection, or the server was reinstalled. Transfers to it are paused.', B('reverify', 'Review the new key…', 'f', 'shield-check') + B('diagnose', 'Diagnose', 'tx', 'activity') + B('servers', 'Servers', 'tx', 'server'),
      '<div class="fpdiff"><div><small>Trusted before</small>' + fpBlock(s.oldFp) + '</div><div class="new"><small>Presented now</small>' + fpBlock(s.newFp) + '</div></div>');
  };
  A.reverifyDialog = (id) => {
    const s = E.server(id); if (!s || s.state !== 'refused') return;
    A.dialog({ icon: 'shield', cls: 'danger', title: 'Trust the new key for ' + s.name + '?', width: 560,
      body: '<p class="fxp" style="margin-top:0">Only continue if you know why the key changed (for example you reinstalled the server). Compare the new fingerprint with the one printed by <span class="mono">rfe status</span> on that machine. You sign in again afterwards: the old login belonged to the old key.</p><div class="fpdiff"><div><small>Trusted before</small>' + fpBlock(s.oldFp) + '</div><div class="new"><small>Presented now</small>' + fpBlock(s.newFp) + '</div></div><label class="fxck"><input type="checkbox" id="rvc"> I compared the new fingerprint on the server itself</label>' + fld('rvn', 'Type the server name to confirm', '', s.name),
      enter: 'go', actions: [{ label: 'Keep it blocked', kind: 'tx' }, { id: 'go', label: 'Trust new key', kind: 'ed', icon: 'shield-check', cb: (c) => { const okc = $('#rvc', c.el).checked; const okn = $('#rvn', c.el).value.trim() === s.name; setErr(c, 'rvn', okn ? '' : 'Type “' + s.name + '” exactly'); if (!okc) A.snack('Tick the box to confirm you compared the fingerprints.', { error: true }); if (!okc || !okn) return false; E.reverify(id); audit('Trusted the new certificate of ' + s.name + ' (confirmed by user)'); setTimeout(() => A.signInDialog(E.server(id)), 150); } }] });
  };

  /* ================= diagnostics ================= */
  A.diagnose = (id) => {
    const s = E.server(id); if (!s) { A.snack('Diagnostics need a server. Pick one in the top bar.'); return; }
    const steps = [
      { t: 'Reach ' + s.addr, advice: 'The PC did not answer. Check it is on, on this network, and that TCP ' + s.port + ' is allowed through its firewall.', run: async () => { const t0 = performance.now(); const p = await E.call('probe_agent', { host: s.addr }); X2.probe = p; return 'TLS handshake done in ' + Math.round(performance.now() - t0) + ' ms'; } },
      { t: 'Certificate matches the pinned key', advice: 'The server’s certificate is not the one you trusted. Do not continue until you know why.', run: async () => { const p = X2.probe; if (p.changed) throw new Error('MISMATCH: presented ' + E.util.fmtFp(p.fingerprint).slice(0, 17) + '…'); return s.pinned ? 'SHA-256 ' + s.fp.slice(0, 17) + '…' : 'Not pinned yet. You are asked to trust it when you connect'; } },
      { t: 'Sign in', advice: 'Sign in again from the Servers page.', run: async () => { if (!s.signedIn) throw new Error('No account is signed in on this server'); const r = await E.call('agent_health', { host: id }); return 'Accepted' + (r.health && r.health.version ? ' · agent ' + r.health.version : ''); } },
      { t: 'List allowed roots', advice: 'The agent did not list any folder for this device. The owner sets them in rfe.', run: async () => { const r = await E.call('files_roots', { host: id }); const l = r.locations || []; return l.length + (l.length === 1 ? ' root: ' : ' roots: ') + l.map((x) => x.path).join(', '); } }
    ];
    let res = []; let ctl; let running = 0;
    const row = (k) => { const r = res[k]; const state = r ? (r.ok ? 'ok' : 'bad') : k === res.length && running ? 'run' : res.some((x) => !x.ok) ? 'skip' : 'wait'; return '<div class="dgs ' + state + '"><span class="dot">' + (state === 'ok' ? ic('check') : state === 'bad' ? ic('x') : state === 'run' ? ic('spinner') : state === 'skip' ? '–' : '') + '</span><div><b>' + esc(steps[k].t) + '</b>' + (r ? '<small>' + esc(r.msg) + '</small>' : '') + (r && !r.ok ? '<div class="advice">' + esc(steps[k].advice) + '</div>' : '') + '</div></div>'; };
    const draw = () => { const d = $('.fxdiag', ctl.el); if (!d) return; const bad = res.findIndex((x) => !x.ok); const fin = res.length === steps.length || bad >= 0; d.innerHTML = steps.map((_, k) => row(k)).join('') + (fin && !running ? (bad < 0 ? '<div class="hint ok" style="margin:.75rem 0 0">' + ic('check-circle') + '<span>Everything checks out. ' + esc(s.name) + ' is reachable and the key matches.</span></div>' : '<div class="hint bad" style="margin:.75rem 0 0">' + ic('alert-circle') + '<span><b>Stopped at: ' + esc(steps[bad].t) + '.</b> ' + esc(steps[bad].advice) + '</span></div>') : ''); const rt = $('[data-id="rt"]', ctl.el); if (rt) rt.style.display = bad === 1 && s.state === 'refused' ? '' : 'none'; };
    const reportText = () => 'RFE diagnostics for ' + s.name + ' (' + s.addr + ', ' + s.via + ')\n' + res.map((r, k) => (r.ok ? '✓ ' : '✗ ') + steps[k].t + ': ' + r.msg).join('\n');
    const run = async () => { const my = ++running; res = []; X2.probe = null; draw(); for (let k = 0; k < steps.length; k++) { let r; try { r = { ok: true, msg: await steps[k].run() }; } catch (e) { r = { ok: false, msg: e.message }; } if (my !== running || !ctl.el.isConnected) return; res.push(r); if (!r.ok) break; draw(); } running = 0; draw(); };
    ctl = A.dialog({ icon: 'activity', title: 'Diagnose ' + s.name, width: 540, body: '<div class="fxdiag"></div>', noFocus: true,
      actions: [{ label: 'Close', kind: 'tx' }, { id: 'cpy', label: 'Copy report', kind: 't', icon: 'copy', cb: () => { A.copy(reportText()); return false; } }, { id: 'rt', label: 'Review key…', kind: 'ed', icon: 'shield', cb: () => { setTimeout(() => A.reverifyDialog(id), 0); } }, { id: 'again', label: 'Run again', kind: 'tx', icon: 'refresh', cb: () => { run(); return false; } }], onOpen: (c) => { ctl = c; run(); } });
  };

  /* ================= allowed roots ================= */
  A.rootsDialog = (id) => {
    const s = E.server(id); const roots = s.roots || [];
    A.dialog({ icon: 'drive', title: 'Allowed roots on ' + s.name, width: 520, noFocus: true,
      body: '<p class="fxp" style="margin-top:0">Only these folders are visible to this device. The list is set by the server’s owner in <span class="mono">rfe</span> and can’t be widened from here.</p>' + (roots.map((r, i) => '<div class="lrow"><div class="gi">' + ic(r.isOs ? 'lock' : 'folder') + '</div><div class="dm"><b class="mono" style="font-weight:400">' + esc(r.path) + '</b><small>' + (r.totalBytes ? U.fmtBytes(r.totalBytes - r.freeBytes) + ' of ' + U.fmtBytes(r.totalBytes) + ' used' : esc(r.label || '')) + '</small></div><button class="btn sm"' + (s.state === 'online' ? ' data-fx="root.open|' + i + '"' : ' disabled') + '>Open</button></div>').join('') || '<div class="fxempty"><span>The agent listed no roots.</span></div>'),
      actions: [{ label: 'Close', kind: 'f' }], onOpen: (c) => c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-fx^="root.open"]'); if (b) { c.close(); A.openPath(id, roots[+b.dataset.fx.split('|')[1]].path, false); } }) });
  };
  A.serverMenuExtra = (s) => ['-', { icon: 'activity', label: 'Run diagnostics', onClick: () => A.diagnose(s.id) }, { icon: 'drive', label: 'Allowed roots…', disabled: !(s.roots || []).length, onClick: () => A.rootsDialog(s.id) },
    s.signedIn ? { icon: 'user', label: 'Sign out' + (s.user ? ' (' + s.user + ')' : ''), onClick: () => E.signOut(s.id).then(() => { audit('Signed out of ' + s.name); A.snack('Signed out of ' + s.name); A.renderAll(); }).catch(fail) } : { icon: 'user', label: 'Sign in…', onClick: () => A.signInDialog(s) }];
  const fav0 = A.favItems;
  A.favItems = () => { const h = A.mainPane.host; const extra = h && h !== 'local' && E.server(h) ? ['-', { head: 'This server' }, { icon: 'activity', label: 'Run diagnostics', onClick: () => A.diagnose(h) }, { icon: 'drive', label: 'Allowed roots…', onClick: () => A.rootsDialog(h) }] : []; return fav0().concat(extra); };

  /* ================= onboarding ================= */
  A.onboarding = (step0) => {
    if ($('.onb')) return; let step = step0 || 0; const el = document.createElement('div'); el.className = 'scrim onb'; el.style.pointerEvents = 'auto';
    const finish = (then) => { S.onboarded = true; A.save(); document.removeEventListener('keydown', key, true); el.remove(); if (then) then(); };
    const themeCard = (v, l) => '<button class="thc ' + v + (S.theme === v ? ' on' : '') + '" data-th="' + v + '" aria-pressed="' + (S.theme === v) + '"><span class="thp"><i></i><i></i><i></i></span><b>' + l + '</b></button>';
    const body = () => {
      const dots = '<div class="dots" aria-hidden="true">' + [0, 1, 2].map((i) => '<i class="' + (i === step ? 'on' : '') + '"></i>').join('') + '</div>';
      if (step === 0) return '<div class="ob-l">' + ic('plug', { size: 88 }) + '<h2>RFE</h2><p>Remote File Explorer</p></div><div class="ob-r"><h1>Welcome</h1><p class="lead">Browse and move files between your own computers. Nothing goes through a cloud.</p><ul class="obl"><li>' + ic('shield-check') + '<div><b>Keys you can verify</b><small>Every computer is pinned by its certificate fingerprint.</small></div></li><li>' + ic('qr') + '<div><b>Phones pair with a QR code</b><small>Approve each phone here and choose what it may do.</small></div></li><li>' + ic('swap') + '<div><b>Transfers that survive drops</b><small>They pause when a link fails and resume by themselves.</small></div></li></ul><div class="obf">' + dots + '<span class="sp"></span><button class="btn tx" data-ob="skip">Skip</button><button class="btn f" data-ob="next">Get started</button></div></div>';
      if (step === 1) return '<div class="ob-l">' + ic('palette', { size: 88 }) + '<h2>Make it yours</h2><p>You can change this later in Settings</p></div><div class="ob-r"><h1>Appearance</h1><p class="lead">Pick a theme. “System” follows your desktop.</p><div class="thg">' + themeCard('light', 'Light') + themeCard('dark', 'Dark') + themeCard('system', 'System') + '</div><div class="fld" style="margin-top:1rem"><label>Row density</label><div class="seg" data-set="density">' + [['comfortable', 'Comfortable'], ['compact', 'Compact']].map((o) => '<button data-od="' + o[0] + '" aria-pressed="' + (S.density === o[0]) + '" class="' + (S.density === o[0] ? 'on' : '') + '">' + o[1] + '</button>').join('') + '</div></div><div class="obf">' + dots + '<span class="sp"></span><button class="btn tx" data-ob="back">Back</button><button class="btn f" data-ob="next">Next</button></div></div>';
      return '<div class="ob-l">' + ic('server', { size: 88 }) + '<h2>Add a computer</h2><p>You need at least one to browse</p></div><div class="ob-r"><h1>Connect your first computer</h1><p class="lead">Install <span class="mono">rfe</span> on it, then choose how to find it.</p><button class="opt" data-ob="disc">' + ic('search') + '<div><b>Find on this network</b><small>Computers announcing rfe on your Wi-Fi or cable</small></div>' + ic('chevron-right') + '</button><button class="opt" data-ob="code">' + ic('hash') + '<div><b>Use a pairing code</b><small>Works over Tailscale too. 8 characters from the other computer</small></div>' + ic('chevron-right') + '</button><button class="opt" data-ob="manual">' + ic('edit') + '<div><b>Enter an address</b><small>Server address and port, then compare its key</small></div>' + ic('chevron-right') + '</button><div class="obf">' + dots + '<span class="sp"></span><button class="btn tx" data-ob="back">Back</button><button class="btn tx" data-ob="skip">Later</button></div></div>';
    };
    const draw = () => { el.innerHTML = '<div class="obc" role="dialog" aria-modal="true" aria-label="Welcome to RFE">' + body() + '</div>'; const f = $('[data-ob="next"]', el) || $('[data-ob="skip"]', el); if (f) f.focus(); };
    const key = (e) => { if (e.key === 'Escape') { e.stopPropagation(); finish(); } };
    el.addEventListener('click', (e) => {
      const th = e.target.closest('[data-th]'), od = e.target.closest('[data-od]'), ob = e.target.closest('[data-ob]');
      if (th) { A.setTheme(th.dataset.th); draw(); const b = $('[data-th="' + th.dataset.th + '"]', el); if (b) b.focus(); } else if (od) { S.density = od.dataset.od; A.save(); document.documentElement.dataset.density = S.density; draw(); const b = $('[data-od="' + od.dataset.od + '"]', el); if (b) b.focus(); } else if (ob) {
        const a = ob.dataset.ob; if (a === 'next') { step++; draw(); } else if (a === 'back') { step--; draw(); } else if (a === 'skip') finish(() => A.snack('Welcome! Press Ctrl+K for everything.'));
        else finish(() => (a === 'disc' ? discDialog() : a === 'code' ? codeDialog() : A.serverDialog()));
      }
    });
    document.addEventListener('keydown', key, true); $('#layer').appendChild(el); draw();
  };
  /* The welcome screen shows once, and only while there is nothing saved yet. */
  A.maybeOnboard = () => { if (!S.onboarded && !THUMB && !E.servers.length) setTimeout(() => A.onboarding(), 250); };
  document.addEventListener('DOMContentLoaded', () => {
    const q = new URLSearchParams(location.search);
    const D = { welcome: () => A.onboarding(), welcome2: () => A.onboarding(1), welcome3: () => A.onboarding(2), codeadd: () => codeDialog(), discover: () => discDialog(), accounts: () => A.accountsDialog() };
    if (D[q.get('demo')]) { D[q.get('demo')](); if (THUMB) $$('.snack', $('#layer')).forEach((x) => x.remove()); }
  });
  A.paletteExtra.push((C) => {
    C('search', 'Find computers on the network…', discDialog); C('hash', 'Add with a pairing code…', codeDialog); C('user', 'Accounts…', A.accountsDialog);
    const h = A.mainPane.host; if (h && h !== 'local' && E.server(h)) { C('activity', 'Diagnose ' + A.hostName(h), () => A.diagnose(h)); C('drive', 'Allowed roots on ' + A.hostName(h), () => A.rootsDialog(h)); }
    C('plug', 'Show the welcome screen', () => A.onboarding());
  });
})();
