import type { Entry } from '../../core/api/models';
import { categoryOf, iconKindFor } from '../../core/entryCategory';
import { defaultVisibility, extensionOf, isEntryHidden, isEntryHiddenInPicker } from '../../core/visibility';
import { basenameOf, buildPathStack, buildPathStackWithinRoot, crumbLabel, dedupedName, folderLabel, joinRemotePath, renameDestination } from './paths';
import { sortEntries } from './sort';

const e = (name: string, o: Partial<Entry> = {}): Entry => ({ name, path: `/${name}`, isDir: false, isSymlink: false, ...o });

describe('paths (POSIX and Windows)', () => {
  it('basename, label, join, rename keep the separator', () => {
    expect(basenameOf('C:\\a\\b.txt')).toBe('b.txt');
    expect(basenameOf('/a/b/')).toBe('b');
    expect(folderLabel('/')).toBe('Root');
    expect(folderLabel('C:\\')).toBe('Root');
    expect(folderLabel('/home/x')).toBe('x');
    expect(joinRemotePath('C:\\dir', 'n')).toBe('C:\\dir\\n');
    expect(joinRemotePath('/', 'n')).toBe('/n');
    expect(joinRemotePath('/a/', 'n')).toBe('/a/n');
    expect(renameDestination('C:\\dir\\f', 'g')).toBe('C:\\dir\\g');
    expect(renameDestination('/f', 'g')).toBe('/g');
    expect(renameDestination('/a/f', 'g')).toBe('/a/g');
  });
  it('buildPathStack and within-root stacks', () => {
    expect(buildPathStack('/home/x/Storage')).toEqual(['/', '/home', '/home/x', '/home/x/Storage']);
    expect(buildPathStack('C:\\a\\b')).toEqual(['C:\\', 'C:\\a', 'C:\\a\\b']);
    expect(buildPathStackWithinRoot('/home/x', '/home/x/a/b')).toEqual(['/home/x', '/home/x/a', '/home/x/a/b']);
    expect(buildPathStackWithinRoot('/home/x', '/etc/passwd')).toEqual(['/home/x']);
    expect(buildPathStackWithinRoot('C:\\Users', 'c:\\users\\me')).toEqual(['C:\\Users', 'c:\\users\\me']);
  });
  it('dedupedName inserts before the extension', () => {
    const s = new Set(['photo.jpg', 'photo (1).jpg', '.bashrc', 'README']);
    expect(dedupedName('photo.jpg', s)).toBe('photo (2).jpg');
    expect(dedupedName('new.txt', s)).toBe('new.txt');
    expect(dedupedName('.bashrc', s)).toBe('.bashrc (1)');
    expect(dedupedName('README', s)).toBe('README (1)');
  });
});

describe('sorting', () => {
  const list = [e('b.txt', { size: 5 }), e('A', { isDir: true }), e('a.txt', { size: 9 }), e('z', { isDir: true })];
  it('folders first, name is case-insensitive, direction flips within groups', () => {
    expect(sortEntries(list, { field: 'name', ascending: true }).map((x) => x.name)).toEqual(['A', 'z', 'a.txt', 'b.txt']);
    expect(sortEntries(list, { field: 'name', ascending: false }).map((x) => x.name)).toEqual(['z', 'A', 'b.txt', 'a.txt']);
    expect(sortEntries(list, { field: 'size', ascending: false }).map((x) => x.name).slice(2)).toEqual(['a.txt', 'b.txt']);
  });
  it('date sorts missing dates as oldest', () => {
    const d = [e('new', { modified: '2026-01-02T00:00:00Z' }), e('none'), e('old', { modified: '2025-01-01T00:00:00Z' })];
    expect(sortEntries(d, { field: 'date', ascending: true }).map((x) => x.name)).toEqual(['none', 'old', 'new']);
  });
});

describe('visibility', () => {
  const p = { ...defaultVisibility(), hiddenExtensions: new Set(['LOG']), hiddenNames: new Set(['Thumbs.db']) };
  it('applies dotfile, extension and name rules; extension never hides folders', () => {
    expect(isEntryHidden(e('.git', { isDir: true }), p)).toBe(true);
    expect(isEntryHidden(e('a.log'), p)).toBe(true);
    expect(isEntryHidden(e('a.log', { isDir: true }), p)).toBe(false);
    expect(isEntryHidden(e('thumbs.DB'), p)).toBe(true);
    expect(isEntryHidden(e('a.txt'), p)).toBe(false);
    expect(isEntryHiddenInPicker(e('a.log'), p)).toBe(false);
    expect(isEntryHiddenInPicker(e('.x'), p)).toBe(true);
    expect(extensionOf('.bashrc')).toBe('');
    expect(extensionOf('a.TAR.GZ')).toBe('gz');
  });
});

describe('categories', () => {
  it('maps MIME prefixes', () => {
    expect(categoryOf({ isDir: true })).toBe('folder');
    expect(categoryOf({ isDir: false, mimeType: 'image/png' })).toBe('image');
    expect(categoryOf({ isDir: false, mimeType: 'application/pdf' })).toBe('document');
    expect(categoryOf({ isDir: false, mimeType: 'application/zip' })).toBe('archive');
    expect(categoryOf({ isDir: false, mimeType: 'application/octet-stream' })).toBe('other');
    expect(iconKindFor({ isDir: false, mimeType: 'application/pdf' }).color).toBe('#F1596B');
  });
});

describe('crumbLabel', () => {
  it('names the root crumb after the root folder', () => {
    expect(crumbLabel(['/home/zaid/RFE Files'], 0)).toBe('RFE Files');
    expect(crumbLabel(['/home/zaid/RFE Files', '/home/zaid/RFE Files/Docs'], 1)).toBe('Docs');
    expect(crumbLabel(['C:\\Users\\x'], 0)).toBe('x');
  });

  it('never returns an empty root label', () => {
    expect(crumbLabel(['C:\\'], 0)).toBe('Root');
    expect(crumbLabel([''], 0)).toBe('/');
  });
});
