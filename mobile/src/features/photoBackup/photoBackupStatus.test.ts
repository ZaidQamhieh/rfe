import { backupStatus, type StatusInput } from './photoBackupStatus';

const base: StatusInput = { hostChosen: true, enabled: true, computer: 'Desk', backedUp: 0, pending: 0, last: null };
const status = (over: Partial<StatusInput>) => backupStatus({ ...base, ...over });

describe('backupStatus: one state per situation, each with its own text keys', () => {
  it('never set up: the hint, and the set-up action lives on the set-up card', () => {
    const s = status({ hostChosen: false, enabled: false });
    expect(s.state).toBe('notSetUp');
    expect(s.row).toEqual({ key: 'phPhotoBackupHint' });
    expect(s.headline).toEqual({ key: 'photoBackupSetupTitle' });
    expect(s.action).toBeNull();
  });

  it('off after set-up: row says Off, the helper text is the enable-first sentence', () => {
    const s = status({ enabled: false });
    expect(s.state).toBe('off');
    expect(s.row).toEqual({ key: 'phBackupOff' });
    expect(s.headline).toEqual({ key: 'enableBackupFirst' });
    expect(s.action).toBeNull();
  });

  it('on, nothing backed up yet', () => {
    const s = status({});
    expect(s.state).toBe('idleEmpty');
    expect(s.row).toEqual({ key: 'phBackupOnEmpty' });
    expect(s.headline).toEqual({ key: 'backupStatusOn' });
    expect(s.detail).toEqual({ key: 'backupStatusOnDetail', params: { computer: 'Desk' } });
    expect(s.tone).toBe('neutral');
  });

  it('on and up to date: the count and the computer', () => {
    const s = status({ backedUp: 12 });
    expect(s.state).toBe('upToDate');
    expect(s.row).toEqual({ key: 'phBackupOnCount', params: { count: 12 } });
    expect(s.headline).toEqual({ key: 'alreadyUpToDate' });
    expect(s.detail).toEqual({ key: 'backupStatusUpToDateDetail', params: { count: 12, computer: 'Desk' } });
    expect(s.tone).toBe('ok');
  });

  it('running: the pending count, and the leave-this-screen reassurance', () => {
    const s = status({ pending: 4, last: { kind: 'enqueued', count: 4 } });
    expect(s.state).toBe('running');
    expect(s.row).toEqual({ key: 'backingUpPhotos', params: { count: 4 } });
    expect(s.headline).toEqual({ key: 'backingUpPhotos', params: { count: 4 } });
    expect(s.detail).toEqual({ key: 'backupStatusRunningDetail' });
    expect(s.action).toBeNull();
  });

  it('running ends when nothing is pending any more', () => {
    expect(status({ pending: 0, backedUp: 4, last: { kind: 'enqueued', count: 4 } }).state).toBe('upToDate');
  });

  it('still uploading', () => {
    const s = status({ pending: 2, last: { kind: 'waiting', count: 2 } });
    expect(s.state).toBe('stillUploading');
    expect(s.row).toEqual({ key: 'backupStillUploading', params: { count: 2 } });
    expect(s.headline).toEqual({ key: 'backupStillUploading', params: { count: 2 } });
  });

  it('paused for Wi-Fi', () => {
    const s = status({ last: { kind: 'skipped', reason: 'wifi' } });
    expect(s.state).toBe('pausedWifi');
    expect(s.row).toEqual({ key: 'phBackupPausedWifi' });
    expect(s.headline).toEqual({ key: 'backupWaitingWifi' });
    expect(s.detail).toEqual({ key: 'backupStatusWifiDetail' });
    expect(s.action).toBeNull();
  });

  it('paused for charging', () => {
    const s = status({ last: { kind: 'skipped', reason: 'charging' } });
    expect(s.state).toBe('pausedCharging');
    expect(s.row).toEqual({ key: 'phBackupPausedCharging' });
    expect(s.headline).toEqual({ key: 'backupWaitingCharging' });
    expect(s.detail).toEqual({ key: 'backupStatusChargingDetail' });
  });

  it('computer unreachable names the computer that was tried and offers Try again', () => {
    const s = status({ last: { kind: 'skipped', reason: 'unreachable', host: 'Laptop' } });
    expect(s.state).toBe('unreachable');
    expect(s.tone).toBe('attention');
    expect(s.row).toEqual({ key: 'phBackupAttentionUnreachable', params: { computer: 'Laptop' } });
    expect(s.headline).toEqual({ key: 'backupStatusUnreachable', params: { computer: 'Laptop' } });
    expect(s.detail).toEqual({ key: 'backupStatusUnreachableDetail' });
    expect(s.action).toBe('tryAgain');
  });

  it('photos not accepted: the existing sentence, and the reset action', () => {
    const s = status({ last: { kind: 'refused', count: 3, reason: 'Uploads are off.' } });
    expect(s.state).toBe('notAccepted');
    expect(s.row).toEqual({ key: 'phBackupAttentionRefused', params: { count: 3 } });
    expect(s.headline).toEqual({ key: 'backupRefused', params: { count: 3, reason: 'Uploads are off.' } });
    expect(s.action).toBe('resetRecord');
  });

  it('photos that could not be prepared', () => {
    const s = status({ last: { kind: 'incomplete', count: 5 } });
    expect(s.state).toBe('couldNotPrepare');
    expect(s.row).toEqual({ key: 'phBackupAttentionIncomplete', params: { count: 5 } });
    expect(s.headline).toEqual({ key: 'backupIncomplete', params: { count: 5 } });
    expect(s.action).toBe('tryAgain');
  });

  it('computer has no backup folder yet', () => {
    const s = status({ last: { kind: 'serverNotConfigured' } });
    expect(s.state).toBe('noDestination');
    expect(s.row).toEqual({ key: 'phBackupAttentionNoDestination' });
    expect(s.headline).toEqual({ key: 'serverDestNotConfigured' });
    expect(s.action).toBeNull();
  });

  it('permission denied wins over everything and offers Open settings, even when the toggle is off', () => {
    const s = status({ enabled: false, last: { kind: 'permissionDenied' } });
    expect(s.state).toBe('permissionDenied');
    expect(s.row).toEqual({ key: 'phBackupNeedsPhotoAccess' });
    expect(s.headline).toEqual({ key: 'backupStatusPhotoAccessOff' });
    expect(s.detail).toEqual({ key: 'backupStatusPhotoAccessDetail' });
    expect(s.action).toBe('openSettings');
  });

  it('phone removed by the computer', () => {
    const s = status({ last: { kind: 'skipped', reason: 'refused' } });
    expect(s.state).toBe('phoneRemoved');
    expect(s.row).toEqual({ key: 'phBackupAttentionRepair' });
    expect(s.headline).toEqual({ key: 'phoneRemovedMessage' });
  });

  it('on but no computer chosen', () => {
    const s = status({ hostChosen: false });
    expect(s.state).toBe('chooseComputer');
    expect(s.headline).toEqual({ key: 'chooseComputerToBackUpTo' });
  });

  it('albums gone and policy blocked keep their existing sentences', () => {
    expect(status({ last: { kind: 'albumsGone' } }).headline).toEqual({ key: 'backupAlbumsGone' });
    const p = status({ last: { kind: 'skipped', reason: 'policy', host: 'Laptop' } });
    expect(p.state).toBe('policyBlocked');
    expect(p.headline).toEqual({ key: 'backupPolicyBlocked', params: { host: 'Laptop' } });
  });
});
