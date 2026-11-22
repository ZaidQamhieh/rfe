/* RFE v5 - the Computers page, the "Add a computer" first step and the "PC changed" dialog (the v5 mockup's markup),
   drawn over the existing logic: the engine's computers (E.servers), E.connect/E.retryNow, A.diagnose, A.rootsDialog,
   the trust question (A.trustDialog: the key check stays, nothing here trusts or pins a key), E.forget.
   Everything technical (address, port, key, raw errors) sits in a closed "Technical details" fold.
   It is used only while the window wears the v5 wrapper (class "d5" on #app); otherwise the old pages stay. */
rfeModule('v5-computers', ['fx6'], function () {
  'use strict';
  const A = window.A, E = A.E, $ = A.$, esc = A.esc, ic = A.ic;
  const T = (A.v5Computers = { pick: null });

  T.on = A.v5on; /* true while the v5 wrapper is on the window (v5-shell.js) */
  const em = A.v5em; /* a mockup colour picture, n pixels wide (v5-shell.js) */
  const pic = (s, n) => em(s && /^(Windows|macOS|Darwin)/i.test(s.os || '') ? 'laptop' : 'desktop_computer', n);
  const dot = (k) => '<i class="k-dot ' + k + '"></i>';
  /* A mockup button; `a` is the data-pa action the page handles. */
  const btn = (label, o) => { o = o || {}; return '<button type="button" class="k-btn' + (o.k ? ' ' + o.k : '') + '"' + (o.a ? ' data-pa="' + o.a + '"' : '') + (o.id ? ' data-id="' + esc(o.id) + '"' : '') + (o.dis ? ' disabled' : '') + '>' + (o.i ? ic(o.i, { size: 16 }) : '') + (label ? '<span>' + esc(label) + '</span>' : '') + '</button>'; };
  const gb = (n) => (n >= 1000 ? +(n / 1000).toFixed(1) + ' TB' : Math.round(n) + ' GB');
  /* The closed "Technical details" fold: rows are [label, value]. */
  const tech = (rows, extra) => A.v5tech(rows, extra);

  /* What a computer is doing, in plain words: [dot colour, word]. Raw errors never show here. */
  const status = (s) => ({ online: ['ok', 'Connected'], trust: ['bad', 'Needs your OK'], refused: ['bad', 'Needs your OK'], connecting: ['warn', 'Connecting'], login: ['warn', 'Signed out'], disconnected: ['off', 'Not connected'] }[s.state] || ['off', 'Offline']);
  /* Where it is reached from, in plain words ('' when unknown). */
  const where = (s) => (s.via === 'LAN' ? 'home network' : s.via ? 'away from home' : '');
  const sysName = (s) => (s.os || '').replace(/ [\d.]+$/, '').split(' ')[0];
  /* The key as the first 8 characters in two groups, for the technical fold only. */
  const shortKey = (fp) => { const h = String(fp || '').replace(/[^0-9a-f]/gi, '').toUpperCase().slice(0, 8); return h ? h.slice(0, 4) + ' ' + h.slice(4) + ' …' : ''; };

  /* The computer shown in the details column: the picked one, else the one Files is on, else the first. */
  const roster = () => (!E.servers.length && A.localPC && A.localPC.srv() ? [A.localPC.srv()] : E.servers);
  const current = () => roster().find((s) => s.id === T.pick) || roster().find((s) => s.id === A.mainPane.host) || roster()[0];

  /* The details column for computer s: picture, name, state, space, the actions that fit its state, and the technical fold. */
  function inspector(s) {
    const [dk, word] = status(s), w = where(s), hasDisk = s.disk && s.disk[1] > 0;
    const needs = s.state === 'trust' || s.state === 'refused';
    const main = s.state === 'online' ? btn('Open files', { i: 'folder', k: 'pri', a: 'open', id: s.id })
      : needs ? btn('Check what changed…', { i: 'shield', k: 'warn', a: 'review', id: s.id })
        : s.mac && T.kindHas(s, 'wake') && !E.wakeDenied(s) ? btn('Wake computer', { i: 'power', k: 'pri', a: 'wake', id: s.id }) : btn(s.state === 'disconnected' ? 'Connect' : 'Try again', { i: 'plug', k: 'pri', a: 'retry', id: s.id });
    return pic(s, 40) + '<h2>' + T.kindIcon(s, 20) + esc(s.name) + '</h2><p class="k-sub">' + dot(dk) + ' ' + word + (w ? ' · ' + w : '') + '</p>' +
      (hasDisk ? '<dl class="k-kv"><dt>Space</dt><dd>' + gb(s.disk[0]) + ' of ' + gb(s.disk[1]) + ' used<span class="k-bar"><i style="width:' + (s.disk[0] / s.disk[1] * 100) + '%"></i></span></dd></dl>' : '') +
      '<div class="k-dact">' + main + btn('Check the connection', { i: 'activity', a: 'diag', id: s.id }) + btn('Settings for ' + s.name + '…', { i: 'sliders', a: 'edit', id: s.id }) +
      btn('Folders it shares…', { i: 'folder', a: 'roots', id: s.id, dis: !(s.roots || []).length }) + btn('Changed lately…', { i: 'clock', a: 'recent', id: s.id, dis: s.state !== 'online' }) + btn('Remove…', { i: 'trash', k: 'ghost rm', a: 'forget', id: s.id }) + '</div>' +
      tech([['System', s.os || '—'], ['Address', s.addr || s.host], ['RFE version', s.agent || '—'], ['Connection', (s.via || '—') + (s.state === 'online' && s.latency ? ', ' + s.latency + ' ms' : '')], ['Key', shortKey(s.fp) || '—']]);
  }

  /* The page: heading, one row per computer, and the details column (or, with none saved, what to do first). */
  T.page = () => {
    const stage = $('#stage'); const lpc = !E.servers.length && A.localPC && A.localPC.srv(); const list = lpc ? [lpc] : E.servers; const add = btn('Add a computer', { i: 'plus', k: 'pri', a: 'add' });
    const head = '<header class="k-ph"><div><h1 class="k-title">Devices</h1><p class="k-sub">' + (list.length ? list.length + ' saved' : 'None saved yet') + '</p></div><div class="k-ph-r">' + (list.length ? add : '') + '</div></header>';
    let html;
    if (!list.length) html = '<div class="cpg solo"><div class="cpl">' + head + '<div class="b-empty">' + em('desktop_computer', 40) + '<b>No devices yet</b><p>Install RFE on a PC and open it once. Then add it here: it shows up by itself on the same network.</p><div class="k-row">' + add + '<button type="button" class="k-btn" data-v5="addlocal"><span>Add this PC</span></button>' + btn('Find on network', { i: 'radar', a: 'find' }) + '</div></div></div></div>';
    else {
      const cur = current(); T.pick = cur.id;
      html = '<div class="cpg"><div class="cpl">' + head + '<div class="li">' + list.map((s) => { const [dk, word] = status(s); const sub = [sysName(s), where(s)].filter(Boolean).join(' · ');
        return '<div class="ro' + (s.id === cur.id ? ' sel' : '') + '" role="button" tabindex="0" data-pa="pick" data-id="' + esc(s.id) + '">' + pic(s, 24) + '<span><b>' + T.kindIcon(s, 14) + esc(s.name) + '</b><small>' + esc(sub) + '</small></span><span class="st">' + dot(dk) + word + '</span></div>'; }).join('') + '</div>' + (A.v5Devices ? A.v5Devices.sectionFor(cur) : '') + '</div><aside class="rail" aria-label="Details">' + inspector(cur) + '</aside></div>';
    }
    stage.className = 'stage'; const DV = A.v5Devices;
    if (DV && list.length) DV.loads(); /* the devices section reads its lists on every tick, as the old Devices page did */
    if (stage._cv === html && stage.querySelector('.cpg')) return;
    if (DV && DV.typing()) return; /* a folder being typed in the devices section is not wiped by a poll */
    /* This runs on every tick: an unchanged page is left alone; a rebuilt one keeps the focused row. */
    const at = document.activeElement; const key = at && stage.contains(at) ? [at.dataset.pa, at.dataset.id] : null; const vp = at && stage.contains(at) && at.dataset ? at.dataset.v5pick : ''; const sc = stage.querySelector('.cpl'); const top = sc ? sc.scrollTop : 0;
    stage.innerHTML = html; stage._cv = html; const nl = stage.querySelector('.cpl'); if (nl) nl.scrollTop = top;
    if (DV) DV.bind();
    if (vp) { const n = [...stage.querySelectorAll('[data-v5pick]')].find((x) => x.dataset.v5pick === vp); if (n) n.focus({ preventScroll: true }); }
    if (key && key[0]) { const back = [...stage.querySelectorAll('[data-pa]')].find((x) => x.dataset.pa === key[0] && x.dataset.id === key[1]); if (back) back.focus({ preventScroll: true }); }
  };

  /* Asks before removing a computer (the engine drops its saved connection and remembered key); same engine call as the old page, in the mockup's words and box. */
  function forget(s) {
    const n = E.tasks.filter((t) => t.host === s.id && A.groupOf(t) !== 'done').length;
    skin(A.dialog({ icon: 'trash', cls: 'danger', title: 'Remove ' + s.name + '?', body: '<p>It leaves your computers here.' + (n ? ' <b>' + n + ' transfer' + (n === 1 ? '' : 's') + ' to it that ' + (n === 1 ? 'has' : 'have') + ' not finished will stop.</b>' : '') + ' Nothing on ' + esc(s.name) + ' is deleted.</p>', actions: [{ label: 'Cancel' }, { label: 'Remove', kind: 'ed', cb: async () => { if (A.mainPane.host === s.id) A.mainPane.setHost('local'); try { await E.forget(s.id); A.snack('Removed ' + s.name); } catch (e) { A.snack(e.message, { error: true }); } A.renderAll(); } }] }), { width: 480 });
  }

  /* "Changed lately": the files moved to or from a computer by this app, in the mockup's list (Engine.recentFiles, the same
     source as the old dialog, never a scan of the computer's disk); choosing a row opens that file or folder in Files. */
  const oldRecent = A.recentOnServer;
  A.recentOnServer = (id) => {
    if (!T.on()) return oldRecent(id);
    const s = E.server(id); if (!s || s.state !== 'online') { A.snack('Connect to a computer first.', { error: true }); return; }
    Promise.resolve(E.recentFiles(id, { limit: 40 })).then((list) => {
      const rows = list.map((e, i) => '<div class="k-lrow b-rc" role="button" tabindex="0" data-ri="' + i + '">' + ic(e.isDir ? 'folder' : 'file', { size: 18 }) + '<span><b>' + esc(e.name) + '</b><small class="mut">in ' + esc(String(e.path || '').replace(/[\\/][^\\/]*$/, '') || '/') + '</small></span><span class="t2">' + (e.modified ? esc(A.fx.rel(Date.parse(e.modified))) : '') + '</span></div>').join('');
      const open = (el, c) => { const r = el.closest('[data-ri]'); if (!r) return; const e = list[+r.dataset.ri]; c.close(); A.openPath(id, e.path, !e.isDir); };
      skin(A.dialog({ icon: 'clock', title: 'Changed lately on ' + s.name, width: 560, noFocus: true, body: list.length ? '<div class="k-list">' + rows + '</div><p class="k-sub b-ln">Files moved to or from this computer with this app, newest first.</p>' : '<p class="k-sub">No files have been moved to or from this computer yet.</p>', actions: [{ label: 'Close', kind: 'f' }],
        onOpen: (c) => { c.el.addEventListener('click', (ev) => open(ev.target, c)); c.el.addEventListener('keydown', (ev) => { if (ev.key === 'Enter' && ev.target.closest('.b-rc')) { ev.preventDefault(); open(ev.target, c); } }); } }), { width: 560 });
    }).catch((e) => A.snack(e.message, { error: true }));
  };

  /* One message of the key check says "fingerprints"; in the v5 window it uses the plain word. */
  const snack0 = A.snack;
  A.snack = (m, o) => snack0(T.on() && m === 'Tick the box to confirm you compared the fingerprints.' ? 'Tick the box to confirm it matches.' : m, o);

  /* Clicks and Enter/Space on the page: pick a row, or run an action of the details column. */
  const act = (el) => {
    const a = el.dataset.pa, id = el.dataset.id, s = id && E.server(id);
    if (a === 'add') { A.serverDialog(); return; }
    /* Find on network opens the same Add dialog as the Files page's button does; its first step already lists the PCs found on the network (the plain dialog is switched to its find tab). */
    if (a === 'find') { A.serverDialog(); A.defer(() => A.clickByText(document.querySelector('#layer'), /Find on network/)); return; }
    if (a === 'pick') { T.pick = id; T.page(); return; }
    if (!s) return;
    if (a === 'open') { A.setMainHost(id); A.go('files'); }
    else if (a === 'retry') { A.setMainHost(id); if (s.state === 'disconnected') E.connect(id); else E.retryNow(id); }
    else if (a === 'wake') { const x = (A.serverMenuExtra(s) || []).find((m) => m && /^Wake/.test(m.label)); if (x && x.onClick) x.onClick(); }
    else if (a === 'review') { if (s.state === 'trust') A.trustDialog(s); else A.reverifyDialog(id); }
    else if (a === 'diag') A.diagnose(id);
    else if (a === 'edit') T.settings(s);
    else if (a === 'roots') A.rootsDialog(id);
    else if (a === 'recent') A.recentOnServer(id);
    else if (a === 'forget') forget(s);
  };
  $('#stage').addEventListener('click', (e) => { const el = e.target.closest('.cpg [data-pa]'); if (el && A.state.view === 'servers' && !el.disabled) act(el); });
  $('#stage').addEventListener('keydown', (e) => { const el = e.target.closest('.cpg .ro[data-pa]'); if (el && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); act(el); } });

  /* ---------- dialogs: the mockup's box (title bar with a close button, body, button row) around the existing dialog engine ---------- */
  /* Rebuilds the box A.dialog made into the mockup's: .k-dlg with a title row and a close button, .k-db, .k-da, mockup buttons. */
  function skin(c, o) {
    const d = c.el, h2 = $('h2', d), db = $('.db', d), df = $('.df', d), di = $('.di', d); if (di) di.remove();    const dh = document.createElement('div'); dh.className = 'k-dh'; d.insertBefore(dh, h2); dh.appendChild(h2);
    const x = document.createElement('button'); x.type = 'button'; x.className = 'k-ib'; x.setAttribute('aria-label', 'Close'); x.setAttribute('data-tip', 'Close (Esc)'); x.innerHTML = ic('x', { size: 16 }); dh.appendChild(x);
    /* "db" and "df" stay as class names: the existing dialogs find their body and button row by them when they redraw */
    d.className = 'k-dlg' + (o.k ? ' ' + o.k : ''); d.style.width = o.width / 16 + 'rem'; db.className = 'k-db db'; df.className = 'k-da df';
    /* the close button does what Cancel / Not now does (so a half-added computer is dropped), else just closes */
    x.addEventListener('click', () => { const cb = [...df.querySelectorAll('button')].find((b) => /^(Cancel|Not now|Leave it for now)/.test(b.textContent.trim())); if (cb) cb.click(); else c.close(); });
    df.querySelectorAll('button').forEach((b) => { const k = /\bf\b/.test(b.className) ? ' pri' : /\bed\b/.test(b.className) ? ' warn' : ''; b.className = 'k-btn' + k; });
    return c;
  }

  const STEPS = [['Find your PC', 'on this network or by its address'], ['Allow it on the PC', 'the same number on both'], ['Ready', 'name it, open its files']];
  /* The title of the add dialog on step n: "Add a computer" on the first, then what the step does (about `name` when it is known). */
  const stepTitle = (n, name) => (n === 1 ? 'Add a computer' : n === 2 ? 'Allow it on ' + (name || 'the PC') : STEPS[2][0]);
  /* The left column of the add dialog: the three steps, step n marked. */
  const rail = (n) => '<div class="b-sx">' + STEPS.map((x, i) => '<div class="' + (i + 1 === n ? 'on' : i + 1 < n ? 'done' : '') + '"><i>' + (i + 1 < n ? ic('check', { size: 12 }) : i + 1) + '</i><span>' + x[0] + '<small>' + x[1] + '</small></span></div>').join('') + '</div>';

  /* Step 1 of adding a computer: the PCs found on this network (and the ones already added), or an address typed in a fold.
     Next adds it through the existing flow, which asks for the key check (A.trustDialog) in this same box. `pre` fills the address after Back. */
  const addStep1 = (pre) => {
    let found = null, sel = 0, kindSel = 'pc', typed = pre && pre.host ? pre.host + (pre.port && pre.port !== A.DEFAULT_PORT ? ':' + pre.port : '') : '', ctlRef = null;
    const free = () => (found || []).filter((f) => !E.servers.some((s) => s.addr === f.hostport));
    const pane = () => {
      const list = found || [], isSaved = (f) => E.servers.some((s) => s.addr === f.hostport); const open = list.filter((f) => !isSaved(f)), done = list.filter(isSaved);
      const none = '<div class="b-none">' + em('desktop_computer', 32) + '<span><b>' + (found ? 'No PCs found' : 'No PCs found yet') + '</b>Install RFE on the PC you want to reach and open it once. It shows up here by itself.</span>' + (found ? '' : '<span class="b-live"><i></i>Looking</span>') + '</div>';
      const row = (f, i, saved) => '<button type="button" class="b-pc' + (!saved && i === sel ? ' on' : '') + '"' + (saved ? ' disabled' : ' data-i="' + i + '"') + '>' + em(/windows/i.test(f.os || '') ? 'laptop' : 'desktop_computer', 28) + '<span><b>' + esc(f.name) + '</b><small>' + esc(saved ? 'already added' : f.os || '') + '</small></span>' + (!saved && i === sel ? ic('check', { size: 16 }) : '') + '</button>';
      return '<h3>Which PC do you want to reach?</h3><p>These PCs have RFE and are on the same network as this one.</p>' + (open.length || done.length ? open.map((f, i) => row(f, i, false)).join('') + done.map((f) => row(f, 0, true)).join('') : none) +
        '<details class="b-more"' + (typed ? ' open' : '') + '><summary>Not in the list?</summary><p>PCs at another place, or reached over Tailscale, are not listed. Type the address RFE shows on that PC, under Status.</p><label class="b-f">' + ic('globe', { size: 16 }) + '<input id="v5addr" value="' + esc(typed) + '" placeholder="Its address" aria-label="Its address" spellcheck="false" autocomplete="off"></label><span class="err" id="v5adde"></span></details>' + kindPick(kindSel) + '<div class="b-cd"><span>Nobody at that PC? If it gave you a pairing code, add it with the code instead.</span><button type="button" class="k-btn sm" data-code="1"><span>Use a code</span></button></div>';
    };
    const draw = (c) => { const p = $('.b-pane', c.el); if (!p) return; p.innerHTML = pane(); const nx = c.btn && c.btn.next; if (nx) nx.disabled = !(typed || free().length); };
    const c = skin(A.dialog({ icon: 'plug', title: 'Add a computer', width: 720, enter: 'next', body: rail(1) + '<div class="b-pane"></div>',
      actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'next', label: 'Next', kind: 'f', icon: 'chevron-right', cb: (ctl) => {
        const addr = ($('#v5addr', ctl.el) || { value: '' }).value.trim(); const f = free()[sel]; const hostport = addr || (f && f.hostport); const er = $('#v5adde', ctl.el);
        if (er) er.textContent = ''; if (!hostport) return false; const pa = E.util.parseAddr(hostport); if (pa.error) { if (er) er.textContent = pa.error; return false; }
        const name = addr ? pa.host : f.name; const o = { name: E.servers.some((s) => s.name.toLowerCase() === name.toLowerCase()) ? pa.addr : name, host: pa.addr };
        try { const had = new Set(E.servers.map((x) => x.id)); const added = A.addAndOpen(o, undefined, ctl); if (added && !had.has(added.id)) T.setKind(added, kindSel); } catch (e) { A.snack(e.message, { error: true }); }
        return false;
      } }] }), { width: 720, k: 'b-add' });
    ctlRef = c; if (c.sc) c.sc.classList.add('top'); /* the same backdrop class as steps 2 and 3 (ui.js); v5-base.css centres it */
    draw(c);
    c.el.addEventListener('click', (e) => {
      const b = e.target.closest('.b-pc[data-i]'); if (b) { sel = +b.dataset.i; draw(c); return; }
      const kb = e.target.closest('[data-kd] button'); if (kb) { kindSel = T.kinds.includes(kb.dataset.v) ? kb.dataset.v : 'pc'; draw(c); return; }
      /* "Use a code": the pairing-code form (fx2) takes over this same box; its Back button returns here */
      if (e.target.closest('[data-code]') && A.fx2 && A.fx2.codeDialog) { A.dialogReuse = c; A.fx2.codeDialog(); A.dialogReuse = null; }
    });
    c.el.addEventListener('input', (e) => { if (e.target.id === 'v5addr') { typed = e.target.value.trim(); const nx = c.btn && c.btn.next; if (nx) nx.disabled = !(typed || free().length); } });
    E.call('discover_agents').then((l) => { found = Array.isArray(l) ? l : []; }).catch(() => { found = []; }).then(() => { if (ctlRef.el.isConnected && $('.b-pane', ctlRef.el)) draw(ctlRef); });
    return c;
  };

  /* "<PC> looks different from last time": what it means and what to do; Reconnect goes on to the existing confirmation
     (compare the key, type the name), so the key is never re-trusted without the person deciding. */
  const keyChanged = (id, confirm) => {
    const s = E.server(id); if (!s) return;
    const c = skin(A.dialog({ icon: 'shield', title: s.name + ' looks different from last time', width: 540, body: '<p>RFE stopped connecting to ' + esc(s.name) + ' to keep your files safe. This is normal after RFE was reinstalled or the PC was reset.</p><ul class="b-ul"><li><b>If that happened</b>, connect again and allow this computer on ' + esc(s.name) + ', like the first time.</li><li><b>If not</b>, someone else may be pretending to be ' + esc(s.name) + '. Leave it and ask whoever looks after it.</li></ul>' + tech([['Before', shortKey(s.oldFp) || '—'], ['Now', shortKey(s.newFp) || '—']]),
      actions: [{ label: 'Leave it for now', kind: 'tx' }, { label: 'Reconnect', kind: 'f', cb: () => { A.defer(() => confirm(id)); } }] }), { width: 540 });
    return c;
  };
  const oldReverify = A.reverifyDialog;
  A.reverifyDialog = (id) => (T.on() ? keyChanged(id, oldReverify) : oldReverify(id));

  /* Steps 2 and 3 of adding a computer are the existing dialogs (waiting for the computer, the key check, signing in); in the v5
     window they sit in the same box as step 1: the mockup's step column on the left, their content in .b-pane. Whenever the dialog
     redraws its body the layout is rebuilt. The key check keeps its tick-box; the key itself moves into "Technical details". */
  const STEP_OF = { Find: 1, Check: 2, 'Sign in': 3 };
  /* The computer a step is about, from the old dialog title ("Trust X?", "Check X", "Sign in to X"). */
  const nameOf = (t) => t.replace(/^(Trust |Check |Sign in to )/, '').replace(/\?$/, '');
  /* Changes the text of the button in row `df` whose text matches `re`; its icon stays. */
  const relabel = (df, re, text) => { const b = df && [...df.querySelectorAll('button')].find((x) => re.test(x.textContent.trim())); if (!b) return; const t = [...b.childNodes].reverse().find((m) => m.nodeType === 3); if (t) t.textContent = text; };
  /* The text node that ends a label (the words after the tick-box), changed to `text`. */
  const relabelText = (el, text) => { const t = el && [...el.childNodes].reverse().find((m) => m.nodeType === 3); if (t) t.textContent = text; };
  /* The first two groups of a key ("E2A7 51C0"): what a person can read out and compare on the other PC. */
  const idOf = (fp) => shortKey(fp).replace(/ …$/, '') || '—';
  /* A mockup button that is not part of the dialog engine's row. */
  const mkBtn = (label, cls, icon, fn) => { const b = document.createElement('button'); b.type = 'button'; b.className = 'k-btn ' + cls; b.innerHTML = (icon ? ic(icon, { size: 16 }) : '') + '<span>' + esc(label) + '</span>'; b.addEventListener('click', fn); return b; };

  /* Step 3, "Ready", in the same box once signing in worked: name it here, add another, or open its files. The name is
     saved through the engine (E.updateServer) when it changes. `real` is the dialog's own close. */
  function ready(c, name, real) {
    const db = $('.db', c.el), df = $('.df', c.el), s = E.servers.find((x) => x.name === name); if (!db || !df) { real(); return; }
    const h2 = $('h2', c.el); if (h2) h2.textContent = stepTitle(3, name);
    db.innerHTML = rail(3) + '<div class="b-pane"><div class="b-done"><span class="b-tile ok">' + ic('check', { size: 22 }) + '</span><h3>' + esc(name) + ' is ready</h3><p>It is added to your computers. You can change what it shares on the PC.</p><label class="b-fl"><small>Name it here</small><span class="b-f">' + ic('edit', { size: 16 }) + '<input value="' + esc(name) + '" aria-label="Name" spellcheck="false"></span></label></div></div>';
    const inp = $('input', db);
    const save = () => {
      const v = inp.value.trim(); if (!s || !v || v === s.name) return;
      if (E.servers.some((x) => x.id !== s.id && x.name.toLowerCase() === v.toLowerCase())) { A.snack('A computer with this name already exists', { error: true }); inp.value = s.name; return; }
      E.updateServer(s.id, { name: v }).then(() => A.renderAll()).catch((e) => A.snack(e.message, { error: true }));
    };
    inp.addEventListener('change', save);
    df.textContent = '';
    df.append(mkBtn('Add another', 'ghost', '', () => { save(); A.dialogReuse = c; A.serverDialog(); A.dialogReuse = null; }), mkBtn('Open its files', 'pri', 'folder', () => { save(); real(); if (s) { A.setMainHost(s.id); A.go('files'); } }));
    c.close = () => { save(); real(); };
  }

  /* Rewrites the content of one add-a-computer step into the mockup's plain words. `kind` is trust, check, signin or code;
     the tick-box, the key and the fields stay where the existing dialog reads them, so the flow itself is unchanged. */
  function plain(c, pane, kind, name, solo) {
    const n = esc(name), df = $('.df', c.el), s = E.servers.find((x) => x.name === name), h = (t) => '<h3>' + t + '</h3>';
    if (kind === 'trust') {
      const keep = ['.fxck', '.fpdiff', '.adtech'].map((q) => pane.querySelector(q));
      relabelText(keep[0], ' It is the same on ' + name);
      pane.innerHTML = h('Is this really ' + n + '?') + '<p>This is the first time this computer talks to ' + n + '. On ' + n + ', open RFE and look under Status. It shows an ID that starts the same way as this one.</p><div class="b-num mono">' + esc(idOf(s && s.fp)) + '</div><p class="b-tn">Not the same? Choose Cancel: someone else may be pretending to be ' + n + '. Nothing is sent to it until you say so.</p>';
      keep.forEach((x) => { if (x) pane.appendChild(x); });
      relabel(df, /^They match/, 'Trust');
    } else if (kind === 'check') {
      const w = pane.querySelector('[data-chk]'), b = w && $('b', w), sm = w && $('small', w);
      if (b) b.textContent = 'Reaching ' + name + '…'; if (sm) sm.textContent = 'Making sure it is the right PC.';
      pane.insertAdjacentHTML('afterbegin', h('Finding ' + n));
    } else if (kind === 'signin') {
      const tabs = $('.fxtabs', pane), pc = $('.pcode', pane);
      if (tabs) {
        /* on its own (not a step) the tabs keep the mockup's sign-in words: Account, Pairing code, Ask the PC */
        tabs.className = 'k-tabs fxtabs'; if (!solo) tabs.querySelectorAll('button').forEach((b) => { b.textContent = { in: 'Password', code: 'Pairing code', ask: 'On the PC' }[b.dataset.m] || b.textContent; });
        const p = $('.fxp', pane), say = { in: 'If ' + n + ' has an RFE account, sign in with its password.', code: 'Type the 8-character code that RFE on ' + n + ' shows under Devices.', ask: 'On ' + n + ', RFE asks <b>“Let this computer in?”</b> Someone there clicks Allow. It starts with browse access only.' };
        if (p) p.innerHTML = say[$('#siu', pane) ? 'in' : $('#sic', pane) ? 'code' : 'ask'];
        if (!solo) pane.insertAdjacentHTML('afterbegin', h('Allow this computer on ' + n));
      }
      if (pc) { const num = document.createElement('div'); num.className = 'b-num mono'; num.textContent = pc.textContent.trim(); pc.replaceWith(num); const sm = $('.fxwait small', pane); if (sm) sm.textContent = 'On ' + name + ', RFE now asks to let this computer in. Check that it shows this number, then click Allow there.'; }
      const cp = $('[data-pcopy]', pane); if (cp) cp.className = 'k-btn sm';
      relabel(df, /^Ask for approval/, 'Ask');
    } else if (kind === 'code') {
      const tabs = $('.fxtabs', pane), p = $('.fxp', pane), hn = $('.hint', pane), names = { pcn: 'Name it here', pch: 'Its address', pcc: 'Pairing code' };
      if (tabs) tabs.remove(); if (p) p.innerHTML = 'Use this when nobody can sit at the PC. On that PC open RFE, then <b>Devices</b>, and choose <b>Pair a phone</b>. Type its address and the 8-character code here.';
      pane.querySelectorAll('label[for]').forEach((l) => { if (names[l.htmlFor]) l.textContent = names[l.htmlFor]; });
      if (hn) { const q = document.createElement('p'); q.className = 'b-tn'; q.textContent = hn.textContent; hn.replaceWith(q); }
      pane.insertAdjacentHTML('afterbegin', h('Add with a pairing code'));
      relabel(df, /^Continue/, 'Next');
      if (df && !df.querySelector('[data-back]')) { const bk = mkBtn('Back', '', '', () => { A.dialogReuse = c; A.serverDialog(); A.dialogReuse = null; }); bk.dataset.back = '1'; df.prepend(bk); }
    }
  }

  function addBox(c) {
    const db = $('.db', c.el); if (!db || db.querySelector(':scope > .b-sx')) return; const ol = db.querySelector(':scope > .addsteps'); if (!ol) return;
    const h2 = $('h2', c.el); if (h2 && !c.el.dataset.t0) c.el.dataset.t0 = h2.textContent; const t0 = c.el.dataset.t0 || '', name = nameOf(t0);
    const kind = $('#pcc', db) ? 'code' : /^Trust /.test(t0) ? 'trust' : /^Check /.test(t0) ? 'check' : /^Sign in to /.test(t0) ? 'signin' : '';
    const n = kind === 'code' ? 1 : kind ? 2 : +(/step (\d)/.exec(ol.getAttribute('aria-label') || '') || [0, 2])[1] || STEP_OF[(ol.querySelector('.on span') || {}).textContent] || 2;
    const pane = document.createElement('div'); pane.className = 'b-pane'; [...db.childNodes].forEach((x) => { if (x !== ol) pane.appendChild(x); });
    ol.hidden = true; const r = document.createElement('div'); r.innerHTML = rail(n); db.append(r.firstChild, pane);
    if (h2) h2.textContent = stepTitle(n, kind ? name : '');
    if (kind) plain(c, pane, kind, name);
    if (kind === 'signin' && !c._rdy) { c._rdy = true; const real = c.close; c.close = () => ready(c, name, real); }
    techFold(pane, kind);
  }

  /* Gathers the key and address blocks of a step into one closed "Technical details" fold at the end of `pane`. */
  function techFold(pane, kind) {
    const box = document.createElement('details'); box.className = 'b-tech'; box.innerHTML = '<summary>Technical details</summary>'; let moved = false;
    pane.querySelectorAll('.fpdiff').forEach((f) => { box.appendChild(f); moved = true; });
    pane.querySelectorAll('.adtech').forEach((t) => { t.className = 'b-tech'; t.querySelectorAll('dl').forEach((dl) => dl.classList.add('k-kv')); if (!moved) { moved = true; box.remove(); pane.appendChild(t); } else { [...t.querySelectorAll('dl')].forEach((dl) => box.appendChild(dl)); t.remove(); } });
    if (box.children.length > 1) pane.appendChild(box);
    if (kind === 'trust') { const tb = pane.querySelector('.b-tech'); if (tb) tb.insertAdjacentHTML('beforeend', '<p class="b-tn">RFE remembers this key and warns you if it ever changes.</p>'); }
  }

  /* The key check and the sign-in when they open on their own (after Reconnect, from Accounts), not as a step of adding a
     computer: the mockup's box around the same content. The content is wrapped again every time the dialog redraws it. */
  function soloBox(c, kind, name) {
    skin(c, { width: kind === 'trust' ? 560 : 520 });
    const db = $('.db', c.el); if (!db) return;
    const wrap = () => {
      if (!db.firstChild || db.querySelector(':scope > .b-pane')) return;
      const at = document.activeElement, back = at && db.contains(at) ? at : null;
      const pane = document.createElement('div'); pane.className = 'b-pane solo'; [...db.childNodes].forEach((x) => pane.appendChild(x)); db.appendChild(pane);
      plain(c, pane, kind, name, true); techFold(pane, kind);
      if (back && back.isConnected) back.focus();
    };
    wrap(); new MutationObserver(wrap).observe(db, { childList: true });
  }

  /* "Check the connection" in the mockup's words: each check of the old diagnose dialog (in order) as one plain line, what to do
     when one fails, and the raw answers (address, timing, key) in the closed "Technical details". */
  const DIAG = ['Found it on the network', 'It is the same PC as before', 'Signed in', 'Shares its folders'];
  const DIAG_ADV = ['It did not answer. Check that it is on and on this network.', 'It does not look like the same PC as before. Do not go on until you know why.', 'Sign in again from the Computers page.', 'It listed no folders for this computer. They are set on that PC.'];
  const DIAG_TECH = ['Answer', 'Key', 'Sign-in', 'Folders'];
  function diagBox(c, name) {
    skin(c, { width: 540 }); const df = $('.df', c.el), host = $('.fxdiag', c.el), s = E.servers.find((x) => x.name === name), h2 = $('h2', c.el);
    if (h2) h2.textContent = 'Connection to ' + name;
    if (df) { const cl = [...df.querySelectorAll('button')].find((b) => /^Close/.test(b.textContent.trim())); if (cl) cl.remove(); relabel(df, /^Copy report/, 'Copy for support'); relabel(df, /^Run again/, 'Check again'); relabel(df, /^Review key/, 'Review'); const cp = df.querySelector('[data-id="cpy"]'); if (cp) cp.classList.add('ghost'); }
    if (!host) return;
    let mo = null, open = false; /* whether the fold was left open; the dialog redraws its list, so it is remembered here */
    host.addEventListener('toggle', (e) => { if (e.target.tagName === 'DETAILS') open = e.target.open; }, true);
    const paint = () => {
      const rows = [...host.querySelectorAll(':scope > .dgs')]; if (!rows.length) return;
      const bad =rows.findIndex((r) => r.classList.contains('bad')), fin = !!host.querySelector(':scope > .hint');
      const raw = rows.map((r) => { const m = r.querySelector('small'); return m ? m.textContent : ''; });
      const mark = (r) => (r.classList.contains('ok') ? ic('check') : r.classList.contains('bad') ? ic('x') : r.classList.contains('run') ? ic('spinner') : '<span class="i dgw">–</span>');
      const list = '<div class="k-list">' + rows.map((r, k) => '<div class="k-lrow ' + ['ok', 'bad', 'run', 'skip', 'wait'].filter((q) => r.classList.contains(q)).join(' ') + '">' + mark(r) + '<span>' + esc(DIAG[k] || '') + '</span></div>').join('') + '</div>';
      const note = !fin ? '' : bad < 0 ? '<p class="dgn ok">' + ic('check-circle') + '<span>Working normally.</span></p>' : '<p class="dgn bad">' + ic('alert-circle') + '<span><b>Stopped at: ' + esc((DIAG[bad] || '').toLowerCase()) + '.</b> ' + esc(DIAG_ADV[bad] || '') + '</span></p>';
      const th = [['Address', (s && s.addr) || '—']].concat(raw.map((m, k) => (m ? [DIAG_TECH[k], m] : null)).filter(Boolean));
      if (mo) mo.disconnect();
      host.innerHTML = list + note + tech(th); const fold = host.querySelector('details'); if (open && fold) fold.open = true;
      if (mo) mo.observe(host, { childList: true });
    };
    mo = new MutationObserver(paint); paint(); mo.observe(host, { childList: true });
  }

  /* "Folders X shares" in the mockup's list: folder name, its full path, and the existing Open button of each row. */
  function rootsBox(c, name) {
    skin(c, { width: 520 }); const db = $('.db', c.el), df = $('.df', c.el), h2 = $('h2', c.el); if (!db) return;
    if (h2) h2.textContent = 'Folders ' + name + ' shares';
    relabel(df, /^Close/, 'Done');
    const p = $('.fxp', db); if (p) p.textContent = 'Only these folders can be opened from here. Change them on ' + name + '.';
    const rows = [...db.querySelectorAll('.lrow')]; if (!rows.length) return;
    const list = document.createElement('div'); list.className = 'k-list'; rows[0].before(list);
    rows.forEach((r) => {
      const path = ($('.dm b', r) || { textContent: '' }).textContent, use = ($('.dm small', r) || { textContent: '' }).textContent, ob = $('button', r), gi = $('.gi', r);
      const k = document.createElement('div'); k.className = 'k-lrow'; k.innerHTML = (gi ? gi.innerHTML : '') + '<span><b></b><small class="mono"></small></span>';
      $('b', k).textContent = path.split(/[\\/]/).filter(Boolean).pop() || path; $('small', k).textContent = path + (use ? ' · ' + use : '');
      if (ob) { ob.className = 'k-btn sm'; k.appendChild(ob); }
      list.appendChild(k); r.remove();
    });
  }

  /* The confirmation after "Reconnect" on a changed computer (A.reverifyDialog of fx2), in the mockup's box and words. The
     tick-box and the typed name stay (they are what lets the new key be trusted); the two keys sit in the closed fold. */
  function reconnectBox(c, name) {
    const db = $('.db', c.el), h2 = $('h2', c.el); if (!db) return;
    const n = esc(name), s = E.servers.find((x) => x.name === name), keep = ['.fxck', '.fld', '.fpdiff'].map((q) => db.querySelector(q));
    relabelText(keep[0], ' I checked on ' + name + ' itself that it matches'); const lb = keep[1] && $('label', keep[1]); if (lb) lb.textContent = 'Type the name ' + name + ' to confirm';
    if (h2) h2.textContent = 'Connect to ' + name + ' again';
    db.innerHTML = '<div class="b-pane"><h3>Is it really ' + n + '?</h3><p>Only go on if you know why ' + n + ' changed, for example it was reinstalled. On ' + n + ', open RFE and look under Status: its ID should start like this. You sign in again afterwards.</p><div class="b-num mono">' + esc(idOf(s && s.newFp)) + '</div></div>';
    const pane = $('.b-pane', db); if (keep[0]) pane.appendChild(keep[0]); if (keep[1]) pane.appendChild(keep[1]);
    const box = document.createElement('details'); box.className = 'b-tech'; box.innerHTML = '<summary>Technical details</summary>'; if (keep[2]) box.appendChild(keep[2]); box.insertAdjacentHTML('beforeend', '<p class="b-tn">RFE remembers the new key and warns you if it ever changes again.</p>'); pane.appendChild(box);
    relabel($('.df', c.el), /^Keep it blocked/, 'Cancel');
  }
  const dialog0 = A.dialog;
  A.dialog = (o) => {
    const c = dialog0(o);
    if (T.on() && o.cls === 'danger' && /^Trust the new key for /.test(o.title || '') && c && c.el && !c.el.classList.contains('k-dlg')) { skin(c, { width: 560 }); reconnectBox(c, o.title.replace(/^Trust the new key for /, '').replace(/\?$/, '')); }
    if (T.on() && /\bconn\b/.test(o.cls || '') && c && c.el && !c.el.classList.contains('k-dlg')) {
      skin(c, { width: 720, k: 'b-add' }); const db = $('.db', c.el);
      if (db) { addBox(c); new MutationObserver(() => addBox(c)).observe(db, { childList: true }); }
    } else if (T.on() && c && c.el && !c.el.classList.contains('k-dlg')) {
      /* the other old dialogs of this page, found by their titles: check the connection, folders it shares, and the key check and sign-in on their own */
      const t = o.title || '';
      if (/^Diagnose /.test(t)) diagBox(c, t.replace(/^Diagnose /, ''));
      else if (/^Allowed roots on /.test(t)) rootsBox(c, t.replace(/^Allowed roots on /, ''));
      else if (/^Sign in to /.test(t)) soloBox(c, 'signin', t.replace(/^Sign in to /, ''));
      else if (/^Trust .+\?$/.test(t) && c.el.querySelector('#tfm')) soloBox(c, 'trust', t.replace(/^Trust /, '').replace(/\?$/, ''));
    }
    return c;
  };

  /* Settings for one computer, as the mockup draws them: its name, what it does when RFE starts, where its downloads go, speed, waking it and
     signing out. Speed, same-time transfers, the replace question and hidden files are the app's own settings (the core keeps one set for every
     computer, so the dialog shows and changes that set). The download folder (sent with each download from this computer) and the other three choices are kept for this
     computer in this window's storage only. Address and key stay in the closed fold. Done saves a changed name. */
  const PKEY = 'rfe-v5-pcprefs-v1';
  /* What this window keeps for each computer by its id: { connect, notify, wake, dir }. A missing value means "not chosen yet". */
  const readPrefs = () => A.store.get(PKEY, {}) || {};
  /* The per-computer values with their defaults: connect on start is on, the offline notice off, wake on when the computer's MAC address is known. */
  T.prefs = (s) => { const p = readPrefs()[s.id] || {}; return { connect: p.connect !== false, notify: !!p.notify, wake: p.wake === undefined ? !!s.mac : !!p.wake, dir: p.dir || '' }; };
  /* Keeps one per-computer value; storage that is not available just forgets it. */
  T.setPref = (s, k, v) => { const all = readPrefs(); all[s.id] = Object.assign({}, all[s.id], { [k]: v }); A.store.set(PKEY, all); };
  /* The kinds a computer can be marked as: [value, word, icon]. "pc" is the default. */
  const KINDS = [['pc', 'PC', 'monitor'], ['laptop', 'Laptop', 'laptop'], ['server', 'Server', 'server'], ['mobile', 'Mobile', 'phone']];
  /* The per-computer options a kind has no use for ("wake": waking it by network signal, "folder": the download folder). */
  const KIND_LACKS = { server: ['wake'], mobile: ['wake', 'folder'] };
  T.kinds = KINDS.map((k) => k[0]);
  /* The kind of computer s: what was chosen for it, and "pc" when nothing or something unknown is stored. */
  T.kind = (s) => { const k = (readPrefs()[s.id] || {}).kind; return T.kinds.includes(k) ? k : 'pc'; };
  /* Keeps the kind of computer s; a value that is not one of the four is kept as "pc". */
  T.setKind = (s, k) => T.setPref(s, 'kind', T.kinds.includes(k) ? k : 'pc');
  /* Whether the options named `opt` ("wake", "folder") make sense for the kind of computer s. */
  T.kindHas = (s, opt) => !(KIND_LACKS[T.kind(s)] || []).includes(opt);
  /* The small inline drawing of the kind of computer s, n pixels wide; its word is the tooltip. */
  T.kindIcon = (s, n) => { const k = T.kind(s), d = KINDS.find((x) => x[0] === k); return '<span class="k-kind" data-kind="' + k + '" data-tip="' + d[1] + '">' + ic(d[2], { size: n }) + '</span>'; };
  /* The four-way choice of a kind, `cur` pressed; the dialogs' click handlers read data-kd and data-v. */
  const kindSeg = (label, cur) => '<span class="k-seg" role="group" aria-label="' + esc(label) + '" data-kd="1">' + KINDS.map((k) => '<button type="button" data-v="' + k[0] + '" aria-pressed="' + (k[0] === cur) + '"' + (k[0] === cur ? ' class="on"' : '') + '>' + k[1] + '</button>').join('') + '</span>';
  /* The add dialog's "what kind of device is it" row. */
  const kindPick = (cur) => '<div class="b-kind"><b>What kind of device is it?</b>' + kindSeg('What kind of device is it?', cur) + '</div>';
  /* The mockup's switch, as a button that the dialog's click handler reads by data-sw. */
  const sw = (key, label) => '<button type="button" class="k-sw" role="switch" aria-checked="false" aria-label="' + esc(label) + '" data-sw="' + key + '"></button>';
  /* The mockup's segmented control; `opts` is [[value, word]], the dialog's click handler reads data-sg and data-v. */
  const seg = (key, label, opts) => '<span class="k-seg" role="group" aria-label="' + esc(label) + '" data-sg="' + key + '">' + opts.map((o) => '<button type="button" data-v="' + o[0] + '" aria-pressed="false">' + o[1] + '</button>').join('') + '</span>';
  /* Connect when RFE starts: the engine asks this for every signed-in computer at startup; "off" leaves it disconnected until the user connects it. */
  E.hooks.autoConnect = (s) => T.prefs(s).connect;
  /* Watches each computer's state. When one that was online goes offline and its notice is on, says so once. When one cannot be reached while it is
     being opened (connecting, then offline) and its wake choice is on, sends the app's existing wake signal (at most once a minute per computer). */
  const lastState = new Map(), wokeAt = new Map();
  E.on((t, s) => {
    if (t !== 'servers') return;
    if (!s || !s.id) { E.servers.forEach((x) => { if (!lastState.has(x.id)) lastState.set(x.id, x.state); }); return; }
    const was = lastState.get(s.id); lastState.set(s.id, s.state);
    if (was === undefined || was === s.state) return;
    const p = T.prefs(s);
    if (was === 'online' && (s.state === 'lost' || s.state === 'offline') && p.notify) A.snack(s.name + ' went offline.');
    if (was === 'connecting' && s.state === 'offline' && p.wake && T.kindHas(s, 'wake') && s.mac && Date.now() - (wokeAt.get(s.id) || 0) > A.T.minute) {
      const x = (A.serverMenuExtra(s) || []).find((m) => m && /^Wake/.test(m.label));
      if (x && x.onClick) { wokeAt.set(s.id, Date.now()); x.onClick(); }
    }
  });
  /* Where this computer's downloads go: the folder chosen for it, else the folder the app saves downloads in. */
  const folderOf = (s) => T.prefs(s).dir || A.downloadDir();
  /* The engine asks this for each download: the folder chosen for that computer, or nothing (then the core's own downloads folder is used). */
  E.destFor = (id) => { const s = E.server(id); return s ? T.prefs(s).dir : ''; };

  /* The dialog of an action that needs a computer when none is saved yet (the mockup's "No computer yet" box). */
  T.noPc = (title) => skin(A.dialog({ icon: 'sliders', title, width: 520, noFocus: true, body: '<div class="b-empty">' + em('desktop_computer', 32) + '<b>No computer yet</b><p>Add a computer first. Its settings and status show up here.</p></div>', actions: [{ label: 'Cancel' }, { label: 'Add a computer', kind: 'f', cb: () => A.serverDialog() }] }), { width: 520 });
  T.settings = (s) => {
    if (!s) return T.noPc('Settings for a computer');
    const row = (t, sub, ctl, lack) => '<div class="k-set"' + (lack ? ' data-lack="' + lack + '"' : '') + '><div><b>' + t + '</b>' + (sub ? '<small>' + sub + '</small>' : '') + '</div><div>' + ctl + '</div></div>';
    const signed = s.signedIn, S = A.S;
    /* Shows the current value on every switch and segmented control of the open dialog. */
    const paint = (el) => {
      const on = { connect: T.prefs(s).connect, notify: T.prefs(s).notify, wake: T.prefs(s).wake, hidden: !!S.showHidden, ask: S.onConflict === 'ask' };
      el.querySelectorAll('[data-sw]').forEach((b) => { const v = !!on[b.dataset.sw]; b.classList.toggle('on', v); b.setAttribute('aria-checked', String(v)); });
      el.querySelectorAll('[data-sg]').forEach((g) => g.querySelectorAll('button').forEach((b) => { const v = +b.dataset.v === +S[g.dataset.sg]; b.classList.toggle('on', v); b.setAttribute('aria-pressed', String(v)); }));
      el.querySelectorAll('[data-kd] button').forEach((b) => { const v = b.dataset.v === T.kind(s); b.classList.toggle('on', v); b.setAttribute('aria-pressed', String(v)); });
      el.querySelectorAll('[data-lack]').forEach((n) => { n.hidden = !T.kindHas(s, n.dataset.lack); });
      const path = $('.b-path', el); if (path) path.textContent = folderOf(s);
    };
    /* A click on a switch, a segment or Change…: the app's own settings go through the same calls the Settings page uses. */
    const press = (c, e) => {
      const b = e.target.closest('[data-sw], [data-sg] button, [data-kd] button, [data-chg]'); if (!b) return;
      if (b.dataset.chg) { A.pickFolder2({ title: 'Downloads from ' + s.name, startDir: folderOf(s) }).then((p) => { if (p) { T.setPref(s, 'dir', p); paint(c.el); } }).catch((er) => A.snack(er.message || String(er), { error: true })); return; }
      const k = b.dataset.sw;
      if (b.closest('[data-kd]')) { T.setKind(s, b.dataset.v); A.renderAll(); }
      else if (b.dataset.v !== undefined) { S[b.closest('[data-sg]').dataset.sg] = +b.dataset.v; A.save(); E.pushPrefs(); }
      else if (k === 'hidden') A.toggleHidden();
      else if (k === 'ask') { S.onConflict = S.onConflict === 'ask' ? 'keep' : 'ask'; A.save(); E.pushPrefs(); }
      else T.setPref(s, k, !T.prefs(s)[k]);
      paint(c.el);
    };
    return skin(A.dialog({ icon: 'sliders', title: 'Settings for ' + s.name, width: A.DIALOG_W, enter: 'done',
      body: '<div class="b-ss"><h3>This computer here</h3>' + row('Name', 'Only changes how it is called in this app', '<span class="b-f" style="width:12.5rem"><input id="v5sn" value="' + esc(s.name) + '" aria-label="Name" spellcheck="false"></span>') + '<span class="err" id="v5sne"></span>' +
        row('Kind of device', 'Hides the options that do not fit it', kindSeg('Kind of device', T.kind(s))) +
        row('Connect when RFE starts', '', sw('connect', 'Connect when RFE starts')) + row('Show a notice when it goes offline', '', sw('notify', 'Show a notice when it goes offline')) +
        '<h3>Files</h3>' + row('Downloads from it go to', '', '<span class="b-path"></span><button type="button" class="k-btn sm" data-chg="1">Change…</button>', 'folder') +
        row('Show hidden files', 'Files whose names start with a dot', sw('hidden', 'Show hidden files')) + row('Ask before replacing a file', '', sw('ask', 'Ask before replacing a file')) +
        '<h3>Speed</h3>' + row('Use at most', 'Leaves room for other things on a slow connection', seg('limit', 'Use at most', [[0, 'No limit'], [10, '10 MB/s'], [2, '2 MB/s']])) + row('Transfers at the same time', '', seg('parallel', 'Transfers at the same time', [[1, '1'], [2, '2'], [4, '4']])) +
        '<h3 data-lack="wake">Waking it up</h3>' + row('Wake it when it is asleep', 'Works on the home network if the PC allows it', sw('wake', 'Wake it when it is asleep'), 'wake') +
        '<h3>Signed in</h3>' + row(signed ? 'As ' + esc(s.user || 'this device') : 'Not signed in', signed ? 'With the account password' : 'Sign in to see its files', '<button type="button" class="k-btn sm" data-so="1">' + (signed ? 'Sign out' : 'Sign in…') + '</button>') + '</div>' +
        tech([['Address', s.addr || s.host], ['Prefer', s.via || '—'], ['Key', shortKey(s.fp) || '—']]),
      actions: [{ id: 'done', label: 'Done', kind: 'f', cb: (c) => {
        const name = $('#v5sn', c.el).value.trim(), er = $('#v5sne', c.el);
        if (!name) { er.textContent = 'Give it a name'; return false; }
        if (name !== s.name) { if (E.servers.some((x) => x.id !== s.id && x.name.toLowerCase() === name.toLowerCase())) { er.textContent = 'A computer with this name already exists'; return false; } E.updateServer(s.id, { name }).then(() => { A.snack('Saved ' + name); A.renderAll(); }).catch((e) => A.snack(e.message, { error: true })); }
      } }],
      onOpen: (c) => { paint(c.el); const nm = $('#v5sn', c.el); if (nm) { nm.focus(); nm.setSelectionRange(nm.value.length, nm.value.length); } c.el.addEventListener('click', (e) => { press(c, e); if (!e.target.closest('[data-so]')) return; c.close(); if (signed) E.signOut(s.id).then(() => { A.snack('Signed out of ' + s.name); A.renderAll(); }).catch((er) => A.snack('Could not sign out of ' + s.name + ': ' + (er && er.message || String(er)), { error: true })); else A.signInDialog(s); }); } }), { width: A.DIALOG_W });
  };

  /* The add dialog is the mockup's three-step box in the v5 window; a saved computer (rename) keeps its dialog. */
  const oldServerDialog = A.serverDialog;
  A.serverDialog = (s, pre) => (T.on() && !s ? addStep1(pre) : oldServerDialog(s, pre));
});
