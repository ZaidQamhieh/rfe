import type { Entry } from '../../core/api/models';
import { defaultVisibility } from '../../core/visibility';
import { activeFilterCount, defaultFilters, filterSearchResults, highlightRange, isGlobQuery, minBytesFor, queryForMode, resolveDatePreset, sortByRelevance } from './searchLogic';

const e = (name: string, isDir = false): Entry => ({ name, path: `/${name}`, isDir, isSymlink: false });

describe('query handling', () => {
  it('detects globs and approximates regex with wildcards', () => {
    expect(isGlobQuery('a*')).toBe(true);
    expect(isGlobQuery('a?b')).toBe(true);
    expect(isGlobQuery('plain')).toBe(false);
    expect(queryForMode('foo', 'regex')).toBe('*foo*');
    expect(queryForMode('foo', 'glob')).toBe('foo');
    expect(queryForMode('foo', 'substring')).toBe('foo');
  });
});

describe('sortByRelevance', () => {
  const list = [e('zebra-report'), e('Report.txt'), e('a-report'), e('REPORTS')];
  it('puts names starting with the query first, then alphabetical', () => {
    expect(sortByRelevance(list, 'report').map((x) => x.name)).toEqual(['Report.txt', 'REPORTS', 'a-report', 'zebra-report']);
  });
  it('is purely alphabetical for globs and empty queries and does not mutate its input', () => {
    const copy = [...list];
    expect(sortByRelevance(list, 'r*').map((x) => x.name)).toEqual(['a-report', 'Report.txt', 'REPORTS', 'zebra-report']);
    expect(sortByRelevance(list, '  ').map((x) => x.name)).toEqual(['a-report', 'Report.txt', 'REPORTS', 'zebra-report']);
    expect(list).toEqual(copy);
  });
});

describe('highlightRange', () => {
  it('finds the first case-insensitive match', () => {
    expect(highlightRange('My Report Report', 'report')).toEqual({ start: 3, end: 9 });
    expect(highlightRange('abc', 'x')).toBeNull();
    expect(highlightRange('abc', '')).toBeNull();
    expect(highlightRange('abc', 'a*')).toBeNull();
  });
});

describe('filterSearchResults', () => {
  it('hides dotfiles unless hidden items are included', () => {
    const rs = [e('.git', true), e('a.txt')];
    expect(filterSearchResults(rs, defaultVisibility(), false).map((x) => x.name)).toEqual(['a.txt']);
    expect(filterSearchResults(rs, defaultVisibility(), true)).toBe(rs);
  });
});

describe('presets and filters', () => {
  it('resolves date presets against now', () => {
    const now = new Date(2026, 5, 15, 12, 0, 0);
    expect(resolveDatePreset('any', now)).toBeUndefined();
    expect(resolveDatePreset('last24h', now)?.getTime()).toBe(now.getTime() - 86_400_000);
    expect(resolveDatePreset('last7d', now)?.getTime()).toBe(now.getTime() - 7 * 86_400_000);
    expect(resolveDatePreset('thisYear', now)).toEqual(new Date(2026, 0, 1));
  });
  it('maps size presets to minimum bytes', () => {
    expect(minBytesFor('any')).toBeUndefined();
    expect(minBytesFor('mb10')).toBe(10 * 1024 * 1024);
    expect(minBytesFor('gb1')).toBe(1024 ** 3);
  });
  it('counts only non-default filters and never the scope toggle', () => {
    const f = defaultFilters();
    expect(activeFilterCount(f)).toBe(0);
    expect(activeFilterCount({ ...f, fromHere: false })).toBe(0);
    expect(activeFilterCount({ ...f, categories: ['image', 'video'], size: 'mb1', date: 'last7d', includeHidden: true })).toBe(5);
  });
});
