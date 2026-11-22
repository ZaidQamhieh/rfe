import { useEffect, useState } from 'react';

import { isAbortError } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import { extensionOf } from '../../core/visibility';
import { videoExtensions } from '../../core/entryCategory';
import { ignoreError } from '../../core/log';
import { clientForHost, hostStore } from '../../services';
import { FetchQueue, MAX_CONCURRENT, type FetchSignal, type Request } from './fetchQueue';

/** At most this many length lookups are in flight at once, however many video cells are on screen. */
/** Remembered answers; the oldest are dropped past this so a long session does not grow without bound. */
const CACHE_LIMIT = 2000;

/** True for a file that is a video, by its MIME type first and its extension when the type is generic. */
export function isVideoEntry(e: Pick<Entry, 'isDir' | 'mimeType' | 'name'>): boolean {
  if (e.isDir) return false;
  if ((e.mimeType ?? '').startsWith('video/')) return true;
  return videoExtensions.has(extensionOf(e.name));
}

/**
 * Looks up video lengths lazily: at most [maxConcurrent] requests at once, one request shared by everyone asking for the
 * same file version, a cancel that drops a queued request, and remembered answers (a failed lookup is not remembered, so
 * it can be tried again). [fetchDuration] resolves the length in milliseconds, or null when the file has none.
 */
export function createDurationSource(fetchDuration: (hostId: string, path: string, signal: FetchSignal) => Promise<number | null>, maxConcurrent = MAX_CONCURRENT, cacheLimit = CACHE_LIMIT) {
  const queue = new FetchQueue<number | null | undefined>(maxConcurrent, undefined);
  const known = new Map<string, number | null>();
  return {
    request(hostId: string, path: string, version: string | number): Request<number | null | undefined> {
      const key = `${hostId}@${path}@${version}`;
      if (known.has(key)) return { promise: Promise.resolve(known.get(key)), cancel: () => undefined };
      const req = queue.request(key, (signal) => fetchDuration(hostId, path, signal));
      return {
        cancel: req.cancel,
        promise: req.promise.then((ms) => {
          // `undefined` is the queue's "cancelled" value: nothing learned, nothing remembered.
          if (ms !== undefined) {
            known.set(key, ms);
            if (known.size > cacheLimit) known.delete(known.keys().next().value as string);
          }
          return ms;
        }),
      };
    },
  };
}

/**
 * The app's source: asks the computer for the file's media info and keeps only its length. When the request is
 * cancelled the call is aborted; the AbortError that follows (nobody is waiting for it) is not an error.
 */
export const videoDurations = createDurationSource(async (hostId, path, signal) => {
  const host = (await hostStore.listHosts()).find((h) => h.id === hostId);
  if (!host || signal.aborted) return null;
  const abort = new AbortController();
  signal.onAbort(() => abort.abort());
  try {
    const info = await (await clientForHost(host)).mediaInfo(path, abort.signal);
    return info.durationMs ?? null;
  } catch (e) {
    if (isAbortError(e)) return null;
    throw e;
  }
});

/** The length in milliseconds of a video [entry], looked up once the cell is on screen; undefined for anything else or until known. */
export function useVideoDuration(hostId: string, entry: Entry): number | undefined {
  const [ms, setMs] = useState<number | undefined>(undefined);
  const video = isVideoEntry(entry);
  useEffect(() => {
    if (!video) return;
    let live = true;
    const req = videoDurations.request(hostId, entry.path, entry.modified ?? '');
    req.promise.then((v) => live && setMs(v ?? undefined)).catch(ignoreError('video duration'));
    return () => {
      live = false;
      req.cancel();
    };
  }, [video, hostId, entry.path, entry.modified]);
  return video ? ms : undefined;
}
