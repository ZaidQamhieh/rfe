// Minimal read-only zip + raw-DEFLATE reader in pure JS, used to open OOXML files (docx, xlsx, pptx) offline.
// Why hand-written: the app has no zip library and none could be added without network access; swap in
// a maintained one (for example fflate) if a dependency is later added. Every read is bounded: entry
// count, output size and input bounds are checked, so a hostile or corrupt archive throws [ZipError].

/** Thrown for anything that is not a readable, in-limits zip entry or OOXML part. */
export class ZipError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ZipError';
  }
}

export type ZipEntry = { name: string; method: number; flags: number; compressedSize: number; size: number; offset: number };

const EOCD_SIG = 0x06054b50;
const CENTRAL_SIG = 0x02014b50;
const LOCAL_SIG = 0x04034b50;
const EOCD_MIN = 22;
const MAX_COMMENT = 0xffff;
const ZIP64_MARKER_16 = 0xffff;
const ZIP64_MARKER_32 = 0xffffffff;
const METHOD_STORED = 0;
const METHOD_DEFLATE = 8;
const FLAG_ENCRYPTED = 1;
/** Entries read from the central directory at most; Office files hold tens to a few thousand parts. */
const MAX_ZIP_ENTRIES = 20_000;
const MAX_BITS = 15;

/** Lists the entries of a zip held in memory by reading its central directory (zip64 and multi-disk are refused). */
export function listZip(bytes: Uint8Array): ZipEntry[] {
  if (bytes.length < EOCD_MIN) throw new ZipError('Not a zip file');
  const v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let eocd = -1;
  for (let i = bytes.length - EOCD_MIN; i >= Math.max(0, bytes.length - EOCD_MIN - MAX_COMMENT); i--) {
    if (v.getUint32(i, true) === EOCD_SIG) {
      eocd = i;
      break;
    }
  }
  if (eocd < 0) throw new ZipError('Not a zip file');
  const count = v.getUint16(eocd + 10, true);
  const cdOffset = v.getUint32(eocd + 16, true);
  if (count === ZIP64_MARKER_16 || cdOffset === ZIP64_MARKER_32) throw new ZipError('Zip64 archives are not supported');
  if (count > MAX_ZIP_ENTRIES) throw new ZipError('Too many entries in the archive');
  const names = new TextDecoder('utf-8');
  const entries: ZipEntry[] = [];
  let p = cdOffset;
  for (let n = 0; n < count; n++) {
    if (p + 46 > bytes.length || v.getUint32(p, true) !== CENTRAL_SIG) throw new ZipError('Corrupt zip directory');
    const nameLen = v.getUint16(p + 28, true);
    const extraLen = v.getUint16(p + 30, true);
    const commentLen = v.getUint16(p + 32, true);
    if (p + 46 + nameLen > bytes.length) throw new ZipError('Corrupt zip directory');
    entries.push({
      name: names.decode(bytes.subarray(p + 46, p + 46 + nameLen)),
      flags: v.getUint16(p + 8, true),
      method: v.getUint16(p + 10, true),
      compressedSize: v.getUint32(p + 20, true),
      size: v.getUint32(p + 24, true),
      offset: v.getUint32(p + 42, true),
    });
    p += 46 + nameLen + extraLen + commentLen;
  }
  return entries;
}

/** Bytes of one entry, inflated when needed; refuses encrypted entries and any output above [maxBytes]. */
export function readZipEntry(bytes: Uint8Array, entry: ZipEntry, maxBytes: number): Uint8Array {
  if (entry.flags & FLAG_ENCRYPTED) throw new ZipError('Encrypted files are not supported');
  if (entry.size > maxBytes) throw new ZipError('Part of the file is too large to preview');
  const v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (entry.offset + 30 > bytes.length || v.getUint32(entry.offset, true) !== LOCAL_SIG) throw new ZipError('Corrupt zip entry');
  const start = entry.offset + 30 + v.getUint16(entry.offset + 26, true) + v.getUint16(entry.offset + 28, true);
  const end = start + entry.compressedSize;
  if (end > bytes.length) throw new ZipError('Zip file is truncated');
  const body = bytes.subarray(start, end);
  if (entry.method === METHOD_STORED) {
    if (body.length > maxBytes) throw new ZipError('Part of the file is too large to preview');
    return body;
  }
  if (entry.method === METHOD_DEFLATE) return inflateRaw(body, maxBytes);
  throw new ZipError(`Unsupported compression method ${entry.method}`);
}

/** One entry decoded as UTF-8 text (see [readZipEntry] for the limits). */
export function readZipText(bytes: Uint8Array, entry: ZipEntry, maxBytes: number): string {
  return new TextDecoder('utf-8').decode(readZipEntry(bytes, entry, maxBytes));
}

type Huffman = { count: Uint16Array; symbol: Uint16Array };

/** Canonical Huffman decoding table from code lengths; rejects over-subscribed sets (as in zlib's puff.c). */
function buildHuffman(lengths: ArrayLike<number>, n: number): Huffman {
  const count = new Uint16Array(MAX_BITS + 1);
  for (let i = 0; i < n; i++) count[lengths[i]]++;
  let left = 1;
  for (let len = 1; len <= MAX_BITS; len++) {
    left = (left << 1) - count[len];
    if (left < 0) throw new ZipError('Corrupt compressed data');
  }
  const offs = new Uint16Array(MAX_BITS + 2);
  for (let len = 1; len <= MAX_BITS; len++) offs[len + 1] = offs[len] + count[len];
  const symbol = new Uint16Array(n);
  for (let s = 0; s < n; s++) if (lengths[s] !== 0) symbol[offs[lengths[s]]++] = s;
  return { count, symbol };
}

