import type { ShareLink } from '../../core/api/models';

/** Links that have not expired at [nowMs], soonest-to-expire first (`expiresAt` is unix seconds). */
export function liveLinks(links: readonly ShareLink[], nowMs: number = Date.now()): ShareLink[] {
  return links.filter((l) => l.expiresAt * 1000 > nowMs).sort((a, b) => a.expiresAt - b.expiresAt);
}

// A link is recorded under the file the computer resolved (a symlink's target), so a symlinked file's own path never
// matches it. The path a link was made for is remembered here, for as long as the app runs.
const resolvedPaths = new Map<string, string>();

/** Remembers that a link made for [requested] was recorded under [resolved]. */
export function rememberResolvedPath(requested: string, resolved: string): void {
  if (requested !== resolved) resolvedPaths.set(requested, resolved);
}

/** The live link for [path] that expires soonest, or undefined when the file has none. */
export function liveLinkForPath(links: readonly ShareLink[], path: string, nowMs: number = Date.now()): ShareLink | undefined {
  const resolved = resolvedPaths.get(path);
  return liveLinks(links, nowMs).find((l) => l.path === path || l.path === resolved);
}

export const withoutLink =(links: readonly ShareLink[], tokenHash: string): ShareLink[] => links.filter((l) => l.tokenHash !== tokenHash);
