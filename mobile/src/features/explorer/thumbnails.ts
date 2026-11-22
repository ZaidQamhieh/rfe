import { Directory, File, Paths } from 'expo-file-system';

import { fetchCancelNative, fetchToFileNative } from '../../core/native';
import { sleep } from '../../core/sleep';
import { clientForHost, hostStore } from '../../services';
import { FetchQueue, MAX_CONCURRENT, type FetchSignal, type Request } from './fetchQueue';

const MAX_BUSY_RETRIES = 3;
const THUMB_CACHE_MAX_BYTES = 32 * 1024 * 1024;
/** Eviction stops once the cache is at this share of THUMB_CACHE_MAX_BYTES. */
const THUMB_CACHE_TARGET_RATIO = 0.8;
/** Busy (429) retry delay: BUSY_RETRY_STEP_MS per attempt when Retry-After is absent, else Retry-After clamped to the min..max seconds. */
const BUSY_RETRY_STEP_MS = 200;
const BUSY_RETRY_AFTER_MIN_S = 1;
const BUSY_RETRY_AFTER_MAX_S = 2;
/** The agent answers 403 on /v1/thumb when this device has no download grant: previews are off for that computer. */
const THUMB_DENIED_STATUS = 403;

/** A non-success answer from the thumbnail endpoint, carrying its HTTP status (no message parsing needed). */
class ThumbnailHttpError extends Error {
  constructor(readonly statusCode: number) {
    super(`thumbnail HTTP ${statusCode}`);
  }
}

/** Computers that refused previews for lack of a download grant. In memory only: cleared by an app restart or `forgetDenied`. */
const denied = new Set<string>();
const deniedListeners = new Set<() => void>();
function deny(hostId: string) {
  if (denied.has(hostId)) return;
  denied.add(hostId);
  deniedListeners.forEach((l) => l());
}

/** Cache key: host, path, size and a version (mtime, else size) so an edited image is never served stale. */
export const thumbnailKey = (hostId: string, path: string, size: number, version: string | number) => `${hostId}@${path}@${size}@${version}`;

/** FNV-1a over the key: short, stable, filesystem-safe file names. */
export function hashKey(key: string): string {
  let h1 = 0x811c9dc5;
  let h2 = 0x01000193;
  for (let i = 0; i < key.length; i++) {
    const c = key.charCodeAt(i);
    h1 = Math.imul(h1 ^ c, 0x01000193) >>> 0;
    h2 = Math.imul(h2 + c, 0x85ebca6b) >>> 0;
  }
  return h1.toString(16).padStart(8, '0') + h2.toString(16).padStart(8, '0');
}

/** Retry delay for a THUMB_BUSY (429) answer: Retry-After clamped to 1..2 s, else 200 ms * attempt. */
export function busyDelayMs(attempt: number, retryAfterSeconds: number | null): number {
  return retryAfterSeconds == null ? BUSY_RETRY_STEP_MS * attempt : Math.min(Math.max(retryAfterSeconds, BUSY_RETRY_AFTER_MIN_S), BUSY_RETRY_AFTER_MAX_S) * 1000;
}

let dir: Directory | null = null;
function cacheDir(): Directory {
  if (!dir) {
    dir = new Directory(Paths.cache, 'thumbs');
    dir.create({ idempotent: true, intermediates: true });
  }
  return dir;
}

const resolved = new Map<string, string>(); // key -> file uri (in-memory index)
const queue = new FetchQueue<string | null>(MAX_CONCURRENT, null);
let evicting = false;

async function evictIfNeeded() {
  if (evicting) return;
  evicting = true;
  try {
    const files = cacheDir().list().filter((f): f is File => f instanceof File);
    let total = files.reduce((n, f) => n + (f.size ?? 0), 0);
    if (total <= THUMB_CACHE_MAX_BYTES) return;
    // oldest first by modification time
    files.sort((a, b) => (a.modificationTime ?? 0) - (b.modificationTime ?? 0));
    for (const f of files) {
      if (total <= THUMB_CACHE_MAX_BYTES * THUMB_CACHE_TARGET_RATIO) break;
      total -= f.size ?? 0;
      resolved.forEach((uri, k) => uri === f.uri && resolved.delete(k));
      f.delete();
    }
  } catch {
    // best-effort housekeeping
  } finally {
    evicting = false;
  }
}

async function load(hostId: string, path: string, size: number, key: string, signal: FetchSignal): Promise<string | null> {
  const file = new File(cacheDir(), `${hashKey(key)}.jpg`);
  if (file.exists) return file.uri;
  if (denied.has(hostId)) return null; // no download grant on that computer: do not ask again
  const hosts = await hostStore.listHosts();
  const host = hosts.find((h) => h.id === hostId);
  if (!host) return null;
  const client = await clientForHost(host);
  const id = `${hashKey(key)}-${Date.now().toString(36)}`;
  signal.onAbort(() => void fetchCancelNative(id));
  for (let attempt = 1; ; attempt++) {
    if (signal.aborted) return null;
    const spec = client.thumbnailSpec(path, size);
    const r = await fetchToFileNative(id, spec.url, spec.headers, spec.pin, file.uri.replace('file://', ''));
    if (r.status >= 200 && r.status < 300) {
      void evictIfNeeded();
      return file.uri;
    }
    if (r.status === 404) return null; // NOT_AVAILABLE: keep the type icon
    if (r.status === 429 && attempt <= MAX_BUSY_RETRIES) {
      await sleep(busyDelayMs(attempt, r.retryAfter));
      continue;
    }
    throw new ThumbnailHttpError(r.status);
  }
}

export const thumbnails = {
  /** Requests a thumbnail file URI (null = unavailable). Cancel when the view goes away. */
  request(hostId: string, path: string, version: string | number, size: number): Request<string | null> {
    const key = thumbnailKey(hostId, path, size, version);
    const known = resolved.get(key);
    if (known) return { promise: Promise.resolve(known), cancel: () => undefined };
    const req = queue.request(key, (signal) => load(hostId, path, size, key, signal));
    return {
      cancel: req.cancel,
      promise: req.promise.then(
        (uri) => {
          if (uri) resolved.set(key, uri);
          return uri;
        },
        (e: unknown) => {
          if (e instanceof ThumbnailHttpError && e.statusCode === THUMB_DENIED_STATUS) {
            deny(hostId);
            return null;
          }
          throw e;
        },
      ),
    };
  },
  /** True once this computer has refused previews for lack of a download grant. */
  isDenied(hostId: string): boolean {
    return denied.has(hostId);
  },
  /** Notified when a computer is newly refused; returns the unsubscribe. */
  subscribeDenied(listener: () => void): () => void {
    deniedListeners.add(listener);
    return () => {
      deniedListeners.delete(listener);
    };
  },
  /** Forgets a refusal so the next view asks again (pull-to-refresh). */
  forgetDenied(hostId: string) {
    if (denied.delete(hostId)) deniedListeners.forEach((l) => l());
  },
};
