import { filterPalette, normalizeTypedPath } from './paletteLogic';

const items = [{ id: 'a', label: 'Search' }, { id: 'b', label: 'Refresh' }, { id: 'c', label: 'Storage by Type' }];

describe('filterPalette', () => {
  it('keeps everything for an empty or blank query', () => {
    expect(filterPalette(items, '')).toEqual(items);
    expect(filterPalette(items, '  ')).toEqual(items);
  });
  it('matches substrings ignoring case', () => {
    expect(filterPalette(items, 'RE').map((i) => i.id)).toEqual(['b']);
    expect(filterPalette(items, 'type').map((i) => i.id)).toEqual(['c']);
    expect(filterPalette(items, 'zzz')).toEqual([]);
  });
});

describe('normalizeTypedPath', () => {
  it('trims and rejects blanks', () => {
    expect(normalizeTypedPath('  /home/me  ')).toBe('/home/me');
    expect(normalizeTypedPath('   ')).toBeNull();
  });
});
