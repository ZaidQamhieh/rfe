import type { EmojiName } from '../../design/components';
import { t } from '../../i18n';

export type ComputerMenuKey = 'trash' | 'apps' | 'phones' | 'about' | 'settings';

/** One line of the computer menu, or the rule that separates the computer's Settings from its other entries. */
export type ComputerMenuEntry = { key: ComputerMenuKey; label: string; emoji: EmojiName; disabled?: boolean } | { key: 'rule' };

/** A navigation target: `navigate` jumps to an existing tab, `push` opens a screen on top. */
export type ComputerMenuTarget = { method: 'push' | 'navigate'; pathname: string; params?: { id: string } };

/**
 * The computer menu in order: Trash, Apps on the computer, Phones with access, About this computer, a rule, then
 * Settings. Apps is greyed out while the computer cannot be reached. Phones with access lists the computer's other
 * phones, so it is left out unless [showPhones] says this phone is an owner (it defaults to shown for older callers).
 */
export function computerMenuEntries(hostName: string, appsAvailable: boolean, showPhones = true): ComputerMenuEntry[] {
  const entries: ComputerMenuEntry[] = [
    { key: 'trash', label: t('trashTitle'), emoji: 'trash' },
    { key: 'apps', label: t('computerMenuApps', { name: hostName }), emoji: 'apps', disabled: !appsAvailable },
    { key: 'phones', label: t('computerMenuPhones'), emoji: 'phone' },
    { key: 'about', label: t('computerMenuAbout'), emoji: 'info' },
    { key: 'rule' },
    { key: 'settings', label: t('settingsMenuItem'), emoji: 'gear' },
  ];
  return showPhones ? entries : entries.filter((e) => e.key !== 'phones');
}

/** Where a menu entry goes for the computer with [hostId]. */
export function computerMenuTarget(key: ComputerMenuKey, hostId: string): ComputerMenuTarget {
  switch (key) {
    case 'trash':
      return { method: 'push', pathname: '/trash' };
    case 'apps':
      return { method: 'push', pathname: '/host/[id]/apps', params: { id: hostId } };
    case 'phones':
      return { method: 'push', pathname: '/host/[id]/phones', params: { id: hostId } };
    case 'about':
      return { method: 'push', pathname: '/host/[id]/about', params: { id: hostId } };
    case 'settings':
      return { method: 'push', pathname: '/host/[id]/settings', params: { id: hostId } };
  }
}
