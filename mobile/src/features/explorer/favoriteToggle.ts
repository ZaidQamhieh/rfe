import type { useToast } from '../../design/components';
import { t } from '../../i18n';
import { folderLabel } from './paths';

/** Toggles the favourite on [path] and confirms with a toast naming [name]; [isFav] is the state before the toggle. */
export async function toggleFavoriteToast(
  toggle: (o: { hostId: string; path: string; label: string }) => Promise<unknown>,
  toast: ReturnType<typeof useToast>,
  o: { hostId: string; path: string; name: string; isFav: boolean },
) {
  await toggle({ hostId: o.hostId, path: o.path, label: folderLabel(o.path) });
  if (o.isFav) toast.info(t('removedFavorite', { name: o.name }));
  else toast.success(t('addedFavorite', { name: o.name }));
}
