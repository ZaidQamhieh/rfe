import type { Entry } from '../../core/api/models';

export type PinSyncDeps = {
  /** Every page of one folder's listing; rejects when the host cannot be reached. */
  listAll: (path: string) => Promise<Entry[]>;
  /** Keeps the fresh listing so the folder opens instantly and stays browsable offline. */
  storeListing: (path: string, entries: Entry[]) => Promise<void>;
  /** Fetches the files that are not yet stored offline; resolves with how many were fetched. */
  precache: (entries: Entry[]) => Promise<number>;
};

export type PinSyncResult = { folders: number; failed: number; filesFetched: number };

/**
 * Brings a host's pinned folders up to date: re-lists each one and downloads files that are new since the pin. A folder
 * that cannot be listed (deleted, offline) is skipped and counted, never fatal. Bodies already stored are left alone;
 * they refresh the next time the file is opened online.
 */
export async function refreshPinnedFolders(paths: readonly string[], deps: PinSyncDeps): Promise<PinSyncResult> {
  const result: PinSyncResult = { folders: 0, failed: 0, filesFetched: 0 };
  for (const path of paths) {
    try {
      const entries = await deps.listAll(path);
      await deps.storeListing(path, entries);
      result.filesFetched += await deps.precache(entries);
      result.folders++;
    } catch {
      result.failed++;
    }
  }
  return result;
}

const doneThisSession = new Set<string>();

/** Whether pin sync still has to run for [hostId] in this app session (it runs once per host per launch). */
export const pinSyncPending = (hostId: string): boolean => !doneThisSession.has(hostId);

/** Marks a host synced for this session; call it only after at least one folder was refreshed, so an offline start retries. */
export const markPinSynced = (hostId: string): void => void doneThisSession.add(hostId);

/** For tests. */
export const resetPinSyncSession = () => doneThisSession.clear();
