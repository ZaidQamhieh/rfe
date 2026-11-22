import type { UsageEntry } from '../../core/api/models';

/** Share of the largest child, 0..1, for the bar length (the biggest row fills the bar). */
export const barFraction = (e: UsageEntry, entries: UsageEntry[]): number => {
  const max = entries.length ? entries[0].bytes : 0;
  return max > 0 ? Math.min(1, e.bytes / max) : 0;
};

/** Share of the whole folder as a whole percent; "<1" below one percent. */
export const sharePercent = (bytes: number, total: number): string => {
  if (total <= 0 || bytes <= 0) return '0';
  const p = (bytes / total) * 100;
  return p < 1 ? '<1' : String(Math.round(p));
};
