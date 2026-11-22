import type { Entry } from '../../core/api/models';
import type { Pin } from '../../core/storage/collections';

/** How many kept files the asleep / lost panel lists (the rest are one tap away in This phone). */
const KEPT_FILES_SHOWN = 6;

/** What `collectKeptFiles` needs: the cached listing of a folder, and whether a file's body is stored on the phone. */
export type KeptFilesDeps = {
  listing: (hostId: string, folderPath: string) => Promise<{ entries: Entry[] } | null>;
  has: (hostId: string, path: string) => Promise<boolean>;
};

/**
 * The files of [hostId] that open without the computer: files in its kept (pinned) folders whose body is stored on the
 * phone, in folder order, at most [limit]. A folder whose listing was never cached, or a body check that fails, is
 * skipped rather than failing the whole list.
 */
export async function collectKeptFiles(hostId: string, pins: readonly Pin[], deps: KeptFilesDeps, limit: number = KEPT_FILES_SHOWN): Promise<Entry[]> {
  const out: Entry[] = [];
  for (const pin of pins) {
    if (pin.hostId !== hostId) continue;
    let listing: { entries: Entry[] } | null = null;
    try {
      listing = await deps.listing(hostId, pin.remotePath);
    } catch {
      continue; // an unreadable cache for one folder only hides that folder
    }
    for (const e of listing?.entries ?? []) {
      if (out.length >= limit) return out;
      if (e.isDir) continue;
      try {
        if (await deps.has(hostId, e.path)) out.push(e);
      } catch {
        // A failed lookup means "not known to be kept": leave the file out.
      }
    }
  }
  return out;
}
