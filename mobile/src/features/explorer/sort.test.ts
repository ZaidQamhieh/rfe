import type { Entry } from '../../core/api/models';
import { naturalCompare, sortEntries } from './sort';

const f = (name: string): Entry => ({ name, path: `/${name}`, isDir: false, isSymlink: false });

describe('naturalCompare', () => {
  it('orders digit runs by value, not by character', () => {
    expect(naturalCompare('file2', 'file10')).toBeLessThan(0);
    expect(naturalCompare('file10', 'file2')).toBeGreaterThan(0);
    expect(naturalCompare('img_009', 'img_10')).toBeLessThan(0);
  });
  it('is case-insensitive and stable for equal names', () => {
    expect(naturalCompare('A1', 'a1')).toBe(0);
    expect(naturalCompare('same', 'same')).toBe(0);
  });
  it('handles huge numbers without overflow and plain text as before', () => {
    expect(naturalCompare('x99999999999999999999', 'x100000000000000000000')).toBeLessThan(0);
    expect(naturalCompare('abc', 'abd')).toBeLessThan(0);
    expect(naturalCompare('a', 'a1')).toBeLessThan(0);
  });
});

it('sortEntries by name is natural, in both directions', () => {
  const list = [f('file10.txt'), f('file2.txt'), f('file1.txt')];
  expect(sortEntries(list, { field: 'name', ascending: true }).map((e) => e.name)).toEqual(['file1.txt', 'file2.txt', 'file10.txt']);
  expect(sortEntries(list, { field: 'name', ascending: false }).map((e) => e.name)).toEqual(['file10.txt', 'file2.txt', 'file1.txt']);
});
