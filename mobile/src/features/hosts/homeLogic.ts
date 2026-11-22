import type { Entry } from '../../core/api/models';
import { bucketOf } from '../recent/recentBuckets';

/** How many recently changed files Home shows as tiles before "See all". */
export const RECENT_SHOWN = 3;
/** How many recently changed files Home asks the computer for, so "See all" can open more without another request. */
export const RECENT_FETCH = 30;
/** How many it asks for before hidden files are dropped, so a burst of config writes cannot empty the list. */
export const RECENT_ASK = 120;

/**
 * Drops files the user never opens on purpose: anything named with a leading dot, or inside a dot-folder (`~/.claude.json`, `.cache/x`).
 * The computer's recent list includes them, so Home would otherwise show configuration and cache churn as "Changed lately".
 */
export function withoutHidden(entries: Entry[]): Entry[] {
  return entries.filter((e) => !e.path.split(/[\\/]/).some((seg) => seg.length > 1 && seg.startsWith('.') && seg !== '..'));
}

/** The files Home lists under "Changed lately": the newest few, or all of them once expanded. Order is kept (newest first). */
export function visibleRecents(entries: Entry[], expanded: boolean): Entry[] {
  return expanded ? entries : entries.slice(0, RECENT_SHOWN);
}

/**
 * The "Changed lately" tiles laid out in rows of RECENT_SHOWN: one row while collapsed, every file once expanded. A short last row
 * is padded with null so every tile keeps the same width instead of stretching.
 */
export function recentRows(entries: Entry[], expanded: boolean): (Entry | null)[][] {
  const shown = visibleRecents(entries, expanded);
  const rows: (Entry | null)[][] = [];
  for (let i = 0; i < shown.length; i += RECENT_SHOWN) {
    const row: (Entry | null)[] = shown.slice(i, i + RECENT_SHOWN);
    while (row.length < RECENT_SHOWN) row.push(null);
    rows.push(row);
  }
  return rows;
}

/** A tile's date line: "Today 09:14", "Yesterday", or a short day and month; empty when the time is unknown. */
export function recentWhen(modified: string | undefined, now: Date, labels: { today: string; yesterday: string }): string {
  if (!modified) return '';
  const d = new Date(modified);
  if (Number.isNaN(d.getTime())) return '';
  const bucket = bucketOf(modified, now);
  if (bucket === 'Today') return `${labels.today} ${d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}`;
  if (bucket === 'Yesterday') return labels.yesterday;
  return d.toLocaleDateString([], { day: 'numeric', month: 'short' });
}
