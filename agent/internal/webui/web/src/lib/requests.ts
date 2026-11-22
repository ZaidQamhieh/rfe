// The request bodies and query strings the web client sends, as pure functions so tests/requests.test.ts can pin them
// to what protocol/openapi.yaml and the agent's handlers read. Its only import is a type, erased when Node runs it directly.
import type { AgentStatus, BatchResponse } from './api';

/** Rename: the agent reads `src` and the full destination path `dst` (a sibling named [newName]). */
export function renameBody(path: string, newName: string): { src: string; dst: string } {
  const sep = pathSep(path);
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

/** Whether to offer Wake-on-LAN: not when the agent reports this device lacks the wake grant (an older agent or an unloaded status hides nothing; the agent still enforces). */
export const canWake = (status: Pick<AgentStatus, 'fileCapabilities'> | undefined): boolean => status?.fileCapabilities?.wake !== false;

/** Share mint: `expiresInSeconds` (the agent defaults to 15 minutes when it is absent). */
export const mintBody = (path: string, expiresInSeconds?: number): { path: string; expiresInSeconds?: number } => ({ path, ...(expiresInSeconds ? { expiresInSeconds } : {}) });

/** The separator of a path: `\` for a Windows drive (C:\...) or UNC (\\server) path, `/` for everything else. */
function pathSep(path: string): string {
  return /^([A-Za-z]:|\\\\)/.test(path) ? '\\' : '/';
}

/** Appends a child name to a folder path with the path's own separator; the root "/" (or "") gives "/name". */
export function joinPath(base: string, name: string): string {
  if (base === '') return `/${name}`;
  const sep = pathSep(base);
  return base.endsWith(sep) ? `${base}${name}` : `${base}${sep}${name}`;
}

/** Splits on either separator when [windows], on "/" only otherwise (a POSIX name may hold a backslash). */
function segmentsOf(path: string, windows: boolean): string[] {
  return path.split(windows ? /[\\/]/ : '/').filter(Boolean);
}

/** The non-empty segments of a path: split on either separator for a Windows path, on "/" only otherwise. */
export function splitPath(path: string): string[] {
  return segmentsOf(path, pathSep(path) === '\\');
}

/** Breadcrumbs for a path: each segment's label and the full path up to it. A Windows drive "C:" is kept as "C:\", a UNC root as "\\server\share\". */
export function crumbsOf(path: string): { label: string; full: string }[] {
  const out: { label: string; full: string }[] = [];
  const windows = pathSep(path) === '\\';
  let acc = '';
  let rest = path;
  // The drive, or the UNC server and share, is one crumb: it has no parent folder to go up into.
  const root = windows ? /^\\\\[^\\/]+(?:[\\/]+[^\\/]+)?|^[A-Za-z]:/.exec(path)?.[0] : undefined;
  if (root) {
    acc = `${root}\\`;
    out.push({ label: root, full: acc });
    rest = path.slice(root.length);
  }
  for (const s of segmentsOf(rest, windows)) {
    acc = joinPath(acc, s);
    out.push({ label: s, full: acc });
  }
  return out;
}

/** A restore answers 200 even when an item could not be restored; this is the reason for the first failure, or null when every item worked. A missing result is a failure, never success. */
export function restoreFailure(res: BatchResponse | undefined): string | null {
  const results = res?.results ?? [];
  if (results.length === 0) return 'Could not restore it';
  const bad = results.find((r) => !r.ok);
  return bad ? (bad.error?.message ?? 'Could not restore it') : null;
}
