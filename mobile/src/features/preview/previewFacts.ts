import type { Entry, MediaInfo } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { t, type StringKey } from '../../i18n';

/** One line of the info block under a preview: the string-table key of its label and the text to show. */
export type FactRow = { labelKey: StringKey; value: string };

const MONTH_KEYS: readonly StringKey[] = ['dateMonthJan', 'dateMonthFeb', 'dateMonthMar', 'dateMonthApr', 'dateMonthMay', 'dateMonthJun', 'dateMonthJul', 'dateMonthAug', 'dateMonthSep', 'dateMonthOct', 'dateMonthNov', 'dateMonthDec'];
const pad2 = (n: number) => String(n).padStart(2, '0');

/** A moment as "8 Nov 2026, 17:02" in the phone's own time zone; empty when the value is not a real date. */
export function formatDateTime(ms: number | undefined): string {
  if (ms === undefined || Number.isNaN(ms)) return '';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.getDate()} ${t(MONTH_KEYS[d.getMonth()])} ${d.getFullYear()}, ${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

/** A length of time as "1:05" or "1:02:03" (hours only when needed), rounded down to whole seconds; empty when it is missing or not positive. */
export function formatDuration(ms: number | undefined): string {
  if (ms === undefined || !Number.isFinite(ms) || ms <= 0) return '';
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h > 0 ? `${h}:${pad2(m)}:${pad2(s)}` : `${m}:${pad2(s)}`;
}

/** "4032 × 3024", or empty unless both sides are positive. */
export function formatDimensions(width: number | undefined, height: number | undefined): string {
  return width && height && width > 0 && height > 0 ? `${width} × ${height}` : '';
}

/**
 * The rows of a file's info block, in reading order: size, then (photos and videos, when the file says so) taken,
 * dimensions and length, then modified. A row appears only when it has a value, so a file with no details shows fewer rows.
 */
export function factRows(entry: Pick<Entry, 'size' | 'modified'>, info?: MediaInfo): FactRow[] {
  const rows: FactRow[] = [];
  const add = (labelKey: StringKey, value: string) => {
    if (value !== '') rows.push({ labelKey, value });
  };
  add('previewFactSize', formatSize(entry.size));
  add('previewFactTaken', formatDateTime(info?.takenAt));
  add('previewFactDimensions', formatDimensions(info?.width, info?.height));
  add('previewFactLength', formatDuration(info?.durationMs));
  add('previewFactModified', entry.modified ? formatDateTime(Date.parse(entry.modified)) : '');
  return rows;
}
