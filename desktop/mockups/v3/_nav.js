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
    s.innerHTML = `<a href="index.html">All 10</a><b>${String(n).padStart(2, '0')} · ${document.title}</b><span class="sp"></span><button id="mkmap">Feature map</button>${prev ? `<a href="${prev}">Previous</a>` : ''}${next ? `<a href="${next}">Next</a>` : ''}`;
    document.body.prepend(s);
    const f = location.pathname.split('/').pop(), h = location.hash;
    const pair = document.createElement('div'); pair.className = 'mk-pair';
    pair.innerHTML = `<h2>Full<span>four computers, transfers running, two phones asking</span></h2><iframe src="${f}?v=full${h}" title="Full"></iframe><h2>Empty<span>first run: no computer added yet</span></h2><iframe src="${f}?v=empty${h}" title="Empty"></iframe>`;
    document.body.appendChild(pair);
    const map = document.createElement('aside'); map.className = 'mk-map';
    map.innerHTML = `<h2>Where every feature lives</h2><p>${window.IDEA || ''}</p><p>Fixed in every v3 design: the navigation of v2 design 12, the file system of v2 design 11, Settings of v2 design 15.</p><dl>${(window.MAP || []).map((r) => `<dt>${r[0]}</dt><dd>${r[1]}</dd><dd class="n" title="Clicks from the resting screen">${r[2]}</dd>`).join('')}</dl>`;
    document.body.appendChild(map);
    document.getElementById('mkmap').onclick = () => map.classList.toggle('open');
    addEventListener('keydown', (e) => { if (e.key === '[' && prev) location = prev; if (e.key === ']' && next) location = next; });
  });
})();
