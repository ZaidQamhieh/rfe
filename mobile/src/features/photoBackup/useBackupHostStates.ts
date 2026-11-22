import { useEffect, useState } from 'react';

import { CertPinMismatch } from '../../core/api/pin';
import type { Host } from '../../core/models/host';
import { clientForHost, hostStore } from '../../services';
import type { BackupHostState } from './photoBackupLogic';

/** How long one computer gets to answer the destination check before it counts as not connected. */
const HOST_CHECK_TIMEOUT_MS = 4_000;

/** Asks one computer for its health and reads whether it removed this phone; a failed check means "not connected". */
async function checkHost(host: Host): Promise<BackupHostState> {
  const refused = await hostStore.getRefused(host.id).catch(() => false);
  try {
    const client = await clientForHost(host, true, HOST_CHECK_TIMEOUT_MS);
    await client.health();
    return { id: host.id, online: true, untrusted: false, refused };
  } catch (e) {
    return { id: host.id, online: false, untrusted: e instanceof CertPinMismatch, refused };
  }
}

/** Checks every paired computer once, in parallel, through the pinned agent client; undefined until all have answered or timed out. */
export function useBackupHostStates(hosts: Host[] | undefined): BackupHostState[] | undefined {
  const [states, setStates] = useState<BackupHostState[] | undefined>(undefined);
  useEffect(() => {
    if (!hosts) return;
    let live = true;
    void Promise.all(hosts.map(checkHost)).then((s) => live && setStates(s));
    return () => {
      live = false;
    };
  }, [hosts]);
  return states;
}
