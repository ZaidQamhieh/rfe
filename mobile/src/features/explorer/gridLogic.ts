import type { Entry } from '../../core/api/models';
import { formatSize } from '../../core/format';

/** Day headers read like "8 November" (the app is English-only). */
const DAY_LOCALE = 'en-GB';

/** The local calendar day of an ISO timestamp as `YYYY-MM-DD`; '' when it is missing or not a date. */
export function dayKey(modified: string | undefined): string {
  if (!modified) return '';
  const d = new Date(modified);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

/** "8 November", with the year added ("8 November 2024") when the day is not in the current year of [now]. */
export function formatDayLabel(date: Date, now: Date = new Date()): string {
  const withYear = date.getFullYear() !== now.getFullYear();
  return date.toLocaleDateString(DAY_LOCALE, withYear ? { day: 'numeric', month: 'long', year: 'numeric' } : { day: 'numeric', month: 'long' });
}

/** One run of neighbouring entries that share a calendar day (`date` is null for entries with no usable date). */
export type DayGroup = { key: string; date: Date | null; entries: Entry[] };

/** Splits [entries] into runs of the same day, keeping the incoming order so a sort by date stays a sort by date. */
export function groupByDay(entries: readonly Entry[]): DayGroup[] {
  const groups: DayGroup[] = [];
  for (const e of entries) {
    const key = dayKey(e.modified);
    const last = groups[groups.length - 1];
    if (last && last.key === key) last.entries.push(e);
    else groups.push({ key, date: key ? new Date(e.modified as string) : null, entries: [e] });
  }
  return groups;
}

/** A line of the photo grid: a day header (with how many items that day holds) or up to `columns` cells. */
export type GridRow = { type: 'day'; key: string; label: string; count: number } | { type: 'cells'; key: string; entries: Entry[] };

/**
 * Lays [entries] out for a grid of [columns] cells. With `byDay` every day gets a header line (`label` turns its date
 * into text) and its cells start on a fresh line; without it the cells simply fill the lines in order.
 */
export function buildGridRows(entries: readonly Entry[], columns: number, opts: { byDay: boolean; label: (date: Date | null) => string }): GridRow[] {
  const cols = Math.max(1, Math.floor(columns));
  const rows: GridRow[] = [];
  const pushCells = (list: readonly Entry[], prefix: string) => {
    for (let i = 0; i < list.length; i += cols) rows.push({ type: 'cells', key: `${prefix}:${i}`, entries: list.slice(i, i + cols) });
  };
  if (!opts.byDay) {
    pushCells(entries, 'all');
    return rows;
  }
  groupByDay(entries).forEach((g, n) => {
    rows.push({ type: 'day', key: `day:${n}:${g.key}`, label: opts.label(g.date), count: g.entries.length });
    pushCells(g.entries, `d${n}`);
  });
  return rows;
}

/** Side of one square cell: [width] split into [columns] cells with [gap] between them (never negative). */
export function cellSize(width: number, columns: number, gap: number): number {
  return Math.max(0, Math.floor((width - gap * (columns - 1)) / columns));
}

/** A video length as `m:ss`, or `h:mm:ss` from one hour up; '' when the length is unknown or not positive. */
export function formatDuration(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return '';
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}

/** The tools-row summary: "11 items · 5.3 MB" ("1 item", "50+ items" while more pages exist; the size is left out for 0 bytes). */
export function folderSummary(entries: readonly Entry[], more: boolean): string {
  const n = entries.length;
  const count = `${n}${more ? '+' : ''} ${n === 1 && !more ? 'item' : 'items'}`;
  let bytes = 0;
  for (const e of entries) if (!e.isDir) bytes += e.size ?? 0;
  return bytes > 0 ? `${count} · ${formatSize(bytes)}` : count;
}
