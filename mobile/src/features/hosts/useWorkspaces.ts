import { useFocusEffect } from 'expo-router';
import { useCallback, useState } from 'react';

import { humanizeError } from '../../core/humanizeError';
import { ignoreError } from '../../core/log';
import type { Host } from '../../core/models/host';
import { sendWakeOnLan } from '../../core/native';
import type { useToast } from '../../design/components';
import { t } from '../../i18n';
import { ensureLegacyImport, hostStore, keyValue } from '../../services';
import { useActiveHost } from '../../state/activeHost';
import { pickHomeHost, readLastHostId, writeLastHostId } from './activeSelection';

/** Makes [host] the computer Home, Files, Apps and Activity work on, and remembers it for the next launch. */
export function activateHost(host: Host) {
  useActiveHost.getState().setActive({ host, health: null });
  void writeLastHostId(keyValue, host.id);
}

/** Sends a Wake-on-LAN packet to [host] and says whether it went out. A host with no MAC address does nothing. */
export async function wakeHost(host: Host, toast: ReturnType<typeof useToast>) {
  if (!host.macAddress) return;
  const sent = await sendWakeOnLan(host.macAddress).catch((e: unknown) => {
    ignoreError('wake on LAN')(e);
    return false;
  });
  if (sent) toast.info(t('wolPacketSent', { hostname: host.label }));
  else toast.error(t('wolPacketFailed'));
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
