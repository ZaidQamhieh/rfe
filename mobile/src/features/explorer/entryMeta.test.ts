import type { Entry } from '../../core/api/models';
import { entryMeta, entrySize } from './entryMeta';

const dir = (o: Partial<Entry> = {}): Entry => ({ name: 'd', path: '/d', isDir: true, isSymlink: false, ...o });
const file = (o: Partial<Entry> = {}): Entry => ({ name: 'f', path: '/f', isDir: false, isSymlink: false, ...o });

/** A fixed "now" keeps the day labels independent of the clock. */
const now = new Date(2026, 10, 11, 17, 24);

describe('entryMeta for folders', () => {
  it('shows the item count only when the listing returned one', () => {
    expect(entryMeta(dir(), now)).toBe('');
    expect(entryMeta(dir({ childCount: 0 }), now)).toBe('0 items');
    expect(entryMeta(dir({ childCount: 1 }), now)).toBe('1 item');
    expect(entryMeta(dir({ childCount: 14 }), now)).toBe('14 items');
    expect(entryMeta(dir({ childCount: 1000 }), now)).toBe('1000+ items');
  });
});

describe('entryMeta for files', () => {
  it('shows Today and Yesterday with the time, and the date for older files', () => {
    expect(entryMeta(file({ modified: new Date(2026, 10, 11, 9, 14).toISOString() }), now)).toBe('Today 09:14');
    expect(entryMeta(file({ modified: new Date(2026, 10, 10, 22, 51).toISOString() }), now)).toBe('Yesterday 22:51');
    const older = new Date(2026, 9, 28, 8, 5);
    expect(entryMeta(file({ modified: older.toISOString() }), now)).toBe(older.toLocaleDateString([], { day: 'numeric', month: 'short' }));
  });

  it('is empty when the listing has no modified time', () => {
    expect(entryMeta(file(), now)).toBe('');
  });
});

describe('entrySize', () => {
  it('gives a file its size and a folder none', () => {
    expect(entrySize(file({ size: 1536 }))).toBe('1.5 KB');
    expect(entrySize(file({ size: undefined }))).toBe('');
    expect(entrySize(dir({ size: 1536 }))).toBe('');
  });
});
