import { MemoryKeyValueStore } from './hostStore';
import { FAVORITES_KEY, RECENT_SEARCHES_KEY, SavedSearches, SAVED_SEARCHES_KEY, addPin, bookmarksList, favoritesList, recentSearchesList, recordSearch, toggleFavorite, upsertBookmark } from './collections';

describe('Flutter-compatible record lists', () => {
  it('reads StringList-encoded favorites, skips corrupt items, and writes the same shape back', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(FAVORITES_KEY, JSON.stringify([JSON.stringify({ hostId: 'h', path: '/a', label: 'A' }), 'garbage', JSON.stringify({ hostId: 'h' }), { hostId: 'h', path: '/b', label: 'B' }]));
    const list = favoritesList(kv);
    const favs = await list.load();
    expect(favs.map((f) => f.path)).toEqual(['/a', '/b']);
    await list.save(toggleFavorite(favs, { hostId: 'h', path: '/c', label: 'C' }));
    const raw = JSON.parse((await kv.get(FAVORITES_KEY))!) as string[];
    expect(raw.every((s) => typeof s === 'string')).toBe(true);
    expect(JSON.parse(raw[2])).toEqual({ hostId: 'h', path: '/c', label: 'C' });
    expect(toggleFavorite(favs, favs[0])).toHaveLength(1);
  });

  it('bookmarks upsert replaces by host+path; pins are unique', async () => {
    const kv = new MemoryKeyValueStore();
    const l = bookmarksList(kv);
    let items = upsertBookmark([], { hostId: 'h', remotePath: '/a', tag: 'x' });
    items = upsertBookmark(items, { hostId: 'h', remotePath: '/a', tag: 'y', color: 5 });
    expect(items).toEqual([{ hostId: 'h', remotePath: '/a', tag: 'y', color: 5 }]);
    await l.save(items);
    expect(await l.load()).toEqual(items);
    const p = addPin(addPin([], { hostId: 'h', remotePath: '/a' }), { hostId: 'h', remotePath: '/a' });
    expect(p).toHaveLength(1);
  });

  it('recent searches are plain strings, deduped, newest first, capped at 10', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(RECENT_SEARCHES_KEY, JSON.stringify(['b', 'a']));
    const l = recentSearchesList(kv);
    expect(await l.load()).toEqual(['b', 'a']);
    let r = recordSearch(['b', 'a'], ' a ');
    expect(r).toEqual(['a', 'b']);
    for (let i = 0; i < 15; i++) r = recordSearch(r, `q${i}`);
    expect(r).toHaveLength(10);
    expect(recordSearch(r, '   ')).toBe(r);
    await l.save(r);
    expect(JSON.parse((await kv.get(RECENT_SEARCHES_KEY))!)[0]).toBe('q14');
  });

  it('saved searches use one JSON list string', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(SAVED_SEARCHES_KEY, JSON.stringify(JSON.stringify([{ name: 'n', query: 'q' }, 5])));
    const s = new SavedSearches(kv);
    expect(await s.load()).toEqual([{ name: 'n', query: 'q' }]);
    await s.save([{ name: 'a', query: 'b' }]);
    expect(await s.load()).toEqual([{ name: 'a', query: 'b' }]);
  });
});