const LEN_BASE = [3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258];
const LEN_EXTRA = [0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0];
const DIST_BASE = [1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577];
const DIST_EXTRA = [0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13];
const CODE_LENGTH_ORDER = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15];
const FIXED_LIT_SYMBOLS = 288;
const FIXED_DIST_SYMBOLS = 30;
const END_OF_BLOCK = 256;

let fixedTables: { lit: Huffman; dist: Huffman } | null = null;
function fixedHuffman() {
  if (!fixedTables) {
    const lit = new Uint8Array(FIXED_LIT_SYMBOLS).fill(8, 0, 144).fill(9, 144, 256).fill(7, 256, 280).fill(8, 280);
    fixedTables = { lit: buildHuffman(lit, FIXED_LIT_SYMBOLS), dist: buildHuffman(new Uint8Array(FIXED_DIST_SYMBOLS).fill(5), FIXED_DIST_SYMBOLS) };
  }
  return fixedTables;
}

/**
 * Decompresses a raw DEFLATE stream (RFC 1951: stored, fixed and dynamic Huffman blocks). The output is
 * never allowed to grow past [maxOut] bytes; corrupt or truncated input throws [ZipError].
 */
export function inflateRaw(src: Uint8Array, maxOut: number): Uint8Array {
  let pos = 0;
  let bitBuf = 0;
  let bitCnt = 0;
  let len = 0;
  let out = new Uint8Array(Math.max(1, Math.min(src.length * 4, maxOut, 1 << 20)));
  const fail = (m = 'Corrupt compressed data'): never => {
    throw new ZipError(m);
  };
  const bits = (n: number): number => {
    while (bitCnt < n) {
      if (pos >= src.length) fail('Unexpected end of compressed data');
      bitBuf |= src[pos++] << bitCnt;
      bitCnt += 8;
    }
    const value = bitBuf & ((1 << n) - 1);
    bitBuf >>>= n;
    bitCnt -= n;
    return value;
  };
  const room = (n: number) => {
    if (len + n > maxOut) fail('Part of the file is too large to preview');
    if (len + n > out.length) {
      const grown = new Uint8Array(Math.min(maxOut, Math.max(out.length * 2, len + n)));
      grown.set(out.subarray(0, len));
      out = grown;
    }
  };
  const decode = (h: Huffman): number => {
    let code = 0;
    let first = 0;
    let index = 0;
    for (let l = 1; l <= MAX_BITS; l++) {
      code |= bits(1);
      const count = h.count[l];
      if (code - count < first) return h.symbol[index + (code - first)];
      index += count;
      first = (first + count) << 1;
      code <<= 1;
    }
    return fail();
  };
  const codes = (lit: Huffman, dist: Huffman) => {
    for (;;) {
      let sym = decode(lit);
      if (sym < END_OF_BLOCK) {
        room(1);
        out[len++] = sym;
      } else if (sym === END_OF_BLOCK) {
        return;
      } else {
        sym -= END_OF_BLOCK + 1;
        if (sym >= LEN_BASE.length) fail();
        const copy = LEN_BASE[sym] + bits(LEN_EXTRA[sym]);
        const dsym = decode(dist);
        if (dsym >= DIST_BASE.length) fail();
        const back = DIST_BASE[dsym] + bits(DIST_EXTRA[dsym]);
        if (back > len) fail();
        room(copy);
        for (let i = 0; i < copy; i++, len++) out[len] = out[len - back];
      }
    }
  };
  let last = 0;
  while (!last) {
    last = bits(1);
    const type = bits(2);
    if (type === 0) {
      bitBuf = 0;
      bitCnt = 0;
      if (pos + 4 > src.length) fail('Unexpected end of compressed data');
      const n = src[pos] | (src[pos + 1] << 8);
      if ((n ^ 0xffff) !== (src[pos + 2] | (src[pos + 3] << 8))) fail();
      pos += 4;
      if (pos + n > src.length) fail('Unexpected end of compressed data');
      room(n);
      out.set(src.subarray(pos, pos + n), len);
      len += n;
      pos += n;
    } else if (type === 1) {
      const f = fixedHuffman();
      codes(f.lit, f.dist);
    } else if (type === 2) {
      const nlen = bits(5) + 257;
      const ndist = bits(5) + 1;
      const ncode = bits(4) + 4;
      if (nlen > 286 || ndist > FIXED_DIST_SYMBOLS) fail();
      const lengths = new Uint8Array(nlen + ndist);
      const meta = new Uint8Array(19);
      for (let i = 0; i < ncode; i++) meta[CODE_LENGTH_ORDER[i]] = bits(3);
      const metaCode = buildHuffman(meta, 19);
      for (let i = 0; i < nlen + ndist; ) {
        const sym = decode(metaCode);
        if (sym < 16) {
          lengths[i++] = sym;
          continue;
        }
        let value = 0;
        let repeat: number;
        if (sym === 16) {
          if (i === 0) fail();
          value = lengths[i - 1];
          repeat = 3 + bits(2);
        } else if (sym === 17) repeat = 3 + bits(3);
        else repeat = 11 + bits(7);
        if (i + repeat > nlen + ndist) fail();
        while (repeat--) lengths[i++] = value;
      }
      if (lengths[END_OF_BLOCK] === 0) fail();
      codes(buildHuffman(lengths.subarray(0, nlen), nlen), buildHuffman(lengths.subarray(nlen), ndist));
    } else fail();
  }
  return out.slice(0, len);
}
