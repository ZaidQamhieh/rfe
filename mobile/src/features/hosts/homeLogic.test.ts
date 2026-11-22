import type { Entry } from '../../core/api/models';
import { RECENT_SHOWN, recentWhen, visibleRecents, withoutHidden } from './homeLogic';

const now = new Date(2026, 5, 15, 9, 30);
const at = (d: number, h: number, m = 0) => new Date(2026, 5, d, h, m).toISOString();
const labels = { today: 'Today', yesterday: 'Yesterday' };
const e = (name: string): Entry => ({ name, path: `/${name}`, isDir: false, isSymlink: false });

describe('visibleRecents', () => {
  const all = ['a', 'b', 'c', 'd', 'e'].map(e);
  it('shows the newest few until expanded', () => {
    expect(visibleRecents(all, false).map((x) => x.name)).toEqual(['a', 'b', 'c']);
    expect(RECENT_SHOWN).toBe(3);
  });
  it('shows everything once expanded and keeps the order', () => {
    expect(visibleRecents(all, true).map((x) => x.name)).toEqual(['a', 'b', 'c', 'd', 'e']);
  });
  it('copes with fewer files than the limit', () => {
    expect(visibleRecents([e('a')], false)).toHaveLength(1);
    expect(visibleRecents([], true)).toEqual([]);
  });
});

describe('recentWhen', () => {
  it('says Today with the clock time, Yesterday alone, a short date earlier', () => {
    expect(recentWhen(at(15, 9, 14), now, labels)).toMatch(/^Today \d{1,2}[:.]14/);
    // The shot shows 24-hour time ("Today 23:05"), never an AM/PM clock.
    expect(recentWhen(at(15, 23, 5), now, labels)).toMatch(/^Today 23:05$/);
    expect(recentWhen(at(14, 23), now, labels)).toBe('Yesterday');
    const earlier = recentWhen(at(8, 12), now, labels);
    expect(earlier).toContain('8');
    expect(earlier).not.toMatch(/Today|Yesterday/);
  });
  it('is empty when the time is missing or invalid', () => {
    expect(recentWhen(undefined, now, labels)).toBe('');
    expect(recentWhen('not a date', now, labels)).toBe('');
  });
});

describe('withoutHidden', () => {
  const at2 = (p: string): Entry => ({ name: p.split('/').pop() ?? p, path: p, isDir: false, isSymlink: false });
  it('drops dotfiles and files inside dot-folders, on / and \\ paths', () => {
    const kept = withoutHidden(['/home/z/.claude.json', '/home/z/.cache/x/y.bin', 'C:\\Users\\z\\.config\\a.txt', '/home/z/notes.md', '/home/z/a.b/c.txt'].map(at2));
    expect(kept.map((x) => x.path)).toEqual(['/home/z/notes.md', '/home/z/a.b/c.txt']);
  });
});
