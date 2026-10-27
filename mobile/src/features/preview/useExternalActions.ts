import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { useToast } from '../../design/components';
import { t } from '../../i18n';
import { openEntryWith, shareEntry } from './externalFiles';

/** Share and "Open with" for one host's files, with the Flutter app's progress and failure toasts. */
export function useExternalActions(host: Host) {
  const toast = useToast();
  const run = async (entry: Entry, preparing: string, failed: string, action: (h: Host, e: Entry) => Promise<boolean>) => {
    toast.info(preparing);
    try {
      if (!(await action(host, entry))) toast.error(failed);
    } catch {
      toast.error(failed);
    }
  };
  return {
    share: (entry: Entry) => run(entry, t('preparingToShare', { name: entry.name }), t('couldNotShare', { name: entry.name }), shareEntry),
    openWith: (entry: Entry) => run(entry, t('preparingToOpen', { name: entry.name }), t('couldNotOpen', { name: entry.name }), openEntryWith),
  };
}
