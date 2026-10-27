import { t } from '../../i18n';

/** Localised "last seen" relative time (HostCard._relative). */
export function relativeLabel(at: Date, now: Date = new Date()): string {
  const s = Math.floor((now.getTime() - at.getTime()) / 1000);
  if (s < 5) return t('relativeJustNow');
  if (s < 60) return t('relativeSecondsAgo', { count: s });
  const m = Math.floor(s / 60);
  if (m < 60) return t('relativeMinutesAgo', { count: m });
  const h = Math.floor(m / 60);
  if (h < 24) return t('relativeHoursAgo', { count: h });
  return t('relativeDaysAgo', { count: Math.floor(h / 24) });
}
