import { Directory, File, Paths } from 'expo-file-system';

import { fetchCancelNative, fetchToFileNative } from '../../core/native';
import { clientForHost, hostStore } from '../../services';
import { FetchQueue, type Request } from './fetchQueue';

const MAX_CONCURRENT = 4;
const MAX_BUSY_RETRIES = 3;
const MAX_CACHE_BYTES = 32 * 1024 * 1024;

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
  return retryAfterSeconds == null ? 200 * attempt : Math.min(Math.max(retryAfterSeconds, 1), 2) * 1000;
}

let dir: Directory | null = null;
function cacheDir(): Directory {
  if (!dir) {
    dir = new Directory(Paths.cache, 'thumbs');
    dir.create({ idempotent: true, intermediates: true });
  }
  return dir;
}

const negative = new Set<string>(); // keys the agent has no thumbnail for
const resolved = new Map<string, string>(); // key -> file uri (in-memory index)
const queue = new FetchQueue<string | null>(MAX_CONCURRENT, null);
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
let evicting = false;

async function evictIfNeeded() {
  if (evicting) return;
  evicting = true;
  try {
    const files = cacheDir().list().filter((f): f is File => f instanceof File);
    let total = files.reduce((n, f) => n + (f.size ?? 0), 0);
    if (total <= MAX_CACHE_BYTES) return;
    // oldest first by modification time
    files.sort((a, b) => (a.modificationTime ?? 0) - (b.modificationTime ?? 0));
    for (const f of files) {
      if (total <= MAX_CACHE_BYTES * 0.8) break;
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

async function load(hostId: string, path: string, size: number, key: string, signal: { readonly aborted: boolean; onAbort(cb: () => void): void }): Promise<string | null> {
  const file = new File(cacheDir(), `${hashKey(key)}.jpg`);
  if (file.exists) return file.uri;
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
    throw new Error(`thumbnail HTTP ${r.status}`);
  }
}

export const thumbnails = {
  /** Requests a thumbnail file URI (null = unavailable). Cancel when the view goes away. */
  request(hostId: string, path: string, version: string | number, size: number): Request<string | null> {
    const key = thumbnailKey(hostId, path, size, version);
    if (negative.has(key)) return { promise: Promise.resolve(null), cancel: () => undefined };
    const known = resolved.get(key);
    if (known) return { promise: Promise.resolve(known), cancel: () => undefined };
    const req = queue.request(key, (signal) => load(hostId, path, size, key, signal));
    return {
      cancel: req.cancel,
      promise: req.promise.then((uri) => {
        if (uri) resolved.set(key, uri);
        return uri;
      }),
    };
  },
  markUnavailable(key: string) {
    negative.add(key);
  },
};
