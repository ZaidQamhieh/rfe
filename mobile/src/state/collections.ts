import { create } from 'zustand';

import {
  SavedSearches,
  addPin,
  bookmarksList,
  favoritesList,
  pinsList,
  recentSearchesList,
  recordSearch,
  removeBookmark,
  removePin,
  toggleFavorite,
  upsertBookmark,
  type Bookmark,
  type Favorite,
  type Pin,
  type SavedSearch,
} from '../core/storage/collections';
import { keyValue, listingCache } from '../services';

const favs = favoritesList(keyValue);
const marks = bookmarksList(keyValue);
const pinsStore = pinsList(keyValue);
const recents = recentSearchesList(keyValue);
const saved = new SavedSearches(keyValue);

type S = {
  loaded: boolean;
  favorites: Favorite[];
  bookmarks: Bookmark[];
  pins: Pin[];
  recentSearches: string[];
  savedSearches: SavedSearch[];
  load(): Promise<void>;
  toggleFavorite(f: Favorite): Promise<void>;
  removeFavorite(hostId: string, path: string): Promise<void>;
  addBookmark(b: Bookmark): Promise<void>;
  removeBookmark(hostId: string, path: string): Promise<void>;
  pin(hostId: string, path: string): Promise<void>;
  unpin(hostId: string, path: string): Promise<void>;
  recordSearch(q: string): Promise<void>;
  removeRecentSearch(q: string): Promise<void>;
  clearRecentSearches(): Promise<void>;
  addSavedSearch(s: SavedSearch): Promise<void>;
  removeSavedSearch(name: string): Promise<void>;
};

// Every mutation reads the current list and writes the next one, so two overlapping calls would each start from the
// same list and the later write would drop the earlier change (audit F-67). Run them one after another.
let tail: Promise<unknown> = Promise.resolve();
function queued<T>(job: () => Promise<T>): Promise<T> {
  const run = tail.then(job, job);
  tail = run.catch(() => undefined);
  return run;
}

/** Favorites, bookmarks, offline pins and searches: persisted first, then mirrored in memory. Keys/encodings match the Flutter app. */
export const useCollections = create<S>((set, get) => ({
  loaded: false,
  favorites: [],
  bookmarks: [],
  pins: [],
  recentSearches: [],
  savedSearches: [],
  async load() {
    const [favorites, bookmarks, pins, recentSearches, savedSearches] = await Promise.all([favs.load(), marks.load(), pinsStore.load(), recents.load(), saved.load()]);
    set({ favorites, bookmarks, pins, recentSearches, savedSearches, loaded: true });
  },
  toggleFavorite(f) {
    return queued(async () => {
      const next = toggleFavorite(get().favorites, f);
      await favs.save(next);
      set({ favorites: next });
    });
  },
  removeFavorite(hostId, path) {
    return queued(async () => {
      const next = get().favorites.filter((f) => !(f.hostId === hostId && f.path === path));
      await favs.save(next);
      set({ favorites: next });
    });
  },
  addBookmark(b) {
    return queued(async () => {
      const next = upsertBookmark(get().bookmarks, b);
      await marks.save(next);
      set({ bookmarks: next });
    });
  },
  removeBookmark(hostId, path) {
    return queued(async () => {
      const next = removeBookmark(get().bookmarks, hostId, path);
      await marks.save(next);
      set({ bookmarks: next });
    });
  },
  pin(hostId, path) {
    return queued(async () => {
      const next = addPin(get().pins, { hostId, remotePath: path });
      await pinsStore.save(next);
      set({ pins: next });
      listingCache.syncPinned(hostId, next.filter((p) => p.hostId === hostId).map((p) => p.remotePath));
    });
  },
  unpin(hostId, path) {
    return queued(async () => {
      const next = removePin(get().pins, hostId, path);
      await pinsStore.save(next);
      set({ pins: next });
      listingCache.syncPinned(hostId, next.filter((p) => p.hostId === hostId).map((p) => p.remotePath));
    });
  },
  recordSearch(q) {
    return queued(async () => {
      const next = recordSearch(get().recentSearches, q);
      await recents.save(next);
      set({ recentSearches: next });
    });
  },
  removeRecentSearch(q) {
    return queued(async () => {
      const next = get().recentSearches.filter((s) => s !== q);
      await recents.save(next);
      set({ recentSearches: next });
    });
  },
  clearRecentSearches() {
    return queued(async () => {
      await recents.save([]);
      set({ recentSearches: [] });
    });
  },
  addSavedSearch(s) {
    return queued(async () => {
      const next = [s, ...get().savedSearches];
      await saved.save(next);
      set({ savedSearches: next });
    });
  },
  removeSavedSearch(name) {
    return queued(async () => {
      const next = get().savedSearches.filter((s) => s.name !== name);
      await saved.save(next);
      set({ savedSearches: next });
    });
  },
}));

export const isPinned = (pins: Pin[], hostId: string, path: string) => pins.some((p) => p.hostId === hostId && p.remotePath === path);
