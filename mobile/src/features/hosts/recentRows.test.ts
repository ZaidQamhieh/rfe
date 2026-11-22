import type { Entry } from '../../core/api/models';
import { RECENT_SHOWN, recentRows } from './homeLogic';

const file = (n: number): Entry => ({ name: `f${n}`, path: `/f${n}`, isDir: false, size: 1 }) as Entry;
const files = (n: number) => Array.from({ length: n }, (_, i) => file(i));

describe('recentRows', () => {
  it('shows one row of the newest files while collapsed', () => {
    const rows = recentRows(files(10), false);
    expect(rows).toHaveLength(1);
    expect(rows[0].map((e) => e?.name)).toEqual(['f0', 'f1', 'f2']);
  });
  it('lays the expanded list out in rows of equal width, so tiles never get squeezed into one row', () => {
    const rows = recentRows(files(8), true);
    expect(rows).toHaveLength(3);
    expect(rows.every((r) => r.length === RECENT_SHOWN)).toBe(true);
    expect(rows[2].map((e) => e?.name ?? null)).toEqual(['f6', 'f7', null]);
  });
  it('keeps every entry once and in order', () => {
    const flat = recentRows(files(30), true).flat().filter((e): e is Entry => e !== null);
    expect(flat.map((e) => e.path)).toEqual(files(30).map((e) => e.path));
  });
  it('pads a short collapsed list and returns no rows for an empty one', () => {
    expect(recentRows(files(2), false)[0].map((e) => e?.name ?? null)).toEqual(['f0', 'f1', null]);
    expect(recentRows([], true)).toEqual([]);
  });
});
