import { FILE_CAPABILITIES, hasAppCapabilities, hasFileCapabilities, type AgentStatus, type Device, type FileCapability } from '../../core/api/models';

export type PermissionKey = FileCapability | 'viewApps' | 'launchApps';
export const PERMISSION_KEYS: PermissionKey[] = [...FILE_CAPABILITIES, 'viewApps', 'launchApps'];

/** `allowed` is null when the host did not say (an older agent). `note` explains a grant the host overrides or cannot honour. */
/** The apps endpoint's own answer for this phone (`null` when it could not be asked). */
export type AppsProbe = { view: boolean; launch: boolean } | null;

export type PermissionRow = { key: PermissionKey; allowed: boolean | null; note?: string };

export const PERMISSION_COPY: Record<PermissionKey, { title: string; subtitle: string }> = {
  browse: { title: 'Browse files', subtitle: 'View folders and file details' },
  download: { title: 'Download files', subtitle: 'Copy files from the computer to this phone' },
  upload: { title: 'Upload files', subtitle: 'Send files to the computer, including replacing existing ones' },
  modify: { title: 'Modify files', subtitle: 'Rename, move, copy and edit' },
  delete: { title: 'Delete files', subtitle: 'Remove files and folders' },
  share: { title: 'Create share links', subtitle: 'Make links others can open' },
  viewApps: { title: 'View apps', subtitle: 'See the computer’s app list' },
  launchApps: { title: 'Launch apps', subtitle: 'Start apps on the computer' },
};

const WRITES: FileCapability[] = ['upload', 'modify', 'delete'];

/**
 * What this phone may do on the host, from real data only: the host's own answer for this device (`status.fileCapabilities`)
 * when it gave one, else the device record (a password-account device is an owner and bypasses file grants). The host's
 * read-only mode, the share-links switch and a folder lock-out then override a grant, with the reason as a note.
 */
export function buildPermissions(status: Pick<AgentStatus, 'fileCapabilities' | 'readOnly' | 'allowSharing' | 'accessDenied'>, device: Device | undefined, probe: AppsProbe = null): PermissionRow[] {
  const grants: Partial<Record<FileCapability, boolean>> | undefined =
    status.fileCapabilities ?? (device?.viaLogin ? Object.fromEntries(FILE_CAPABILITIES.map((c) => [c, true])) : device && hasFileCapabilities(device) ? Object.fromEntries(FILE_CAPABILITIES.map((c) => [c, device[c] === true])) : undefined);
  const rows: PermissionRow[] = FILE_CAPABILITIES.map((key): PermissionRow => {
    const granted = grants ? grants[key] === true : null;
    if (granted === true && status.accessDenied) return { key, allowed: false, note: 'No folder access for this phone' };
    if (granted === true && status.readOnly && WRITES.includes(key)) return { key, allowed: false, note: 'This computer is in read-only mode' };
    if (granted === true && key === 'share' && !status.allowSharing) return { key, allowed: false, note: 'Share links are turned off on this computer' };
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
