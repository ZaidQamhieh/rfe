import { useEffect, useState } from 'react';

import type { Entry } from '../../core/api/models';
import { offlineBodies } from '../../core/native';
import { listingCache } from '../../services';
import { useCollections } from '../../state/collections';
import { collectKeptFiles } from './keptFiles';

/**
 * The files of one computer that open without it (null while the first look is running). Re-reads when the kept
 * folders change; [enabled] false skips the work, for a computer that is answering.
 */
export function useKeptFiles(hostId: string, enabled: boolean): Entry[] | null {
  const pins = useCollections((s) => s.pins);
  const [files, setFiles] = useState<Entry[] | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    void collectKeptFiles(hostId, pins, { listing: (h, p) => listingCache.get(h, p), has: (h, p) => offlineBodies.has(h, p) }).then((l) => live && setFiles(l));
    return () => {
      live = false;
    };
  }, [hostId, pins, enabled]);
  return files;
}
