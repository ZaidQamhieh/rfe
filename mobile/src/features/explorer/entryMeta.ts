import type { Entry } from '../../core/api/models';
import { calendarDaysBetween, formatSize } from '../../core/format';
import { t } from '../../i18n';

/** The listing stops counting a folder's children here; a folder at the cap reads "1000+ items". */
const CHILD_COUNT_CAP = 1000;

/**
 * Second line of a row: the item count for a folder, the modified time for a file ("Today 09:14", "Yesterday 22:51",
 * else the date). Empty when the listing does not return the value.
 */
export function entryMeta(e: Entry, now: Date = new Date()): string {
  if (e.isDir) {
    if (e.childCount === undefined) return '';
    return e.childCount >= CHILD_COUNT_CAP ? t('itemCountCapped', { count: CHILD_COUNT_CAP }) : t('itemCount', { count: e.childCount });
  }
  return e.modified ? dateLabel(new Date(e.modified), now) : '';
}

/** Trailing size of a file row; empty for folders and when the size is unknown. */
export function entrySize(e: Entry): string {
  return e.isDir ? '' : formatSize(e.size);
}

function dateLabel(d: Date, now: Date): string {
  const days = calendarDaysBetween(d, now);
  if (days > 1) return d.toLocaleDateString([], d.getFullYear() === now.getFullYear() ? { day: 'numeric', month: 'short' } : { day: 'numeric', month: 'short', year: 'numeric' });
  const time = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  return `${days === 1 ? t('yesterdayLabel') : t('todayLabel')} ${time}`;
}
