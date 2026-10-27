import { Directory, File, Paths } from 'expo-file-system';
import { useCallback, useEffect, useRef, useState } from 'react';

import { statusError } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { fetchCancelNative, fetchToFileNative } from '../../core/native';
import { offlineDeps } from '../offline/offlineDeps';
import { keepOfflineCopy, restoreOfflineCopy } from '../offline/offlineBodies';
import { clientForHost } from '../../services';
import { hashKey } from '../explorer/thumbnails';
import { previewExtension } from './previewKind';

// Size caps from preview_common.dart.
export const MAX_IN_MEMORY_PREVIEW_BYTES = 50 * 1024 * 1024;
export const MAX_AUDIO_PREVIEW_BYTES = 100 * 1024 * 1024;
export const MAX_EDITABLE_BYTES = 5 * 1024 * 1024;
const MAX_CACHE_BYTES = 256 * 1024 * 1024;
/** Bottom padding for scrolling viewers so the pager's "n of m" chip never hides the last line. */
export const PAGER_CHIP_CLEARANCE = 56;

export class TooLargeError extends Error {
  constructor(readonly size: number) {
    super(`File too large to preview (${size} bytes)`);
    this.name = 'TooLargeError';
  }
}

/** Cache file name: host, path and version (mtime + size) so an edited file is never served stale. */
export function previewCacheName(hostId: string, e: Pick<Entry, 'path' | 'name' | 'modified' | 'size'>): string {
  const ext = previewExtension(e.name);
  return `${hashKey(`${hostId}@${e.path}@${e.modified ?? ''}@${e.size ?? ''}`)}${ext ? `.${ext}` : ''}`;
}

let dir: Directory | null = null;
function cacheDir(): Directory {
  if (!dir) {
    dir = new Directory(Paths.cache, 'preview');
    dir.create({ idempotent: true, intermediates: true });
  }
  return dir;
}

function evictIfNeeded(keep: string) {
  try {
    const files = cacheDir().list().filter((f): f is File => f instanceof File && f.uri !== keep && !f.uri.endsWith('.part'));
    let total = files.reduce((n, f) => n + (f.size ?? 0), 0);
    if (total <= MAX_CACHE_BYTES) return;
    files.sort((a, b) => (a.modificationTime ?? 0) - (b.modificationTime ?? 0));
    for (const f of files) {
      if (total <= MAX_CACHE_BYTES * 0.8) break;
      total -= f.size ?? 0;
      f.delete();
    }
  } catch {
    // best-effort housekeeping
  }
}

const codeOf = (e: unknown) => (typeof e === 'object' && e !== null && 'code' in e ? String((e as { code: unknown }).code) : '');

/**
 * Fetches a file's bytes to the preview cache through the pinned native client and returns its
 * file:// URI. Refuses up front when the listed size exceeds [maxBytes], and the native side aborts
 * if the body grows past it anyway (a stale or hostile size never exhausts storage).
 */
export async function fetchPreviewFile(host: Host, entry: Entry, maxBytes: number, onCancel?: (cancel: () => void) => void): Promise<string> {
  if (entry.size != null && entry.size > maxBytes) throw new TooLargeError(entry.size);
  const file = new File(cacheDir(), previewCacheName(host.id, entry));
  const nativePath = decodeURIComponent(file.uri.replace('file://', ''));
  if (file.exists) {
    // Already fetched: make sure a pinned folder's file also has its offline copy.
    void keepOfflineCopy(offlineDeps, host, entry, nativePath, false);
    return file.uri;
  }
  const client = await clientForHost(host);
  const spec = client.downloadSpec(entry.path);
  const id = `p${hashKey(entry.path)}${Date.now().toString(36)}`;
  onCancel?.(() => void fetchCancelNative(id));
  let r;
  try {
    r = await fetchToFileNative(id, spec.url, spec.headers, spec.pin, nativePath, 60_000, maxBytes);
  } catch (e) {
    if (codeOf(e) === 'ERR_TOO_LARGE') throw new TooLargeError(entry.size ?? maxBytes + 1);
    // Host unreachable: a pinned folder's file opens from its encrypted offline copy.
    if (codeOf(e) === 'ERR_CONNECTION' && (await restoreOfflineCopy(offlineDeps, host, entry, nativePath))) return file.uri;
    throw e;
  }
  if (r.status < 200 || r.status >= 300) throw statusError(r.status);
  evictIfNeeded(file.uri);
  void keepOfflineCopy(offlineDeps, host, entry, nativePath, true);
  return file.uri;
}

export type PreviewFileState =
  | { status: 'loading' }
  | { status: 'ready'; uri: string }
  | { status: 'tooLarge'; size: number }
  | { status: 'error'; error: unknown };

/** Loads [entry] into the preview cache while mounted (and [enabled]); cancels the fetch on unmount. */
export function usePreviewFile(host: Host, entry: Entry, maxBytes: number, enabled = true) {
  const [state, setState] = useState<PreviewFileState>({ status: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const cancel = useRef<(() => void) | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    fetchPreviewFile(host, entry, maxBytes, (c) => (cancel.current = c)).then(
      (uri) => live && setState({ status: 'ready', uri }),
      (error) => live && setState(error instanceof TooLargeError ? { status: 'tooLarge', size: error.size } : { status: 'error', error }),
    );
    return () => {
      live = false;
      cancel.current?.();
      cancel.current = null;
    };
  }, [host, entry, maxBytes, enabled, attempt]);
  const retry = useCallback(() => {
    setState({ status: 'loading' });
    setAttempt((n) => n + 1);
  }, []);
  return { state, retry };
}
