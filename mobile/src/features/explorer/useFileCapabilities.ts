import { useEffect, useState } from 'react';

import type { FileCapability } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { clientForHost } from '../../services';

type Caps = Record<FileCapability, boolean> | undefined;

// The last answer per host, so sheets opened later render the right actions at once. A failed or
// older-agent answer is `undefined`: nothing is hidden and the agent still refuses what is not allowed.
const cache = new Map<string, Caps>();

/** What this device may do with files on [host] (undefined until known). */
export function useFileCapabilities(host: Host): Caps {
  const [caps, setCaps] = useState<Caps>(() => cache.get(host.id));
  useEffect(() => {
    let live = true;
    void clientForHost(host)
      .then((c) => c.status())
      .then((s) => {
        cache.set(host.id, s.fileCapabilities);
        if (live) setCaps(s.fileCapabilities);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [host]);
  return caps;
}
