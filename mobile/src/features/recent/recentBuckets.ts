import type { Entry } from '../../core/api/models';

export type RecentBucket = 'Today' | 'Yesterday' | 'Earlier';
export const BUCKET_ORDER: RecentBucket[] = ['Today', 'Yesterday', 'Earlier'];

/** Calendar-day bucket of [modified] relative to [now]; unknown or future times count as Today/Earlier respectively. */
export function bucketOf(modified: string | undefined, now: Date): RecentBucket {
  if (!modified) return 'Earlier';
  const d = new Date(modified);
  if (Number.isNaN(d.getTime())) return 'Earlier';
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(now) - day(d)) / 86_400_000);
  return days <= 0 ? 'Today' : days === 1 ? 'Yesterday' : 'Earlier';
}

/** Non-empty buckets in display order, each keeping the agent's newest-first order. */
export function groupRecent(entries: Entry[], now: Date): { label: RecentBucket; entries: Entry[] }[] {
  const map = new Map<RecentBucket, Entry[]>();
  for (const e of entries) {
    const b = bucketOf(e.modified, now);
    map.set(b, [...(map.get(b) ?? []), e]);
  }
  return BUCKET_ORDER.filter((b) => map.has(b)).map((label) => ({ label, entries: map.get(label)! }));
}
