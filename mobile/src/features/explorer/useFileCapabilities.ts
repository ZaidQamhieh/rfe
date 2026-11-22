import { useEffect, useState } from 'react';
import { AppState } from 'react-native';

import type { FileCapability } from '../../core/api/models';
import { ignoreError } from '../../core/log';
import type { Host } from '../../core/models/host';
import { clientForHost } from '../../services';

type Caps = Record<FileCapability, boolean> | undefined;

// The last answer per host, so sheets opened later render the right actions at once. A failed or
// older-agent answer is `undefined`: nothing is hidden and the agent still refuses what is not allowed.
const cache = new Map<string, Caps>();

const listeners = new Set<() => void>();

/** Asks every open screen to read its grants again: the PC owner may have changed them since they were first read. */
export function refreshFileCapabilities(): void {
  listeners.forEach((l) => l());
}

/** What this device may do with files on [host] (undefined until known). Read again on a refresh and when the app returns to the foreground. */
export function useFileCapabilities(host: Host): Caps {
  const [caps, setCaps] = useState<Caps>(() => cache.get(host.id));
  useEffect(() => {
    let live = true;
    const read = () => {
      void clientForHost(host)
        .then((c) => c.status())
        .then((s) => {
          cache.set(host.id, s.fileCapabilities);
          if (live) setCaps(s.fileCapabilities);
        })
        .catch(ignoreError('file capabilities'));
    };
    read();
    listeners.add(read);
    const sub = AppState.addEventListener('change', (st) => {
      if (st === 'active') read();
    });
    return () => {
      live = false;
      listeners.delete(read);
      sub.remove();
    };
  }, [host]);
  return caps;
}
