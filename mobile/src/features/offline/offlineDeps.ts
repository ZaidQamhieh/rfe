import { offlineBodies } from '../../core/native';
import { isPinned, useCollections } from '../../state/collections';
import type { OfflineDeps } from './offlineBodies';

/** The real vault and pin state behind the offline logic. */
export const offlineDeps: OfflineDeps = {
  isPinnedFolder: (hostId, folderPath) => isPinned(useCollections.getState().pins, hostId, folderPath),
  vault: offlineBodies,
};
