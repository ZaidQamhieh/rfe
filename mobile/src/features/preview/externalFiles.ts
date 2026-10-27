import { Directory, File, Paths } from 'expo-file-system';

import { statusError } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { fetchToFileNative, openFileExternal, shareFileExternal } from '../../core/native';
import { clientForHost } from '../../services';
import { hashKey } from '../explorer/thumbnails';
import { mimeFromExtension } from './previewKind';

/** Largest file handed to another app; it is copied into the app cache first. */
export const MAX_EXTERNAL_BYTES = 1024 * 1024 * 1024;

/** A file name that is safe on disk and readable to the receiving app: anything outside word characters, dots and dashes becomes `_`. */
export function safeFileName(name: string): string {
  const cleaned = name.replace(/[^\w.-]/g, '_').replace(/^\.+/, '').slice(-120);
  return cleaned === '' ? 'file' : cleaned;
}

/** MIME type for the receiving app: the agent's type without parameters, else guessed from the extension. */
export function externalMime(e: Pick<Entry, 'name' | 'mimeType'>): string {
  const base = e.mimeType?.split(';')[0].trim();
  return base ? base : mimeFromExtension(e.name);
}

/** Downloads [entry] into `cache/<folder>/<safe name>`, first emptying that folder so only the latest hand-off stays on disk. Returns the native path. */
async function copyForHandOff(host: Host, entry: Entry, folder: 'share' | 'open'): Promise<string> {
  const dir = new Directory(Paths.cache, folder);
  dir.create({ idempotent: true, intermediates: true });
  for (const f of dir.list()) f.delete();
  const file = new File(dir, safeFileName(entry.name));
  const native = decodeURIComponent(file.uri.replace('file://', ''));
  const spec = (await clientForHost(host)).downloadSpec(entry.path);
  const r = await fetchToFileNative(`x${hashKey(entry.path)}${Date.now().toString(36)}`, spec.url, spec.headers, spec.pin, native, 120_000, MAX_EXTERNAL_BYTES);
  if (r.status < 200 || r.status >= 300) throw statusError(r.status);
  return native;
}

/** Shares the file through the system share sheet; false when no app can take it. */
export async function shareEntry(host: Host, entry: Entry): Promise<boolean> {
  return shareFileExternal(await copyForHandOff(host, entry, 'share'), externalMime(entry));
}

/** Opens the file in another app; false when none can handle its type. */
export async function openEntryWith(host: Host, entry: Entry): Promise<boolean> {
  return openFileExternal(await copyForHandOff(host, entry, 'open'), externalMime(entry));
}
