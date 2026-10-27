/* RFE Tonal - part 3: hidden files, text editor, previews, bulk actions, saved searches. Reads and writes go to the core. */
(function () {
  'use strict';
  const A = window.A, E = A.E, U = A.U, S = A.S, $ = A.$, $$ = A.$$, esc = A.esc, ic = A.ic, fic = A.fic, st = A.state, F = A.fx, H = A.fxHandlers;
  const audit = F.audit;
  const THUMB = /[?&](thumb|selftest)=1/.test(location.search);
  const fail = (e) => A.snack((e && e.message) || String(e), { error: true });
  const SKEY = 'rfe-tonal-search-v1';
  let ss = {}; try { ss = JSON.parse(localStorage.getItem(SKEY) || '{}') || {}; } catch (e) { /* storage unavailable */ }
  const X3 = A.X3 = { recentQ: ss.recentQ || [], saved: ss.saved || [] };
  const keepSearches = () => { try { localStorage.setItem(SKEY, JSON.stringify(X3)); } catch (e) { /* ignore */ } };
  const pathOf = (dir, n) => n.path || U.join(dir, n.n);
  const MAX_TEXT = 1 << 20;

  /* ================= hidden files ================= */
  E.fs.setDotsHidden(() => !S.showHidden);
  const view0 = E.fs.view;
  E.fs.view = (items, o) => view0(S.showHidden ? items : items.filter((n) => n.n.charAt(0) !== '.'), o);
  const refreshPanes = () => { for (const p of [A.mainPane, A.localPane]) if (p.el && p.el.isConnected && p.online()) { p.refreshItems(); p.render(); } };
  A.toggleHidden = () => { S.showHidden = !S.showHidden; A.save(); refreshPanes(); A.snack(S.showHidden ? 'Showing hidden files' : 'Hiding files that start with a dot'); if (st.view === 'settings') A.pageSettings(); };
  document.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'h' && !/INPUT|TEXTAREA|SELECT/.test((e.target || {}).tagName || '')) { e.preventDefault(); A.toggleHidden(); } });
  $('#stage').addEventListener('click', (e) => { if (e.target.closest('[data-sw="showHidden"]')) setTimeout(refreshPanes, 0); });

  /* ================= reading text ================= */
  const TEXTY = /\.(txt|md|markdown|json|csv|tsv|log|ya?ml|toml|ini|conf|cfg|sh|js|ts|py|go|rs|java|kt|xml|html|css|properties|gradle|env|gitignore|sql)$/i;
  const isText = (n) => n.t === 'file' && (TEXTY.test(n.n) || n.n.charAt(0) === '.' || ['text', 'code'].includes(U.kindOf(n))) && !/\.(jks|apk|aab|deb)$/i.test(n.n);
  A.isTextFile = isText;
  const readText = (host, dir, n) => (host === 'local' ? E.call('local_read_text', { path: pathOf(dir, n) }) : E.call('files_read_text', { host, path: pathOf(dir, n) }));
  const bytesOf = (s) => new TextEncoder().encode(s).length;
  const canModify = (host) => host !== 'local' && E.fs.canWrite(null, host);

  /* ================= editor ================= */
  A.openEditor = (host, dir, n) => {
    const full = pathOf(dir, n); const big = n.b > MAX_TEXT; const ro = big || !canModify(host);
    let orig = ''; let base = ''; let dirty = false, loaded = false, ctl;
    const stat = () => { const ta = $('.fxta', ctl.el); const v = ta.value; const pos = ta.selectionStart; const before = v.slice(0, pos).split('\n'); $('.edst', ctl.el).innerHTML = '<span>Ln ' + before.length + ', Col ' + (before[before.length - 1].length + 1) + '</span><span>' + v.split('\n').length + ' lines</span><span>' + U.fmtBytes(bytesOf(v)) + '</span><span>UTF-8</span>' + (!loaded ? '<span class="tag">Loading…</span>' : ro ? '<span class="tag warn">Read only</span>' : dirty ? '<span class="tag pri">Unsaved changes</span>' : '<span class="tag ok">Saved</span>'); const sv = $('[data-id="save"]', ctl.el); if (sv) sv.disabled = ro || !dirty || !loaded; const t = $('h2', ctl.el.parentNode); if (t) t.textContent = (dirty ? '● ' : '') + n.n; };
    const write = (path, text, baseMod) => E.call('files_write_text', { host, path, text, baseModified: baseMod });
    const doSave = (c, mode) => {
      const ta = $('.fxta', ctl.el); const v = ta.value;
      const ok = (e) => { base = e.modified || base; orig = v; dirty = false; stat(); E.fs.refresh(host, dir).catch(() => {}); };
      const p = mode === 'copy' ? (() => { const nn = E.fs.uniqueName(host, dir, n.n); const np = U.join(dir, nn); return E.call('files_create_file', { host, parent: dir, name: nn }).then(() => write(np, v, '')).then((e) => { audit('Saved a copy of ' + n.n + ' as ' + nn + ' on ' + A.hostName(host)); A.snack('Saved as “' + nn + '”'); ok(e); }); })()
        : write(full, v, mode === 'over' ? '' : base).then((e) => { audit('Edited ' + n.n + ' on ' + A.hostName(host) + ' (' + U.fmtBytes(bytesOf(v)) + ')'); A.snack('Saved “' + n.n + '”'); ok(e); });
      p.catch((e) => {
        if (e.code === 'STALE_WRITE') A.dialog({ icon: 'alert', title: 'This file changed on the server', width: 480, body: '<div>Someone else saved <b>' + esc(n.n) + '</b> after you opened it. Overwriting loses their changes.</div>', actions: [{ label: 'Cancel', kind: 'tx' }, { label: 'Save as copy', kind: 't', cb: () => { doSave(c, 'copy'); } }, { label: 'Overwrite', kind: 'ed', cb: () => { doSave(c, 'over'); } }] });
        else fail(e);
      });
      return false;
    };
    const guard = () => { if (!dirty) return true; if ($('.eddc', ctl.el)) return false; const bar = document.createElement('div'); bar.className = 'eddc'; bar.innerHTML = ic('alert-circle') + '<span>Discard your unsaved changes?</span><button class="btn sm e" data-d="1">Discard</button><button class="btn sm" data-d="0">Keep editing</button>'; $('.db', ctl.el).prepend(bar); bar.addEventListener('click', (e) => { const b = e.target.closest('[data-d]'); if (!b) return; if (b.dataset.d === '1') { dirty = false; ctl.close(); } else bar.remove(); }); return false; };
    ctl = A.dialog({ title: n.n, nolocal: true, width: 880, cls: 'fxed', modal: true, noFocus: true, beforeClose: guard,
      body: '<div class="edhd">' + fic(n) + '<span class="mono">' + esc(host === 'local' ? '' : A.hostName(host) + ':') + esc(full) + '</span><span class="sp"></span><label class="fxck" style="margin:0"><input type="checkbox" id="edw" checked> Wrap lines</label></div>' + (ro ? '<div class="hint" style="margin:.5rem 0 0">' + ic('lock') + '<span>' + (big ? 'This file is ' + U.fmtBytes(n.b) + ', too large to edit here (limit 1 MB).' : host === 'local' ? 'Files on this computer open read-only here. Edit them in your own editor.' : 'You can read this file but not change it: this device may not modify files on ' + esc(A.hostName(host)) + '.') + '</span></div>' : '') + '<textarea class="fxta" spellcheck="false" aria-label="File contents" aria-description="Tab indents. Press Ctrl+M to make Tab move focus instead." title="Tab indents; Ctrl+M makes Tab move focus" readonly></textarea><div class="edst"></div>',
      actions: [{ label: 'Close', kind: 'tx', cb: () => (guard() ? undefined : false) }, { id: 'save', label: 'Save', kind: 'f', icon: 'check', cb: (c) => doSave(c) }],
      onOpen: (c) => { ctl = c; const ta = $('.fxta', c.el); ta.value = ''; stat();
        ta.addEventListener('input', () => { dirty = ta.value !== orig; stat(); }); ['keyup', 'click'].forEach((ev) => ta.addEventListener(ev, stat));
        ta.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); if (!ro && dirty && loaded) doSave(c); } if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'm') { e.preventDefault(); ta._tabFocus = !ta._tabFocus; A.snack(ta._tabFocus ? 'Tab now moves focus out of the editor. Press Ctrl+M to indent with Tab again.' : 'Tab indents again.'); } else if (e.key === 'Tab' && !ro && !e.shiftKey && !ta._tabFocus) { e.preventDefault(); document.execCommand('insertText', false, '  '); } });
        $('#edw', c.el).addEventListener('change', (e) => { ta.style.whiteSpace = e.target.checked ? 'pre-wrap' : 'pre'; ta.style.overflowX = e.target.checked ? 'hidden' : 'auto'; });
        if (big) { ta.value = 'Too large to open.'; return; }
        readText(host, dir, n).then((r) => { if (!c.el.isConnected) return; orig = r.text; base = r.modified || ''; ta.value = r.text; ta.readOnly = ro; loaded = true; ta.focus(); stat(); }).catch((e) => { ta.value = e.message; stat(); }); } });
    return ctl;
  };

  /* ================= previews ================= */
  const mdHTML = (src) => {
    const lines = src.split('\n'); let out = '', list = null, code = false;
    const inline = (s) => esc(s).replace(/`([^`]+)`/g, '<code>$1</code>').replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>').replace(/\*([^*]+)\*/g, '<i>$1</i>').replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<u>$1</u>');
    const close = () => { if (list) { out += '</' + list + '>'; list = null; } };
    for (const l of lines) {
      if (/^```/.test(l)) { close(); out += code ? '</pre>' : '<pre>'; code = !code; continue; } if (code) { out += esc(l) + '\n'; continue; }
      let m; if ((m = /^(#{1,3})\s+(.*)$/.exec(l))) { close(); out += '<h' + m[1].length + '>' + inline(m[2]) + '</h' + m[1].length + '>'; }
      else if ((m = /^\s*[-*]\s+(.*)$/.exec(l))) { if (list !== 'ul') { close(); out += '<ul>'; list = 'ul'; } out += '<li>' + inline(m[1]) + '</li>'; }
      else if ((m = /^\s*\d+\.\s+(.*)$/.exec(l))) { if (list !== 'ol') { close(); out += '<ol>'; list = 'ol'; } out += '<li>' + inline(m[1]) + '</li>'; }
      else if ((m = /^>\s?(.*)$/.exec(l))) { close(); out += '<blockquote>' + inline(m[1]) + '</blockquote>'; }
      else if (!l.trim()) close(); else { close(); out += '<p>' + inline(l) + '</p>'; }
    }
    close(); return out;
  };
  const jsonHTML = (txt) => { let pretty = txt; try { pretty = JSON.stringify(JSON.parse(txt), null, 2); } catch (e) { /* show raw */ } return esc(pretty).replace(/(&quot;|")([^"\n]*?)(&quot;|")(\s*:)/g, '<i class="jk">$1$2$3</i>$4').replace(/: (&quot;|")([^\n]*?)(&quot;|")/g, ': <i class="js">$1$2$3</i>').replace(/: (-?\d[\d.]*)/g, ': <i class="jn">$1</i>'); };
  const numbered = (html) => { const ls = html.split('\n'); return '<div class="codev"><div class="ln">' + ls.map((_, i) => i + 1).join('\n') + '</div><pre>' + html + '</pre></div>'; };
  const csvTable = (n, raw) => {
    const sep = /\.tsv$/i.test(n.n) ? '\t' : ','; const rows = raw.trim().split('\n').map((r) => r.split(sep)); const head = rows[0] || [], body = rows.slice(1, 15);
    return '<div class="csvv"><table><thead><tr><th>#</th>' + head.map((h) => '<th>' + esc(h) + '</th>').join('') + '</tr></thead><tbody>' + body.map((r, i) => '<tr><td>' + (i + 1) + '</td>' + r.map((c) => '<td>' + esc(c) + '</td>').join('') + '</tr>').join('') + '</tbody></table></div><div class="pvnote">Showing the first ' + body.length + ' of ' + Math.max(0, rows.length - 1).toLocaleString(U.lang()) + ' rows</div>';
  };
  const AUDIO = /\.(mp3|wav|flac|ogg|m4a|opus|aac)$/i, PDF = /\.pdf$/i, CSV = /\.(csv|tsv)$/i, MD = /\.(md|markdown)$/i, JSN = /\.json$/i, ARC = /\.(zip|tar|tgz|7z|rar|zst|gz|xz)$/i;
  const IMG = /\.(png|jpe?g|gif|webp|bmp|svg)$/i;
  /* Whether a click on the file shows something worth opening: a picture, text, or the list inside an archive. */
  A.hasViewer = (host, n) => { if (!E.fs.canRead(n)) return false; const k = U.kindOf(n); return IMG.test(n.n) || k === 'image' || isText(n) || (ARC.test(n.n) && host !== 'local'); };
  const nop = (icon, title, text, extra) => '<div class="pv nop">' + ic(icon) + '<b>' + title + '</b>' + text + (extra || '') + '</div>';

  /* What the preview body shows once the file is read; the dialog is already open with "Loading…". */
  async function build(host, dir, n) {
    const k = U.kindOf(n); const r = { html: '', init: null };
    if (!E.fs.canRead(n)) { r.html = nop('lock', 'Can’t read this file', 'It has mode ' + U.permOctal(n.perm) + ' on the server. Ask the owner of the file to change it.'); return r; }
    if (IMG.test(n.n) || k === 'image') {
      if (n.b > 8 << 20 && host === 'local') { r.html = nop('image', 'Too large to preview', U.fmtBytes(n.b) + ' is more than the 8 MB preview limit.'); return r; }
      const src = host === 'local' ? await E.call('local_read_image', { path: pathOf(dir, n) }) : await E.call('files_thumb', { host, path: pathOf(dir, n), size: 1024 });
      r.html = '<div class="imgv"><div class="canvas" id="imC"><div class="im" id="imI"><img alt="' + esc(n.n) + '" id="imG"></div></div><div class="imc"><button class="ib sm" data-im="out" aria-label="Zoom out">' + ic('minus') + '</button><span id="imZ" aria-live="polite">Fit</span><button class="ib sm" data-im="in" aria-label="Zoom in">' + ic('plus') + '</button><button class="btn sm tx" data-im="fit">Fit</button><button class="btn sm tx" data-im="100">100%</button></div></div><div class="pvnote" id="imN">' + U.fmtBytes(n.b) + '</div>';
      r.init = (c) => {
        const g = $('#imG', c.el); let z = 1, fitW = 0;
        /* "Fit" is the picture scaled to the box; "100%" is its real pixel size; the box scrolls when it is larger. */
        const set = (v, lab) => { if (!fitW) return; z = Math.max(0.25, Math.min(16, v)); if (fitW) g.style.width = (fitW * z) + 'px'; $('#imZ', c.el).textContent = lab || Math.round(z * fitW / g.naturalWidth * 100) + '%'; };
        g.addEventListener('load', () => { const cv = $('#imC', c.el); fitW = Math.max(1, Math.min(g.naturalWidth, g.naturalWidth * Math.min(cv.clientWidth / g.naturalWidth, cv.clientHeight / g.naturalHeight))); set(1); $('#imN', c.el).textContent = g.naturalWidth + ' × ' + g.naturalHeight + ' px' + (host === 'local' ? '' : ' preview. Download for the original file.'); });
        g.addEventListener('error', () => { $('.pvbody', c.el).innerHTML = nop('image', 'Could not load this picture', 'The file may be damaged or not an image this app can show.'); });
        g.src = src;
        c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-im]'); if (!b) return; const a = b.dataset.im; if (a === 'in') set(z * 1.25); else if (a === 'out') set(z / 1.25); else if (a === 'fit') set(1); else set(g.naturalWidth / fitW); });
      };
      return r;
    }
    if (ARC.test(n.n) && !(isText(n) && !ARC.test(n.n))) {
      if (host === 'local') { r.html = nop('archive', 'No preview for archives on this computer', U.fmtBytes(n.b)); return r; }
      const ents = await E.call('files_archive_list', { host, path: pathOf(dir, n), limit: 200 });
      r.html = '<div class="arcv"><div class="arh">' + ic('archive') + '<b>' + ents.length + (ents.length >= 200 ? '+' : '') + (ents.length === 1 ? ' entry' : ' entries') + '</b><span>' + (ents.length >= 200 ? 'at least ' : '') + U.fmtBytes(ents.reduce((a, e) => a + (e.size || 0), 0)) + ' uncompressed · ' + U.fmtBytes(n.b) + ' packed</span></div><table><tbody>' + ents.slice(0, 200).map((e) => '<tr><td>' + ic(e.isDir ? 'folder' : 'file') + '</td><td class="mono">' + esc(e.path) + '</td><td class="r">' + (e.isDir ? '' : U.fmtBytes(e.size || 0)) + '</td></tr>').join('') + '</tbody></table></div>';
      r.arc = true; return r;
    }
    if (isText(n)) {
      if (n.b > MAX_TEXT) { r.html = nop('file', 'Too large to preview', U.fmtBytes(n.b) + ' is more than the 1 MB text preview limit.', host === 'local' ? '' : '<div style="margin-top:.75rem"><button class="btn sm f" data-pvx="dl">' + ic('download') + 'Download to read it</button></div>'); r.init = (c) => c.el.addEventListener('click', (e) => { if (e.target.closest('[data-pvx="dl"]')) { A.transferNames(host, dir, [n.n]); c.close(); } }); return r; }
      const t = await readText(host, dir, n); const txt = t.text; r.edit = true;
      r.html = MD.test(n.n) ? '<div class="mdv">' + mdHTML(txt) + '</div>' : CSV.test(n.n) ? csvTable(n, txt) : JSN.test(n.n) ? numbered(jsonHTML(txt)) : numbered(esc(txt));
      return r;
    }
    const why = AUDIO.test(n.n) || k === 'audio' ? 'The app does not play audio.' : k === 'video' ? 'The app does not play video.' : PDF.test(n.n) ? 'The app does not render PDFs.' : 'The app has no preview for this type.';
    r.html = nop('file', 'No preview', why + ' ' + U.KINDS[k] + ' · ' + U.fmtBytes(n.b), host === 'local' ? '<div style="margin-top:.75rem"><button class="btn sm" data-pvx="open">Open with default app</button></div>' : '<div style="margin-top:.75rem"><button class="btn sm f" data-pvx="dl">' + ic('download') + 'Download to open it</button></div>');
    r.init = (c) => c.el.addEventListener('click', (e) => { if (e.target.closest('[data-pvx="open"]')) E.call('local_open', { path: pathOf(dir, n) }).catch(fail); else if (e.target.closest('[data-pvx="dl"]')) { A.transferNames(host, dir, [n.n]); c.close(); } });
    return r;
  }
  function showPreview(host, dir, n) {
    const sib = (host === A.mainPane.host && dir === A.mainPane.path ? A.mainPane.items : E.fs.list(host, dir) || []).filter((x) => x.t === 'file' && E.fs.canRead(x)); const i = sib.findIndex((x) => x.n === n.n);
    const nav = '<div class="pvbar"><button class="ib sm" data-pn="-1" aria-label="Previous file"' + (i <= 0 ? ' disabled' : '') + '>' + ic('chevron-left') + '</button><button class="ib sm" data-pn="1" aria-label="Next file"' + (i < 0 || i >= sib.length - 1 ? ' disabled' : '') + '>' + ic('chevron-right') + '</button><span class="pvcnt">' + (i >= 0 ? (i + 1) + ' of ' + sib.length : '') + '</span><span class="sp"></span>' + fic(n) + '<span class="mono">' + esc(pathOf(dir, n)) + '</span></div>';
    const acts = [{ label: 'Close', kind: 'tx' }];
    if (isText(n) && n.b <= MAX_TEXT) acts.push({ id: 'edit', label: canModify(host) ? 'Edit' : 'View source', kind: 't', icon: 'edit', cb: () => { setTimeout(() => A.openEditor(host, dir, n), 0); } });
    if (host !== 'local' && ARC.test(n.n)) acts.push({ id: 'ex', label: 'Extract here', kind: 't', icon: 'archive', cb: () => { A.mainPane.host === host && A.mainPane.path === dir ? F.extract(A.mainPane, n) : A.snack('Open the folder in Files to extract.'); } });
    if (host !== 'local') acts.push({ id: 'sv', label: 'Save as…', kind: 'tx', icon: 'download', cb: () => { A.doEnqueue('down', host, dir, A.downloadDir(), [n.n], { askWhere: true }); } });
    else acts.push({ id: 'op', label: 'Open', kind: 'tx', icon: 'external', cb: () => { E.call('local_open', { path: pathOf(dir, n) }).catch(fail); return false; } });
    acts.push({ id: 'dl', label: host === 'local' ? 'Upload' : 'Download', kind: 'f', icon: host === 'local' ? 'upload' : 'download', cb: () => { A.transferNames(host, dir, [n.n]); } });
    let kn0 = null;
    const ctl = A.dialog({ title: n.n, nolocal: true, width: 780, cls: 'fxpv', body: nav + '<div class="pvbody" tabindex="0" role="region" aria-label="File preview"><div class="pv nop"><span class="spin">' + ic('spinner') + '</span><b>Loading…</b></div></div><div class="pvmeta">' + U.KINDS[U.kindOf(n)] + ' · ' + U.fmtBytes(n.b) + ' · modified ' + U.fmtDate(n.mod) + (n.own ? ' · ' + esc(n.own) : '') + ' · ' + esc(n.perm) + '</div>', actions: acts, noFocus: true, onClose: () => { if (kn0) document.removeEventListener('keydown', kn0); if (ctl && ctl.onGone) ctl.onGone(); },
      onOpen: (c) => {
        c.el.addEventListener('click', (e) => { const b = e.target.closest('[data-pn]'); if (!b || b.disabled) return; const nx = sib[i + +b.dataset.pn]; if (!nx) return; c.close(); showPreview(host, dir, nx); });
        const kn = (e) => { if (!c.el.isConnected) { document.removeEventListener('keydown', kn); return; } if (e.target.matches && e.target.matches('input,textarea,select')) return; if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { const cv = $('#imC', c.el); if (cv && (cv.scrollWidth > cv.clientWidth + 1 || cv.scrollHeight > cv.clientHeight + 1)) return; const b = $('[data-pn="' + ((e.key === 'ArrowLeft') === (document.documentElement.dir !== 'rtl') ? -1 : 1) + '"]', c.el); if (b && !b.disabled) { e.preventDefault(); b.click(); } } };
        document.addEventListener('keydown', kn); kn0 = kn;
        build(host, dir, n).then((r) => { if (!c.el.isConnected) return; $('.pvbody', c.el).innerHTML = r.html; if (r.init) r.init(c); }).catch((e) => { if (c.el.isConnected) $('.pvbody', c.el).innerHTML = nop('alert-circle', 'Could not show this file', esc(e.message)); });
      } });
    return ctl;
  }
  A.previewDialog = (host, dir, n) => { if (A.addRecent && host !== 'local') A.addRecent(host, pathOf(dir, n), n.n); return showPreview(host, dir, n); };

  /* edit entries in the context menu + F4 */
  const ctx2 = A.Pane.prototype.ctxItems;
  A.Pane.prototype.ctxItems = function (onItem) {
    const items = ctx2.call(this, onItem); if (!onItem || !this.online()) return items; const s = this.selected(); if (s.length !== 1 || !isText(s[0]) || s[0].b > MAX_TEXT) return items;
    const one = s[0]; const i = items.findIndex((x) => x && x.label === 'Copy to…'); const add = { icon: 'edit', label: canModify(this.host) ? 'Edit' : 'View source', kbd: 'F4', onClick: () => A.openEditor(this.host, this.path, one) }; items.splice(i >= 0 ? i : 2, 0, add); return items;
  };
  document.addEventListener('keydown', (e) => { if (e.key === 'F4' && st.view === 'files' && !$('.scrim')) { const p = A.activePane || A.mainPane; const s = p.selected(); if (s.length === 1 && isText(s[0]) && s[0].b <= MAX_TEXT) { e.preventDefault(); A.openEditor(p.host, p.path, s[0]); } } });

  /* ================= bulk actions ================= */
  const bar = document.createElement('div'); bar.id = 'bulk'; bar.className = 'bulk'; bar.hidden = true; bar.setAttribute('role', 'toolbar'); bar.setAttribute('aria-label', 'Actions for the selected items'); $('#main').appendChild(bar);
  const bulkUpdate = () => {
    const p = A.mainPane; const sel = p.selected ? p.selected() : []; const show = st.view === 'files' && sel.length >= 2 && p.online() && !$('.scrim');
    bar.hidden = !show; if (!show) return; const remote = p.host !== 'local';
    bar.innerHTML = '<b>' + sel.length + ' selected</b><span class="sep"></span>' + (remote ? '<button class="btn sm f" data-bk="download">' + ic('download') + 'Download</button><button class="btn sm" data-bk="copy">' + ic('copy') + 'Copy to…</button><button class="btn sm" data-bk="move">' + ic('move') + 'Move to…</button><button class="btn sm" data-bk="zip">' + ic('archive') + 'Compress</button>' : '<button class="btn sm f" data-bk="download">' + ic('upload') + 'Upload</button>') + '<button class="btn sm" data-bk="rename">' + ic('edit') + 'Rename…</button><button class="btn sm e" data-bk="trash">' + ic('trash') + 'Delete</button><button class="ib sm" data-bk="clear" aria-label="Clear selection">' + ic('x') + '</button>';
  };
  bar.addEventListener('click', (e) => { const b = e.target.closest('[data-bk]'); if (!b) return; const p = A.mainPane; switch (b.dataset.bk) { case 'download': p.transfer(); break; case 'copy': F.fileOp(p, 'copy'); break; case 'move': F.fileOp(p, 'move'); break; case 'zip': F.compress(p); break; case 'rename': F.batchRename(p); break; case 'trash': p.del(); break; case 'clear': p.sel.clear(); p.selChanged(); break; } setTimeout(bulkUpdate, 0); });
  const os0 = A.mainPane.o.onSelect; A.mainPane.o.onSelect = function () { if (os0) os0.apply(this, arguments); bulkUpdate(); };
  const go0 = A.go; A.go = function () { const r = go0.apply(this, arguments); bulkUpdate(); return r; };
  new MutationObserver(bulkUpdate).observe($('#layer'), { childList: true });

  /* ================= saved and recent searches ================= */
  A.recordQuery = (q) => { X3.recentQ = [q].concat(X3.recentQ.filter((x) => x !== q)).slice(0, 8); keepSearches(); };
  A.searchExtra = () => {
    const rec = X3.recentQ.map((q) => '<button class="chip" data-fx="q.run|' + esc(q) + '">' + ic('clock') + esc(q) + '</button>').join(''); const sv = X3.saved.map((s, i) => '<span class="chip on svq"><button data-fx="q.save.run|' + i + '">' + ic('star') + esc(s.q) + '</button><button data-fx="q.save.rm|' + i + '" aria-label="Remove saved search ' + esc(s.q) + '">' + ic('x') + '</button></span>').join('');
    if (!sv && !rec) return '';
    return '<div class="qlists">' + (sv ? '<div><small>Saved</small><div class="chips" style="padding:.375rem 0">' + sv + '</div></div>' : '') + (rec ? '<div><small>Recent</small><div class="chips" style="padding:.375rem 0">' + rec + '</div></div>' : '') + '</div>';
  };
  A.searchSave = (sr) => { const on = X3.saved.some((s) => s.q === sr.q); return ' <button class="ib" data-fx="q.save|' + esc(sr.q) + '" aria-pressed="' + on + '" aria-label="' + (on ? 'Remove from saved searches' : 'Save this search') + '" title="' + (on ? 'Saved' : 'Save this search') + '" style="vertical-align:middle">' + ic('star') + '</button>'; };
  const runSaved = (s) => { st.scope = s.scope || 'server'; st.sfilter = s.filter || 'all'; A.runQuery(s.q); };
  Object.assign(H, {
    'q.run': (q) => A.runQuery(q), 'q.save.run': (i) => runSaved(X3.saved[+i]), 'q.save.rm': (i) => { X3.saved.splice(+i, 1); keepSearches(); if (st.view === 'search') A.go('search'); },
    'q.save': (q) => { const sr = A.searchRun || {}; const i = X3.saved.findIndex((s) => s.q === q); if (i >= 0) { X3.saved.splice(i, 1); A.snack('Removed saved search “' + q + '”'); } else { X3.saved.unshift({ q, scope: sr.scope || 'server', filter: sr.filter || 'all' }); A.snack('Saved search “' + q + '”'); } keepSearches(); const b = $('[data-fx^="q.save|"]'); if (b) b.outerHTML = A.searchSave({ q }).trim(); }
  });
  const sb = $('.sbar'); if (sb) {
    sb.style.position = 'relative'; const dd = document.createElement('div'); dd.className = 'qdrop'; dd.hidden = true; dd.setAttribute('role', 'listbox'); sb.appendChild(dd); const inp = $('#q');
    const fill = () => { const v = inp.value.trim().toLowerCase(); const rec = X3.recentQ.filter((q) => !v || q.toLowerCase().includes(v)).slice(0, 5); const sv = X3.saved.filter((s) => !v || s.q.toLowerCase().includes(v)); dd.innerHTML = (sv.length ? '<small>Saved</small>' + sv.map((s) => '<div class="qi" role="option" data-q="' + esc(s.q) + '">' + ic('star') + esc(s.q) + '<small>' + (s.scope === 'all' ? 'all servers' : 'this server') + '</small></div>').join('') : '') + (rec.length ? '<small>Recent</small>' + rec.map((q) => '<div class="qi" role="option" data-q="' + esc(q) + '">' + ic('clock') + esc(q) + '</div>').join('') : ''); dd.hidden = !dd.innerHTML; };
    inp.addEventListener('focus', fill); inp.addEventListener('input', fill); inp.addEventListener('blur', () => setTimeout(() => { dd.hidden = true; }, 160)); inp.addEventListener('keydown', (e) => { if (e.key === 'Escape') dd.hidden = true; });
    /* The suggestions can be picked with the keyboard: Down and Up move through them, Enter runs the marked one. */
    dd.id = 'qdrop'; inp.setAttribute('role', 'combobox'); inp.setAttribute('aria-controls', 'qdrop'); inp.setAttribute('aria-autocomplete', 'list'); let qa = -1;
    const mark = () => { const its = $$('[data-q]', dd); its.forEach((x, i) => { x.id = 'qo' + i; x.setAttribute('aria-selected', String(i === qa)); x.classList.toggle('on', i === qa); }); if (qa >= 0 && its[qa]) inp.setAttribute('aria-activedescendant', its[qa].id); else inp.removeAttribute('aria-activedescendant'); inp.setAttribute('aria-expanded', String(!dd.hidden)); };
    inp.addEventListener('input', () => { qa = -1; setTimeout(mark, 0); }); inp.addEventListener('focus', () => { qa = -1; setTimeout(mark, 0); });
    inp.addEventListener('keydown', (e) => {
      const its = $$('[data-q]', dd); if (dd.hidden || !its.length) { if (e.key === 'ArrowDown' && inp.value === '') { fill(); setTimeout(mark, 0); } return; }
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); qa = e.key === 'ArrowDown' ? Math.min(its.length - 1, qa + 1) : Math.max(-1, qa - 1); mark(); }
      else if (e.key === 'Enter' && qa >= 0 && its[qa]) { e.preventDefault(); e.stopImmediatePropagation(); const q = its[qa].dataset.q; dd.hidden = true; qa = -1; mark(); A.runQuery(q, true); }
      else if (e.key === 'Escape') { qa = -1; mark(); }
    }, true);
    dd.addEventListener('mousedown', (e) => { const it = e.target.closest('[data-q]'); if (it) { e.preventDefault(); dd.hidden = true; A.runQuery(it.dataset.q); } });
  }

  const se0 = A.settingsExtra;
  A.settingsExtra = (k) => se0(k).replace('<div class="sg"><h3>Files</h3>', '<div class="sg"><h3>Files</h3>' + k.row('Show hidden files', '', k.sw('showHidden')));

  /* ================= palette ================= */
  A.paletteExtra.push((C) => {
    C('eye', (S.showHidden ? 'Hide' : 'Show') + ' hidden files', A.toggleHidden, 'Ctrl+H'); const p = A.activePane || A.mainPane; const s = p.selected ? p.selected() : [];
    if (s.length === 1 && isText(s[0]) && s[0].b <= MAX_TEXT) C('edit', 'Edit ' + s[0].n, () => A.openEditor(p.host, p.path, s[0]), 'F4');
  });
  document.addEventListener('DOMContentLoaded', () => { if (THUMB) $$('.snack', $('#layer')).forEach((x) => x.remove()); });
})();
