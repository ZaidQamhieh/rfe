import type { Device, FileCapability } from '../../core/api/models';
import { FILE_CAPABILITIES } from '../../core/api/models';
import { formatSize } from '../../core/format';
import { t } from '../../i18n';

/** Upload/download limit choices in bytes per second; 0 is unlimited. */
export const BANDWIDTH_PRESETS = [0, 1, 5, 10, 50].map((mb) => mb * 1024 * 1024);

export const bandwidthLabel = (bytesPerSec: number) => (bytesPerSec === 0 ? t('bandwidthUnlimited') : `${formatSize(bytesPerSec)}/s`);

/** Picker options: the presets, plus the host's current value first when it is not one of them. */
export const bandwidthOptions = (current: number) => (BANDWIDTH_PRESETS.includes(current) ? BANDWIDTH_PRESETS : [current, ...BANDWIDTH_PRESETS]);

/**
 * The app-access pair to send after a switch flips. Launching needs catalog access, so turning the catalog off
 * turns launching off in the same PATCH; turning launching on is ignored while the catalog is off.
 */
export function nextAppCapabilities(d: Pick<Device, 'viewApps' | 'launchApps'>, change: { viewApps?: boolean; launchApps?: boolean }): { viewApps: boolean; launchApps: boolean } {
  const viewApps = change.viewApps ?? d.viewApps ?? false;
  const currentLaunch = d.viewApps === true && d.launchApps === true;
  return { viewApps, launchApps: change.viewApps === false ? false : viewApps ? (change.launchApps ?? currentLaunch) : false };
}

/** All six file grants after flipping one; the host always receives the complete set. */
export function nextFileGrants(d: Pick<Device, FileCapability>, change: Partial<Record<FileCapability, boolean>>): Record<FileCapability, boolean> {
  return Object.fromEntries(FILE_CAPABILITIES.map((c) => [c, change[c] ?? d[c] ?? false])) as Record<FileCapability, boolean>;
}

/** Applies [patch] to the device with [id], leaving the rest of the list untouched. */
export const patchDevice = (list: Device[], id: string, patch: Partial<Device>) => list.map((d) => (d.id === id ? { ...d, ...patch } : d));

export const FILE_CAPABILITY_LABEL: Record<FileCapability, string> = {
  browse: 'Browse files',
  download: 'Download files',
  upload: 'Upload files',
  modify: 'Modify files',
  delete: 'Delete files',
  share: 'Create share links',
};

/** The audit-trail actions the app knows; anything newer falls back to its raw name. */
export const AUDIT_LABEL: Record<string, Parameters<typeof t>[0]> = {
  pair: 'auditPair',
  register: 'auditRegister',
  login: 'auditLogin',
  login_failed: 'auditLoginFailed',
  device_revoked: 'auditDeviceRevoked',
  device_removed: 'auditDeviceRemoved',
  device_updated: 'auditDeviceUpdated',
  share_created: 'auditShareCreated',
  share_revoked: 'auditShareRevoked',
  agent_restart: 'auditAgentRestart',
};
export const auditLabel = (action: string) => (AUDIT_LABEL[action] ? t(AUDIT_LABEL[action]) : action);
