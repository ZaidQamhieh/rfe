import { useEffect, useState } from 'react';

import type { Host } from '../../core/models/host';
import { hostStore } from '../../services';

/** The paired host for a route id: `undefined` while loading, `null` when it is not (or no longer) paired. */
export function useHostById(id: string | undefined): Host | null | undefined {
  const [host, setHost] = useState<Host | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    void hostStore.listHosts().then((l) => live && setHost(l.find((h) => h.id === id) ?? null));
    return () => {
      live = false;
    };
  }, [id]);
  return host;
}
