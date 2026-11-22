// Strip above a mockup: previous, next, index, and the feature map from window.MAP.
(function () {
  const F = window.MK_FILES || [], m = location.pathname.match(/(\d\d)-[^/]+\.html$/); if (!m) return;
  const n = +m[1], prev = F[n - 2], next = F[n];
  const s = document.createElement('div'); s.className = 'mk-strip';
  s.innerHTML = `<a href="index.html">All 20</a><b>${String(n).padStart(2, '0')} · ${document.title}</b><span class="sp"></span><button id="mkmap">Feature map</button>${prev ? `<a href="${prev}">Previous</a>` : ''}${next ? `<a href="${next}">Next</a>` : ''}`;
  document.body.prepend(s);
  const map = document.createElement('aside'); map.className = 'mk-map';
  map.innerHTML = `<h2>Where every feature lives</h2><p>${window.IDEA || ''}</p><dl>${(window.MAP || []).map((r) => `<dt>${r[0]}</dt><dd>${r[1]}</dd><dd class="n" title="Clicks from the resting screen">${r[2]}</dd>`).join('')}</dl><p style="margin-top:16px">The number is clicks or keys from the resting screen. Every item in FEATURES.md is reachable; the mockup is clickable.</p>`;
  document.body.appendChild(map);
  document.getElementById('mkmap').onclick = () => map.classList.toggle('open');
  addEventListener('keydown', (e) => { if (e.target.closest('input,textarea,[contenteditable]')) return; if (e.key === '[' && prev) location = prev; if (e.key === ']' && next) location = next; });
})();
