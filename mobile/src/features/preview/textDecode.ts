// Port of decodeAsText (text_editor.dart): strict UTF-8 or a NotText error, never garbled output.
// Hand-rolled because Hermes' TextDecoder support for `fatal` is not guaranteed.

export class NotTextError extends Error {
  constructor() {
    super("Can't preview this as text — it doesn't look like a valid UTF-8 text file.");
    this.name = 'NotTextError';
  }
}

/** NUL in the first 8 KiB means binary; otherwise decode as strict UTF-8 (BOM dropped). */
export function decodeAsText(bytes: Uint8Array): string {
  const sample = Math.min(bytes.length, 8192);
  for (let i = 0; i < sample; i++) if (bytes[i] === 0) throw new NotTextError();

  let i = bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf ? 3 : 0;
  const parts: string[] = [];
  let codes: number[] = [];
  const flush = () => {
    parts.push(String.fromCharCode(...codes));
    codes = [];
  };
  const cont = (k: number) => {
    const b = bytes[k];
    if (b === undefined || (b & 0xc0) !== 0x80) throw new NotTextError();
    return b & 0x3f;
  };
  while (i < bytes.length) {
    const b = bytes[i];
    let cp: number;
    if (b < 0x80) {
      cp = b;
      i += 1;
    } else if (b >= 0xc2 && b <= 0xdf) {
      cp = ((b & 0x1f) << 6) | cont(i + 1);
      i += 2;
    } else if (b >= 0xe0 && b <= 0xef) {
      cp = ((b & 0x0f) << 12) | (cont(i + 1) << 6) | cont(i + 2);
      // overlong forms and UTF-16 surrogates are invalid UTF-8
      if (cp < 0x800 || (cp >= 0xd800 && cp <= 0xdfff)) throw new NotTextError();
      i += 3;
    } else if (b >= 0xf0 && b <= 0xf4) {
      cp = ((b & 0x07) << 18) | (cont(i + 1) << 12) | (cont(i + 2) << 6) | cont(i + 3);
      if (cp < 0x10000 || cp > 0x10ffff) throw new NotTextError();
      i += 4;
    } else {
      throw new NotTextError();
    }
    if (cp > 0xffff) {
      cp -= 0x10000;
      codes.push(0xd800 + (cp >> 10), 0xdc00 + (cp & 0x3ff));
    } else {
      codes.push(cp);
    }
    if (codes.length >= 8192) flush();
  }
  flush();
  return parts.join('');
}

/** UTF-8 byte length of [s] (for the editor's 5 MiB PUT cap). */
export function utf8Length(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x80) n += 1;
    else if (c < 0x800) n += 2;
    else if (c >= 0xd800 && c <= 0xdbff) {
      n += 4;
      i++;
    } else n += 3;
  }
  return n;
}
