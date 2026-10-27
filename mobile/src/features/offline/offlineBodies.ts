import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { parentDirOf } from '../explorer/paths';

/** Largest single file kept offline. */
export const MAX_OFFLINE_FILE_BYTES = 50 * 1024 * 1024;
/** Budget for every host's offline bodies together. */
export const MAX_OFFLINE_CACHE_BYTES = 500 * 1024 * 1024;

/** The parts of the native vault and app state the offline logic needs (faked in tests). */
export type OfflineDeps = {
  isPinnedFolder: (hostId: string, folderPath: string) => boolean;
  vault: {
    has(hostId: string, path: string): Promise<boolean>;
    put(hostId: string, path: string, srcPath: string): Promise<void>;
    restore(hostId: string, path: string, destPath: string): Promise<boolean>;
    totalBytes(): Promise<number>;
  };
};

/** Whether a file is eligible for the offline vault: in a pinned folder and small enough. */
export const wantsOfflineCopy = (d: Pick<OfflineDeps, 'isPinnedFolder'>, host: Host, e: Pick<Entry, 'path' | 'isDir' | 'size'>) =>
  !e.isDir && (e.size ?? 0) <= MAX_OFFLINE_FILE_BYTES && d.isPinnedFolder(host.id, parentDirOf(e.path));

/**
 * Stores [localPath] (a file just fetched from the host) in the vault when its folder is pinned. `replace` is
 * for a fresh network fetch, which supersedes any older body; without it an existing copy is left alone. Best
 * effort and silent, like the network path it rides on.
 */
export async function keepOfflineCopy(d: OfflineDeps, host: Host, e: Pick<Entry, 'path' | 'isDir' | 'size'>, localPath: string, replace: boolean): Promise<boolean> {
  try {
    if (!wantsOfflineCopy(d, host, e)) return false;
    const had = await d.vault.has(host.id, e.path);
    if (!replace && had) return false;
    // A replaced body's old copy is already in the total, so only a new entry adds its size to the budget.
    if ((await d.vault.totalBytes()) + (had ? 0 : (e.size ?? 0)) > MAX_OFFLINE_CACHE_BYTES) return false;
    await d.vault.put(host.id, e.path, localPath);
    return true;
  } catch {
    return false;
  }
}

/** Puts the offline body at [destPath] if there is one. An entry that fails authentication is deleted natively and counts as absent. */
export async function restoreOfflineCopy(d: OfflineDeps, host: Host, e: Pick<Entry, 'path'>, destPath: string): Promise<boolean> {
  try {
    return await d.vault.restore(host.id, e.path, destPath);
  } catch {
    return false;
  }
}

/**
 * Downloads the files of a folder that was just pinned so they open offline. Skips folders, files over the
 * per-file cap, ones already stored and any that would push the cache past its budget. `fetchFile` must leave
 * the body in the vault (the preview fetch does when the folder is pinned). Returns how many were fetched.
 */
export async function precachePinnedFolder(d: OfflineDeps, host: Host, entries: Entry[], fetchFile: (host: Host, e: Entry) => Promise<unknown>): Promise<number> {
  let used: number;
  try {
    used = await d.vault.totalBytes();
  } catch {
    return 0; // the store cannot be measured, so nothing is added to it (audit F-114)
  }
  let fetched = 0;
  for (const e of entries) {
    if (e.isDir || (e.size ?? 0) > MAX_OFFLINE_FILE_BYTES) continue;
    try {
      if (await d.vault.has(host.id, e.path)) continue;
    } catch {
      continue; // one failing lookup must not abort the rest of the folder (audit F-114)
    }
    // A file that would overshoot the budget is skipped; a smaller one later in the folder may still fit.
    if (used + (e.size ?? 0) > MAX_OFFLINE_CACHE_BYTES) continue;
    try {
      await fetchFile(host, e);
      used += e.size ?? 0;
      fetched++;
    } catch {
      // Best effort: the host may be slow or the file unreadable.
    }
  }
  return fetched;
}
