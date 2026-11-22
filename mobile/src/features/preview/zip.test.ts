import { deflateRawSync } from 'node:zlib';

import { ZipError, inflateRaw, listZip, readZipText } from './zip';
import { buildZip } from './zipFixture';

// Deterministic pseudo-random bytes (a linear congruential generator) so the tests never depend on a seed.
function noise(n: number): Uint8Array {
  const out = new Uint8Array(n);
  let s = 12345;
  for (let i = 0; i < n; i++) {
    s = (s * 1103515245 + 12345) & 0x7fffffff;
    out[i] = s >> 16;
  }
  return out;
}

describe('inflateRaw', () => {
  it('matches zlib on text (dynamic Huffman), noise (stored blocks) and tiny input (fixed Huffman)', () => {
    const text = new TextEncoder().encode(Array.from({ length: 4000 }, (_, i) => `row ${i % 97} value ${(i * 31) % 1013}\n`).join(''));
    for (const src of [text, noise(50_000), new TextEncoder().encode('hello hello hello'), new Uint8Array(0)]) {
      expect(Array.from(inflateRaw(new Uint8Array(deflateRawSync(src)), 10_000_000))).toEqual(Array.from(src));
    }
  });
  it('refuses output beyond the cap and rejects corrupt data', () => {
    const packed = new Uint8Array(deflateRawSync(new Uint8Array(100_000)));
    expect(() => inflateRaw(packed, 1000)).toThrow(ZipError);
    expect(() => inflateRaw(new Uint8Array([0xff, 0xff, 0xff, 0xff]), 1000)).toThrow(ZipError);
    expect(() => inflateRaw(packed.slice(0, 5), 1_000_000)).toThrow(ZipError);
  });
});

describe('listZip / readZipText', () => {
  it('lists names and reads deflated and stored entries', () => {
    for (const store of [false, true]) {
      const zip = buildZip({ 'a.txt': 'alpha', 'dir/b é.xml': '<x>béta</x>' }, { store });
      const entries = listZip(zip);
      expect(entries.map((e) => e.name)).toEqual(['a.txt', 'dir/b é.xml']);
      expect(readZipText(zip, entries[0], 1000)).toBe('alpha');
      expect(readZipText(zip, entries[1], 1000)).toBe('<x>béta</x>');
    }
  });
  it('rejects an entry whose declared or real size exceeds the cap', () => {
    const zip = buildZip({ big: 'x'.repeat(5000) });
    expect(() => readZipText(zip, listZip(zip)[0], 100)).toThrow(ZipError);
  });
  it('rejects non-zip bytes, truncation and encrypted entries', () => {
    expect(() => listZip(new TextEncoder().encode('not a zip at all'))).toThrow(ZipError);
    expect(() => listZip(new Uint8Array(0))).toThrow(ZipError);
    const zip = buildZip({ a: 'x' }, { flags: 1 });
    expect(() => readZipText(zip, listZip(zip)[0], 100)).toThrow(/encrypted/i);
    const ok = buildZip({ a: 'hello world' });
    expect(() => readZipText(ok.slice(0, 20), listZip(ok)[0], 100)).toThrow(ZipError);
  });
});
