import type { ShareLink } from '../../core/api/models';

/** Links that have not expired at [nowMs], soonest-to-expire first (`expiresAt` is unix seconds). */
export function liveLinks(links: readonly ShareLink[], nowMs: number = Date.now()): ShareLink[] {
  return links.filter((l) => l.expiresAt * 1000 > nowMs).sort((a, b) => a.expiresAt - b.expiresAt);
}

export const withoutLink = (links: readonly ShareLink[], tokenHash: string): ShareLink[] => links.filter((l) => l.tokenHash !== tokenHash);
