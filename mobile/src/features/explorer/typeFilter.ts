import type { Entry } from '../../core/api/models';
import { categoryOf, type EntryCategory } from '../../core/entryCategory';

const ORDER: EntryCategory[] = ['folder', 'image', 'video', 'audio', 'document', 'archive', 'other'];

/** Categories worth a chip for [entries]: those present, only when there is more than one (a lone chip filters nothing). */
export function typeChips(entries: readonly Entry[]): EntryCategory[] {
  const present = new Set(entries.map(categoryOf));
  return present.size > 1 ? ORDER.filter((c) => present.has(c)) : [];
}

/** The entries of one category, or [entries] untouched when no type is active. */
export function filterByType(entries: Entry[], category: EntryCategory | null): Entry[] {
  return category ? entries.filter((e) => categoryOf(e) === category) : entries;
}
