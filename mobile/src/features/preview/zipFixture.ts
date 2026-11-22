import { deflateRawSync } from 'node:zlib';

// Test-only helper: builds a small zip archive in memory (CRC left 0: the reader under test does not check it).

const enc = new TextEncoder();

/** Zip of [files] (name -> text or bytes); each entry is deflated unless [store] is set. [flags] sets the general-purpose bit flag. */
export function buildZip(files: Record<string, string | Uint8Array>, opts: { store?: boolean; flags?: number } = {}): Uint8Array {
  const parts: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;
  for (const [name, content] of Object.entries(files)) {
    const raw = Buffer.from(typeof content === 'string' ? enc.encode(content) : content);
    const body = opts.store ? raw : deflateRawSync(raw);
    const method = opts.store ? 0 : 8;
    const nameBytes = Buffer.from(enc.encode(name));
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(opts.flags ?? 0, 6);
    local.writeUInt16LE(method, 8);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(raw.length, 22);
    local.writeUInt16LE(nameBytes.length, 26);
    parts.push(local, nameBytes, body);
    const cd = Buffer.alloc(46);
    cd.writeUInt32LE(0x02014b50, 0);
    cd.writeUInt16LE(20, 4);
    cd.writeUInt16LE(20, 6);
    cd.writeUInt16LE(opts.flags ?? 0, 8);
    cd.writeUInt16LE(method, 10);
    cd.writeUInt32LE(body.length, 20);
    cd.writeUInt32LE(raw.length, 24);
    cd.writeUInt16LE(nameBytes.length, 28);
    cd.writeUInt32LE(offset, 42);
    central.push(cd, nameBytes);
    offset += local.length + nameBytes.length + body.length;
  }
  const cdBytes = Buffer.concat(central);
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0);
  eocd.writeUInt16LE(Object.keys(files).length, 8);
  eocd.writeUInt16LE(Object.keys(files).length, 10);
  eocd.writeUInt32LE(cdBytes.length, 12);
  eocd.writeUInt32LE(offset, 16);
  return new Uint8Array(Buffer.concat([...parts, cdBytes, eocd]));
}
