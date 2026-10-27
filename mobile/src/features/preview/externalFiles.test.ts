import { externalMime, safeFileName } from './externalFiles';

jest.mock('expo-file-system', () => ({ Directory: class {}, File: class {}, Paths: {} }));
jest.mock('../../core/native', () => ({}));
jest.mock('../../services', () => ({}));

describe('safeFileName', () => {
  it('keeps word characters, dots and dashes and replaces the rest', () => {
    expect(safeFileName('My Report (final).pdf')).toBe('My_Report__final_.pdf');
    expect(safeFileName('a/b\\c:d.txt')).toBe('a_b_c_d.txt');
  });
  it('cannot climb out of its folder or hide, and never comes back empty', () => {
    expect(safeFileName('../../etc/passwd')).toBe('_.._etc_passwd');
    expect(safeFileName('.hidden')).toBe('hidden');
    expect(safeFileName('...')).toBe('file');
    expect(safeFileName('')).toBe('file');
  });
  it('bounds very long names, keeping the extension end', () => {
    const n = safeFileName(`${'x'.repeat(300)}.jpg`);
    expect(n.length).toBe(120);
    expect(n.endsWith('.jpg')).toBe(true);
  });
});

describe('externalMime', () => {
  it('drops parameters and falls back to the extension', () => {
    expect(externalMime({ name: 'a.md', mimeType: 'text/markdown; charset=utf-8' })).toBe('text/markdown');
    expect(externalMime({ name: 'a.pdf' })).toBe('application/pdf');
    expect(externalMime({ name: 'a.unknown' })).toBe('application/octet-stream');
    expect(externalMime({ name: 'a.pdf', mimeType: '' })).toBe('application/pdf');
  });
});
