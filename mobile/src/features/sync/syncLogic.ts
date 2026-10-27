import type { Entry } from '../../core/api/models';

/**
 * Whether [remote] must be downloaded again to keep the local copy current: missing, a different size, or changed on
 * the computer after the local file was written (a same-size edit still re-syncs).
 */
export function needsSync(remote: Entry, local: { size: number; modified: number | null } | undefined): boolean {
  if (!local) return true;
  if (local.size !== (remote.size ?? 0)) return true;
  const remoteMs = remote.modified ? Date.parse(remote.modified) : NaN;
  if (!Number.isNaN(remoteMs) && local.modified !== null) return remoteMs > local.modified;
  return false;
}

/** A remote name that is safe to create locally: one plain path segment. */
export const isSafeName = (name: string) => name !== '' && name !== '.' && name !== '..' && !/[\\/\u0000]/.test(name);

/**
 * A `file://` URI for a native path. `#`, `%` and `?` in a name (`report#1.pdf`) must be percent-encoded, or the URI
 * parser cuts the name at the `#` and the copy or delete targets another file.
 */
export const fileUriFromPath = (path: string) => `file://${path.split('/').map(encodeURIComponent).join('/')}`;

/** The native path of a `file://` URI, tolerating a name with a stray `%` that was never encoded. */
export function pathFromFileUri(uri: string): string {
  const raw = uri.replace(/^file:\/\//, '');
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}
