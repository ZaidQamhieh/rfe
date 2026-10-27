import { useFocusEffect } from 'expo-router';
import { useCallback, useState } from 'react';

import type { Host } from '../../core/models/host';
import { humanizeError } from '../pairing/pairingService';
import { ensureLegacyImport, hostStore, keyValue } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { pickHomeHost, readLastHostId, writeLastHostId } from './activeSelection';

/** Makes [host] the computer Home, Files, Apps and Activity work on, and remembers it for the next launch. */
export function activateHost(host: Host) {
  useActiveHost.getState().setActive({ host, health: null });
  void writeLastHostId(keyValue, host.id);
}

/** Every paired host (reloaded whenever the screen gains focus) and which one is selected. `hosts` is null until first read. */
export function useWorkspaces() {
  const [hosts, setHosts] = useState<Host[] | null>(null);
  const [lastId, setLastId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const activeId = useActiveHost((s) => s.active?.host.id ?? null);

  const reload = useCallback(async () => {
    try {
      await ensureLegacyImport();
      const [list, last] = await Promise.all([hostStore.listHosts(), readLastHostId(keyValue)]);
      setHosts(list);
      setLastId(last);
      setError(null);
    } catch (e) {
      setError(humanizeError(e));
    }
  }, []);
  useFocusEffect(
    useCallback(() => {
      void reload();
    }, [reload]),
  );

  const selected = hosts ? pickHomeHost(hosts, activeId, lastId) : null;
  return { hosts, selected, error, reload };
}
