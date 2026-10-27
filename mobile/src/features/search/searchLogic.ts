// Pure search helpers ported from search_logic.dart and search_types.dart.
import type { Entry } from '../../core/api/models';
import { isEntryHidden, type VisibilityPrefs } from '../../core/visibility';
import type { StringKey } from '../../i18n';

export type SearchMode = 'substring' | 'glob' | 'regex';

/** A query with `*` or `?` is matched as a glob by the agent. */
export const isGlobQuery = (q: string) => q.includes('*') || q.includes('?');

/** The agent speaks substring and glob only; a regex is approximated by wrapping it in wildcards. */
export function queryForMode(q: string, mode: SearchMode): string {
  return mode === 'regex' ? `*${q}*` : q;
}

const byName = (a: Entry, b: Entry) => {
  const x = a.name.toLowerCase();
  const y = b.name.toLowerCase();
  return x < y ? -1 : x > y ? 1 : 0;
};

/** Names starting with the query first, then alphabetical; glob or empty queries are purely alphabetical. Returns a new list. */
export function sortByRelevance(entries: Entry[], query: string): Entry[] {
  const q = query.trim().toLowerCase();
  const sorted = [...entries];
  if (q === '' || isGlobQuery(query)) return sorted.sort(byName);
  const rank = (e: Entry) => (e.name.toLowerCase().startsWith(q) ? 0 : 1);
  return sorted.sort((a, b) => rank(a) - rank(b) || byName(a, b));
}

/** Results filtered like the explorer listing (dotfiles, hidden extensions and names) unless hidden items are included. */
export function filterSearchResults(results: Entry[], prefs: VisibilityPrefs, includeHidden: boolean): Entry[] {
  return includeHidden ? results : results.filter((e) => !isEntryHidden(e, prefs));
}

/** First case-insensitive match of [query] in [name] as `[start, end)`, or null for empty, glob or absent matches. */
export function highlightRange(name: string, query: string): { start: number; end: number } | null {
  const q = query.trim();
  if (q === '' || isGlobQuery(q)) return null;
  const idx = name.toLowerCase().indexOf(q.toLowerCase());
  return idx < 0 ? null : { start: idx, end: idx + q.length };
}

export const SEARCH_CATEGORIES = ['folder', 'image', 'video', 'audio', 'document', 'archive', 'other'] as const;
export type SearchCategory = (typeof SEARCH_CATEGORIES)[number];

export const CATEGORY_LABEL: Record<SearchCategory, StringKey> = {
  folder: 'searchCategoryFolders',
  image: 'searchCategoryImages',
  video: 'searchCategoryVideos',
  audio: 'searchCategoryAudio',
  document: 'searchCategoryDocs',
  archive: 'searchCategoryArchives',
  other: 'searchCategoryOther',
};

const MB = 1024 * 1024;
export const SIZE_PRESETS = [
  { key: 'any', minBytes: undefined, label: 'sizePresetAny' },
  { key: 'mb1', minBytes: MB, label: 'sizePresetMb1' },
  { key: 'mb10', minBytes: 10 * MB, label: 'sizePresetMb10' },
  { key: 'mb100', minBytes: 100 * MB, label: 'sizePresetMb100' },
  { key: 'gb1', minBytes: 1024 * MB, label: 'sizePresetGb1' },
] as const satisfies readonly { key: string; minBytes: number | undefined; label: StringKey }[];
export type SizePresetKey = (typeof SIZE_PRESETS)[number]['key'];

const HOUR = 3600_000;
export const DATE_PRESETS = [
  { key: 'any', label: 'datePresetAny' },
  { key: 'last24h', label: 'datePresetLast24h' },
  { key: 'last7d', label: 'datePresetLast7d' },
  { key: 'last30d', label: 'datePresetLast30d' },
  { key: 'thisYear', label: 'datePresetThisYear' },
] as const satisfies readonly { key: string; label: StringKey }[];
export type DatePresetKey = (typeof DATE_PRESETS)[number]['key'];

/** `modifiedAfter` bound for a date preset relative to [now], or undefined for "any time". */
export function resolveDatePreset(key: DatePresetKey, now: Date): Date | undefined {
  switch (key) {
    case 'any':
      return undefined;
    case 'last24h':
      return new Date(now.getTime() - 24 * HOUR);
    case 'last7d':
      return new Date(now.getTime() - 7 * 24 * HOUR);
    case 'last30d':
      return new Date(now.getTime() - 30 * 24 * HOUR);
    case 'thisYear':
      return new Date(now.getFullYear(), 0, 1);
  }
}

export const minBytesFor = (key: SizePresetKey) => SIZE_PRESETS.find((p) => p.key === key)?.minBytes;

export type SearchFilters = { categories: SearchCategory[]; size: SizePresetKey; date: DatePresetKey; fromHere: boolean; includeHidden: boolean; mode: SearchMode };
export const defaultFilters = (): SearchFilters => ({ categories: [], size: 'any', date: 'any', fromHere: true, includeHidden: false, mode: 'substring' });

/** Number of non-default filters, shown as the badge on the filter button. The scope toggle is always visible so it is not counted. */
export const activeFilterCount = (f: SearchFilters) => f.categories.length + (f.size !== 'any' ? 1 : 0) + (f.date !== 'any' ? 1 : 0) + (f.includeHidden ? 1 : 0);
