// The request bodies and query strings the web client sends, as pure functions so tests/requests.test.ts can pin them
// to what protocol/openapi.yaml and the agent's handlers read. No imports: Node runs it directly.

/** Rename: the agent reads `src` and the full destination path `dst` (a sibling named [newName]). */
export function renameBody(path: string, newName: string): { src: string; dst: string } {
  const sep = path.includes('\\') ? '\\' : '/';
  const idx = path.lastIndexOf(sep);
  // "/a.txt" has the root as parent; "C:\\a.txt" has the drive "C:", which needs its separator before the name too.
  return { src: path, dst: idx <= 0 ? `${sep}${newName}` : `${path.slice(0, idx)}${sep}${newName}` };
}

/** Trash restore and delete-forever both take `ids`, an array; a request with no ids empties or restores nothing sensible. */
export const trashIdsBody = (ids: string[]): { ids: string[] } => ({ ids });

/** Search narrowed to a folder uses `root`; the agent ignores any other name and searches everywhere. */
export function searchParams(query: string, root?: string): URLSearchParams {
  return new URLSearchParams({ q: query, ...(root ? { root } : {}) });
}

/** Share mint: `expiresInSeconds` (the agent defaults to 15 minutes when it is absent). */
export const mintBody = (path: string, expiresInSeconds?: number): { path: string; expiresInSeconds?: number } => ({ path, ...(expiresInSeconds ? { expiresInSeconds } : {}) });

/** Wake-on-LAN: the agent reads `mac`. */
export const wolBody = (mac: string): { mac: string } => ({ mac });

type BatchLike = { results?: { path: string; ok: boolean; error?: { code?: string; message?: string } }[] } | undefined;

/** A restore answers 200 even when an item could not be restored; this is the reason for the first failure, or null when every item worked. A missing result is a failure, never success. */
export function restoreFailure(res: BatchLike): string | null {
  const results = res?.results ?? [];
  if (results.length === 0) return 'Could not restore it';
  const bad = results.find((r) => !r.ok);
  return bad ? (bad.error?.message ?? 'Could not restore it') : null;
}
