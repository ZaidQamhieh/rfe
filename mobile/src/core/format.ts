// Port of core/ui/format.dart (single source for sizes/dates, per the app's conventions).
import { t } from '../i18n';

const KB = 1024;
export const MB = KB * 1024;
export const GB = MB * 1024;
const DAY_MS = 86_400_000;

/** Seconds below which a relative time reads as "just now". */
const JUST_NOW_SECONDS = 5;

export function formatSize(bytes: number | null | undefined): string {
  if (bytes == null) return '';
  if (bytes < KB) return `${bytes} B`;
  if (bytes < MB) return `${(bytes / KB).toFixed(1)} KB`;
  if (bytes < GB) return `${(bytes / MB).toFixed(1)} MB`;
  return `${(bytes / GB).toFixed(2)} GB`;
}

/** A transfer or bandwidth rate, e.g. "1.5 MB/s". */
export function formatRate(bytesPerSec: number): string {
  return `${formatSize(bytesPerSec)}/s`;
}

export function formatDate(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

/** Whole calendar days from [from] to [to] on the phone's calendar, not in 24-hour steps. */
export function calendarDaysBetween(from: Date, to: Date): number {
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  return Math.round((day(to) - day(from)) / DAY_MS);
}

/** Localised relative time ("5s ago", "3h ago", "2d ago"), using the relative* keys in en.json. */
export function relativeLabel(at: Date, now: Date = new Date()): string {
  const s = Math.floor((now.getTime() - at.getTime()) / 1000);
  if (s < JUST_NOW_SECONDS) return t('relativeJustNow');
  if (s < 60) return t('relativeSecondsAgo', { count: s });
  const m = Math.floor(s / 60);
  if (m < 60) return t('relativeMinutesAgo', { count: m });
  const h = Math.floor(m / 60);
  if (h < 24) return t('relativeHoursAgo', { count: h });
  return t('relativeDaysAgo', { count: Math.floor(h / 24) });
}

const RELATIVE_WINDOW_DAYS = 7;

/** Relative wording for the last week, the calendar date for anything older. */
export function relativeOrDate(at: Date, now: Date = new Date()): string {
  return now.getTime() - at.getTime() >= RELATIVE_WINDOW_DAYS * DAY_MS ? formatDate(at) : relativeLabel(at, now);
}
