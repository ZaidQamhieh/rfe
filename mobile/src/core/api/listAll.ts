import type { Entry, Listing } from './models';

/** Every entry of a folder: pages through `nextCursor` until the host reports no more. Errors propagate to the caller. */
export async function listAllEntries(fetchPage: (cursor: string | undefined) => Promise<Listing>): Promise<Entry[]> {
  const out: Entry[] = [];
  let cursor: string | undefined;
  do {
    const page = await fetchPage(cursor);
    out.push(...page.entries);
    cursor = page.nextCursor;
  } while (cursor);
  return out;
}
