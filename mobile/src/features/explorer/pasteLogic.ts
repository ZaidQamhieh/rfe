import { parentDirOf } from './paths';

/**
 * The clipboard paths a paste should act on. A cut source that already sits in the destination folder has nothing to
 * move; keeping it would make it look like a name clash with itself (and offer to overwrite it). A copy into the
 * same folder is a real duplicate, so it is kept.
 */
export function pasteSources(paths: readonly string[], dest: string, isCut: boolean): string[] {
  return isCut ? paths.filter((p) => parentDirOf(p) !== dest) : [...paths];
}

/** Why [dest] cannot take [sources]: a folder cannot go into itself or one of its own sub-folders. Null when it can. */
export function destinationProblem(sources: readonly string[], dest: string): 'inside-itself' | null {
  const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '');
  const d = norm(dest);
  return sources.some((s) => d === norm(s) || d.startsWith(`${norm(s)}/`)) ? 'inside-itself' : null;
}

/**
 * True when a paste or move finished with no failed item. Only then may a cut clipboard be cleared or the selection
 * dropped: after a partial failure the failed items must stay available to paste again.
 */
export const transferFullySucceeded = (res: { failed: readonly unknown[] }): boolean => res.failed.length === 0;
