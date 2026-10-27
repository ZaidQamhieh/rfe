import type { Entry } from '../../core/api/models';
import { entryMeta } from './entryMeta';

const dir = (o: Partial<Entry> = {}): Entry => ({ name: 'd', path: '/d', isDir: true, isSymlink: false, ...o });

describe('entryMeta for folders', () => {
  it('shows the item count only when the listing returned one', () => {
    expect(entryMeta(dir())).toBe('Folder');
    expect(entryMeta(dir({ childCount: 0 }))).toBe('Folder  ·  0 items');
    expect(entryMeta(dir({ childCount: 1 }))).toBe('Folder  ·  1 item');
    expect(entryMeta(dir({ childCount: 12 }))).toBe('Folder  ·  12 items');
    expect(entryMeta(dir({ childCount: 1000 }))).toBe('Folder  ·  1000+ items');
  });
});
