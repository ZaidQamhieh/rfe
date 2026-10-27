import { useEffect, useMemo } from 'react';
import { useStore } from 'zustand';

import type { AgentClient } from '../../core/api/agentClient';
import type { Host } from '../../core/models/host';
import { humanizeError } from '../pairing/pairingService';
import { clientForHost, listingCache } from '../../services';
import { useResolvedView, useResolvedVisibility } from '../../state/settings';
import { createExplorer, deriveDisplay, type Explorer, type ExplorerState } from './explorerStore';
import type { StoreApi } from 'zustand/vanilla';

type Handle = StoreApi<ExplorerState> & Explorer;
const registry = new Map<string, Handle>();

/** One explorer per (host, root); kept alive while its screen is mounted and reused on remount so navigation state survives tab switches. */
export function explorerFor(host: Host, rootPath: string, getClient: () => Promise<AgentClient>): Handle {
  const key = `${host.id}\u0000${rootPath}`;
  let h = registry.get(key);
  if (!h) {
    h = createExplorer({ hostId: host.id, rootPath, getClient, cache: listingCache, humanize: humanizeError });
    registry.set(key, h);
  }
  return h;
}

export function dropExplorer(hostId: string, rootPath: string) {
  registry.delete(`${hostId}\u0000${rootPath}`);
}

/** Subscribes a screen to an explorer plus its resolved view/visibility settings and derived display lists. */
export function useExplorer(host: Host, rootPath: string) {
  const ex = useMemo(() => explorerFor(host, rootPath, () => clientForHost(host)), [host, rootPath]);
  const state = useStore(ex);
  const view = useResolvedView(host.id);
  const vis = useResolvedVisibility(host.id);
  const derived = useMemo(() => deriveDisplay(state, view.sort, vis), [state.entries, state.showHidden, view.sort, vis]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (ex.getState().entries.length === 0 && !ex.getState().loading) void ex.load();
  }, [ex]);
  return { ex, state, view, ...derived };
}
