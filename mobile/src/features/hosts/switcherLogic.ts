import type { Host } from '../../core/models/host';
import { t } from '../../i18n';
import { stateLine } from './nameplateLogic';
import { seenWhen } from './relative';

/** What the computer switcher shows for one computer. */
type SwitcherKind = 'checking' | 'online' | 'asleep' | 'unreachable' | 'untrusted' | 'refused';

/** The short button at the right of a switcher row: wake a sleeping computer, review a changed identity, pair again after a removal, or none. */
type SwitcherAction = 'wake' | 'review' | 'pair' | null;

export type SwitcherRowState = {
  kind: SwitcherKind;
  /** The state phrase under the name, in plain words. */
  phrase: string;
  /** The picture is drawn grey when the computer cannot be used right now. */
  off: boolean;
  action: SwitcherAction;
};

/**
 * Works out one switcher row from the live status of a computer. Online and checking reuse the nameplate wording; a
 * changed identity offers Review; an offline computer counts as asleep only when its network-card address is known
 * (so Wake can reach it), otherwise it is just not reachable.
 */
export function switcherRowState(o: { online: boolean; checking: boolean; untrusted: boolean; refused?: boolean; activeAddress: string | null; lastSeen: Date | null }, host: Host, now: Date = new Date()): SwitcherRowState {
  if (o.checking) return { kind: 'checking', phrase: stateLine(o, host, now).phrase, off: false, action: null };
  if (o.untrusted) return { kind: 'untrusted', phrase: t('switcherIdentityChanged'), off: true, action: 'review' };
  if (o.refused) return { kind: 'refused', phrase: stateLine(o, host, now).phrase, off: false, action: 'pair' };
  if (o.online) return { kind: 'online', phrase: stateLine(o, host, now).phrase, off: false, action: null };
  if (host.macAddress) {
    return { kind: 'asleep', phrase: o.lastSeen ? t('switcherAsleep', { relative: seenWhen(o.lastSeen, now) }) : t('switcherAsleepNoTime'), off: true, action: 'wake' };
  }
  return { kind: 'unreachable', phrase: t('switcherNotReachable'), off: true, action: null };
}

/**
 * Which "computer is not answering" panel Home shows above its places: "asleep" when it can be woken (its network-card
 * address is known), "lost" when it is simply unreachable, "refused" when it removed this phone's access (shown whether or
 * not it answers), and null while checking, online, or when the identity changed (that has its own Review flow).
 */
/**
 * The address "Pair again" opens the add-computer screen with: the address that answered this phone last, otherwise the
 * saved one. The screen's own Technical details row then shows it, so the user does not retype address:port.
 */
export function pairAgainAddress(o: { activeAddress: string | null }, host: Host): string {
  return o.activeAddress ?? host.address;
}

export function homeStateView(o: { online: boolean; checking: boolean; untrusted: boolean; refused?: boolean; activeAddress: string | null; lastSeen: Date | null }, host: Host): 'asleep' | 'lost' | 'refused' | null {
  const kind = switcherRowState(o, host).kind;
  return kind === 'asleep' ? 'asleep' : kind === 'unreachable' ? 'lost' : kind === 'refused' ? 'refused' : null;
}
