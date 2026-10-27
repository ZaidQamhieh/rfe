/** Remaining time as `mm:ss`, or `h:mm:ss` from an hour up; never negative. */
export function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = String(Math.floor((total % 3600) / 60)).padStart(2, '0');
  const s = String(total % 60).padStart(2, '0');
  return h > 0 ? `${h}:${m}:${s}` : `${m}:${s}`;
}

/** Milliseconds until a link's `expiresAt` (unix seconds). */
export const remainingMs = (expiresAtSeconds: number, now: number = Date.now()) => Math.max(0, expiresAtSeconds * 1000 - now);
