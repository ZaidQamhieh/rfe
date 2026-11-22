// Pure helpers for the Files page's folder listing. Its only import is a type, erased when Node runs it directly.
import type { Entry } from './api';

/**
 * The entries of every loaded page of a folder, in order. A folder can change between two page requests (or while all
 * pages are refetched), so an entry that shows up on a second page is dropped rather than listed twice.
 */
export function entriesOfPages(pages: { entries: Entry[] }[] | undefined): Entry[] {
  const seen = new Set<string>();
  const out: Entry[] = [];
  for (const page of pages ?? []) {
    for (const entry of page.entries) {
      if (seen.has(entry.path)) continue;
      seen.add(entry.path);
      out.push(entry);
    }
  }
  return out;
}
