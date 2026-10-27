import type { Entry } from '../../core/api/models';

import { defaultSort, type EntryDensity, type SortField, type SortOrder } from '../../core/models/sort';

// The sort model lives in core so persisted settings can name it without depending on a feature.
export { defaultSort, type EntryDensity, type SortField, type SortOrder };

const chunks = (s: string) => s.toLowerCase().match(/\d+|\D+/g) ?? [];

/** Case-insensitive name order where digit runs compare by value ("file2" before "file10"); no Intl dependency. */
export function naturalCompare(a: string, b: string): number {
  const x = chunks(a);
  const y = chunks(b);
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    const p = x[i];
    const q = y[i];
    if (p === q) continue;
    if (/^\d/.test(p) && /^\d/.test(q)) {
      const m = p.replace(/^0+(?=\d)/, '');
      const n = q.replace(/^0+(?=\d)/, '');
      if (m.length !== n.length) return m.length < n.length ? -1 : 1;
      if (m !== n) return m < n ? -1 : 1;
      continue;
    }
    return p < q ? -1 : 1;
  }
  return x.length === y.length ? 0 : x.length < y.length ? -1 : 1;
}

const ms = (s?: string) => (s ? Date.parse(s) || 0 : 0);

/** Directories first, then files; each group sorted by [sort]. Stable, and independent of which other entries exist. */
export function sortEntries(entries: readonly Entry[], sort: SortOrder): Entry[] {
  const cmp = (a: Entry, b: Entry): number => {
    let r: number;
    switch (sort.field) {
      case 'name':
        r = naturalCompare(a.name, b.name);
        break;
      case 'size':
        r = (a.size ?? 0) - (b.size ?? 0);
        break;
      case 'date':
        r = ms(a.modified) - ms(b.modified);
        break;
      case 'type': {
        const x = a.mimeType ?? '';
        const y = b.mimeType ?? '';
        r = x < y ? -1 : x > y ? 1 : 0;
        break;
      }
    }
    return sort.ascending ? r : -r;
  };
  const dirs = entries.filter((e) => e.isDir).sort(cmp);
  const files = entries.filter((e) => !e.isDir).sort(cmp);
  return [...dirs, ...files];
}
