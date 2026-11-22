import type { Entry } from '../../core/api/models';
import { buildGridRows, cellSize, dayKey, folderSummary, formatDayLabel, formatDuration, groupByDay } from './gridLogic';

// Local-time timestamps (no zone suffix) so the day does not depend on the machine's zone.
const e = (name: string, modified?: string, o: Partial<Entry> = {}): Entry => ({ name, path: `/p/${name}`, isDir: false, modified, ...o }) as Entry;

describe('dayKey / formatDayLabel', () => {
  it('uses the local calendar day and tolerates bad input', () => {
    expect(dayKey('2026-11-08T21:30:00')).toBe('2026-11-08');
    expect(dayKey(undefined)).toBe('');
    expect(dayKey('not a date')).toBe('');
  });

  it('writes "8 November", adding the year for another year', () => {
    const now = new Date(2026, 10, 10);
    expect(formatDayLabel(new Date(2026, 10, 8), now)).toBe('8 November');
    expect(formatDayLabel(new Date(2025, 10, 8), now)).toBe('8 November 2025');
  });
});

describe('groupByDay', () => {
  it('splits neighbouring entries by day and keeps their order', () => {
    const g = groupByDay([e('a', '2026-11-08T10:00:00'), e('b', '2026-11-08T09:00:00'), e('c', '2026-11-02T10:00:00'), e('d')]);
    expect(g.map((x) => [x.key, x.entries.map((y) => y.name)])).toEqual([
      ['2026-11-08', ['a', 'b']],
      ['2026-11-02', ['c']],
      ['', ['d']],
    ]);
    expect(g[2].date).toBeNull();
  });

  it('starts a new group when a day comes back after another one', () => {
    expect(groupByDay([e('a', '2026-11-08T10:00:00'), e('b', '2026-11-07T10:00:00'), e('c', '2026-11-08T08:00:00')])).toHaveLength(3);
  });
});

describe('buildGridRows', () => {
  const label = (d: Date | null) => (d ? formatDayLabel(d, new Date(2026, 10, 10)) : 'Undated');

  it('puts a header before each day and starts its cells on a new line', () => {
    const rows = buildGridRows([e('a', '2026-11-08T10:00:00'), e('b', '2026-11-08T09:00:00'), e('c', '2026-11-08T08:00:00'), e('d', '2026-11-08T07:00:00'), e('x', '2026-11-02T10:00:00')], 3, { byDay: true, label });
    expect(rows.map((r) => (r.type === 'day' ? `day ${r.label} ${r.count}` : `cells ${r.entries.map((y) => y.name).join('')}`))).toEqual(['day 8 November 4', 'cells abc', 'cells d', 'day 2 November 1', 'cells x']);
  });

  it('fills lines in order without headers when grouping is off', () => {
    const rows = buildGridRows([e('a'), e('b'), e('c'), e('d')], 3, { byDay: false, label });
    expect(rows.map((r) => r.type)).toEqual(['cells', 'cells']);
    expect(new Set(rows.map((r) => r.key)).size).toBe(2);
  });

  it('gives an empty list no rows and treats a bad column count as one', () => {
    expect(buildGridRows([], 3, { byDay: true, label })).toEqual([]);
    expect(buildGridRows([e('a'), e('b')], 0, { byDay: false, label })).toHaveLength(2);
  });
});

describe('cellSize', () => {
  it('shares the width between columns minus the gaps', () => {
    expect(cellSize(412, 3, 2)).toBe(136);
    expect(cellSize(0, 3, 2)).toBe(0);
    expect(cellSize(1, 3, 2)).toBe(0);
  });
});

describe('formatDuration', () => {
  it('writes m:ss and h:mm:ss, empty when unknown', () => {
    expect(formatDuration(42_000)).toBe('0:42');
    expect(formatDuration(68_000)).toBe('1:08');
    expect(formatDuration(3_725_000)).toBe('1:02:05');
    expect(formatDuration(0)).toBe('');
    expect(formatDuration(undefined)).toBe('');
    expect(formatDuration(NaN)).toBe('');
  });
});

describe('folderSummary', () => {
  it('counts items and adds the size of the files', () => {
    expect(folderSummary([e('a', undefined, { size: 2048 }), e('b', undefined, { size: 1024 }), e('dir', undefined, { isDir: true, size: 999 })], false)).toBe('3 items · 3.0 KB');
  });

  it('says 1 item, hides a zero size and marks more pages with a plus', () => {
    expect(folderSummary([e('a')], false)).toBe('1 item');
    expect(folderSummary([e('a')], true)).toBe('1+ items');
    expect(folderSummary([], false)).toBe('0 items');
  });
});
