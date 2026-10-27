import type { Entry } from '../../core/api/models';
import { isSafeName, needsSync } from './syncLogic';

/** One local file as the runner needs it. */
export type LocalInfo = { size: number; modified: number | null };

/** Where the mirror lives. Implemented over the folder picker's tree URI, or a plain path. */
export interface LocalFolder {
  list(): Promise<Map<string, LocalInfo>>;
  /** Replaces or creates [name] with the finished download at [stagedPath]. */
  put(name: string, stagedPath: string): Promise<void>;
}

export interface SyncDeps {
  /** Every page of the remote folder's listing. */
  listRemote(path: string): Promise<Entry[]>;
  /** Downloads [entry] to [destPath] (a staging file), rejecting on any failure. */
  fetch(entry: Entry, destPath: string): Promise<void>;
  /** A fresh staging path for [name]; removed by [discard] once the file is in place or has failed. */
  staging(name: string): string;
  discard(path: string): void;
  local: LocalFolder;
}

export type SyncProgress = { current: number; total: number; name: string };

/** [firstError] is the reason the first failed file gave, for the message shown to the person. */
export type SyncResult = { downloaded: number; failed: string[]; skipped: number; firstError?: string };

/**
 * One pass over a rule: lists the remote folder, then downloads the files that are missing, resized or changed.
 * Each file is downloaded to a staging file first and moved in only when complete, so an interrupted transfer never
 * leaves a half file that looks synced. A file that fails is reported and the pass carries on with the rest.
 * Subfolders are not mirrored (as in the Flutter app).
 */
export async function runSync(remotePath: string, deps: SyncDeps, onProgress?: (p: SyncProgress) => void, cancelled: () => boolean = () => false): Promise<SyncResult> {
  const files = (await deps.listRemote(remotePath)).filter((e) => !e.isDir);
  const local = await deps.local.list();
  const result: SyncResult = { downloaded: 0, failed: [], skipped: 0 };
  for (let i = 0; i < files.length; i++) {
    if (cancelled()) break;
    const entry = files[i];
    onProgress?.({ current: i + 1, total: files.length, name: entry.name });
    if (!isSafeName(entry.name)) {
      result.skipped++;
      continue;
    }
    if (!needsSync(entry, local.get(entry.name))) continue;
    const staged = deps.staging(entry.name);
    try {
      await deps.fetch(entry, staged);
      await deps.local.put(entry.name, staged);
      result.downloaded++;
    } catch (e) {
      result.failed.push(entry.name);
      result.firstError ??= e instanceof Error ? e.message : String(e);
    } finally {
      deps.discard(staged);
    }
  }
  return result;
}
