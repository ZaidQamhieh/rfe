import { NotTextError, decodeAsText, utf8Length } from './textDecode';

const enc = (s: string) => new Uint8Array(Buffer.from(s, 'utf8'));

describe('decodeAsText', () => {
  it('round-trips ASCII, multi-byte and astral text and drops a BOM', () => {
    const s = 'héllo — 日本 😀\nline2';
    expect(decodeAsText(enc(s))).toBe(s);
    expect(decodeAsText(new Uint8Array([0xef, 0xbb, 0xbf, 0x61]))).toBe('a');
    expect(decodeAsText(new Uint8Array())).toBe('');
  });
  it('decodes large inputs (chunked String.fromCharCode)', () => {
    const s = 'x'.repeat(100_000) + 'é';
    expect(decodeAsText(enc(s))).toBe(s);
  });
  it('rejects NUL, truncated, overlong and surrogate encodings', () => {
    for (const bad of [[0x61, 0x00], [0xc3], [0xc0, 0xaf], [0xe0, 0x80, 0xaf], [0xed, 0xa0, 0x80], [0xf5, 0x80, 0x80, 0x80], [0x80]]) {
      expect(() => decodeAsText(new Uint8Array(bad))).toThrow(NotTextError);
    }
  });
});

test('utf8Length matches Buffer', () => {
  for (const s of ['', 'abc', 'é', '日本', '😀x']) expect(utf8Length(s)).toBe(Buffer.byteLength(s, 'utf8'));
});
