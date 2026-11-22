import type { Drive } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { formatSize } from '../../core/format';
import { t } from '../../i18n';
import { folderLabel } from '../explorer/paths';
import type { RootResolution } from '../explorer/rootResolution';
import { seenWhen } from './relative';

/** Which lamp the nameplate shows: connected, hollow (off), waiting or stopped. */
export type Lamp = 'go' | 'off' | 'wait' | 'stop';

/**
 * The lamp and the short phrase under a computer's name, in plain words (no addresses or ports): "Connected", or the
 * phrase for a computer that is not answering.
 */
export function stateLine(o: { online: boolean; checking: boolean; untrusted: boolean; refused?: boolean; lastSeen: Date | null }, host: Host, now: Date = new Date()): { lamp: Lamp; phrase: string } {
  if (o.checking) return { lamp: 'wait', phrase: t('checkingStatus') };
  if (o.untrusted) return { lamp: 'stop', phrase: t('statusUntrusted') };
  if (o.refused) return { lamp: 'stop', phrase: t('statusRefused') };
  if (o.online) return { lamp: 'go', phrase: t('stateConnected') };
  return { lamp: 'off', phrase: offlinePhrase(o, host, now) };
}

/**
 * The phrase for a computer that is not answering: "Not running · seen 22:41 yesterday" when its network-card address is
 * known (so it can be woken), otherwise "Not reachable".
 */
function offlinePhrase(o: { lastSeen: Date | null }, host: Host, now: Date = new Date()): string {
  if (!host.macAddress) return t('switcherNotReachable');
  return o.lastSeen ? t('switcherAsleep', { relative: seenWhen(o.lastSeen, now) }) : t('switcherAsleepNoTime');
}

/** A place to start browsing: a shared folder, a drive, or the whole computer. */
export type Place = { key: string; kind: 'folder' | 'drive'; title: string; subtitle: string; path: string };

/**
 * The Places rows for what the computer lets this phone browse. Drives need their list loaded; with none yet the result
 * is empty and the screen shows its generic Files row instead.
 */
export function placesFor(res: RootResolution, drives: readonly Drive[] | null): Place[] {
  if (res.kind === 'denied') return [];
  if (res.kind === 'roots') {
    return [...res.roots]
      .sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase()))
      .map((r) => ({ key: r, kind: 'folder', title: folderLabel(r), subtitle: t('placeSharedFolder'), path: r }));
  }
  if (res.kind === 'select') return [{ key: res.rootPath, kind: 'folder', title: t('placeAllFiles'), subtitle: t('placeWholeComputer'), path: res.rootPath }];
  return (drives ?? []).map((d) => ({
    key: d.path,
    kind: 'drive',
    title: d.label ? d.label : folderLabel(d.path),
    subtitle: d.totalBytes != null && d.freeBytes != null ? t('placeDriveFree', { free: formatSize(d.freeBytes), total: formatSize(d.totalBytes) }) : t('placeDrive'),
    path: d.path,
  }));
}

/** "Home › Documents": the last two folders above [path], or the folder's own name for one at the top. */
export function pathCrumb(path: string): string {
  const parts = path.split(/[/\\]/).filter(Boolean);
  const parents = parts.slice(0, -1).slice(-2);
  return (parents.length ? parents : parts.slice(-1)).join(' › ');
}
