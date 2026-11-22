import { t } from '../../i18n';

/**
 * Why [name] cannot be the new name of an item, or null when it can. A rename takes a name, not a path: the agent
 * treats `a/b` as a move into a new folder, and `..` would climb out of the folder.
 */
export function invalidNameReason(name: string): string | null {
  if (name.trim() === '') return t('nameEmpty');
  if (name === '.' || name === '..') return t('nameNotUsable', { name });
  if (/[/\\]/.test(name)) return t('nameSeparator');
  if (/[\u0000-\u001f]/.test(name)) return t('nameControlChars');
  return null;
}
