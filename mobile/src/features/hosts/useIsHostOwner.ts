import { useEffect, useState } from 'react';

import { canManageHost } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { clientForHost } from '../../services';

/**
 * Whether this phone is an owner of [host] (signed in with the computer's account), read from the computer's settings
 * while it is reachable. False until the computer has answered, so owner-only entries stay hidden rather than flash.
 */
export function useIsHostOwner(host: Host, online: boolean): boolean {
  const [owner, setOwner] = useState(false);
  useEffect(() => {
    if (!online) return;
    let live = true;
    void clientForHost(host)
      .then((client) => client.status())
      .then((status) => live && setOwner(canManageHost(status)))
      .catch(() => undefined); // Not reachable or refused: the entry stays hidden.
    return () => {
      live = false;
    };
  }, [host, online]);
  return owner;
}
