/* RFE Tonal - part 2: onboarding, adding computers (pairing code, discovery), trust and sign-in, certificate change, diagnostics, roots. */
rfeModule('fx2', ['fpages'], function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, st = A.state, X = A.X, F = A.fx, H = A.fxHandlers;
  const audit = F.audit;
  /* The pairing code: CODE_LEN characters from CODE_SET (the digits 2 to 9 and the letters without I and O, so it reads clearly). CODE_GROUP is where a space goes while it is typed. */
  const CODE_SET = '2-9A-HJ-NP-Z', CODE_LEN = 8, CODE_GROUP = 4;
  const CODE_RE = new RegExp('^[' + CODE_SET + ']{' + CODE_LEN + '}$'), CODE_NOT_SET = new RegExp('[^' + CODE_SET + ']', 'g');
  /* Input handler for a pairing-code field: upper-cases, drops characters outside the set, caps the length and inserts the group space. */
  const formatCodeInput = (e) => { const v = e.target.value.toUpperCase().replace(CODE_NOT_SET, '').slice(0, CODE_LEN); e.target.value = v.length > CODE_GROUP ? v.slice(0, CODE_GROUP) + ' ' + v.slice(CODE_GROUP) : v; };
  const X2 = A.X2 = { auto: {}, code: {}, found: null };
  A.fx2 = {};
  const THUMB = A.QUIET;
  const fld = (id, label, val, ph, o) => { o = o || {}; return '<div class="fld"' + (o.style ? ' style="' + o.style + '"' : '') + '><label for="' + id + '">' + label + '</label><input id="' + id + '" type="' + (o.type || 'text') + '" value="' + esc(val || '') + '" placeholder="' + esc(ph || '') + '" spellcheck="false" autocomplete="off"' + (o.max ? ' maxlength="' + o.max + '"' : '') + (o.mode ? ' inputmode="' + o.mode + '"' : '') + '><span class="err" id="' + id + 'e"></span></div>'; };
  const setErr = (ctl, id, m) => { const e = $('#' + id + 'e', ctl.el); if (e) e.textContent = m || ''; const i = $('#' + id, ctl.el); if (i) i.style.borderColor = m ? 'var(--err)' : ''; return !m; };
  const fail = (e) => A.snack((e && e.message) || String(e), { error: true });

  /* ================= adding a computer ================= */
  /* The three steps of adding a computer (1 find it, 2 check it, 3 sign in), drawn as a numbered row with step n marked current. */
  X2.adding = {}; /* ids of computers being added through the dialog: their later screens carry the step row */
  A.addSteps = (n) => '<ol class="addsteps" aria-label="Adding a computer: step ' + n + ' of 3">' + ['Find', 'Check', 'Sign in'].map((t, i) => '<li' + (i + 1 === n ? ' class="on" aria-current="step"' : i + 1 < n ? ' class="done"' : '') + '><i>' + (i + 1) + '</i><span>' + t + '</span></li>').join('') + '</ol>';
  /* A "Technical details" fold, closed to start with, holding the addresses and key wording that most people never need. */
  /* Also used by the Settings About row and the About dialog (A.techBox). */
  const techBox = A.techBox = (rows, extra) => '<details class="adtech"><summary>Technical details</summary><dl>' + rows.map((r) => '<dt>' + r[0] + '</dt><dd class="mono">' + esc(r[1]) + '</dd>').join('') + '</dl>' + (extra || '') + '</details>';
  /* The open add-a-computer dialog (found by its step row), or null when it was closed. */
  const addDlg = () => { for (const sc of $$('.scrim')) if ($('.addsteps', sc) && sc._ctl) return sc._ctl; return null; };
  /* Shows the next step in the same dialog: the open box is swapped for the one `open` makes. Returns what `open` returned. */
  const nextStep = (open) => { A.dialogReuse = addDlg(); try { return open(); } finally { A.dialogReuse = null; } };
  /* Back from step 2: stop connecting, drop the half-added computer, and show step 1 again with what was typed. */
  /* Cancel while a computer is still being added (never saved): stop connecting and drop it, so no half-added entry stays in the list. */
  const dropHalfAdded = (s) => { E.cancelConnect(s.id); delete X2.auto[s.id]; delete X2.adding[s.id]; delete X2.code[s.id]; if (!s.saved) { if (A.mainPane.host === s.id) A.mainPane.setHost('local'); E.forget(s.id).then(() => A.renderAll()).catch(A.noop); } };
  const backToFind = (c, s) => {
    E.cancelConnect(s.id); delete X2.auto[s.id]; delete X2.adding[s.id]; delete X2.code[s.id];
    const pre = { name: s.name, host: s.host, port: s.port };
    if (!s.saved) { if (A.mainPane.host === s.id) A.mainPane.setHost('local'); E.forget(s.id).then(() => A.renderAll()).catch(A.noop); }
    A.dialogReuse = c; A.serverDialog(undefined, pre); A.dialogReuse = null;
  };
  /* Step 2 while the computer answers: a short wait screen. The state listener below replaces it with the trust question, the sign-in step or an error. */
  function checkDialog(s) {
    return A.dialog({ icon: 'shield', cls: 'conn', title: 'Check ' + s.name, width: 520, modal: true,
      body: A.addSteps(2) + '<div class="fxwait" data-chk><span class="spin">' + ic('spinner') + '</span><b>Checking ' + esc(s.name) + '…</b><small>Making sure it is the right computer.</small></div>' + techBox([['Address', s.addr]]),
      actions: [{ label: 'Back', kind: 'tx', cb: (c) => { backToFind(c, s); return false; } }, { label: 'Cancel', kind: 'tx', cb: () => { dropHalfAdded(s); } }], noFocus: true });
  }
  A.connTabs = (cur) => A.addSteps(1) + '<div class="seg fxtabs" role="tablist" aria-label="How to add a computer">' + [['manual', 'Manual'], ['code', 'Pairing code'], ['disc', 'Find on network']].map((t) => '<button role="tab" aria-selected="' + (cur === t[0]) + '" data-fx="conn.tab|' + t[0] + '" class="' + (cur === t[0] ? 'on' : '') + '">' + t[1] + '</button>').join('') + '</div>';
  H['conn.tab'] = (tab, a2, b) => { if (b.classList.contains('on')) return; const sc = b.closest('.scrim'); A.dialogReuse = sc && sc._ctl ? sc._ctl : null; (tab === 'manual' ? A.serverDialog() : tab === 'code' ? codeDialog() : discDialog()); A.dialogReuse = null; };
  /* The trust and sign-in dialogs follow a connection the user started here, one step after the other. */
  A.watchConnect = (id) => { X2.auto[id] = true; };
  /* Adds the computer and starts connecting. With `ctl` (the step 1 dialog) that dialog turns into step 2 and the flow continues in it. */
  const addAndOpen = (o, code, ctl) => {
    const s = E.addServer(o); if (code) X2.code[s.id] = code; else delete X2.code[s.id]; /* a code belongs to this attempt only */
    A.watchConnect(s.id); A.setMainHost(s.id); A.go('files');
    if (ctl) {
      if (s.state === 'online') { delete X2.code[s.id]; ctl.close(); A.snack(s.name + ' is already connected'); A.renderAll(); return s; }
      X2.adding[s.id] = true; A.dialogReuse = ctl;
      if (s.state === 'trust') A.trustDialog(s); else checkDialog(s);
      A.dialogReuse = null;
    }
    E.connect(s.id); A.renderAll(); return s;
  };
  A.addAndOpen = addAndOpen;
  const lastState = {};
  /* Follows a computer being added: the trust question when its key is new (the person decides), step 3 straight away when the key is already trusted, an error on step 2 when it cannot be reached. */
  E.on((t, d) => {
    if (t !== 'servers' || !d || !d.id) return; const was = lastState[d.id]; lastState[d.id] = d.state; if (was === d.state || !X2.auto[d.id]) return;
    const adding = !!X2.adding[d.id]; const show = (f) => { if (adding) nextStep(f); else setTimeout(f, A.T.settle); };
    if (d.state === 'trust') show(() => A.trustDialog(d));
    else if (d.state === 'login') { delete X2.auto[d.id]; const code = X2.code[d.id]; delete X2.code[d.id]; if (code) pairNow(d, code); else show(() => A.signInDialog(d)); }
    else if (d.state === 'refused' && adding) { delete X2.auto[d.id]; delete X2.adding[d.id]; delete X2.code[d.id]; nextStep(() => A.reverifyDialog(d.id)); }
    else if (d.state === 'offline' && adding) { delete X2.auto[d.id]; delete X2.code[d.id]; const c = addDlg(); const w = c && $('[data-chk]', c.el); if (w) { w.className = 'hint bad'; w.removeAttribute('data-chk'); w.innerHTML = ic('alert-circle') + '<span>' + esc(d.error || 'Could not reach ' + d.name + '.') + '</span>'; } }
    else if (d.state === 'offline' || d.state === 'refused' || d.state === 'disconnected' || d.state === 'online') { delete X2.auto[d.id]; delete X2.adding[d.id]; delete X2.code[d.id]; }
  });
  /* Pairs with a code typed in step 1. In the add flow the dialog stays open as step 3 while it works, and falls back to the sign-in screen with the error if it fails. */
  async function pairNow(s, code) {
    const pc = X2.adding[s.id] ? nextStep(() => A.dialog({ icon: 'user', cls: 'conn', title: 'Sign in to ' + s.name, width: 520, modal: true, body: A.addSteps(3) + '<div class="fxwait"><span class="spin">' + ic('spinner') + '</span><b>Pairing with ' + esc(s.name) + '…</b></div>', actions: [], noFocus: true })) : null;
    A.snack('Pairing with ' + s.name + '…');
    try { await E.signInWithCode(s.id, code); audit('Paired with ' + s.name + ' using a code'); delete X2.adding[s.id]; if (pc) pc.close(); A.snack('Paired with ' + s.name); A.renderAll(); }
    catch (e) { A.dialogReuse = pc && pc.sc.isConnected ? pc : null; A.signInDialog(s, { mode: 'code', error: e.message }); A.dialogReuse = null; }
  }
  function codeDialog() {
    A.dialog({
      icon: 'plug', cls: 'conn', title: 'Add a computer', width: 520,
      body: A.connTabs('code') + '<p class="fxp">On the other computer open <b>Devices → Pair a phone</b> in RFE, or run <span class="mono">rfe pair</span>. Enter the address it shows and the 8-character code.</p>' + fld('pcn', 'Name', '', 'e.g. render-farm') + fld('pch', 'Address', '', '100.64.0.12:8765 or host.example.com', {}) + fld('pcc', 'Pairing code', '', 'ABCD 2345', { max: 9 }) + '<div class="hint" style="margin:0">' + ic('shield-check') + '<span>The computer is checked before the code is sent. The code works once and expires after a few minutes.</span></div>',
      enter: 'go', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'go', label: 'Continue', kind: 'f', icon: 'plug', cb: (c) => {
        const g = (i) => $('#' + i, c.el).value.trim(); const name = g('pcn'), host = g('pch'), code = g('pcc').replace(/\s/g, '').toUpperCase(); let ok = true;
        ok = setErr(c, 'pcn', !name ? 'Give it a name' : E.servers.some((s) => s.name.toLowerCase() === name.toLowerCase()) ? 'You already have “' + name + '”' : '') && ok;
        const pa = E.util.parseAddr(host); ok = setErr(c, 'pch', pa.error || '') && ok;
        ok = setErr(c, 'pcc', !CODE_RE.test(code.toUpperCase()) ? 'The code is 8 letters and digits' : '') && ok; if (!ok) return false;
        addAndOpen({ name, host: pa.addr }, code, c); return false;
      } }],
      onOpen: (c) => { $('#pcc', c.el).addEventListener('input', formatCodeInput); $('#pcn', c.el).focus(); }
    });
  }
  function discDialog() {
    let found = null, err = '';
    const body = (c) => {
      const el = $('.fxdisc', c.el); if (!el) return;
      if (found === null) { el.innerHTML = '<div class="fxwait"><span class="spin">' + ic('spinner') + '</span><b>Looking for computers…</b><small>On this network</small></div>'; return; }
      if (err) { el.innerHTML = '<div class="hint bad">' + ic('alert-circle') + '<span>' + esc(err) + '</span></div>'; return; }
      el.innerHTML = found.length ? '<div class="fxsub" style="margin-bottom:.375rem">Found ' + found.length + (found.length === 1 ? ' computer' : ' computers') + ' with RFE on this network.</div>' + found.map((f, i) => { const saved = E.servers.some((s) => s.addr === f.hostport); return '<div class="lrow"><div class="gi">' + ic('server') + '</div><div class="dm"><b>' + esc(f.name) + '</b>' + (saved ? '<small>Already added</small>' : '') + '</div>' + (saved ? '<span class="tag">Saved</span>' : '<button class="btn sm f" data-fx="disc.add|' + i + '">Add</button>') + '</div>'; }).join('') + techBox(found.map((f) => [f.name, f.hostport + (f.version ? ' · version ' + f.version : '')])) : empty('search', 'Nothing found', 'No computer with RFE answered. It must be on the same Wi-Fi or cable network. For Tailscale machines use a pairing code or the address.');
    };
    let scanId = 0;
    const scan = (c) => {
      const mine = ++scanId; found = null; err = ''; body(c);
      const btn = c.btn && c.btn.scan; if (btn) btn.disabled = true;
      /* An older scan that answers late must not overwrite a newer one; no answer for 10 s is reported. */
      const slow = new Promise((_, no) => setTimeout(() => no(new Error('No answer after ' + A.T.long / 1000 + ' seconds. Check that the computer is on this network, or use a pairing code or an address.')), A.T.long));
      Promise.race([E.call('discover_agents'), slow]).then((l) => { if (mine === scanId) found = l; }).catch((e) => { if (mine === scanId) { found = []; err = e.message; } }).then(() => { if (mine !== scanId) return; if (btn) btn.disabled = false; if (c.el.isConnected) body(c); });
    };
    A.dialog({ icon: 'plug', cls: 'conn', title: 'Add a computer', width: 520, body: A.connTabs('disc') + '<div class="fxdisc"></div><p class="fxp" style="margin-top:.75rem">Only computers on the same Wi-Fi or cable network appear. Listing a computer does not trust it. You check it in the next step.</p>', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'scan', label: 'Scan again', kind: 't', icon: 'refresh', cb: (c) => { scan(c); return false; } }], noFocus: true,
      onOpen: (c) => { scan(c); c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-fx^="disc.add"]'); if (!b) return; const f = found[+b.dataset.fx.split('|')[1]]; addAndOpen({ name: f.name, host: f.hostport }, undefined, c); }); } });
  }
  const empty = (icn, t, s) => '<div class="fxempty">' + ic(icn, { size: 40 }) + '<b>' + t + '</b><span>' + s + '</span></div>';
  A.fx2.codeDialog = codeDialog;

  /* ================= trust and sign-in ================= */
  const fpBlock = (fp) => { const b = String(fp || '').split(':'); const g = []; for (let i = 0; i < b.length; i += 8) g.push('<span class="fpg">' + esc(b.slice(i, i + 8).join(':') + (i + 8 < b.length ? ':' : '')) + '</span>'); return '<code class="fpfull">' + g.join('') + '</code>'; };
  A.trustDialog = (s) => {
    /* Part of adding a computer: step 2 of 3, in the same box, with a Back button. The key itself stays in view because comparing it is the decision; addresses and the key's type are folded away. */
    const adding = !!X2.adding[s.id];
    A.dialog({ icon: 'shield', cls: adding ? 'conn' : '', title: 'Trust ' + s.name + '?', width: adding ? 520 : 540, modal: true, enter: 'go',
      body: (adding ? A.addSteps(2) : '') + '<p class="fxp" style="margin-top:0">This is the first time RFE connects to <b>' + esc(s.name) + '</b>. Check that the key below is the one that computer shows, so you know it is really that computer.</p><div class="fpdiff one"><div class="new"><small>Key shown by the computer</small>' + fpBlock(s.fp) + '</div></div><p class="fxp">On that computer run <span class="mono">rfe status</span> and compare. If they differ, someone may be intercepting the connection. Nothing is sent to the computer until you trust it.</p><label class="fxck"><input type="checkbox" id="tfm"> I compared this key with the one on that computer and they match</label>' + techBox([['Address', s.addr], ['Key type', 'SHA-256 fingerprint of the computer’s certificate']]),
      actions: (adding ? [{ label: 'Back', kind: 'tx', cb: (c) => { backToFind(c, s); return false; } }] : []).concat([{ label: 'Cancel', kind: 'tx', cb: () => { if (adding) dropHalfAdded(s); else { E.cancelConnect(s.id); delete X2.auto[s.id]; delete X2.adding[s.id]; } } }, { id: 'go', label: 'They match: trust', kind: 'f', icon: 'shield-check', cb: (c) => { const box = document.getElementById('tfm'); if (!box || !box.checked) { const d = document.querySelector('.dlg .db'); let er = d && d.querySelector('#tfe'); if (d && !er) { er = document.createElement('div'); er.id = 'tfe'; er.className = 'hint bad'; er.setAttribute('role', 'alert'); d.appendChild(er); } if (er) er.textContent = 'Tick the box to confirm you compared the fingerprints.'; A.snack('Tick the box to confirm you compared the fingerprints.', { error: true }); return false; } X2.auto[s.id] = true; E.trust(s.id); if (adding) { if (addDlg() === c) c.close(); return false; } } }]) });
  };
  A.signInDialog = (s, o) => {
    o = o || {}; let mode = o.mode || 'in'; let timer = null; let wait = null; let busy = false; let closed = false; let msg = o.error || ''; let typedUser = null;
    const stop = () => { if (timer) { clearInterval(timer); timer = null; } if (wait) { wait = null; E.cancelApproval().catch(A.noop); } };
    const done = (c, how) => { stop(); A.snack('Signed in to ' + s.name + (s.user ? ' as ' + s.user : '')); c.close(); if (A.mainPane.host == null || A.mainPane.host === 'local') A.setMainHost(s.id); A.renderAll(); };
    const draw = (c) => {
      const tabs = '<div class="seg fxtabs" role="tablist" aria-label="Sign-in method">' + [['in', 'Account'], ['code', 'Pairing code'], ['ask', 'Ask the PC']].map((t) => '<button role="tab" aria-selected="' + (mode === t[0]) + '" data-m="' + t[0] + '" class="' + (mode === t[0] ? 'on' : '') + '">' + t[1] + '</button>').join('') + '</div>';
      const err = msg ? '<div class="hint bad" style="margin:.625rem 0 0">' + ic('alert-circle') + '<span>' + esc(msg) + '</span></div>' : '';
      let b;
      if (mode === 'in') b = '<p class="fxp">Sign in to <b>' + esc(s.name) + '</b> with the account made on that computer (<span class="mono">rfe adduser</span>).</p>' + fld('siu', 'Username', typedUser != null ? typedUser : s.user || '', '') + fld('sip', 'Password', '', '', { type: 'password' }) + '<div class="fxsub">The password goes only to the computer you just checked.</div>';
      else if (mode === 'code') b = '<p class="fxp">Enter the 8-character pairing code from <b>Devices → Pair a phone</b> on ' + esc(s.name) + ', or from <span class="mono">rfe pair</span>.</p>' + fld('sic', 'Pairing code', o.code || '', 'ABCD 2345', { max: 9 });
      else if (wait) b = '<div class="fxwait"><span class="spin">' + ic('spinner') + '</span><b>Waiting for approval…</b><small>On ' + esc(s.name) + ' open <b>Devices</b> and approve this computer. It must show this code:</small></div><div class="pcode" style="margin:.625rem 0"><span>' + esc(wait.matchCode) + '</span></div><div><button type="button" class="btn sm tx" data-pcopy="1">' + ic('copy') + 'Copy code</button></div>';
      else b = '<p class="fxp">No password needed: ' + esc(s.name) + ' shows a request, and someone at that computer approves it. A new device starts with browse access only.</p>';
      $('.db', c.el).innerHTML = (X2.adding[s.id] ? A.addSteps(3) : '') + tabs + b + err;
      const go = $('[data-id="go"]', c.el); if (go) { go.style.display = mode === 'ask' && wait ? 'none' : ''; go.lastChild && (go.lastChild.textContent = mode === 'ask' ? 'Ask for approval' : mode === 'code' ? 'Pair' : 'Sign in'); go.disabled = busy; }
      const ci = $('#sic', c.el); if (ci) ci.addEventListener('input', formatCodeInput);
    };
    /* Step 3 of adding a computer when it was added through the dialog (its step row is drawn in the body); the same screen is a plain sign-in otherwise. */
    A.dialog({ icon: 'user', cls: X2.adding[s.id] ? 'conn' : '', title: 'Sign in to ' + s.name, width: X2.adding[s.id] ? 520 : 480, body: '', enter: 'go', modal: true, onClose: () => { closed = true; stop(); delete X2.adding[s.id]; },
      actions: [{ label: 'Not now', kind: 'tx', cb: () => { stop(); } },
        { id: 'go', label: 'Sign in', kind: 'f', icon: 'user', cb: (c) => {
          msg = ''; busy = true;
          const finish = (p) => p.then(() => done(c, mode === 'code' ? 'pairing code' : 'password')).catch((e) => { msg = e.message; busy = false; if (c.el.isConnected) draw(c); });
          if (mode === 'in') { const u = $('#siu', c.el).value.trim(), p = $('#sip', c.el).value; typedUser = u; let ok = setErr(c, 'siu', u ? '' : 'Enter your username'); ok = setErr(c, 'sip', p ? '' : 'Enter your password') && ok; if (!ok) { busy = false; return false; } finish(E.signIn(s.id, u, p)); }
          else if (mode === 'code') { const code = $('#sic', c.el).value.replace(/\s/g, '').toUpperCase(); if (!setErr(c, 'sic', CODE_RE.test(code.toUpperCase()) ? '' : 'The code is 8 letters and digits')) { busy = false; return false; } finish(E.signInWithCode(s.id, code)); }
          else {
            E.requestApproval(s.id).then((w) => {
              if (closed) { E.cancelApproval().catch(A.noop); return; } /* the dialog was closed while the request was on its way: withdraw it, poll nothing */
              wait = w; busy = false; let polls = 0; draw(c);
              timer = setInterval(() => { E.pollApproval(s.id).then((r) => {
                if (!wait) return; if (r.status === 'approved') { wait = null; done(c, 'approved on the PC'); } else if (r.status === 'rejected' || r.status === 'expired') { clearInterval(timer); timer = null; wait = null; msg = r.status === 'rejected' ? 'The request was rejected on the PC.' : 'The request expired or was already used. Ask again.'; draw(c); }
                else if (++polls > 150) { clearInterval(timer); timer = null; wait = null; E.cancelApproval().catch(A.noop); msg = 'The request timed out. Ask again.'; draw(c); }
              }).catch((e) => { clearInterval(timer); timer = null; wait = null; msg = e.message; draw(c); }); }, A.T.poll);
            }).catch((e) => { msg = e.message; busy = false; draw(c); });
          }
          draw(c); return false;
        } }],
      onOpen: (c) => { draw(c); c.el.addEventListener('click', (e) => { const cp = e.target.closest('[data-pcopy]'); if (cp && wait) { A.copy(wait.matchCode, 'Code copied'); return; } const m = e.target.closest('[data-m]'); if (m && !busy) { stop(); mode = m.dataset.m; msg = ''; draw(c); const nf = mode === 'ask' ? $('[data-id="go"]', c.el) : $(mode === 'code' ? '#sic' : '#siu', c.el); if (nf) nf.focus(); } }); const i = $(mode === 'code' ? '#sic' : '#siu', c.el); if (i) i.focus(); } });
  };
  A.accountsDialog = () => {
    const draw = (c) => { $('.db', c.el).innerHTML = E.servers.map((s) => '<div class="kv2"><div><b>' + esc(s.name) + '</b><small>' + (s.signedIn ? 'Signed in as <b>' + esc(s.user || 'this device') + '</b>' : 'Not signed in') + '</small></div>' + (s.signedIn ? '<button class="btn sm" data-so="' + esc(s.id) + '">Sign out</button>' : '<button class="btn sm f" data-si="' + esc(s.id) + '">Sign in…</button>') + '</div>').join('') || '<div class="fxempty"><span>No computers yet.</span></div>'; };
    A.dialog({ icon: 'user', title: 'Accounts', width: 480, body: '', actions: [{ label: 'Done', kind: 'f' }], noFocus: true, onOpen: (c) => { draw(c); c.el.addEventListener('click', (e) => { const so = e.target.closest('[data-so]'), si = e.target.closest('[data-si]'); if (so) { const s = E.server(so.dataset.so); E.signOut(s.id).then(() => { audit('Signed out of ' + s.name); A.snack('Signed out of ' + s.name); draw(c); A.renderAll(); }).catch(fail); } else if (si) { const s = E.server(si.dataset.si); c.close(); if (s.state === 'trust') A.trustDialog(s); else { if (!s.pinned) { X2.auto[s.id] = true; E.connect(s.id); } else A.signInDialog(s); } } }); } });
  };

  /* ================= certificate change ================= */
  A.stateExtra = (s, st0, head, wrap, B) => {
    if (st0 !== 'refused') return null;
    return head + wrap('bad', 'shield', 'Certificate changed. Connection refused', '<b>' + esc(s.name) + '</b> presented a different certificate from the one you trusted. Someone may be intercepting the connection, or the computer was reinstalled. Transfers to it are paused.', B('reverify', 'Review the new key…', 'f', 'shield-check') + B('diagnose', 'Diagnose', 'tx', 'activity') + B('servers', 'Computers', 'tx', 'server'),
      '<div class="fpdiff"><div><small>Trusted before</small>' + fpBlock(s.oldFp) + '</div><div class="new"><small>Presented now</small>' + fpBlock(s.newFp) + '</div></div>');
  };
  A.reverifyDialog = (id) => {
    const s = E.server(id); if (!s || s.state !== 'refused') return;
    A.dialog({ icon: 'shield', cls: 'danger', title: 'Trust the new key for ' + s.name + '?', width: 560,
      body: '<p class="fxp" style="margin-top:0">Only continue if you know why the key changed (for example you reinstalled the computer). Compare the new fingerprint with the one printed by <span class="mono">rfe status</span> on that machine. You sign in again afterwards: the old login belonged to the old key.</p><div class="fpdiff"><div><small>Trusted before</small>' + fpBlock(s.oldFp) + '</div><div class="new"><small>Presented now</small>' + fpBlock(s.newFp) + '</div></div><label class="fxck"><input type="checkbox" id="rvc"> I compared the new fingerprint on the computer itself</label>' + fld('rvn', 'Type the computer name to confirm', '', s.name),
      enter: 'go', actions: [{ label: 'Keep it blocked', kind: 'tx' }, { id: 'go', label: 'Reconnect', kind: 'ed', icon: 'shield-check', cb: (c) => { const okc = $('#rvc', c.el).checked; const okn = $('#rvn', c.el).value.trim() === s.name; setErr(c, 'rvn', okn ? '' : 'Type “' + s.name + '” exactly'); if (!okc) A.snack('Tick the box to confirm you compared the fingerprints.', { error: true }); if (!okc || !okn) return false; E.reverify(id); audit('Trusted the new certificate of ' + s.name + ' (confirmed by user)'); setTimeout(() => A.signInDialog(E.server(id)), 150); } }] });
  };

  /* ================= diagnostics ================= */
  A.diagnose = (id) => {
    const s = E.server(id); if (!s) { A.snack('Diagnostics need a computer. Pick one in the top bar.'); return; }
    const steps = [
      { t: 'Reach ' + s.addr, advice: 'The PC did not answer. Check it is on, on this network, and that TCP ' + s.port + ' is allowed through its firewall.', run: async () => { const t0 = performance.now(); const p = await E.call('probe_agent', { host: s.addr }); X2.probe = p; return 'TLS handshake done in ' + Math.round(performance.now() - t0) + ' ms'; } },
      { t: 'Certificate matches the pinned key', advice: 'The computer’s certificate is not the one you trusted. Do not continue until you know why.', run: async () => { const p = X2.probe; if (p.changed) throw new Error('MISMATCH: presented ' + E.util.fmtFp(p.fingerprint).slice(0, 17) + '…'); return s.pinned ? 'SHA-256 ' + s.fp.slice(0, 17) + '…' : 'Not pinned yet. You are asked to trust it when you connect'; } },
      { t: 'Sign in', advice: 'Sign in again from the Computers page.', run: async () => { if (!s.signedIn) throw new Error('No account is signed in on this computer'); const r = await E.call('agent_health', { host: id }); return 'Accepted' + (r.health && r.health.version ? ' · agent ' + r.health.version : ''); } },
      { t: 'List allowed roots', advice: 'The agent did not list any folder for this device. The owner sets them in rfe.', run: async () => { const r = await E.call('files_roots', { host: id }); const l = r.locations || []; return l.length + (l.length === 1 ? ' root: ' : ' roots: ') + l.map((x) => x.path).join(', '); } }
    ];
    let res = []; let ctl; let running = 0;
    const row = (k) => { const r = res[k]; const state = r ? (r.ok ? 'ok' : 'bad') : k === res.length && running ? 'run' : res.some((x) => !x.ok) ? 'skip' : 'wait'; return '<div class="dgs ' + state + '"><span class="dot">' + (state === 'ok' ? ic('check') : state === 'bad' ? ic('x') : state === 'run' ? ic('spinner') : state === 'skip' ? '–' : '') + '</span><div><b>' + esc(steps[k].t) + '</b>' + (r ? '<small>' + esc(r.msg) + '</small>' : '') + (r && !r.ok ? '<div class="advice">' + esc(steps[k].advice) + '</div>' : '') + '</div></div>'; };
    const draw = () => { const d = $('.fxdiag', ctl.el); if (!d) return; const bad = res.findIndex((x) => !x.ok); const fin = res.length === steps.length || bad >= 0; d.innerHTML = steps.map((_, k) => row(k)).join('') + (fin && !running ? (bad < 0 ? '<div class="hint ok" style="margin:.75rem 0 0">' + ic('check-circle') + '<span>Everything checks out. ' + esc(s.name) + ' is reachable and the key matches.</span></div>' : '<div class="hint bad" style="margin:.75rem 0 0">' + ic('alert-circle') + '<span><b>Stopped at: ' + esc(steps[bad].t) + '.</b> ' + esc(steps[bad].advice) + '</span></div>') : ''); const rt = $('[data-id="rt"]', ctl.el); if (rt) rt.style.display = bad === 1 && s.state === 'refused' ? '' : 'none'; };
    const reportText = () => 'RFE diagnostics for ' + s.name + ' (' + s.addr + ', ' + s.via + ')\n' + res.map((r, k) => (r.ok ? '✓ ' : '✗ ') + steps[k].t + ': ' + r.msg).join('\n');
    const run = async () => { const my = ++running; res = []; X2.probe = null; draw(); for (let k = 0; k < steps.length; k++) { let r; try { r = { ok: true, msg: await steps[k].run() }; } catch (e) { r = { ok: false, msg: e.message }; } if (my !== running || !ctl.el.isConnected) return; res.push(r); if (!r.ok) break; draw(); } running = 0; draw(); };
    ctl = A.dialog({ icon: 'activity', title: 'Diagnose ' + s.name, width: 540, body: '<div class="fxdiag"></div>', noFocus: true,
      actions: [{ label: 'Close', kind: 'tx' }, { id: 'cpy', label: 'Copy report', kind: 't', icon: 'copy', cb: () => { A.copy(reportText()); return false; } }, { id: 'rt', label: 'Review key…', kind: 'ed', icon: 'shield', cb: () => { A.defer(() => A.reverifyDialog(id)); } }, { id: 'again', label: 'Run again', kind: 'tx', icon: 'refresh', cb: () => { run(); return false; } }], onOpen: (c) => { ctl = c; run(); } });
  };

  /* ================= allowed roots ================= */
  A.rootsDialog = (id) => {
    const s = E.server(id); const roots = s.roots || [];
    A.dialog({ icon: 'drive', title: 'Allowed roots on ' + s.name, width: 520, noFocus: true,
      body: '<p class="fxp" style="margin-top:0">Only these folders are visible to this device. The list is set by the computer’s owner in <span class="mono">rfe</span> and can’t be widened from here.</p>' + (roots.map((r, i) => '<div class="lrow"><div class="gi">' + ic(r.isOs ? 'lock' : 'folder') + '</div><div class="dm"><b class="mono" style="font-weight:400">' + esc(r.path) + '</b><small>' + (r.totalBytes ? U.fmtBytes(r.totalBytes - r.freeBytes) + ' of ' + U.fmtBytes(r.totalBytes) + ' used' : esc(r.label || '')) + '</small></div><button class="btn sm"' + (s.state === 'online' ? ' data-fx="root.open|' + i + '"' : ' disabled') + '>Open</button></div>').join('') || '<div class="fxempty"><span>The agent listed no roots.</span></div>'),
      actions: [{ label: 'Close', kind: 'f' }], onOpen: (c) => c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-fx^="root.open"]'); if (b) { c.close(); A.openPath(id, roots[+b.dataset.fx.split('|')[1]].path, false); } }) });
  };
  A.serverMenuExtra = (s) => ['-', { icon: 'activity', label: 'Run diagnostics', onClick: () => A.diagnose(s.id) }, { icon: 'drive', label: 'Allowed roots…', disabled: !(s.roots || []).length, onClick: () => A.rootsDialog(s.id) },
    s.signedIn ? { icon: 'user', label: 'Sign out' + (s.user ? ' (' + s.user + ')' : ''), onClick: () => E.signOut(s.id).then(() => { audit('Signed out of ' + s.name); A.snack('Signed out of ' + s.name); A.renderAll(); }).catch(fail) } : { icon: 'user', label: 'Sign in…', onClick: () => A.signInDialog(s) }];
  const fav0 = A.favItems;
  A.favItems = () => { const h = A.mainPane.host; const extra = h && h !== 'local' && E.server(h) ? ['-', { head: 'This computer' }, { icon: 'activity', label: 'Run diagnostics', onClick: () => A.diagnose(h) }, { icon: 'drive', label: 'Allowed roots…', onClick: () => A.rootsDialog(h) }] : []; return fav0().concat(extra); };

  /* ================= onboarding ================= */
  /* Shows the three-step welcome tour (optionally from a later step) over the page; Escape or Skip closes it and returns focus to the control that opened it. */
  A.onboarding = (step0) => {
    if ($('.onb')) return; let step = step0 || 0; const el = document.createElement('div'); el.className = 'scrim onb'; el.style.pointerEvents = 'auto';
    /* The control that opened the tour gets the focus back when it closes (found again by its data-sx if the page was redrawn); a follow-up dialog then takes it from there. */
    const opener = document.activeElement; const again = opener && opener.dataset && opener.dataset.sx ? '[data-sx="' + opener.dataset.sx + '"]' : '';
    const finish = (then) => { S.onboarded = true; A.save(); document.removeEventListener('keydown', key, true); el.remove(); const back = opener && opener.isConnected ? opener : again ? $(again) : null; if (back && back !== document.body && back.focus) back.focus(); if (then) then(); };
    const themeCard = (v, l) => '<button class="thc ' + v + (S.theme === v ? ' on' : '') + '" data-th="' + v + '" aria-pressed="' + (S.theme === v) + '"><span class="thp"><i></i><i></i><i></i></span><b>' + l + '</b></button>';
    const body = () => {
      const dots = '<div class="dots" aria-hidden="true">' + [0, 1, 2].map((i) => '<i class="' + (i === step ? 'on' : '') + '"></i>').join('') + '</div>';
      if (step === 0) return '<div class="ob-l">' + ic('plug', { size: 88 }) + '<h2>RFE</h2><p>Remote File Explorer</p></div><div class="ob-r"><h1>Welcome</h1><p class="lead">Browse and move files between your own computers. Nothing goes through a cloud.</p><ul class="obl"><li>' + ic('shield-check') + '<div><b>Keys you can verify</b><small>Every computer is pinned by its certificate fingerprint.</small></div></li><li>' + ic('qr') + '<div><b>Phones pair with a QR code</b><small>Approve each phone here and choose what it may do.</small></div></li><li>' + ic('swap') + '<div><b>Transfers that survive drops</b><small>They pause when a link fails and resume by themselves.</small></div></li></ul><div class="obf">' + dots + '<span class="sp"></span><button class="btn tx" data-ob="skip">Skip</button><button class="btn f" data-ob="next">Get started</button></div></div>';
      if (step === 1) return '<div class="ob-l">' + ic('palette', { size: 88 }) + '<h2>Make it yours</h2><p>You can change this later in Settings</p></div><div class="ob-r"><h1>Appearance</h1><p class="lead">Pick a theme. “System” follows your desktop.</p><div class="thg">' + themeCard('light', 'Light') + themeCard('dark', 'Dark') + themeCard('system', 'System') + '</div><div class="fld" style="margin-top:1rem"><label>Row density</label><div class="seg" data-set="density">' + [['comfortable', 'Comfortable'], ['compact', 'Compact']].map((o) => '<button data-od="' + o[0] + '" aria-pressed="' + (S.density === o[0]) + '" class="' + (S.density === o[0] ? 'on' : '') + '">' + o[1] + '</button>').join('') + '</div></div><div class="obf">' + dots + '<span class="sp"></span><button class="btn tx" data-ob="back">Back</button><button class="btn f" data-ob="next">Next</button></div></div>';
      return '<div class="ob-l">' + ic('server', { size: 88 }) + '<h2>Add a computer</h2><p>You need at least one to browse</p></div><div class="ob-r"><h1>Connect your first computer</h1><p class="lead">Install <span class="mono">rfe</span> on it, then choose how to find it.</p><button class="opt" data-ob="disc">' + ic('search') + '<div><b>Find on this network</b><small>Computers announcing rfe on your Wi-Fi or cable</small></div>' + ic('chevron-right') + '</button><button class="opt" data-ob="code">' + ic('hash') + '<div><b>Use a pairing code</b><small>Works over Tailscale too. 8 characters from the other computer</small></div>' + ic('chevron-right') + '</button><button class="opt" data-ob="manual">' + ic('edit') + '<div><b>Enter an address</b><small>Computer address and port, then compare its key</small></div>' + ic('chevron-right') + '</button><div class="obf">' + dots + '<span class="sp"></span><button class="btn tx" data-ob="back">Back</button><button class="btn tx" data-ob="skip">Later</button></div></div>';
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
  A.maybeOnboard = () => { if (!S.onboarded && !THUMB && !E.servers.length) setTimeout(() => A.onboarding(), A.T.onboard); };
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
});
