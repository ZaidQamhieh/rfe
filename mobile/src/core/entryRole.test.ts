import { roleOf } from './entryCategory';

describe('roleOf', () => {
  it.each([
    [{ isDir: true }, 'folder'],
    [{ isDir: false, mimeType: 'image/png' }, 'photo'],
    [{ isDir: false, mimeType: 'video/mp4' }, 'photo'],
    [{ isDir: false, mimeType: 'audio/mpeg' }, 'route'],
    [{ isDir: false, mimeType: 'application/pdf' }, 'doc'],
    [{ isDir: false, mimeType: 'text/markdown' }, 'doc'],
    [{ isDir: false, mimeType: 'application/zip' }, 'warn'],
    [{ isDir: false, mimeType: 'application/octet-stream', name: 'notes.docx' }, 'doc'],
    [{ isDir: false, mimeType: 'application/octet-stream', name: 'pack.tar' }, 'warn'],
  ] as const)('%j -> %s', (entry, role) => {
    expect(roleOf(entry as never)).toBe(role);
  });

  it('never colours unknown types', () => {
    expect(roleOf({ isDir: false, mimeType: 'application/octet-stream', name: 'big.bin' })).toBeNull();
    expect(roleOf({ isDir: false })).toBeNull();
  });
});
