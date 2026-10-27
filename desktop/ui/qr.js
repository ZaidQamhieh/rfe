/* Minimal QR Code encoder: byte mode, ECC L/M, versions 1-10. window.QR.make(text, ecc) -> {size, get(x, y)}; QR.svg(text, opts) -> SVG string. */
(function (g) {
  'use strict';
  const ECC = { L: { f: 1, b: [0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4], e: [0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18] }, M: { f: 0, b: [0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5], e: [0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26] } };
  const rawModules = (v) => { let r = (16 * v + 128) * v + 64; if (v >= 2) { const n = Math.floor(v / 7) + 2; r -= (25 * n - 10) * n - 55; if (v >= 7) r -= 36; } return r; };
  const dataWords = (v, E) => Math.floor(rawModules(v) / 8) - E.e[v] * E.b[v];
  const utf8 = (s) => Array.from(unescape(encodeURIComponent(s)), (c) => c.charCodeAt(0));
  const alignPos = (v, size) => { if (v === 1) return []; const n = Math.floor(v / 7) + 2; const step = Math.ceil((v * 4 + 4) / (n * 2 - 2)) * 2; const r = [6]; for (let p = size - 7; r.length < n; p -= step) r.splice(1, 0, p); return r; };
  /* GF(256) */
  const mul = (x, y) => { let z = 0; for (let i = 7; i >= 0; i--) { z = (z << 1) ^ ((z >>> 7) * 0x11d); z ^= ((y >>> i) & 1) * x; } return z; };
  const rsGen = (d) => { const r = new Array(d).fill(0); r[d - 1] = 1; let root = 1; for (let i = 0; i < d; i++) { for (let j = 0; j < d; j++) { r[j] = mul(r[j], root); if (j + 1 < d) r[j] ^= r[j + 1]; } root = mul(root, 2); } return r; };
  const rsRem = (data, gen) => { const r = new Array(gen.length).fill(0); for (const b of data) { const f = b ^ r.shift(); r.push(0); gen.forEach((c, i) => { r[i] ^= mul(c, f); }); } return r; };

  function make(text, eccName) {
    const bytes = utf8(text); let E = ECC[eccName || 'M']; let v = 1;
    const fit = (e) => { for (let k = 1; k <= 10; k++) { const cb = k < 10 ? 8 : 16; if (4 + cb + bytes.length * 8 <= dataWords(k, e) * 8) return k; } return 0; };
    /* a payload that does not fit with the asked error correction is tried with the lowest one before giving up */
    v = fit(E) || (E = ECC.L, fit(E));
    if (!v) throw new Error('QR payload too long');
    const bits = []; const put = (val, n) => { for (let i = n - 1; i >= 0; i--) bits.push((val >>> i) & 1); };
    put(4, 4); put(bytes.length, v < 10 ? 8 : 16); bytes.forEach((b) => put(b, 8));
    const cap = dataWords(v, E) * 8; put(0, Math.min(4, cap - bits.length)); while (bits.length % 8) bits.push(0);
    for (let p = 0xec; bits.length < cap; p ^= 0xec ^ 0x11) put(p, 8);
    const data = []; for (let i = 0; i < bits.length; i += 8) data.push(parseInt(bits.slice(i, i + 8).join(''), 2));
    /* error correction + interleave */
    const nb = E.b[v], eccLen = E.e[v], raw = Math.floor(rawModules(v) / 8), nShort = nb - (raw % nb), shortLen = Math.floor(raw / nb), gen = rsGen(eccLen);
    const blocks = []; for (let i = 0, k = 0; i < nb; i++) { const dat = data.slice(k, k + shortLen - eccLen + (i < nShort ? 0 : 1)); k += dat.length; const ecc = rsRem(dat, gen); if (i < nShort) dat.push(0); blocks.push(dat.concat(ecc)); }
    const out = []; for (let i = 0; i < blocks[0].length; i++) blocks.forEach((b, j) => { if (i !== shortLen - eccLen || j >= nShort) out.push(b[i]); });
    /* matrix */
    const size = v * 4 + 17; const M = Array.from({ length: size }, () => new Array(size).fill(false)); const F = Array.from({ length: size }, () => new Array(size).fill(false));
    const set = (x, y, d) => { M[y][x] = d; F[y][x] = true; };
    for (let i = 0; i < size; i++) { set(6, i, i % 2 === 0); set(i, 6, i % 2 === 0); }
    [[3, 3], [size - 4, 3], [3, size - 4]].forEach(([cx, cy]) => { for (let dy = -4; dy <= 4; dy++) for (let dx = -4; dx <= 4; dx++) { const d = Math.max(Math.abs(dx), Math.abs(dy)), x = cx + dx, y = cy + dy; if (x >= 0 && x < size && y >= 0 && y < size) set(x, y, d !== 2 && d !== 4); } });
    const ap = alignPos(v, size); ap.forEach((cx, i) => ap.forEach((cy, j) => { if ((i === 0 && j === 0) || (i === 0 && j === ap.length - 1) || (i === ap.length - 1 && j === 0)) return; for (let dy = -2; dy <= 2; dy++) for (let dx = -2; dx <= 2; dx++) set(cx + dx, cy + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1); }));
    const fmt = (mask) => {
      const d = (E.f << 3) | mask; let rem = d; for (let i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537); const b = ((d << 10) | rem) ^ 0x5412; const bit = (i) => ((b >>> i) & 1) !== 0;
      for (let i = 0; i <= 5; i++) set(8, i, bit(i)); set(8, 7, bit(6)); set(8, 8, bit(7)); set(7, 8, bit(8)); for (let i = 9; i < 15; i++) set(14 - i, 8, bit(i));
      for (let i = 0; i < 8; i++) set(size - 1 - i, 8, bit(i)); for (let i = 8; i < 15; i++) set(8, size - 15 + i, bit(i)); set(8, size - 8, true);
    };
    fmt(0);
    if (v >= 7) { let rem = v; for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25); const b = (v << 12) | rem; for (let i = 0; i < 18; i++) { const a = size - 11 + (i % 3), c = Math.floor(i / 3), d = ((b >>> i) & 1) !== 0; set(a, c, d); set(c, a, d); } }
    let i = 0;
    for (let right = size - 1; right >= 1; right -= 2) { if (right === 6) right = 5; for (let vert = 0; vert < size; vert++) for (let j = 0; j < 2; j++) { const x = right - j, up = ((right + 1) & 2) === 0, y = up ? size - 1 - vert : vert; if (!F[y][x] && i < out.length * 8) { M[y][x] = ((out[i >>> 3] >>> (7 - (i & 7))) & 1) !== 0; i++; } } }
    const mask = (m) => { for (let y = 0; y < size; y++) for (let x = 0; x < size; x++) { const inv = [(x + y) % 2 === 0, y % 2 === 0, x % 3 === 0, (x + y) % 3 === 0, (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0, ((x * y) % 2) + ((x * y) % 3) === 0, (((x * y) % 2) + ((x * y) % 3)) % 2 === 0, (((x + y) % 2) + ((x * y) % 3)) % 2 === 0][m]; if (!F[y][x] && inv) M[y][x] = !M[y][x]; } };
    const penalty = () => {
      let p = 0; const line = (get) => { for (let a = 0; a < size; a++) { let run = 1; for (let b = 1; b < size; b++) { if (get(a, b) === get(a, b - 1)) { run++; if (run === 5) p += 3; else if (run > 5) p++; } else run = 1; } } };
      line((a, b) => M[a][b]); line((a, b) => M[b][a]);
      for (let y = 0; y < size - 1; y++) for (let x = 0; x < size - 1; x++) if (M[y][x] === M[y][x + 1] && M[y][x] === M[y + 1][x] && M[y][x] === M[y + 1][x + 1]) p += 3;
      let dark = 0; M.forEach((r) => r.forEach((c) => { if (c) dark++; })); p += Math.floor(Math.abs(dark * 20 - size * size * 10) / (size * size)) * 10; return p;
    };
    let best = 0, bp = Infinity; for (let m = 0; m < 8; m++) { mask(m); fmt(m); const p = penalty(); if (p < bp) { bp = p; best = m; } mask(m); }
    mask(best); fmt(best);
    return { size, version: v, get: (x, y) => x >= 0 && y >= 0 && x < size && y < size && M[y][x] };
  }
  function svg(text, o) {
    o = o || {}; const q = make(text, o.ecc || 'M'), b = o.border == null ? 2 : o.border, n = q.size + b * 2; let d = '';
    for (let y = 0; y < q.size; y++) for (let x = 0; x < q.size; x++) if (q.get(x, y)) d += 'M' + (x + b) + ' ' + (y + b) + 'h1v1h-1z';
    return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + n + ' ' + n + '" width="' + (o.px || 200) + '" height="' + (o.px || 200) + '" shape-rendering="crispEdges" role="img" aria-label="' + String(o.label || 'QR code').replace(/[&<>"']/g, (c) => '&#' + c.charCodeAt(0) + ';') + '"><rect width="' + n + '" height="' + n + '" fill="' + (o.bg || '#fff') + '"/><path d="' + d + '" fill="' + (o.fg || '#000') + '"/></svg>';
  }
  g.QR = { make, svg };
})(window);
