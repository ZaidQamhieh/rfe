import type { Entry } from '../../core/api/models';
import { deleteSubtitle, pathsText, rangePaths, selectedBytes } from './selectionLogic';

const f = (name: string, size?: number, isDir = false): Entry => ({ name, path: `/r/${name}`, isDir, isSymlink: false, size });

describe('rangePaths', () => {
  const order = ['/r/a', '/r/b', '/r/c', '/r/d', '/r/e'];
  it('covers everything between the anchor and the target, in either direction', () => {
    expect(rangePaths(order, '/r/b', '/r/d')).toEqual(['/r/b', '/r/c', '/r/d']);
    expect(rangePaths(order, '/r/d', '/r/b')).toEqual(['/r/b', '/r/c', '/r/d']);
  });
  it('is just the target without an anchor, or when the anchor is not on screen', () => {
    expect(rangePaths(order, null, '/r/c')).toEqual(['/r/c']);
    expect(rangePaths(order, '/r/gone', '/r/c')).toEqual(['/r/c']);
  });
  it('is empty when the target is not on screen', () => {
    expect(rangePaths(order, '/r/a', '/r/zzz')).toEqual([]);
  });
});

describe('selectedBytes', () => {
  it('sums the files that are selected and skips folders and unknown sizes', () => {
    const entries = [f('a', 100), f('b', 50), f('dir', 4096, true), f('nosize'), f('c', 7)];
    const sel = new Set(['/r/a', '/r/dir', '/r/nosize', '/r/c', '/r/elsewhere']);
    expect(selectedBytes(entries, sel)).toBe(107);
  });
});

describe('pathsText', () => {
  it('joins the selected paths with newlines in listing order', () => {
    const entries = [f('a'), f('b'), f('c')];
    expect(pathsText(entries, new Set(['/r/c', '/r/a']))).toBe('/r/a\n/r/c');
  });
});

describe('deleteSubtitle', () => {
  it('names the host and, for one item, the item', () => {
    expect(deleteSubtitle({ count: 1, hostLabel: 'Desktop', name: 'a.txt' })).toContain('"a.txt"');
    expect(deleteSubtitle({ count: 1, hostLabel: 'Desktop', name: 'a.txt' })).toContain('Desktop');
    const many = deleteSubtitle({ count: 3, hostLabel: 'Laptop' });
    expect(many).toContain('Laptop');
    expect(many).toContain('3');
  });
});
