// Runs first in each v3 page. At the top level it shows the design twice (full, then empty) in frames and the
// strip above; inside a frame (?v=full or ?v=empty) it only marks the body so the window fills the frame.
(function () {
  const q = new URLSearchParams(location.search);
  if (q.has('v')) { document.addEventListener('DOMContentLoaded', () => document.body.classList.add('inframe')); return; }
  window.MK_VIEWER = true;
  document.addEventListener('DOMContentLoaded', () => {
    const F = window.MK_FILES || [], m = location.pathname.match(/(\d\d)-[^/]+\.html$/); const n = m ? +m[1] : 0, prev = F[n - 2], next = F[n];
    const win = document.querySelector('.win'); if (win) win.remove();
    const s = document.createElement('div'); s.className = 'mk-strip';
    s.innerHTML = `<span>v5</span><b>${String(n).padStart(2, '0')} · ${document.title}</b><span class="sp"></span><span class="mk-views">${[['','Overview'],['sr','Search'],['go=results','Show all'],['go=files','Files'],['go=servers','Devices'],['go=history','History'],['dlg=newconn&tab=1','Add 1'],['dlg=newconn&tab=2','Add 2'],['dlg=newconn&tab=3','Add 3'],['dlg=keychanged','PC changed'],['go=servers&dlg=srvset','PC settings']].map((v)=>`<button data-h="${v[0]}">${v[1]}</button>`).join('')}</span><button id="mktheme" aria-label="Switch the design between light and dark">Dark mode</button><button id="mkmap">Feature map</button>${prev ? `<a href="${prev}">Previous</a>` : ''}${next ? `<a href="${next}">Next</a>` : ''}`;
    document.body.prepend(s);
    const f = location.pathname.split('/').pop(), h = location.hash || '#';
    const pair = document.createElement('div'); pair.className = 'mk-pair';
    let th = q.get('theme') === 'dark' ? '&theme=dark' : ''; const tb = s.querySelector('#mktheme'); tb.textContent = th ? 'Light mode' : 'Dark mode';
    pair.innerHTML = `<h2>Full<span>four computers, transfers running, two phones asking</span></h2><iframe src="${f}?v=full${th}${h}" title="Full"></iframe><h2>Empty<span>first run: no computer added yet</span></h2><iframe src="${f}?v=empty${th}${h}" title="Empty"></iframe>`;
    document.body.appendChild(pair);
    const map = document.createElement('aside'); map.className = 'mk-map';
    map.innerHTML = `<h2>Where every feature lives</h2><p>${window.IDEA || ''}</p><p>The final design: v4 fixed parts, search from 9 with 6’s computer switches, add a computer from the steps mockup, technical details folded.</p><dl>${(window.MAP || []).map((r) => `<dt>${r[0]}</dt><dd>${r[1]}</dd><dd class="n" title="Clicks from the resting screen">${r[2]}</dd>`).join('')}</dl>`;
    document.body.appendChild(map);
    const mark = (hh) => s.querySelectorAll('.mk-views button').forEach((b) => b.classList.toggle('on', '#' + b.dataset.h === hh));
    mark(h);
    s.querySelectorAll('.mk-views button').forEach((b) => (b.onclick = () => { const hh = '#' + b.dataset.h; try { history.replaceState(null, '', hh); } catch (e) { location.hash = hh; } pair.querySelectorAll('iframe').forEach((fr, i) => (fr.src = `${f}?v=${i ? 'empty' : 'full'}${th}${hh}`)); mark(hh); }));
    tb.onclick = () => { th = th ? '' : '&theme=dark'; tb.textContent = th ? 'Light mode' : 'Dark mode'; document.body.classList.toggle('mk-dark', !!th); const hh = location.hash || '#'; pair.querySelectorAll('iframe').forEach((fr, i) => (fr.src = `${f}?v=${i ? 'empty' : 'full'}${th}${hh}`)); };
    document.body.classList.toggle('mk-dark', !!th);
    document.getElementById('mkmap').onclick = () => map.classList.toggle('open');
    addEventListener('keydown', (e) => { if (e.key === '[' && prev) location = prev; if (e.key === ']' && next) location = next; });
  });
})();
