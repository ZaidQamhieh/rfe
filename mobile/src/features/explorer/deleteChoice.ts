import type { useDialogs } from '../../design/components';
import { t } from '../../i18n';

/** The delete chooser: move to trash or delete forever; resolves to null when dismissed. */
export function chooseDeleteMode(dialogs: ReturnType<typeof useDialogs>, o: { subtitle: string; tint: string }) {
  return dialogs.choose<'trash' | 'forever'>({
    title: t('deleteTitle'),
    subtitle: o.subtitle,
    tint: o.tint,
    options: [
      { value: 'trash', label: t('moveToTrashButton') },
      { value: 'forever', label: t('deleteForeverButton'), tint: o.tint },
    ],
  });
}
