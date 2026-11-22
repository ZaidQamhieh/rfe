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
    s.innerHTML = `<a href="index.html">All 10</a><b>${String(n).padStart(2, '0')} · ${document.title}</b><span class="sp"></span><span class="mk-views">${[['sr','Search menu'],['','Overview'],['go=history','History'],['go=servers','Servers'],['dlg=pairphone','Pair a phone']].map((v)=>`<button data-h="${v[0]}">${v[1]}</button>`).join('')}</span><button id="mkmap">Feature map</button>${prev ? `<a href="${prev}">Previous</a>` : ''}${next ? `<a href="${next}">Next</a>` : ''}`;
    document.body.prepend(s);
    const f = location.pathname.split('/').pop(), h = location.hash || '#sr';
    const pair = document.createElement('div'); pair.className = 'mk-pair';
    pair.innerHTML = `<h2>Full<span>four computers, transfers running, two phones asking</span></h2><iframe src="${f}?v=full${h}" title="Full"></iframe><h2>Empty<span>first run: no computer added yet</span></h2><iframe src="${f}?v=empty${h}" title="Empty"></iframe>`;
    document.body.appendChild(pair);
    const map = document.createElement('aside'); map.className = 'mk-map';
    map.innerHTML = `<h2>Where every feature lives</h2><p>${window.IDEA || ''}</p><p>Fixed in every v4 design: navigation (v2 12), Files (v2 11), Settings (v2 15), Overview (v3 2), Servers, Devices and Transfers (v3 4), Tools (v3 2), History with filters.</p><dl>${(window.MAP || []).map((r) => `<dt>${r[0]}</dt><dd>${r[1]}</dd><dd class="n" title="Clicks from the resting screen">${r[2]}</dd>`).join('')}</dl>`;
    document.body.appendChild(map);
    const mark = (hh) => s.querySelectorAll('.mk-views button').forEach((b) => b.classList.toggle('on', '#' + b.dataset.h === hh));
    mark(h);
    s.querySelectorAll('.mk-views button').forEach((b) => (b.onclick = () => { const hh = '#' + b.dataset.h; history.replaceState(null, '', hh); pair.querySelectorAll('iframe').forEach((fr, i) => (fr.src = `${f}?v=${i ? 'empty' : 'full'}${hh}`)); mark(hh); }));
    document.getElementById('mkmap').onclick = () => map.classList.toggle('open');
    addEventListener('keydown', (e) => { if (e.key === '[' && prev) location = prev; if (e.key === ']' && next) location = next; });
  });
})();
