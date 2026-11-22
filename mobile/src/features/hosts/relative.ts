import { calendarDaysBetween } from '../../core/format';
import { t } from '../../i18n';

export { relativeLabel } from '../../core/format';

/**
 * When a computer was last seen, as a clock time: "22:41" today, "22:41 yesterday", or "22:41 on 3 Nov" for older.
 * Days are counted on the phone's calendar, not in 24-hour steps.
 */
export function seenWhen(at: Date, now: Date = new Date()): string {
  const time = at.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const days = calendarDaysBetween(at, now);
  if (days <= 0) return time;
  if (days === 1) return t('seenYesterday', { time });
  return t('seenOnDate', { time, date: at.toLocaleDateString([], { day: 'numeric', month: 'short' }) });
}
