import { CopyPlus, RefreshCw, SkipForward, X } from 'lucide-react-native';
import { createElement } from 'react';

import { useDialogs } from '../../design/components';
import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { folderLabel } from './paths';

export type ConflictResolution = 'keepBoth' | 'overwrite' | 'skip' | 'cancel';

/** The name-conflict chooser shared by paste and upload flows; resolves to `cancel` when dismissed. */
export function useConflictPrompt() {
  const dialogs = useDialogs();
  const c = useScheme();
  return async function askConflict(colliding: number, total: number, dest: string): Promise<ConflictResolution> {
    const r = await dialogs.choose<ConflictResolution>({
      title: t('nameConflictTitle'),
      subtitle: t('nameConflictBody', { collidingCount: colliding, totalCount: total, dest: folderLabel(dest) }),
      tint: '#F3A73F',
      options: [
        { value: 'skip', label: t('skipTheseButton'), icon: createElement(SkipForward, { size: 20, color: c.onSurfaceVariant }) },
        { value: 'keepBoth', label: t('keepBothButton'), icon: createElement(CopyPlus, { size: 20, color: c.onSurfaceVariant }) },
        { value: 'overwrite', label: t('overwriteButton'), tint: c.error, icon: createElement(RefreshCw, { size: 20, color: c.error }) },
        { value: 'cancel', label: t('cancelButton'), icon: createElement(X, { size: 20, color: c.onSurfaceVariant }) },
      ],
    });
    return r ?? 'cancel';
  };
}
