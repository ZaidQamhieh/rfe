import { FILE_CAPABILITIES, hasAppCapabilities, hasFileCapabilities, type AgentStatus, type Device, type FileCapability } from '../../core/api/models';
import { t } from '../../i18n';

export type PermissionKey = FileCapability | 'viewApps' | 'launchApps';
/** The apps endpoint's own answer for this phone (`null` when it could not be asked). */
export type AppsProbe = { view: boolean; launch: boolean } | null;

/** `allowed` is null when the host did not say (an older agent). `note` explains a grant the host overrides or cannot honour. */
export type PermissionRow = { key: PermissionKey; allowed: boolean | null; note?: string };

/** Resolved at module load: the app has one language (en.json), so t() cannot change at runtime. */
export const PERMISSION_COPY: Record<PermissionKey, { title: string; subtitle: string }> = {
  browse: { title: t('trustPermBrowseTitle'), subtitle: t('trustPermBrowseSubtitle') },
  download: { title: t('trustPermDownloadTitle'), subtitle: t('trustPermDownloadSubtitle') },
  upload: { title: t('trustPermUploadTitle'), subtitle: t('trustPermUploadSubtitle') },
  modify: { title: t('trustPermModifyTitle'), subtitle: t('trustPermModifySubtitle') },
  delete: { title: t('trustPermDeleteTitle'), subtitle: t('trustPermDeleteSubtitle') },
  share: { title: t('trustPermShareTitle'), subtitle: t('trustPermShareSubtitle') },
  viewApps: { title: t('trustPermViewAppsTitle'), subtitle: t('trustPermViewAppsSubtitle') },
  launchApps: { title: t('launchAppsLabel'), subtitle: t('trustPermLaunchAppsSubtitle') },
};

const WRITES: FileCapability[] = ['upload', 'modify', 'delete'];

const grantsOf = (granted: (c: FileCapability) => boolean) => Object.fromEntries(FILE_CAPABILITIES.map((c) => [c, granted(c)]));

/**
 * What this phone may do on the host, from real data only: the host's own answer for this device (`status.fileCapabilities`)
 * when it gave one, else the device record (a password-account device is an owner and bypasses file grants). The host's
 * read-only mode, the share-links switch and a folder lock-out then override a grant, with the reason as a note.
 */
export function buildPermissions(status: Pick<AgentStatus, 'fileCapabilities' | 'readOnly' | 'allowSharing' | 'accessDenied'>, device: Device | undefined, probe: AppsProbe = null): PermissionRow[] {
  const grants: Partial<Record<FileCapability, boolean>> | undefined =
    status.fileCapabilities ?? (device?.viaLogin ? grantsOf(() => true) : device && hasFileCapabilities(device) ? grantsOf((c) => device[c] === true) : undefined);
  const rows: PermissionRow[] = FILE_CAPABILITIES.map((key): PermissionRow => {
    const granted = grants ? grants[key] === true : null;
    if (granted === true && status.accessDenied) return { key, allowed: false, note: t('trustNoFolderAccess') };
    if (granted === true && status.readOnly && WRITES.includes(key)) return { key, allowed: false, note: t('trustReadOnlyNote') };
    if (granted === true && key === 'share' && !status.allowSharing) return { key, allowed: false, note: t('trustShareLinksOffNote') };
    return { key, allowed: granted };
  });
  const apps = device && hasAppCapabilities(device);
  const view = apps ? device.viewApps === true : probe ? probe.view : null;
  rows.push({ key: 'viewApps', allowed: view });
  rows.push({ key: 'launchApps', allowed: apps ? view === true && device.launchApps === true : probe ? probe.launch : null });
  return rows;
}

export type IdentityState = 'checking' | 'verified' | 'mismatch' | 'unpinned' | 'unreachable';

/** Whether the pinned identity was confirmed just now, from the pin in secure storage and the outcome of the status call. */
export function identityState(o: { pinned: boolean | null; outcome: 'loading' | 'ok' | 'pinMismatch' | 'failed' }): IdentityState {
  if (o.pinned === false) return 'unpinned';
  if (o.outcome === 'loading' || o.pinned === null) return 'checking';
  if (o.outcome === 'pinMismatch') return 'mismatch';
  return o.outcome === 'ok' ? 'verified' : 'unreachable';
}
