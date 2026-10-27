import { useEffect, useState } from 'react';

import type { Host } from '../../core/models/host';
import { hostStore } from '../../services';

/** Every paired host, `undefined` until first read. */
export function useHosts(): Host[] | undefined {
  const [hosts, setHosts] = useState<Host[] | undefined>(undefined);
  useEffect(() => {
    let live = true;
    void hostStore.listHosts().then((l) => live && setHosts(l));
    return () => {
      live = false;
    };
  }, []);
  return hosts;
}
