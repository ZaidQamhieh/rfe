import { bitsToOctal, bitsToSymbolic, extractFolderName, hashMatches, isExtractableArchive, parseModeBits } from './metaLogic';
import { parentDirOf } from './paths';

describe('isExtractableArchive', () => {
  it('matches the formats the agent extracts, case-insensitively', () => {
    for (const n of ['a.zip', 'A.ZIP', 'x.tar.gz', 'x.TGZ']) expect(isExtractableArchive(n)).toBe(true);
    for (const n of ['a.tar', 'a.rar', 'a.gz', 'zip', 'a.zip.txt']) expect(isExtractableArchive(n)).toBe(false);
  });
});

describe('permission bits', () => {
  it('round-trips a listing mode through octal and symbolic forms', () => {
    const bits = parseModeBits('-rwxr-xr--');
    expect(bitsToOctal(bits)).toBe('0754');
    expect(bitsToSymbolic(bits)).toBe('rwxr-xr--');
  });
  it('reads a missing or short mode as no access', () => {
    expect(bitsToOctal(parseModeBits(undefined))).toBe('0000');
    expect(bitsToOctal(parseModeBits('rwx'))).toBe('0000');
  });
  it('encodes all bits', () => {
    expect(bitsToOctal(parseModeBits('-rwxrwxrwx'))).toBe('0777');
    expect(bitsToOctal(parseModeBits('drw-r-----'))).toBe('0640');
  });
});

describe('parentDirOf', () => {
  it('keeps the separator style and stops at the root', () => {
    expect(parentDirOf('/a/b.txt')).toBe('/a');
    expect(parentDirOf('/a')).toBe('/');
    expect(parentDirOf('C:\\a\\b.txt')).toBe('C:\\a');
    expect(parentDirOf('/')).toBe('/');
    expect(parentDirOf('C:\\x.zip')).toBe('C:\\');
    expect(parentDirOf('C:\\a')).toBe('C:\\');
  });
});

describe('extractFolderName', () => {
  it('names the folder after the archive without its extension', () => {
    expect(extractFolderName('photos.zip', new Set())).toBe('photos');
    expect(extractFolderName('Backup.TAR.GZ', new Set())).toBe('Backup');
    expect(extractFolderName('src.tgz', new Set())).toBe('src');
    expect(extractFolderName('v1.2.zip', new Set())).toBe('v1.2');
  });
  it('picks a free name instead of merging into an existing folder', () => {
    expect(extractFolderName('a.zip', new Set(['a']))).toBe('a (1)');
    expect(extractFolderName('a.zip', new Set(['a', 'a (1)', 'a (2)']))).toBe('a (3)');
    expect(extractFolderName('a.zip', new Set(['a.zip']))).toBe('a');
  });
  it('treats names that differ only in case as taken (Windows and macOS hosts are case-insensitive)', () => {
    expect(extractFolderName('photos.zip', new Set(['Photos']))).toBe('photos (1)');
  });
  it('never returns an empty name', () => {
    expect(extractFolderName('.zip', new Set())).toBe('extracted');
  });
});

describe('hashMatches', () => {
  const h = 'ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789';
  it('ignores case and surrounding or inner whitespace', () => {
    expect(hashMatches(` ${h.toLowerCase()}\n`, h)).toBe(true);
    expect(hashMatches(h.toLowerCase().replace(/(.{8})/g, '$1 '), h)).toBe(true);
  });
  it('accepts a sha256sum line and a sha256: prefix', () => {
    expect(hashMatches(`${h}  photo.jpg`, h)).toBe(true);
    expect(hashMatches(`sha256:${h}`, h)).toBe(true);
  });
  it('rejects a different or empty hash', () => {
    expect(hashMatches(`${h.slice(0, -1)}0`, h)).toBe(false);
    expect(hashMatches('', h)).toBe(false);
    expect(hashMatches('not a hash', h)).toBe(false);
  });
});
