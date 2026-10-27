/* RFE Tonal - UI core: helpers, overlays, file pane, details, transfer cards. */
(function () {
  'use strict';
  const E = Engine, U = E.util, S = E.settings, R = RFE;
  const A = (window.A = { E, U, S, state: { view: 'files', sideTab: 'details', sheetOpen: false, sheetTab: 'active', q: '', histFilter: 'all', drag: null } });
  /* One rem on screen, as a multiple of 16 px: the page scales with the window, so measurements in px are taken against it. */
  let unit = 0; A.u = () => unit || (unit = (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16) / 16); A.uReset = () => { unit = 0; };
  addEventListener('resize', A.uReset);
  /* The root size follows the window, so a resize changes every rem length at once; transitions would play that change as a lag. */
  let resizeIdle = 0;
  addEventListener('resize', () => { document.documentElement.classList.add('winsize'); clearTimeout(resizeIdle); resizeIdle = setTimeout(() => document.documentElement.classList.remove('winsize'), 200); });
  const $ = (s, r) => (r || document).querySelector(s);
  const $$ = (s, r) => Array.from((r || document).querySelectorAll(s));
  const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const ic = (n, o) => R.icon(n, o);
  const fic = (n) => R.fileIcon(n);
  A.$ = $; A.$$ = $$; A.esc = esc; A.ic = ic; A.fic = fic;
  const hostName = (h) => (h === 'local' ? 'This computer' : (E.server(h) || { name: h }).name);
  A.hostName = hostName;
  const ago = (ms) => { const s = Math.max(0, Math.round((E.now() - ms) / 1000)); if (s < 5) return 'just now'; if (s < 60) return s + ' s ago'; if (s < 3600) return Math.floor(s / 60) + ' min ago'; if (s < 86400) return Math.floor(s / 3600) + ' h ago'; return Math.floor(s / 86400) + ' d ago'; };
  A.ago = ago;
  const hl = (name, q) => { const i = q ? name.toLowerCase().indexOf(q.toLowerCase()) : -1; return '<span data-nolocal dir="auto" title="' + esc(name) + '">' + (i < 0 ? esc(name) : esc(name.slice(0, i)) + '<mark>' + esc(name.slice(i, i + q.length)) + '</mark>' + esc(name.slice(i + q.length))) + '</span>'; };
  A.hl = hl;
  const itemSize = (n) => (n.t === 'dir' ? (n.cc == null && !n.kids ? '—' : E.fs.itemsLabel(n)) : U.fmtBytes(n.b));
  A.itemSize = itemSize;

  /* ---------- overlays ---------- */
  const layer = () => $('#layer');
  let snackTimer = null;
  A.snack = (msg, o) => {
    o = o || {};
    /* An Undo that is still on offer stays: the new message stacks above it instead of replacing it. */
    const olds = $$('.snack', layer()); const keep = !o.action && olds.find((x) => x.querySelector('.sna')); olds.forEach((x) => { if (x !== keep) { clearTimeout(x._t); x.remove(); } });
    const el = document.createElement('div'); el.className = 'snack' + (o.error ? ' err' : '');
    /* Announced through a live region that is always in the page: a region created together with its text is often not read. */
    const lr = document.getElementById(o.error ? 'liveA' : 'liveP'); if (lr) { lr.textContent = ''; setTimeout(() => { lr.textContent = msg + (o.action ? '. ' + o.action + ' is available' : ''); }, 60); }
    el.innerHTML = (o.error ? ic('alert-circle') : '') + '<span>' + esc(msg) + '</span>' + (o.action ? '<button type="button" class="sna" data-a="1">' + esc(o.action) + '</button>' : '') + '<button type="button" class="ib sm" data-x="1" aria-label="Dismiss">' + ic('x', { size: 18 }) + '</button>';
    el.addEventListener('click', (e) => { const t = e.target.closest('button'); if (!t) return; if (t.dataset.a && o.onAction) o.onAction(); if (el.contains(document.activeElement)) { const rw = document.querySelector('[data-rows]'); if (rw) rw.focus({ preventScroll: true }); } el.remove(); });
    /* It stays while the pointer or the keyboard is on it, and an Undo gets longer (WCAG 2.2.1). */
    const arm = () => { clearTimeout(el._t); el._t = setTimeout(() => el.remove(), o.timeout || (o.action ? 10000 : 6000)); };
    el.addEventListener('mouseenter', () => clearTimeout(el._t)); el.addEventListener('mouseleave', arm);
    el.addEventListener('focusin', () => clearTimeout(el._t)); el.addEventListener('focusout', arm);
    let stack = layer().querySelector('.snacks'); if (!stack) { stack = document.createElement('div'); stack.className = 'snacks'; layer().appendChild(stack); } stack.appendChild(el); arm();
  };
  /* A short fade-and-rise for content that has just been swapped in. */
  A.dialog = (o) => {
    closeMenus();
    /* The tabs of "add a computer" are one dialog: the open backdrop stays and only the box in it is swapped, in one step, so no frame is ever drawn without it. */
    const reuse = A.dialogReuse && A.dialogReuse.sc.isConnected ? A.dialogReuse : null; A.dialogReuse = null;
    const sc = reuse ? reuse.sc : document.createElement('div'); if (!reuse) sc.className = 'scrim';
    const d = document.createElement('div'); d.className = 'dlg ' + (o.cls || ''); if (/\bconn\b/.test(o.cls || '')) sc.classList.add('top'); /* the add-a-computer tabs share one top edge, so the tab bar stays put while the box changes height */ d.style.setProperty('--w', (o.width || 480) / 16 + 'rem');
    d.setAttribute('role', 'dialog'); d.setAttribute('aria-modal', 'true'); const hid = 'dlgh' + (A.dlgN = (A.dlgN || 0) + 1); d.setAttribute('aria-labelledby', hid);
    d.innerHTML = (o.icon ? '<span class="di">' + ic(o.icon) + '</span>' : '') + '<h2 id="' + hid + '"' + (o.nolocal ? ' data-nolocal' : '') + '>' + esc(o.title) + '</h2><div class="db">' + (o.body || '') + '</div><div class="df"></div>';
    const df = $('.df', d);
    const opener = reuse ? reuse.opener : document.activeElement;
    const close = () => { sc.remove(); document.removeEventListener('keydown', onKey, true); if (o.onClose) o.onClose(); if (opener && opener !== document.body && opener.isConnected && typeof opener.focus === 'function' && !$('.scrim .dlg')) opener.focus(); };
    const ctl = { el: d, sc, opener, close, btn: {}, retire: () => { document.removeEventListener('keydown', onKey, true); sc.removeEventListener('mousedown', onDown); if (o.onClose) o.onClose(); } };
    for (const a of o.actions || []) {
      const b = document.createElement('button'); b.className = 'btn ' + (a.kind || 'tx'); b.innerHTML = (a.icon ? ic(a.icon) : '') + esc(a.label); if (a.id) { b.dataset.id = a.id; ctl.btn[a.id] = b; }
      b.addEventListener('click', () => { const r = a.cb ? a.cb(ctl) : undefined; if (r !== false) close(); }); df.appendChild(b);
    }
    const isTop = () => { const all = layer().querySelectorAll('.scrim'); return all[all.length - 1] === sc; };
    const onKey = (e) => {
      if (!isTop()) return;
      if (e.key === 'Escape') { e.stopPropagation(); if (o.beforeClose && o.beforeClose() === false) return; close(); }
      else if (e.key === 'Tab' && sc.isConnected) {
        /* The dialog is modal: Tab and Shift+Tab stay inside it. */
        const f = $$('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])', d).filter((x) => x.offsetParent !== null);
        if (!f.length) { e.preventDefault(); return; }
        const at = document.activeElement;
        if (!d.contains(at)) { e.preventDefault(); f[0].focus(); }
        else if (e.shiftKey && (at === f[0] || at === d)) { e.preventDefault(); f[f.length - 1].focus(); }
        else if (!e.shiftKey && at === f[f.length - 1]) { e.preventDefault(); f[0].focus(); }
      }
      else if (e.key === 'Enter' && o.enter && e.target.tagName !== 'TEXTAREA' && e.target.tagName !== 'BUTTON' && !e.target.closest('.menu')) { const b = $('[data-id="' + o.enter + '"]', d); if (b && !b.disabled) { e.preventDefault(); b.click(); } }
    };
    document.addEventListener('keydown', onKey, true);
    sc._ctl = ctl;
    const onDown = (e) => { if (e.target === sc && !o.modal) { if (o.beforeClose && o.beforeClose() === false) return; close(); } };
    sc.addEventListener('mousedown', onDown);
    if (reuse) { reuse.retire(); sc.classList.add('still'); sc.replaceChild(d, reuse.el); } else { sc.appendChild(d); layer().appendChild(sc); }
    if (o.onOpen) o.onOpen(ctl);
    const f = $('input,select', d); if (f && !o.noFocus) { f.focus(); if (f.select && f.type === 'text') f.select(); }
    else if (!o.noFocus) { const b = $('.df .btn.f', d) || $('.df .btn', d); if (b) b.focus(); } /* the safe main action, never a destructive one: a delete dialog starts on Cancel */
    return ctl;
  };
  function closeMenus() { $$('.menu', layer()).forEach((m) => m.remove()); }
  A.closeMenus = closeMenus;
  A.menu = (x, y, items, o) => {
    o = o || {}; closeMenus();
    const m = document.createElement('div'); m.className = 'menu ' + (o.cls || ''); m.setAttribute('role', 'menu');
    m.innerHTML = items.map((it, i) => it === '-' ? '<hr>' : it.head ? '<div class="mh">' + esc(it.head) + '</div>' : '<div class="mi ' + (it.danger ? 'danger ' : '') + (it.disabled ? 'dis' : '') + '" data-i="' + i + '" role="' + (it.radio === undefined ? 'menuitem' : 'menuitemradio') + '"' + (it.radio === undefined ? '' : ' aria-checked="' + !!it.radio + '"') + (it.disabled ? ' aria-disabled="true"' : '') + '>' + (it.dot ? '<i class="sd" style="background:' + it.dot + '"></i>' : it.icon ? ic(it.icon) : '') + '<span>' + esc(it.label) + '</span>' + (it.kbd ? '<kbd>' + esc(it.kbd) + '</kbd>' : '') + (it.sub ? '<small>' + esc(it.sub) + '</small>' : '') + '</div>').join('');
    layer().appendChild(m);
    const r = m.getBoundingClientRect(); m.style.left = Math.max(8, Math.min(x, innerWidth - r.width - 8)) + 'px'; m.style.top = Math.max(8, Math.min(y, innerHeight - r.height - 8)) + 'px';
    m.addEventListener('click', (e) => { const mi = e.target.closest('.mi'); if (!mi) return; const it = items[+mi.dataset.i]; closeMenus(); if (it && it.onClick) it.onClick(); });
    /* Keyboard: the menu takes focus, Up/Down move between its items, Enter or Space chooses, Escape closes and gives focus back. */
    const opener = document.activeElement; const mis = $$('.mi:not(.dis)', m);
    /* The page may have redrawn while the menu was open: find the opener again by what names it. */
    const qa = (n, v) => '[' + n + '="' + String(v).replace(/["\\]/g, '\\$&') + '"]';
    const again = opener && opener.dataset && opener.dataset.fx ? qa('data-fx', opener.dataset.fx) : opener && opener.getAttribute && opener.getAttribute('aria-label') ? qa('aria-label', opener.getAttribute('aria-label')) : '';
    const refocus = () => { const t = opener && opener.isConnected ? opener : again ? $(again) : null; if (t && t !== document.body) t.focus(); };
    $$('.mi', m).forEach((x) => { x.tabIndex = -1; });
    m.addEventListener('keydown', (e) => {
      const at = mis.indexOf(document.activeElement);
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); e.stopPropagation(); if (mis.length) mis[(at + (e.key === 'ArrowDown' ? 1 : -1) + mis.length) % mis.length].focus(); }
      else if (e.key === 'Home' || e.key === 'End') { e.preventDefault(); if (mis.length) mis[e.key === 'Home' ? 0 : mis.length - 1].focus(); }
      else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); if (at >= 0) mis[at].click(); }
      else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); closeMenus(); refocus(); }
      else if (e.key === 'Tab') { e.preventDefault(); closeMenus(); refocus(); }
    });
    if (o.noFocus !== true && mis.length) mis[0].focus();
    return m;
  };
  document.addEventListener('mousedown', (e) => { if (!e.target.closest('.menu')) closeMenus(); }, true);

  /* ---------- selection helpers ---------- */
  /* what the server's settings say about changing files here, or '' */
  /* The list of an agent's allowed folders is not a folder: nothing can be created in it. */
  A.atVirtualRoot = (host, path) => { const sv = host === 'local' ? null : E.server(host); return !!sv && !!sv.virtualRoot && path === sv.virtualRoot; };
  A.readOnlyNote = (host) => { const sv = host === 'local' ? null : E.server(host); if (!sv) return ''; if (sv.readOnly) return 'The agent is read-only, so nothing here can be created, renamed or deleted.'; return sv.caps && sv.caps.modify === false ? 'This computer may look but not change files on this agent.' : ''; };
  A.confirmDelete = (host, dir, names, after) => {
    const nodes = names.map((n) => E.fs.get(host, U.join(dir, n))).filter(Boolean);
    const tot = nodes.reduce((a, n) => { const t = E.fs.total(n); return { b: a.b + t.bytes, f: a.f + t.files }; }, { b: 0, f: 0 });
    const lst = nodes.slice(0, 50).map((n) => '<li>' + fic(n) + '<span class="trunc" data-nolocal>' + esc(n.n) + '</span><small>' + itemSize(n) + '</small></li>').join('');
    A.dialog({
      icon: 'trash', cls: 'danger', title: (host === 'local' || S.trash === false ? 'Delete ' : 'Move ') + (names.length === 1 ? '“' + (names[0].length > 30 ? names[0].slice(0, 30) + '…' : names[0]) + '”' : names.length + ' items') + (host === 'local' || S.trash === false ? '?' : ' to Trash?'),
      body: '<div>' + (host === 'local' ? 'These will be deleted from this computer for good.' : (S.trash === false ? 'These will be deleted from <b>' + esc(hostName(host)) + '</b> for good.' : 'These move to Trash on <b>' + esc(hostName(host)) + '</b> and can be restored from Tools → Trash.')) + ' ' + tot.f.toLocaleString(U.lang()) + ' file' + (tot.f === 1 ? '' : 's') + ', ' + U.fmtBytes(tot.b) + '.</div><ul class="names">' + lst + '</ul>',
      enter: 'del', actions: [{ label: 'Cancel', kind: 'tx' }, { id: 'del', label: host === 'local' || S.trash === false ? 'Delete' : 'Move to Trash', kind: 'ed', icon: 'trash', cb: async () => { try { const rem = await E.fs.remove(host, dir, names); A.snack((host === 'local' || S.trash === false ? 'Deleted ' : 'Moved to Trash: ') + rem.length + (rem.length === 1 ? ' item' : ' items'), host === 'local' || S.trash === false ? {} : { action: 'Undo', onAction: async () => { try { await E.fs.restore(host, dir, rem); A.snack('Restored'); } catch (e) { A.snack(e.message, { error: true }); } } }); if (after) after(); } catch (e) { A.snack(e.message, { error: true }); } } }]
    });
  };

  /* ---------- the file pane ---------- */
  class Pane {
    static n = 0;
    constructor(id, host, o) {
      this.id = id; this.host = host; this.o = o || {}; this.compact = !!this.o.compact; this.paths = {}; this.hist = []; this.fwd = []; this.sel = new Set(); this.anchor = -1; this.cursor = -1; this.kb = false;
      this.sort = { key: 'name', dir: 1 }; this.filter = 'all'; this.q = ''; this.mode = 'list'; this.items = []; this.renaming = null; this.creating = false; this.editPath = false; this.loading = false; this.scroll = 0; this.cut = new Set();
      this.refreshItems();
    }
    get path() { return this.paths[this.host] || E.start(this.host); }
    set path(p) { this.paths[this.host] = p; }
    get rhb() { return this.compact ? Math.min(40, this.o.rh || 40) : Math.round(parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--rh')) * 16) || 46; } /* a row's height at the base size, in px */
    get rh() { return this.rhb * A.u(); } /* and as it is on screen now */
    online() { return E.connected(this.host); }
    refreshItems() {
      const on = this.online(); const l = on ? E.fs.list(this.host, this.path) : null; const stt = on ? E.fs.state(this.host, this.path) : 'none';
      this.loading = on && !l && stt !== 'error'; this.missing = on && stt === 'error'; this.err = this.missing ? E.fs.error(this.host, this.path) : null;
      this.items = l ? E.fs.view(l, { key: this.sort.key, dir: this.sort.dir, filter: this.filter, q: this.q }) : [];
      this.total = l ? l.length : 0;
      for (const n of Array.from(this.sel)) if (!this.items.find((x) => x.n === n)) this.sel.delete(n);
    }
    selected() { return this.items.filter((x) => this.sel.has(x.n)); }
    go(path, o) {
      o = o || {}; path = U.norm(path);
      if (o.push !== false && path !== this.path) { this.hist.push(this.path); this.fwd = []; }
      this.path = path; this.sel.clear(); this.anchor = -1; this.cursor = -1; this.q = ''; this.renaming = null; this.creating = false; this.scroll = 0;
      E.fs.drop(this.host, path); this.loadSim(); if (this.o.onNav) this.o.onNav(this); return true;
    }
    loadSim() { this.refreshItems(); this.render(); }
    up() { if (!U.isRoot(this.path) || this.path !== '/' && E.server(this.host) && E.server(this.host).virtualRoot) this.go(U.isRoot(this.path) ? '/' : U.parentOf(this.path)); }
    back() { if (this.hist.length) { this.fwd.push(this.path); this.go(this.hist.pop(), { push: false }); } }
    forward() { if (this.fwd.length) { this.hist.push(this.path); this.go(this.fwd.pop(), { push: false }); } }
    setHost(h) { this.host = h; this.hist = []; this.fwd = []; this.sel.clear(); this.anchor = -1; this.cursor = -1; this.q = ''; this.renaming = null; this.creating = false; this.scroll = 0; this.loadSim(); if (this.o.onNav) this.o.onNav(this); }
    refresh(spin) { if (spin && this.o.onSpin) this.o.onSpin(); this.refreshItems(); this.render(); if (spin && this.online()) E.fs.refresh(this.host, this.path).catch(() => {}); /* read the folder again, not the copy kept */ }
    open(n) { if (n.t === 'dir') this.go(n.path || U.join(this.path, n.n)); else this.preview(n); }
    selChanged() { this.renderRows(); this.renderActions(); this.renderFoot(); if (this.o.onSelect) this.o.onSelect(this); }
    /* ----- render ----- */
    mount(el) {
      this.el = el; el.classList.add('pane'); el.classList.toggle('compact', this.compact); el.dataset.pane = this.id;
      if (!el._bound) { el._bound = true; this.bind(el); }
      this.render();
    }
    crumbs() {
      const sg = U.segs(this.path); const root = this.host === 'local' ? 'This computer' : hostName(this.host);
      if (this.editPath) return '<input class="pathin" value="' + esc(this.path) + '" spellcheck="false" aria-label="Path">';
      const top = U.upTo(this.path, 0); /* a drive or share root (C:\\, \\\\nas\\data\\) is a step of its own; '/' is the root crumb */
      let parts = [{ n: root, p: '/' }].concat(top !== '/' ? [{ n: top, p: top }] : [], sg.map((s, i) => ({ n: s, p: U.upTo(this.path, i + 1) })));
      let pre = '';
      const max = this.compact ? 3 : 6;
      if (parts.length > max) { pre = '<span class="c" data-p="' + esc(parts[parts.length - max - 1].p) + '" title="Show hidden path">…</span><i>' + ic('chevron-right') + '</i>'; parts = parts.slice(-max); }
      return pre + parts.map((p) => '<span class="c" data-p="' + esc(p.p) + '">' + esc(p.n) + '</span>').join('<i>' + ic('chevron-right') + '</i>');
    }
    render() {
      if (!this.el) return; const el = this.el;
      /* A redraw replaces every control: the focused one is found again afterwards so the keyboard keeps its place. */
      const fa = document.activeElement; let back = '';
      if (fa && fa !== document.body && el.contains(fa)) { const ks = ['data-filter', 'data-sort', 'data-act', 'data-rows', 'data-mode', 'data-view']; for (const k of ks) { const v = fa.getAttribute && fa.getAttribute(k); if (v !== null && v !== undefined) { back = '[' + k + (v ? '="' + v + '"' : '') + ']'; break; } } }
      try { this.renderInner(); } finally { if (back && !this.editPath) { const n = $(back, el); if (n) n.focus({ preventScroll: true }); } }
    }
    renderInner() {
      const el = this.el;
      if (!this.online()) { el.innerHTML = this.stateHTML(); return; }
      const name = this.path === '/' ? hostName(this.host) : U.baseOf(this.path);
      const head = this.compact
        ? '<div class="lh"><div class="nav"><button class="ib sm" data-act="up" title="Up" aria-label="Up">' + ic('arrow-up') + '</button></div><div class="crumb" data-crumb>' + this.crumbs() + '</div><button class="ib sm" data-act="edit-path" title="Edit path" aria-label="Edit path">' + ic('edit') + '</button><button class="ib sm" data-act="refresh" title="Refresh" aria-label="Refresh">' + ic('refresh') + '</button></div>'
        : '<div class="lh"><div class="nav"><button class="ib sm" data-act="back" title="Back (Alt+←)" aria-label="Back">' + ic('arrow-left') + '</button><button class="ib sm" data-act="forward" title="Forward (Alt+→)" aria-label="Forward">' + ic('arrow-right') + '</button><button class="ib sm" data-act="up" title="Up (Backspace)" aria-label="Up">' + ic('arrow-up') + '</button></div><h1>' + esc(name) + '</h1><div class="crumb" data-crumb>' + this.crumbs() + '</div><button class="ib sm star' + (A.isFav && A.isFav(this.host, this.path) ? ' on' : '') + '" data-act="fav" title="' + (A.isFav && A.isFav(this.host, this.path) ? 'Remove from favorites' : 'Add folder to favorites') + '" aria-label="Favorite folder">' + ic('star') + '</button><button class="ib sm" data-act="edit-path" title="Edit path (Ctrl+L)" aria-label="Edit path">' + ic('edit') + '</button><span data-slot="act" style="display:contents"></span></div>';
      const F = [['all', 'All'], ['folders', 'Folders'], ['packages', 'Packages'], ['media', 'Media'], ['archives', 'Archives'], ['big', 'Over 100 MB']];
      const chips = '<div class="chips">' + (this.compact ? '' : F.map((f) => '<button class="chip' + (this.filter === f[0] ? ' on' : '') + '" aria-pressed="' + (this.filter === f[0]) + '" data-filter="' + f[0] + '">' + (this.filter === f[0] ? ic('check') : '') + f[1] + '</button>').join('')) + '<span class="sp"></span><span class="ctl"><label class="mini">' + ic('filter') + '<input data-q placeholder="Filter this folder" value="' + esc(this.q) + '" aria-label="Filter this folder"></label>' + (this.compact ? '' : '<button class="chip" data-act="sortmenu">' + ic('sort') + { name: 'Name', size: 'Size', mod: 'Modified', kind: 'Type' }[this.sort.key] + '</button><button class="chip" data-act="mode" title="List or grid">' + ic(this.mode === 'list' ? 'list' : 'grid') + (this.mode === 'list' ? 'List' : 'Grid') + '</button>') + '</span></div>';
      const as = (k) => (this.sort.key === k ? ' aria-sort="' + (this.sort.dir === 1 ? 'ascending' : 'descending') + '"' : '');
      const arrow = (k) => (this.sort.key === k ? ic(this.sort.dir === 1 ? 'arrow-up' : 'arrow-down') : '');
      const cols = '<div class="cols"><span></span><span data-sort="name" tabindex="0" role="button"' + as('name') + '>Name ' + arrow('name') + '</span><span class="r" data-sort="size" tabindex="0" role="button"' + as('size') + '>Size ' + arrow('size') + '</span><span class="hm" data-sort="mod" tabindex="0" role="button"' + as('mod') + ' style="padding-left:1rem">Modified ' + arrow('mod') + '</span><span class="hm">Permissions</span><span class="hm"></span></div>';
      el.innerHTML = head + chips + (this.mode === 'list' ? cols : '') + '<div class="rows' + (this.mode === 'grid' ? ' grid' : '') + '" data-rows tabindex="0" role="grid" aria-multiselectable="true" aria-label="Files"><div class="vs"></div></div><div class="pfoot"></div>';
      this.renderRows(); this.renderActions(); this.renderFoot();
      const rows = $('[data-rows]', el); rows.scrollTop = this.scroll;
      if (this.editPath) { const pi = $('.pathin', el); pi.focus(); pi.select(); }
    }
    stateHTML() {
      const s = E.server(this.host); const st = s ? s.state : 'disconnected';
      const wrap = (cls, icon, h, p, row, extra) => '<div class="es ' + cls + '"><div class="box"><div class="ico">' + ic(icon) + '</div><h3>' + h + '</h3><p>' + p + '</p>' + (extra || '') + '<div class="row">' + row + '</div></div></div>';
      const B = (a, l, k, i) => '<button class="btn ' + k + '" data-act="' + a + '">' + (i ? ic(i) : '') + l + '</button>';
      const head = '<div class="lh"><h1>' + esc(hostName(this.host)) + '</h1></div>';
      const xs = A.stateExtra && A.stateExtra(s, st, head, wrap, B); if (xs) return xs;
      if (st === 'lost') return head + wrap('bad', 'wifi-off', 'Connection lost', 'Lost the connection to <b>' + esc(s.name) + '</b> ' + Math.round((E.now() - s.lostAt) / 1000) + ' s ago. ' + (S.autoReconnect ? 'Reconnecting in <b>' + Math.max(0, Math.ceil(s.retryIn)) + ' s</b>.' : 'Auto-reconnect is off.') + ' Transfers to this server are paused and will resume.', B('retry', 'Retry now', 'f', 'refresh') + B('servers', 'Servers', 'tx', 'server'));
      if (st === 'offline') return head + wrap('bad', 'power', 'Server offline', 'The PC did not answer on <b>' + esc(s.host) + ':' + s.port + '</b>. Check that it is powered on and on this network. Last seen ' + (s.lastSeen ? ago(s.lastSeen) : 'never') + '.', B('retry', 'Try again', 'f', 'refresh') + B('diagnose', 'Diagnose', 'tx', 'activity') + B('edit', 'Edit connection', 'tx', 'edit'));
      if (st === 'connecting') return head + '<div class="es"><div class="conncard">' + A.serverCard(s).replace(/data-sa="(\w+)" data-id="[^"]*"/g, 'data-act="$1"').replace(/<button class="ib sm" data-act="more"[^>]*>.*?<\/button>/, '') + '</div></div>';
      if (st === 'trust') return head + wrap('warn', 'shield', 'Check this server’s key', 'First connection to <b>' + esc(s.name) + '</b>. Nothing is sent until you have compared its key with the one on that computer.', B('cancel', 'Cancel', 'tx') + B('trust', 'Review key…', 'f', 'shield-check'));
      return head + wrap('', 'plug', 'Not connected', '<b>' + esc(hostName(this.host)) + '</b> · ' + esc(s ? s.host : '') + '. Connect to browse and transfer files.', B('connect', 'Connect', 'f', 'plug') + B('servers', 'Servers', 'tx', 'server'));
    }
    renderActions() {
      const slot = $('[data-slot="act"]', this.el); if (!slot) return; const sel = this.selected();
      let h = '';
      if (this.host !== 'local' && sel.length) h += '<button class="btn f sm" data-act="download">' + ic('download') + 'Download ' + (sel.length > 1 ? sel.length : '') + '</button>';
      h += '<button class="btn t sm" data-act="newfolder"' + (A.readOnlyNote(this.host) || A.atVirtualRoot(this.host, this.path) ? ' disabled title="Open one of the folders first"' : '') + '>' + ic('folder-plus') + 'New folder</button>';
      slot.innerHTML = h;
    }
    renderFoot() {
      const f = $('.pfoot', this.el); if (!f) return; const sel = this.selected(); const sv = E.server(this.host); const cp = E.fs.cached(this.host, this.path); const ro = A.readOnlyNote(this.host); const free = this.host !== 'local' && sv && sv.disk[1] > 0 ? U.fmtBytes((sv.disk[1] - sv.disk[0]) * 1e9) + ' free' : '';
      const bytes = sel.reduce((a, n) => a + (n.t === 'dir' ? 0 : n.b), 0);
      f.innerHTML = '<span><b>' + this.items.length.toLocaleString(U.lang()) + '</b> ' + (this.q || this.filter !== 'all' ? 'of ' + this.total.toLocaleString(U.lang()) + ' ' : '') + (this.items.length === 1 && !this.q && this.filter === 'all' ? 'item' : 'items') + '</span>' + (sel.length ? '<span><b>' + sel.length + '</b> selected' + (bytes ? ' · ' + U.fmtBytes(bytes) : '') + '</span>' : '') + '<span class="sp"></span>' + (cp && cp.more ? '<span class="ro">' + ic('alert-circle') + 'The agent lists more items than are shown.</span>' : '') + (ro ? '<span class="ro">' + ic('lock') + esc(ro) + '</span>' : '') + '<span>' + free + '</span>';
    }
    renderRows() {
      const rows = $('[data-rows]', this.el); if (!rows) return; const vs = $('.vs', rows);
      if (this.loading) { vs.style.height = 'auto'; vs.innerHTML = '<div class="skel">' + '<i></i>'.repeat(10) + '</div>'; return; }
      if (this.missing) { vs.style.height = 'auto'; vs.innerHTML = '<div class="es warn"><div class="box"><div class="ico">' + ic('folder') + '</div><h3>' + (this.err && /denied|permission|forbidden/i.test(this.err.message || '') ? 'No access to this folder' : this.err && /network|connect|timed? ?out|offline/i.test(this.err.message || '') ? 'Can’t reach the server' : 'Can’t open this folder') + '</h3><p>' + esc(this.err && this.err.message || ('Could not list ' + this.path + '.')) + '</p><div class="row">' + (U.isRoot(this.path) ? '' : '<button class="btn f" data-act="up">Go up</button>') + '<button class="btn" data-act="refresh">Try again</button></div></div></div>'; return; }
      const n = this.items.length;
      const sv0 = E.server(this.host);
      if (!n && sv0 && this.host !== 'local' && sv0.accessDenied) { vs.style.height = 'auto'; vs.innerHTML = '<div class="es warn"><div class="box"><div class="ico">' + ic('lock') + '</div><h3>No folder is open to this login</h3><p>The agent shows this computer nothing to browse. Ask the owner of the PC to allow a folder for this login.</p><div class="row"><button class="btn tx" data-act="diagnose">' + ic('activity') + 'Diagnose</button></div></div></div>'; return; }
      if (!n && !this.creating) {
        vs.style.height = 'auto';
        const filtered = this.q || this.filter !== 'all';
        vs.innerHTML = '<div class="es"><div class="box"><div class="ico">' + ic(filtered ? 'search' : 'folder-open') + '</div><h3>' + (filtered ? 'Nothing matches' : 'This folder is empty.') + '</h3><p>' + (filtered ? 'No items match the current filter in this folder.' : this.host === 'local' ? 'Create a folder or drop files here.' : 'Drop files here to upload them to <b>' + esc(hostName(this.host)) + '</b>, or create a folder.') + '</p><div class="row">' + (filtered ? '<button class="btn t" data-act="clearfilter">Clear filter</button>' : '<button class="btn t" data-act="newfolder">' + ic('folder-plus') + 'New folder</button>') + '</div></div></div>';
        return;
      }
      if (this.mode === 'grid') {
        rows.classList.add('grid'); vs.style.height = 'auto'; const cap = this.items.slice(0, 600);
        vs.innerHTML = cap.map((it, i) => '<div role="row" id="' + this.rid(i) + '" aria-selected="' + this.sel.has(it.n) + '" class="tile' + (this.sel.has(it.n) ? ' sel' : '') + (this.kb && this.cursor === i ? ' cur' : '') + '" data-i="' + i + '" draggable="true"><button class="tck" data-lead tabindex="-1" title="Select" aria-label="Select ' + esc(it.n) + '">' + ic('check') + '</button><div class="big">' + fic(it) + '</div><b>' + hl(it.n, this.q) + '</b><small>' + itemSize(it) + '</small></div>').join('') + (n > 600 ? '<div style="grid-column:1/-1;padding:.75rem;color:var(--on-var)">Showing the first 600 of ' + n.toLocaleString(U.lang()) + '. Use the list view for the full folder.</div>' : ''); this.activeDesc(rows);
        return;
      }
      rows.classList.remove('grid'); const rh = this.rh, rb = this.rhb; const off = this.creating ? 1 : 0; vs.style.height = (n + off) * rb / 16 + 'rem';
      const st = rows.scrollTop, vh = rows.clientHeight || 600; const a = Math.max(0, Math.floor(st / rh) - 6), b = Math.min(n + off, Math.ceil((st + vh) / rh) + 6);
      let h = '';
      for (let r = a; r < b; r++) {
        if (this.creating && r === 0) { h += '<div class="li" style="top:0;height:' + rb / 16 + 'rem"><div class="lead">' + ic('folder-plus') + '</div><div class="t newrow"><input class="rn" data-new value="New folder" spellcheck="false" aria-label="New folder name"><span class="rerr" data-err></span></div></div>'; continue; }
        const i = r - off; const it = this.items[i]; const sel = this.sel.has(it.n); const rn = this.renaming === it.n;
        const nameCell = rn ? '<div class="t newrow"><input class="rn" data-rn value="' + esc(it.n) + '" spellcheck="false" aria-label="New name"><span class="rerr" data-err></span></div>' : '<div class="t" role="gridcell"><b>' + hl(it.n, this.q) + (!E.fs.canRead(it) ? '<span class="lk">' + esc(it.own) + ' only</span>' : '') + '</b></div>';
        h += '<div role="row" id="' + this.rid(i) + '" aria-selected="' + !!sel + '" class="li' + (sel ? ' sel' : '') + (this.cut.has(it.n) ? ' cut' : '') + (this.kb && this.cursor === i ? ' cur' : '') + '" data-i="' + i + '" draggable="' + (rn ? 'false' : 'true') + '" style="top:' + r * rb / 16 + 'rem;height:' + rb / 16 + 'rem"><div class="lead" data-lead title="Select">' + (sel ? ic('check') : fic(it)) + '</div>' + nameCell + '<div class="s" role="gridcell">' + itemSize(it) + '</div><div class="m hm" role="gridcell">' + U.fmtDate(it.mod) + '</div><div class="p hm" role="gridcell">' + esc(it.perm) + '</div><div class="qa hm">' + (this.compact ? '' : '<button class="ib sm" data-q="' + (this.host === 'local' ? 'upload' : 'download') + '" title="' + (this.host === 'local' ? 'Upload' : 'Download') + '" aria-label="Transfer">' + ic(this.host === 'local' ? 'upload' : 'download') + '</button><button class="ib sm" data-q="rename" title="Rename" aria-label="Rename">' + ic('edit') + '</button><button class="ib sm" data-q="more" title="More" aria-label="More">' + ic('more-v') + '</button>') + '</div></div>';
      }
      const edit = this._keepEdit ? $('input.rn', vs) : null; const editVal = edit ? edit.value : null; if (edit) edit._skip = true;
      vs.innerHTML = h; this.activeDesc(rows);
      if (editVal !== null) { const ni = $('input.rn', vs); if (ni) { ni._f = 1; ni.value = editVal; ni.focus({ preventScroll: true }); ni.setSelectionRange(editVal.length, editVal.length); } }
      const inp = $('input.rn', vs); if (inp && !inp._f) { inp._f = 1; inp.focus(); const dot = inp.value.lastIndexOf('.'); inp.setSelectionRange(0, this.renaming && dot > 0 && !(E.fs.get(this.host, U.join(this.path, this.renaming)) || {}).kids ? dot : inp.value.length); }
    }
    /* ----- interactions ----- */
    rid(i) { return (this.uid || (this.uid = 'pn' + (++Pane.n))) + 'r' + i; }
    activeDesc(rows) { const c = this.cursor; if (c >= 0 && rows.querySelector('[data-i="' + c + '"]')) rows.setAttribute('aria-activedescendant', this.rid(c)); else rows.removeAttribute('aria-activedescendant'); }
    cols() { const t = $$('.tile', this.el); if (!t.length) return 1; const top = t[0].offsetTop; let c = 0; while (c < t.length && t[c].offsetTop === top) c++; return c; }
    setCursor(i) { this.cursor = i; this.kb = true; this.renderRows(); }
    selectIdx(i, e) {
      this.lead = -1; /* the keyboard sets it again after a Shift+Arrow step */
      const it = this.items[i]; if (!it) return; this.cursor = i; this.kb = !!(e && e.type === 'keydown');
      if (e && e.shiftKey && this.anchor >= 0) { const [a, b] = [Math.min(this.anchor, i), Math.max(this.anchor, i)]; this.sel.clear(); for (let k = a; k <= b; k++) this.sel.add(this.items[k].n); }
      else if (e && (e.ctrlKey || e.metaKey || e.lead)) { this.sel.has(it.n) ? this.sel.delete(it.n) : this.sel.add(it.n); this.anchor = i; }
      else { this.sel.clear(); this.sel.add(it.n); this.anchor = i; }
      this.selChanged();
    }
    selectAll() { const l = this.mode === 'grid' ? this.items.slice(0, 600) : this.items; /* the grid draws at most 600 tiles: only what is shown is selected */ this.sel = new Set(l.map((x) => x.n)); this.selChanged(); }
    startRename(n) { if (!n) return; this.renaming = n.n; if (this.mode !== 'list') { this.mode = 'list'; this.render(); } else this.renderRows(); } /* the grid draws no input, so renaming shows the list */
    startNew() { if (A.atVirtualRoot(this.host, this.path)) return; this.creating = true; this.mode = 'list'; this.q = ''; this.filter = 'all'; this.refreshItems(); this.render(); const rows = $('[data-rows]', this.el); if (rows) rows.scrollTop = 0; }
    async commitRename(inp) {
      if (this.renameBusy) return; /* a second Enter while the first is still being sent */
      const old = this.renaming; const v = inp.value.trim(); const err = $('[data-err]', inp.parentNode);
      if (!v || v === old) { this.renaming = null; this.renderRows(); return; }
      this.renameBusy = true; try { await E.fs.rename(this.host, this.path, old, v); this.renaming = null; this.sel = new Set([v]); this.refreshItems(); this.render(); if (this.o.onSelect) this.o.onSelect(this); A.snack('Renamed to “' + v + '”'); }
      catch (e) { err.textContent = e.message; err.setAttribute('role', 'alert'); inp.setAttribute('aria-invalid', 'true'); inp.style.borderColor = 'var(--err)'; inp.focus(); } finally { this.renameBusy = false; }
    }
    async commitNew(inp) {
      if (this.newBusy) return; /* a second Enter while the first is still being sent */
      const v = inp.value.trim(); const err = $('[data-err]', inp.parentNode);
      if (!v) { this.creating = false; this.renderRows(); return; }
      this.newBusy = true; try { await E.fs.mkdir(this.host, this.path, v); this.creating = false; this.sel = new Set([v]); this.refreshItems(); this.render(); if (this.o.onSelect) this.o.onSelect(this); A.snack('Created folder “' + v + '”'); }
      catch (e) { err.textContent = e.message; err.setAttribute('role', 'alert'); inp.setAttribute('aria-invalid', 'true'); inp.style.borderColor = 'var(--err)'; inp.focus(); } finally { this.newBusy = false; }
    }
    preview(n) { A.previewDialog(this.host, this.path, n); }
    del() { const s = this.selected(); if (!s.length) return; A.confirmDelete(this.host, this.path, s.map((x) => x.n), () => { this.sel.clear(); this.refreshItems(); this.render(); if (this.o.onSelect) this.o.onSelect(this); }); }
    transfer() {
      const s = this.selected(); if (!s.length) return;
      A.transferSel(this, s.map((x) => x.n));
    }
    ctxItems(onItem) {
      const s = this.selected(); const one = s.length === 1 ? s[0] : null; const loc = this.host === 'local';
      if (!onItem) return [{ icon: 'folder-plus', label: 'New folder', kbd: 'Ctrl+Shift+N', onClick: () => this.startNew() }, { icon: 'refresh', label: 'Refresh', kbd: 'F5', onClick: () => this.refresh(true) }, '-', { icon: 'check', label: 'Select all', kbd: 'Ctrl+A', onClick: () => this.selectAll() }, { icon: 'copy', label: 'Copy folder path', onClick: () => A.copy(this.path) }];
      return [
        one && one.t === 'dir' ? { icon: 'folder-open', label: 'Open', kbd: 'Enter', onClick: () => this.open(one) } : one ? { icon: 'eye', label: 'Preview', kbd: 'Enter', onClick: () => this.preview(one) } : { icon: 'check-circle', label: s.length + ' selected', disabled: true },
        loc ? { icon: 'upload', label: 'Upload to ' + hostName(A.mainHost()), kbd: 'Ctrl+U', onClick: () => this.transfer(), disabled: A.mainHost() === 'local' } : { icon: 'download', label: 'Download', kbd: 'Ctrl+D', onClick: () => this.transfer() },
        '-',
        { icon: 'edit', label: 'Rename', kbd: 'F2', disabled: !one, onClick: () => this.startRename(one) },
        { icon: 'copy', label: 'Copy path', onClick: () => A.copy(one ? U.join(this.path, one.n) : s.map((x) => U.join(this.path, x.n)).join('\n')) },
        { icon: 'info', label: 'Properties', onClick: () => A.propsDialog(this.host, this.path, one) },
        '-',
        { icon: 'trash', label: 'Delete', kbd: 'Del', danger: true, onClick: () => this.del() }
      ];
    }
    bind(el) {
      const idx = (t) => { const r = t.closest('[data-i]:not(svg)'); return r ? +r.dataset.i : -1; };
      el.addEventListener('keydown', (e) => { const h = e.target.closest && e.target.closest('[data-sort]'); if (h && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); e.stopPropagation(); h.click(); } });
      el.addEventListener('click', (e) => {
        const t = e.target;
        const act = t.closest('[data-act]'); if (act) { this.act(act.dataset.act, e); return; }
        const q = t.closest('[data-q]'); if (q && q.tagName === 'BUTTON') { const i = idx(t); if (i >= 0) { if (!this.sel.has(this.items[i].n)) { this.sel = new Set([this.items[i].n]); this.selChanged(); } if (q.dataset.q === 'more') { const r = q.getBoundingClientRect(); A.menu(r.left - 150, r.bottom, this.ctxItems(true)); } else if (q.dataset.q === 'rename') this.startRename(this.items[i]); else this.transfer(); } return; }
        const c = t.closest('.crumb .c'); if (c) { this.go(c.dataset.p); return; }
        const f = t.closest('[data-filter]'); if (f) { this.cursor = -1; this.anchor = -1; this.filter = f.dataset.filter; this.refreshItems(); this.sel.clear(); this.render(); if (this.o.onSelect) this.o.onSelect(this); return; }
        const so = t.closest('[data-sort]'); if (so) { this.cursor = -1; this.anchor = -1; const k = so.dataset.sort; this.sort = this.sort.key === k ? { key: k, dir: -this.sort.dir } : { key: k, dir: 1 }; this.refreshItems(); this.render(); return; }
        if (t.closest('input')) return;
        const row = t.closest('[data-i]:not(svg)');
        if (row) {
          const lead = t.closest('[data-lead]'); const i = +row.dataset.i; const keys = e.shiftKey || e.ctrlKey || e.metaKey;
          /* One click only opens (a folder goes in, a file is previewed, nothing gets selected); the round icon, Ctrl and Shift select. "Two clicks" in Settings turns that around. */
          if (lead || keys || S.openMode === 'double') this.selectIdx(i, lead ? { lead: true } : e);
          else { const it = this.items[i]; if (it && this.renaming == null && (it.t === 'dir' || !A.hasViewer || A.hasViewer(this.host, it))) this.open(it); else this.selectIdx(i, e); } /* it opens and nothing is selected; only a file with nothing to show is selected */
        }
        else if (t.closest('[data-rows]')) { if (this.sel.size) { this.sel.clear(); this.selChanged(); } }
      });
      el.addEventListener('dblclick', (e) => { if (S.openMode !== 'double') return; const row = e.target.closest('[data-i]:not(svg)'); if (row && !e.target.closest('input,button')) { const it = this.items[+row.dataset.i]; if (it) this.open(it); } });
      el.addEventListener('contextmenu', (e) => {
        if (!e.target.closest('[data-rows]')) return; e.preventDefault(); let row = e.target.closest('[data-i]:not(svg)');
        /* Shift+F10 or the Menu key: the target is the list itself, so the menu opens for the cursor (or selected) row, anchored on it. */
        if (!row && e.clientX === 0 && e.clientY === 0 && e.detail === 0) { const ci = this.cursor >= 0 ? this.cursor : this.sel.size ? this.items.findIndex((x) => this.sel.has(x.n)) : -1; const r = ci >= 0 ? $('[data-i="' + ci + '"]', this.el) : null; if (r) { row = r; const b = r.getBoundingClientRect(); A.setActive(this); if (!this.sel.has(this.items[ci].n)) { this.sel = new Set([this.items[ci].n]); this.anchor = ci; this.selChanged(); } A.menu(b.left + 40, b.bottom, this.ctxItems(true)); return; } }
        if (row) { const it = this.items[+row.dataset.i]; if (!this.sel.has(it.n)) { this.sel = new Set([it.n]); this.anchor = +row.dataset.i; this.selChanged(); } }
        else if (this.sel.size) { this.sel.clear(); this.selChanged(); }
        A.setActive(this); A.menu(e.clientX, e.clientY, this.ctxItems(!!row));
      });
      el.addEventListener('mousedown', () => A.setActive(this), true);
      el.addEventListener('input', (e) => { if (e.target.matches('[data-q]') && e.target.tagName === 'INPUT') { this.cursor = -1; this.anchor = -1; this.q = e.target.value; this.scroll = 0; const rows = $('[data-rows]', el); if (rows) rows.scrollTop = 0; this.refreshItems(); this.renderRows(); this.renderFoot(); } });
      el.addEventListener('keydown', (e) => {
        const t = e.target;
        if (t.matches('input.rn')) { if (e.key === 'Enter') { e.preventDefault(); t.hasAttribute('data-new') ? this.commitNew(t) : this.commitRename(t); } else if (e.key === 'Escape') { e.stopPropagation(); this.renaming = null; this.creating = false; this.renderRows(); } return; }
        if (t.matches('.pathin')) { if (e.key === 'Enter') { const p = t.value; this.editPath = false; if (!this.go(p)) this.render(); } else if (e.key === 'Escape') { e.stopPropagation(); this.editPath = false; this.render(); } return; }
      });
      el.addEventListener('focusout', (e) => {
        const t = e.target; if (t.matches && t.matches('.pathin')) { setTimeout(() => { if (this.editPath) { this.editPath = false; this.render(); } }, 120); }
        if (t.matches && t.matches('input.rn')) setTimeout(() => { if (t._skip || document.activeElement === t || !(this.renaming || this.creating)) return; /* clicking away keeps what was typed; an empty or unchanged name just closes the box */ const v = t.value.trim(); if (t.isConnected && v && !this.renameBusy && (this.creating || v !== this.renaming)) { this.creating ? this.commitNew(t) : this.commitRename(t); return; } this.renaming = null; this.creating = false; this.renderRows(); }, 150);
      });
      el.addEventListener('scroll', (e) => { if (e.target.matches && e.target.matches('[data-rows]')) { this.scroll = e.target.scrollTop; /* rows slide under a still pointer: no hover fades while it moves */ e.target.classList.add('scrolling'); clearTimeout(this._scEnd); const rows = e.target; this._scEnd = setTimeout(() => rows.classList.remove('scrolling'), 140); if (!this._raf) this._raf = requestAnimationFrame(() => { this._raf = 0; this._keepEdit = true; try { this.renderRows(); } finally { this._keepEdit = false; } }); } }, true);
      /* drag and drop */
      el.addEventListener('dragstart', (e) => {
        const row = e.target.closest('[data-i]:not(svg)'); if (!row) return; const it = this.items[+row.dataset.i]; if (!it) return;
        if (!this.sel.has(it.n)) { this.sel = new Set([it.n]); this.selChanged(); }
        const names = this.selected().map((x) => x.n); A.state.drag = { pane: this.id, host: this.host, dir: this.path, names };
        e.dataTransfer.effectAllowed = 'copy'; e.dataTransfer.setData('application/x-rfe', JSON.stringify(A.state.drag)); e.dataTransfer.setData('text/plain', names.join('\n'));
        const g = document.createElement('div'); g.style.cssText = 'position:fixed;left:-18.75rem;top:0;padding:.5rem .875rem;border-radius:1.25rem;background:var(--primary);color:var(--on-primary);font:500 .8125rem var(--font)'; g.textContent = names.length === 1 ? names[0] : names.length + ' items'; document.body.appendChild(g); e.dataTransfer.setDragImage(g, 10, 10); setTimeout(() => g.remove(), 0);
      });
      el.addEventListener('dragend', () => { A.state.drag = null; this.clearDrop(); });
      el.addEventListener('dragover', (e) => {
        const tgt = this.dropTarget(e); if (!tgt) { if (A.state.drag || (e.dataTransfer && Array.from(e.dataTransfer.types || []).includes('Files'))) { e.dataTransfer.dropEffect = 'none'; } return; }
        e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; this.clearDrop(); el.classList.add('dropz');
        const rows = $('[data-rows]', el); rows.dataset.drop = tgt.row ? 'Drop to ' + (this.host === 'local' ? 'copy into “' + tgt.name + '”' : 'upload into “' + tgt.name + '”') : (this.host === 'local' ? 'Drop to save in ' : 'Drop to upload to ') + hostName(this.host) + ':' + tgt.dir;
        if (tgt.row) { rows.classList.remove('dropz'); el.classList.remove('dropz'); tgt.row.classList.add('dropt'); }
      });
      el.addEventListener('dragleave', (e) => { if (!el.contains(e.relatedTarget)) this.clearDrop(); });
      el.addEventListener('drop', (e) => {
        const tgt = this.dropTarget(e); this.clearDrop(); if (!tgt) return; e.preventDefault();
        if (A.state.drag) { const d = A.state.drag; A.state.drag = null; A.dropTransfer(d, this.host, tgt.dir); }
      });
    }
    clearDrop() { this.el.classList.remove('dropz'); $$('.dropt', this.el).forEach((x) => x.classList.remove('dropt')); }
    dropTarget(e) {
      if (!this.online()) return null; const d = A.state.drag; const hasFiles = e.dataTransfer && Array.from(e.dataTransfer.types || []).includes('Files');
      if (!d && !hasFiles) return null;
      if (d && (d.host === this.host || (d.host !== 'local' && this.host !== 'local'))) return null;
      if (hasFiles && this.host === 'local') return null;
      const row = e.target.closest && e.target.closest('.li[data-i],.tile[data-i]'); let dir = this.path, name = '';
      if (row) { const it = this.items[+row.dataset.i]; if (it && it.t === 'dir' && !(d && d.pane === this.id && d.names.includes(it.n))) { dir = U.join(this.path, it.n); name = it.n; return { dir, name, row }; } }
      return { dir, name: '', row: null };
    }
    act(a, e) {
      switch (a) {
        case 'back': this.back(); break; case 'forward': this.forward(); break; case 'up': this.up(); break;
        case 'refresh': this.refresh(true); break;
        case 'fav': if (A.toggleFav) A.toggleFav(this.host, this.path, U.baseOf(this.path) || A.hostName(this.host), 'dir'); this.render(); break;
        case 'diagnose': if (A.diagnose) A.diagnose(this.host); break;
        case 'reverify': if (A.reverifyDialog) A.reverifyDialog(this.host); break;
        case 'edit-path': this.editPath = true; this.render(); break;
        case 'newfolder': this.startNew(); break;
        case 'download': this.transfer(); break;
        case 'clearfilter': this.q = ''; this.filter = 'all'; this.refreshItems(); this.render(); break;
        case 'mode': this.mode = this.mode === 'list' ? 'grid' : 'list'; this.render(); break;
        case 'sortmenu': { const r = e.target.closest('button').getBoundingClientRect(); const mk = (k, l) => ({ radio: this.sort.key === k, icon: this.sort.key === k ? 'check' : '', label: l, sub: this.sort.key === k ? ({ size: ['Smallest first', 'Largest first'], mod: ['Oldest first', 'Newest first'] }[k] || ['A→Z', 'Z→A'])[this.sort.dir === 1 ? 0 : 1] : '', onClick: () => { this.sort = this.sort.key === k ? { key: k, dir: -this.sort.dir } : { key: k, dir: 1 }; this.refreshItems(); this.render(); } }); A.menu(r.left, r.bottom + 4, [mk('name', 'Name'), mk('size', 'Size'), mk('mod', 'Modified'), mk('kind', 'Type')]); break; }
        case 'retry': E.retryNow(this.host); break; case 'connect': E.connect(this.host); break; case 'cancel': E.cancelConnect(this.host); break;
        case 'trust': { const ts = E.server(this.host); if (ts) A.trustDialog(ts); break; } case 'servers': A.go('servers'); break; case 'edit': A.serverDialog(E.server(this.host)); break;
      }
    }
  }
  /* The folder's path beside its name is dropped when there is no room for all of it (a clipped path is worse than none). */
  Pane.prototype.fit = function () {
    if (!this.el) return;
    this.el.classList.toggle('narrow', this.el.clientWidth > 0 && this.el.clientWidth < 43.75 * 16 * A.u()); /* too little room for every column: the names come first */
    const c = this.el.querySelector('.crumb'); if (!c || c.querySelector('input')) return;
    const h1 = this.el.querySelector('.lh h1'); c.classList.remove('off'); if (c.scrollWidth > c.clientWidth + 1 || c.clientWidth < 8.75 * 16 * A.u() || (h1 && h1.scrollWidth > h1.clientWidth + 1)) c.classList.add('off');
  };
  const renderOnce = Pane.prototype.render; Pane.prototype.render = function (...a) { const v = renderOnce.apply(this, a); this.fit(); return v; };
  A.Pane = Pane;

  /* ---------- previews ---------- */
  const hue = (s) => { let h = 0; for (const c of s) h = (h * 31 + c.charCodeAt(0)) % 360; return h; };
  A.previewHTML = (host, dir, n, small) => {
    const k = U.kindOf(n); const h = hue(n.n);
    if (n.t === 'dir') return '<div class="dprev">' + ic('folder', { size: 56 }) + '</div>';
    if (!E.fs.canRead(n)) return '<div class="dprev" style="background:var(--warn-c);color:var(--on-warn-c)">' + ic('lock', { size: 56 }) + '</div>';
    if (k === 'image') return '<div class="dprev art" style="--a:hsl(' + h + ' 55% 48%);--b:hsl(' + ((h + 60) % 360) + ' 60% 38%)">' + ic('image', { size: 56 }) + '</div>';
    if (k === 'video') return '<div class="dprev art" style="--a:hsl(' + h + ' 30% 28%);--b:hsl(' + ((h + 40) % 360) + ' 40% 18%)">' + ic('play', { size: 56 }) + '</div>';
    if (n.content) return '<div class="dprev"><pre>' + esc(n.content.split('\n').slice(0, 9).join('\n')) + '</pre></div>';
    return '<div class="dprev">' + fic(n).replace('class="ic ', 'class="ic ') + '</div>';
  };
  A.previewDialog = (host, dir, n) => {
    const k = U.kindOf(n); const h = hue(n.n); let body;
    if (!E.fs.canRead(n)) body = '<div class="pv nop">' + ic('lock') + '<b>Can’t read this file</b>It is owned by ' + esc(n.own) + ' with mode ' + U.permOctal(n.perm) + '.</div>';
    else if (k === 'image') body = '<div class="pv art" style="--a:hsl(' + h + ' 55% 48%);--b:hsl(' + ((h + 60) % 360) + ' 60% 38%)">' + ic('image') + '</div>';
    else if (k === 'video') body = '<div class="pv art" style="--a:hsl(' + h + ' 30% 28%);--b:hsl(' + ((h + 40) % 360) + ' 40% 18%)">' + ic('play') + '</div>';
    else if (n.content) body = '<div class="pv"><pre>' + esc(n.content) + '</pre></div>';
    else body = '<div class="pv nop">' + ic('file') + '<b>No preview for this type</b>' + U.KINDS[k] + ' · ' + U.fmtBytes(n.b) + '</div>';
    A.dialog({
      title: n.n, width: 620, body: body + '<div style="margin-top:.625rem;color:var(--on-var);font-size:.8125rem">' + U.KINDS[k] + ' · ' + U.fmtBytes(n.b) + ' · ' + U.fmtDate(n.mod) + ' · <span class="mono">' + esc(U.join(dir, n.n)) + '</span></div>',
      actions: [{ label: 'Close' }].concat(E.fs.canRead(n) || true ? [{ label: host === 'local' ? 'Upload' : 'Download', kind: 'f', icon: host === 'local' ? 'upload' : 'download', cb: () => { A.transferNames(host, dir, [n.n]); } }] : [])
    });
  };

  /* ---------- transfers glue ---------- */
  A.copy = async (text, msg) => { try { await navigator.clipboard.writeText(text); A.snack(msg || 'Copied path'); } catch (e) { A.snack('Could not copy. Select the text and press Ctrl+C.', { error: true }); } };
  A.transferNames = (host, dir, names) => {
    if (host === 'local') { const m = A.mainHost(); if (m === 'local') { A.snack('Pick a server first: switch the main view to a server.', { error: true }); return; } return A.doEnqueue('up', m, dir, A.mainPane.path, names); }
    return A.doEnqueue('down', host, dir, A.downloadDir(), names);
  };
  A.transferSel = (pane, names) => A.transferNames(pane.host, pane.path, names);
  A.downloadDir = () => E.settings.downloadDir || (E.local && E.local.path) || '/';
  A.doEnqueue = (dir, host, srcDir, dstDir, names, o) => {
    const r = E.enqueue(Object.assign({ dir, host, srcDir, dstDir, names }, o || {}));
    if (r.error) { A.snack(r.error + '. Connect first.', { error: true, action: 'Servers', onAction: () => A.go('servers') }); return r; }
    if (!r.tasks.length) { A.snack('Nothing to transfer'); return r; }
    const n = r.tasks.length; const first = r.tasks[0].name;
    A.snack((dir === 'up' ? 'Uploading ' : 'Downloading ') + (n === 1 ? '“' + first + '”' : n + ' items') + (dir === 'up' ? ' to ' + hostName(host) + ' › ' + (U.baseOf(dstDir) || '/') : ' to ' + (U.baseOf(dstDir) || dstDir)), { action: 'View', onAction: () => { A.state.sheetOpen = true; A.state.sheetTab = 'active'; A.renderSheet(true); } });
    A.state.sheetOpen = true; A.renderSheet(true); return r;
  };
  A.dropTransfer = (d, targetHost, targetDir) => {
    if (d.host === 'local' && targetHost !== 'local') A.doEnqueue('up', targetHost, d.dir, targetDir, d.names);
    else if (d.host !== 'local' && targetHost === 'local') A.doEnqueue('down', d.host, d.dir, targetDir, d.names);
    else A.snack('Copying between two servers isn’t supported.', { error: true });
  };

  /* transfer cards */
  const groupOf = (t) => (t.state === 'running' || t.state === 'paused' ? 'active' : t.state === 'conflict' || t.state === 'failed' || t.state === 'waiting' ? 'attn' : t.state === 'queued' ? 'queued' : 'done');
  A.groupOf = groupOf;
  A.card = (t) => {
    const up = t.dir === 'up'; const sv = hostName(t.host); const route = up ? 'This computer → ' + sv : sv + ' → This computer'; const where = up ? t.dstDir : t.dstDir;
    const icn = (n) => '<div class="ico">' + ic(n) + '</div>';
    const head = (i, extra) => '<div class="r1">' + icn(i) + '<div class="nm"><b data-nolocal title="' + esc(t.name) + '">' + esc(t.name) + (t.isDir ? ' <span style="font-weight:400;color:var(--on-var)">· ' + t.files + ' files</span>' : '') + '</b><small>' + esc(route) + ' · ' + esc(where) + '</small></div>' + (extra || '') + '</div>';
    const x = (act, i, title) => '<button class="ib sm" data-t="' + act + '" title="' + title + '" aria-label="' + title + '">' + ic(i) + '</button>';
    const stats = '<div class="st"><span><b data-f="pct"></b> · <span data-f="done"></span> of ' + U.fmtBytes(t.bytes) + '</span><span><b data-f="speed"></b> · <span data-f="eta"></span></span></div>';
    const B = (a, l, k) => '<button class="btn sm ' + (k || '') + '" data-t="' + a + '">' + l + '</button>';
    let c = '', cls = up ? '' : 'down';
    switch (t.state) {
      case 'running': c = head(up ? 'upload' : 'download', x('pause', 'pause', 'Pause') + x('cancel', 'x', 'Cancel')) + '<div class="lin"><i></i></div>' + stats; break;
      case 'paused': cls += ' paused'; c = head('pause', x('resume', 'play', 'Resume') + x('cancel', 'x', 'Cancel')) + '<div class="lin"><i></i></div><div class="st"><span><b data-f="pct"></b> · paused</span><span><span data-f="done"></span> of ' + U.fmtBytes(t.bytes) + '</span></div>'; break;
      case 'queued': cls = 'q'; c = head('clock', x('cancel', 'x', 'Cancel')) + '<div class="msg" style="color:var(--on-var)">Waiting for a free slot · ' + U.fmtBytes(t.bytes) + '</div>'; break;
      case 'conflict': cls = 'warn'; c = head('alert') + '<div class="msg">' + (t.conflict && t.conflict.name && t.isDir ? '“' + esc(t.conflict.name) + '” already exists' + (t.conflict.more ? ' (and ' + t.conflict.more + ' more names are taken)' : '') : esc(t.msg)) + (t.conflict ? ' (' + (t.conflict.dir ? 'folder' : U.fmtBytes(t.conflict.size)) + (t.conflict.mod ? ', ' + U.fmtDate(t.conflict.mod) : '') + ')' : '') + '. Yours is ' + U.fmtBytes(t.conflict && t.conflict.mine ? t.conflict.mine : t.bytes) + '.</div><div class="ac">' + B('replace', 'Replace', 'f') + B('keep', 'Keep both') + B('skip', 'Skip') + B('cancel', 'Cancel') + '<label class="all"><input type="checkbox" data-all> Apply to all</label></div>'; break;
      case 'waiting': cls = 'warn'; c = head('wifi-off', x('cancel', 'x', 'Cancel')) + '<div class="lin"><i></i></div><div class="msg">' + esc(t.msg) + (S.autoReconnect === false ? '. Auto-reconnect is off: reconnect ' + esc(sv) + ' in Servers.' : '. Will resume automatically when ' + esc(sv) + ' is back.') + '</div><div class="ac">' + B('servers', 'Servers') + '</div>'; break;
      case 'failed': cls = 'bad'; c = head(/Permission/.test(t.msg) ? 'lock' : 'alert-circle') + '<div class="msg">' + esc(t.msg) + '</div>' + (t.fix ? '<div class="msg" style="color:var(--on-var)">' + esc(t.fix) + '</div>' : '') + '<div class="ac">' + B('retry', 'Retry', 'f') + B('dismiss', 'Dismiss') + '</div>'; break;
      case 'cancelled': cls = 'q'; c = head('x', x('dismiss', 'x', 'Dismiss')) + '<div class="msg" style="color:var(--on-var)">Cancelled · ' + U.fmtBytes(t.done) + ' of ' + U.fmtBytes(t.bytes) + ' was transferred.</div>'; break;
      case 'done': cls += ' done'; c = head('check-circle', x('dismiss', 'x', 'Dismiss')) + '<div class="st"><span>' + U.fmtBytes(t.bytes) + ' · ' + U.fmtDur((t.finishedAt - (t.startedAt || t.queuedAt)) / 1000) + (t.verified ? ' · Checksum verified' : '') + '</span><span>' + ago(t.finishedAt) + '</span></div><div class="ac">' + B('reveal', 'Show in folder') + '</div>'; break;
    }
    return '<div class="card ' + cls + '" data-tid="' + t.id + '" data-st="' + t.state + '">' + c + '</div>';
  };
  A.patchCards = (root) => {
    for (const t of E.tasks) {
      if (t.state !== 'running' && t.state !== 'paused' && t.state !== 'waiting') continue; const p = t.done / t.bytes * 100;
      $$('[data-tid="' + t.id + '"]', root).forEach((c) => {
        const lin = $('.lin', c); if (lin) lin.style.setProperty('--p', p.toFixed(1));
        const set = (f, v) => $$('[data-f="' + f + '"]', c).forEach((n) => { if (n.textContent !== v) n.textContent = v; });
        set('pct', Math.floor(p) + '%'); set('done', U.fmtBytes(t.done)); set('speed', U.fmtSpeed(t.speed)); set('eta', p >= 99.9 ? 'Checking the file' : isFinite(E.eta(t)) ? U.fmtDur(E.eta(t)) + ' left' : '—');
      });
    }
  };
  A.cardClick = (e, root) => {
    const b = e.target.closest('[data-t]'); if (!b) return; const card = b.closest('[data-tid]'); const id = card.dataset.tid; const t = E.task(id); const a = b.dataset.t;
    try {
    if (a === 'pause') Promise.resolve(E.pause(id)).catch((err) => A.snack(err.message, { error: true })); else if (a === 'resume') E.resumeTask(id); else if (a === 'cancel') { const go = () => { E.cancel(id); A.snack('Cancelled “' + (t ? t.name : '') + '”'); }; if (t && t.done > 0 && t.state !== 'queued') A.dialog({ title: 'Cancel this transfer?', body: '<p>“' + esc(t.name) + '” is ' + Math.min(99, Math.floor(100 * t.done / Math.max(1, t.bytes))) + '% done. Cancelling stops it and keeps what was already copied.</p>', modal: true, actions: [{ id: 'k', label: 'Keep going', kind: 't', cb: (c) => c.close() }, { id: 'c', label: 'Cancel transfer', kind: 'f', cb: (c) => { c.close(); go(); } }] }); else go(); }
    else if (a === 'retry') E.retry(id);
    else if (a === 'replace' || a === 'keep' || a === 'skip') E.resolve(id, a, !!(card.querySelector('[data-all]') || {}).checked);
    else if (a === 'dismiss') E.dismiss(id); else if (a === 'servers') A.go('servers');
    else if (a === 'reveal' && t) { A.go('files'); const side = t.dir === 'up' ? t.host : 'local'; if (side === 'local') { A.state.sideTab = 'local'; A.renderSide(); A.localPane.go(t.dstDir); } else { A.setMainHost(side); A.mainPane.go(t.dstDir); } }
    } catch (err) { A.snack(err.message, { error: true }); }
  };
})();
