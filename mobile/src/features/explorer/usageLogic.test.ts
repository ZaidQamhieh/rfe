import { parseUsage } from '../../core/api/models';
import { barFraction, sharePercent } from './usageLogic';

describe('storage usage', () => {
  it('parses the answer and the time-budget header', () => {
    const r = parseUsage({ path: '/a', totalBytes: 30, entries: [{ name: 'x', isDir: true, bytes: 20 }, { name: 'y', bytes: 10 }] }, { 'x-search-time-budget': '1' });
    expect(r.partial).toBe(true);
    expect(r.entries).toEqual([{ name: 'x', isDir: true, bytes: 20 }, { name: 'y', isDir: false, bytes: 10 }]);
  });
  it('survives a malformed body', () => {
    expect(parseUsage(null, {})).toEqual({ path: '', totalBytes: 0, partial: false, entries: [] });
  });
  it('computes bar fractions and percents', () => {
    const e = [{ name: 'a', isDir: true, bytes: 80 }, { name: 'b', isDir: false, bytes: 20 }];
    expect(barFraction(e[1], e)).toBe(0.25);
    expect(sharePercent(20, 100)).toBe('20');
    expect(sharePercent(1, 1000)).toBe('<1');
    expect(sharePercent(5, 0)).toBe('0');
  });
});
