import { calendarDaysBetween } from '../../core/format';

export type RecentBucket = 'Today' | 'Yesterday' | 'Earlier';

/** Calendar-day bucket of [modified] relative to [now]; unknown or future times count as Today/Earlier respectively. */
export function bucketOf(modified: string | undefined, now: Date): RecentBucket {
  if (!modified) return 'Earlier';
  const d = new Date(modified);
  if (Number.isNaN(d.getTime())) return 'Earlier';
  const days = calendarDaysBetween(d, now);
  return days <= 0 ? 'Today' : days === 1 ? 'Yesterday' : 'Earlier';
}
