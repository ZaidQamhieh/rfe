import type { StringKey } from '../../i18n';
import type { Params } from '../../i18n/format';
import type { BackupResult } from './photoBackupService';

/** One piece of text as a string-table key plus its values, so a caller can translate it and a test can check which text was chosen. */
export type Msg = { key: StringKey; params?: Params };

export type StatusState =
  | 'notSetUp' | 'off' | 'chooseComputer' | 'idleEmpty' | 'upToDate' | 'running' | 'stillUploading'
  | 'pausedWifi' | 'pausedCharging' | 'unreachable' | 'notAccepted' | 'couldNotPrepare' | 'noDestination'
  | 'permissionDenied' | 'phoneRemoved' | 'albumsGone' | 'policyBlocked';

/** The one action a status offers besides the page's own Back up now button. */
export type StatusAction = 'tryAgain' | 'resetRecord' | 'openSettings' | null;

export type BackupStatus = {
  state: StatusState;
  /** Attention states are drawn with a warning icon as well as words; colour alone never carries state. */
  tone: 'neutral' | 'ok' | 'attention';
  /** The short line under "Photo backup" on the This phone screen. */
  row: Msg;
  /** The first line of the status row on the Photo backup page (also what a toast echoes). */
  headline: Msg;
  /** The second line of the status row, or null when the headline says it all. */
  detail: Msg | null;
  action: StatusAction;
};

export type StatusInput = {
  /** A computer to back up to has been chosen. */
  hostChosen: boolean;
  enabled: boolean;
  /** Label of the chosen computer. */
  computer: string;
  /** Photos backed up to the chosen computer so far. */
  backedUp: number;
  /** Photos queued or uploading that have not finished yet. */
  pending: number;
  /** What the latest backup run or permission request ended with; null when none happened on this screen. */
  last: BackupResult | null;
};

const make = (state: StatusState, tone: BackupStatus['tone'], row: Msg, headline: Msg, detail: Msg | null = null, action: StatusAction = null): BackupStatus => ({ state, tone, row, headline, detail, action });

/** The status for a run that ended in a problem or a wait, or null when [last] is not one (the caller then reads the counts). */
function fromResult(last: BackupResult, i: StatusInput): BackupStatus | null {
  switch (last.kind) {
    case 'incomplete':
      return make('couldNotPrepare', 'attention', { key: 'phBackupAttentionIncomplete', params: { count: last.count } }, { key: 'backupIncomplete', params: { count: last.count } }, null, 'tryAgain');
    case 'refused':
      return make('notAccepted', 'attention', { key: 'phBackupAttentionRefused', params: { count: last.count } }, { key: 'backupRefused', params: { count: last.count, reason: last.reason } }, null, 'resetRecord');
    case 'disabled':
      return make('off', 'neutral', { key: 'phBackupOff' }, { key: 'enableBackupFirst' });
    case 'notConfigured':
      return make('chooseComputer', 'attention', { key: 'phBackupAttentionChooseComputer' }, { key: 'pickPcFirst' });
    case 'albumsGone':
      return make('albumsGone', 'attention', { key: 'phBackupAttentionAlbumsGone' }, { key: 'backupAlbumsGone' });
    case 'serverNotConfigured':
      return make('noDestination', 'attention', { key: 'phBackupAttentionNoDestination' }, { key: 'serverDestNotConfigured' });
    case 'skipped': {
      const host = last.host ?? i.computer;
      if (last.reason === 'wifi') return make('pausedWifi', 'neutral', { key: 'phBackupPausedWifi' }, { key: 'backupWaitingWifi' }, { key: 'backupStatusWifiDetail' });
      if (last.reason === 'charging') return make('pausedCharging', 'neutral', { key: 'phBackupPausedCharging' }, { key: 'backupWaitingCharging' }, { key: 'backupStatusChargingDetail' });
      if (last.reason === 'refused') return make('phoneRemoved', 'attention', { key: 'phBackupAttentionRepair' }, { key: 'phoneRemovedMessage' });
      if (last.reason === 'unreachable') return make('unreachable', 'attention', { key: 'phBackupAttentionUnreachable', params: { computer: host } }, { key: 'backupStatusUnreachable', params: { computer: host } }, { key: 'backupStatusUnreachableDetail' }, 'tryAgain');
      return make('policyBlocked', 'attention', { key: 'phBackupAttentionPolicy' }, { key: 'backupPolicyBlocked', params: { host } });
    }
    default:
      return null;
  }
}

/**
 * Turns the saved settings, the photo counts and the last run's result into the single state the person should see.
 * Permission denied outranks everything (it can be the reason the toggle is off); then set-up and on/off; then a run's
 * problem or wait; then uploads still in flight; and last the resting state read from the backed-up count.
 */
export function backupStatus(i: StatusInput): BackupStatus {
  const { last, computer, backedUp, pending } = i;
  if (last?.kind === 'permissionDenied') {
    return make('permissionDenied', 'attention', { key: 'phBackupNeedsPhotoAccess' }, { key: 'backupStatusPhotoAccessOff' }, { key: 'backupStatusPhotoAccessDetail' }, 'openSettings');
  }
  if (!i.enabled) {
    return i.hostChosen
      ? make('off', 'neutral', { key: 'phBackupOff' }, { key: 'enableBackupFirst' })
      : make('notSetUp', 'neutral', { key: 'phPhotoBackupHint' }, { key: 'photoBackupSetupTitle' });
  }
  if (!i.hostChosen) return make('chooseComputer', 'attention', { key: 'phBackupAttentionChooseComputer' }, { key: 'chooseComputerToBackUpTo' });
  const problem = last ? fromResult(last, i) : null;
  if (problem) return problem;
  if (pending > 0) {
    const count = { count: pending };
    if (last?.kind === 'waiting') return make('stillUploading', 'neutral', { key: 'backupStillUploading', params: count }, { key: 'backupStillUploading', params: count }, { key: 'backupStatusRunningDetail' });
    return make('running', 'neutral', { key: 'backingUpPhotos', params: count }, { key: 'backingUpPhotos', params: count }, { key: 'backupStatusRunningDetail' });
  }
  if (backedUp === 0) return make('idleEmpty', 'neutral', { key: 'phBackupOnEmpty' }, { key: 'backupStatusOn' }, { key: 'backupStatusOnDetail', params: { computer } });
  return make('upToDate', 'ok', { key: 'phBackupOnCount', params: { count: backedUp } }, { key: 'alreadyUpToDate' }, { key: 'backupStatusUpToDateDetail', params: { count: backedUp, computer } });
}
