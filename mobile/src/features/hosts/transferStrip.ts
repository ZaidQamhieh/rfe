import type { TransferRecord } from '../../core/native';
import { t } from '../../i18n';
import { groupTransfers, isUpload, transferName } from '../transfers/transferLogic';

/** What the in-progress strip above the bottom nav shows: one title, one detail line, a 0..1 bar and what its button does. */
export type StripModel = {
  title: string;
  line: string;
  progress: number;
  /** Every counted transfer is paused, so the button resumes instead of pausing. */
  paused: boolean;
  /** The transfers the button acts on: the running ones when pausing, the paused ones when resuming. */
  targetIds: string[];
};

/** How many transfers are in progress (running, waiting or paused): the plain number beside "Transfers" in the nav. */
export function activeTransferCount(all: readonly TransferRecord[]): number {
  return groupTransfers([...all]).active.length;
}

/**
 * The strip for the transfers still in progress, or null when there are none. Direction and computer name are only named
 * when every transfer agrees; the bar is bytes done over bytes known across all of them.
 */
export function stripModel(all: readonly TransferRecord[], hostNames: Readonly<Record<string, string>>, formatSize: (n: number) => string): StripModel | null {
  const active = groupTransfers([...all]).active;
  if (active.length === 0) return null;

  const uploads = active.filter(isUpload).length;
  const verb = uploads === active.length ? 'Sending' : uploads === 0 ? 'Receiving' : 'Moving';
  const one = active.length === 1;
  const title =
    verb === 'Moving'
      ? t('stripMovingMany', { count: active.length })
      : verb === 'Sending'
        ? one
          ? t('stripSendingOne', { name: transferName(active[0]) })
          : t('stripSendingMany', { count: active.length })
        : one
          ? t('stripReceivingOne', { name: transferName(active[0]) })
          : t('stripReceivingMany', { count: active.length });

  const hostIds = new Set(active.map((r) => r.hostId));
  const hostName = hostIds.size === 1 ? hostNames[active[0].hostId] : undefined;
  const named = hostName && verb !== 'Moving' ? t(verb === 'Sending' ? 'stripToHost' : 'stripFromHost', { title, host: hostName }) : title;

  const known = active.filter((r) => r.total > 0);
  const total = known.reduce((n, r) => n + r.total, 0);
  const done = known.reduce((n, r) => n + Math.min(r.received, r.total), 0);
  const paused = active.every((r) => r.state === 'PAUSED');
  const params = { done: formatSize(done), total: formatSize(total) };
  const line = total <= 0 ? '' : paused ? t('stripPausedLine', params) : t('stripProgressLine', params);

  return {
    title: named,
    line,
    progress: total > 0 ? Math.min(1, done / total) : 0,
    paused,
    targetIds: active.filter((r) => (paused ? r.state === 'PAUSED' : r.state !== 'PAUSED')).map((r) => r.id),
  };
}
