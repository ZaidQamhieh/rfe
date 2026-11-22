import { t } from '../../i18n';

/** The success toast after a delete: says the file was deleted for good, or moved to Trash. */
export function deleteDoneMessage(permanent: boolean, name: string): string {
  return permanent ? t('deletedName', { name }) : t('movedToTrashName', { name });
}
