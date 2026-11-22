import { calendarDaysBetween, formatSize } from '../../core/format';
import type { TransferRecord } from '../../core/native';
import { t } from '../../i18n';
import { planUploads, type Resolution } from './uploadPlan';
import { assetIdFromTransferId } from '../photoBackup/photoBackupLogic';
import { groupTransfers, isActive, isNameClash, isUpload, transferProgress } from './transferLogic';

/** The Transfers screen's four groups, each newest first. */
export type TransferSections = {
  /** Uploads stopped by a name clash: they need the owner to choose. */
  waiting: TransferRecord[];
  /** Running, queued or paused. */
  moving: TransferRecord[];
  /** Failed for any other reason. */
  failed: TransferRecord[];
  /** Finished or cancelled. */
  done: TransferRecord[];
};

/** Splits every transfer into the screen's groups; a name clash leaves the failed list and goes to "waiting". */
export function sectionTransfers(all: readonly TransferRecord[]): TransferSections {
  const g = groupTransfers([...all]);
  return {
    waiting: g.failed.filter(isNameClash),
    moving: g.active,
    failed: g.failed.filter((r) => !isNameClash(r)),
    done: g.finished,
  };
}

/**
 * Splits the finished transfers: the photo-backup uploads that finished (one per photo, grouped into a single summary row)
 * and everything else. Failed and running backups are not finished, so they stay individual rows in their own groups.
 */
export function splitBackupDone(done: readonly TransferRecord[]): { backups: TransferRecord[]; others: TransferRecord[] } {
  const isFinishedBackup = (r: TransferRecord) => r.state === 'DONE' && isUpload(r) && assetIdFromTransferId(r.id) !== null;
  return { backups: done.filter(isFinishedBackup), others: done.filter((r) => !isFinishedBackup(r)) };
}

/** Whole percent done, or null while the size is not known. */
export function percentDone(r: TransferRecord): number | null {
  const p = transferProgress(r);
  return p === null ? null : Math.floor(p * 100);
}

/** "to Studio PC · 63%" (or "from ..."); the computer part is left out when its name is not known, the percent while the size is unknown. */
export function movingLine(r: TransferRecord, computer: string | undefined): string {
  const where = computer ? t(isUpload(r) ? 'trTo' : 'trFrom', { computer }) : t(isUpload(r) ? 'trSending' : 'trReceiving');
  const pct = percentDone(r);
  return pct === null ? where : `${where} · ${pct}%`;
}

/** The pause-all button: how many transfers it would pause, and whether it resumes instead (nothing runs, something is paused). */
export function pauseAllTarget(moving: readonly TransferRecord[]): { ids: string[]; resume: boolean } {
  const running = moving.filter(isActive);
  if (running.length > 0) return { ids: running.map((r) => r.id), resume: false };
  return { ids: moving.filter((r) => r.state === 'PAUSED').map((r) => r.id), resume: true };
}

/** What is known about one side of a name clash. Missing numbers are shown as an unknown, never as zero. */
export type ClashSide = { size?: number; modifiedAt?: number };
export type ClashColumn = { heading: string; size: string; when: string };

const pad = (n: number) => String(n).padStart(2, '0');

/** "Today 14:20", "Yesterday 09:14" or a short date, in local time; empty when the moment is unknown. */
export function clashWhen(at: number | undefined, now: Date = new Date()): string {
  if (at === undefined || !Number.isFinite(at) || at <= 0) return '';
  const d = new Date(at);
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  const days = calendarDaysBetween(d, now);
  if (days === 0) return t('trToday', { time });
  if (days === 1) return t('trYesterday', { time });
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${time}`;
}

/** The two columns of the clash compare: the file being sent, and the one already on the computer (null while still loading or if it vanished). */
export function clashColumns(sending: ClashSide, existing: ClashSide | null, computer: string, now: Date = new Date()): [ClashColumn, ClashColumn] {
  const col = (heading: string, s: ClashSide | null): ClashColumn => ({
    heading,
    size: s?.size === undefined ? t('trUnknown') : formatSize(s.size),
    when: clashWhen(s?.modifiedAt, now),
  });
  return [col(t('trClashSending'), sending), col(t('trClashThere', { computer }), existing)];
}

/**
 * The upload to queue again once the owner has chosen: the name to use on the computer and whether to overwrite, or null
 * for Skip. `taken` is every name already in that folder (the clashing one included); Keep both picks the next free "(1)" name.
 */
export function planClashResolution(name: string, resolution: Resolution, taken: ReadonlySet<string>): { targetName: string; overwrite: boolean } | null {
  const colliding = new Set(taken);
  colliding.add(name);
  const [item] = planUploads([{ name, uri: '' }], colliding, resolution);
  return item ? { targetName: item.targetName, overwrite: item.overwrite } : null;
}
