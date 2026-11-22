import { DEVICE_GRANTS, hasFileCapabilities, type Device, type DeviceGrant, type PairRequest } from '../../core/api/models';
import { t, type StringKey } from '../../i18n';
import { relativeLabel } from './relative';

/** The plain-words label of each thing a phone may be allowed to do. */
const PHONE_PERMISSION_KEY: Record<DeviceGrant, StringKey> = {
  browse: 'phonesSeeFiles',
  download: 'phonesSaveToPhone',
  upload: 'phonesSendFiles',
  modify: 'phonesRenameMove',
  delete: 'phonesDelete',
  share: 'phonesShareLinks',
  wake: 'phonesWakeComputer',
};

/** The toggles of a phone in display order, each with its current value (missing means off); Wake appears only when the computer reports that grant. */
export function phoneToggles(d: Device): { cap: DeviceGrant; label: string; on: boolean }[] {
  return DEVICE_GRANTS.filter((cap) => cap !== 'wake' || d.wake !== undefined).map((cap) => ({ cap, label: t(PHONE_PERMISSION_KEY[cap]), on: d[cap] === true }));
}

/** The line under a phone's name: its role, that it is blocked, or when it was last used. */
export function phoneSubtitle(d: Device, now: Date = new Date()): string {
  if (d.revoked) return t('phonesBlockedSub');
  if (d.viaLogin) return t('phonesOwner');
  if (d.current) return t('phonesThisPhoneDetail');
  return d.lastSeen > 0 ? t('phonesLastUsed', { relative: relativeLabel(new Date(d.lastSeen), now) }) : t('phonesNeverUsed');
}

/**
 * Whether the six toggles are shown for [d]: only an owner may change them, only for a phone that has grants of its own
 * (a signed-in owner phone has everything) and is not blocked.
 */
export function canEditPhone(d: Device, viewerIsOwner: boolean): boolean {
  return viewerIsOwner && hasFileCapabilities(d) && !d.viaLogin && !d.revoked;
}

/** Whether Block applies: a phone that is not this one and not already blocked. */
export function canBlockPhone(d: Device): boolean {
  return !d.current && !d.revoked;
}

/** How often the waiting-phones list is asked for again while the screen is open (the computer's own prompt lasts about two minutes). */
export const PENDING_POLL_MS = 5000;

/** The line under a waiting phone: that it is asking to come in, the number both screens show, and whom it would take over. */
export function pendingPhoneSubtitle(r: Pick<PairRequest, 'matchCode' | 'replaces'>): string {
  if (!r.matchCode) return t('phonesAskNoNumber');
  return r.replaces ? t('phonesAskReplaces', { label: r.replaces, code: r.matchCode }) : t('phonesAskNumber', { code: r.matchCode });
}

/** The waiting list without the request just answered. */
export const withoutRequest = (list: readonly PairRequest[], id: string): PairRequest[] => list.filter((r) => r.id !== id);

/** The folder a phone is kept inside, or plain words when it is not kept inside one. */
export const onlyInsideLabel = (d: Pick<Device, 'jailRoot'>): string => (d.jailRoot ? d.jailRoot : t('phonesOnlyInsideAnywhere'));

/** Whether [p] reads as a full path on the computer: Unix `/...`, a Windows drive `C:\` or `C:/`, or a network `\\share`. */
const isFullPath = (p: string): boolean => /^(\/|[A-Za-z]:[\\/]|\\\\)/.test(p);

/** The prompt's check on a folder typed for a phone: a message when it is not a full path, otherwise null. Blank is allowed (it clears). */
export const folderProblem = (value: string): string | null => (value.trim() === '' || isFullPath(value.trim()) ? null : t('phonesOnlyInsideBadPath'));
