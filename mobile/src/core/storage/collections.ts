import type { KeyValueStore } from './hostStore';

/**
 * A persisted list of records under one key, compatible with the Flutter StringList encoding
 * (a JSON array whose items are JSON strings) and with plain object arrays. Corrupt items are
 * skipped so one bad record never hides the rest (PR-54).
 */
export class RecordList<T> {
  constructor(
    private readonly kv: KeyValueStore,
    private readonly key: string,
    private readonly parse: (raw: unknown) => T | null,
    private readonly serialize: (item: T) => unknown = (x) => x,
  ) {}

  async load(): Promise<T[]> {
    const raw = await this.kv.get(this.key);
    if (raw === null) return [];
    let arr: unknown;
    try {
      arr = JSON.parse(raw);
    } catch {
      return [];
    }
    if (!Array.isArray(arr)) return [];
    const out: T[] = [];
    for (const item of arr) {
      let v: unknown = item;
      if (typeof item === 'string') {
        try {
          v = JSON.parse(item);
        } catch {
          v = item; // plain strings (recent searches)
        }
      }
      const parsed = this.parse(v);
      if (parsed !== null) out.push(parsed);
    }
    return out;
  }

  /** Writes in the Flutter StringList shape: each object item JSON-encoded as a string. */
  async save(items: T[]): Promise<void> {
    await this.kv.set(this.key, JSON.stringify(items.map((i) => (typeof this.serialize(i) === 'string' ? this.serialize(i) : JSON.stringify(this.serialize(i))))));
  }
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown): string | undefined => (typeof v === 'string' ? v : undefined);

export type Favorite = { hostId: string; path: string; label: string };
export type Bookmark = { hostId: string; remotePath: string; tag?: string; color?: number };
export type Pin = { hostId: string; remotePath: string };
export type SavedSearch = { name: string; query: string };

export const FAVORITES_KEY = 'rfe_favorites_v1';
export const BOOKMARKS_KEY = 'bookmarks_v1';
export const PINS_KEY = 'offline_pins_v1';
export const RECENT_SEARCHES_KEY = 'rfe_recent_searches_v1';
export const SAVED_SEARCHES_KEY = 'rfe_saved_searches_v1';
export const MAX_RECENT_SEARCHES = 10;

export const parseFavorite = (v: unknown): Favorite | null => (isObj(v) && str(v.hostId) && str(v.path) && str(v.label) !== undefined ? { hostId: v.hostId as string, path: v.path as string, label: v.label as string } : null);
export const parseBookmark = (v: unknown): Bookmark | null =>
  isObj(v) && str(v.hostId) && str(v.remotePath)
    ? { hostId: v.hostId as string, remotePath: v.remotePath as string, ...(str(v.tag) ? { tag: v.tag as string } : {}), ...(typeof v.color === 'number' ? { color: v.color } : {}) }
    : null;
export const parsePin = (v: unknown): Pin | null => (isObj(v) && str(v.hostId) && str(v.remotePath) ? { hostId: v.hostId as string, remotePath: v.remotePath as string } : null);
export const parseSavedSearch = (v: unknown): SavedSearch | null => (isObj(v) ? { name: str(v.name) ?? '', query: str(v.query) ?? '' } : null);

export const favoritesList = (kv: KeyValueStore) => new RecordList<Favorite>(kv, FAVORITES_KEY, parseFavorite);
export const bookmarksList = (kv: KeyValueStore) => new RecordList<Bookmark>(kv, BOOKMARKS_KEY, parseBookmark);
export const pinsList = (kv: KeyValueStore) => new RecordList<Pin>(kv, PINS_KEY, parsePin);
export const recentSearchesList = (kv: KeyValueStore) => new RecordList<string>(kv, RECENT_SEARCHES_KEY, (v) => (typeof v === 'string' ? v : null));

/** Saved searches are stored as ONE JSON list string (not a StringList). */
export class SavedSearches {
  constructor(private readonly kv: KeyValueStore) {}
  async load(): Promise<SavedSearch[]> {
    const raw = await this.kv.get(SAVED_SEARCHES_KEY);
    if (raw === null) return [];
    try {
      let v: unknown = JSON.parse(raw);
      if (typeof v === 'string') v = JSON.parse(v);
      return Array.isArray(v) ? v.map(parseSavedSearch).filter((s): s is SavedSearch => s !== null) : [];
    } catch {
      return [];
    }
  }
  async save(items: SavedSearch[]) {
    await this.kv.set(SAVED_SEARCHES_KEY, JSON.stringify(JSON.stringify(items)));
  }
}

// Pure list operations shared by the stores.
export const toggleFavorite = (favs: Favorite[], fav: Favorite): Favorite[] =>
  favs.some((f) => f.hostId === fav.hostId && f.path === fav.path) ? favs.filter((f) => !(f.hostId === fav.hostId && f.path === fav.path)) : [...favs, fav];

export const upsertBookmark = (items: Bookmark[], b: Bookmark): Bookmark[] => [...items.filter((x) => !(x.hostId === b.hostId && x.remotePath === b.remotePath)), b];
export const removeBookmark = (items: Bookmark[], hostId: string, remotePath: string) => items.filter((x) => !(x.hostId === hostId && x.remotePath === remotePath));
export const addPin = (pins: Pin[], p: Pin): Pin[] => (pins.some((x) => x.hostId === p.hostId && x.remotePath === p.remotePath) ? pins : [...pins, p]);
export const removePin = (pins: Pin[], hostId: string, remotePath: string) => pins.filter((x) => !(x.hostId === hostId && x.remotePath === remotePath));
export const recordSearch = (recent: string[], query: string): string[] => {
  const q = query.trim();
  if (!q) return recent;
  return [q, ...recent.filter((s) => s !== q)].slice(0, MAX_RECENT_SEARCHES);
};
