import type { Entry } from '../../core/api/models';
import { filterByType, typeChips } from './typeFilter';

const f = (name: string, mimeType?: string, isDir = false): Entry => ({ name, path: `/r/${name}`, isDir, isSymlink: false, mimeType });
const list = [f('d', undefined, true), f('a.jpg', 'image/jpeg'), f('b.png', 'image/png'), f('c.mp4', 'video/mp4'), f('n.txt', 'text/plain'), f('x.bin')];

describe('typeChips', () => {
  it('lists only the categories present, in a fixed order', () => {
    expect(typeChips(list)).toEqual(['folder', 'image', 'video', 'document', 'other']);
  });
  it('offers nothing when everything is one kind: a single chip filters nothing', () => {
    expect(typeChips([f('a.jpg', 'image/jpeg'), f('b.jpg', 'image/jpeg')])).toEqual([]);
    expect(typeChips([])).toEqual([]);
  });
});

describe('filterByType', () => {
  it('keeps one category, and everything when none is active', () => {
    expect(filterByType(list, 'image').map((e) => e.name)).toEqual(['a.jpg', 'b.png']);
    expect(filterByType(list, 'folder').map((e) => e.name)).toEqual(['d']);
    expect(filterByType(list, null)).toBe(list);
  });
});
