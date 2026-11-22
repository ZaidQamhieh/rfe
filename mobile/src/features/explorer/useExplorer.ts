import { useEffect, useMemo } from 'react';
import { useStore } from 'zustand';

import type { Host } from '../../core/models/host';
import { humanizeError } from '../../core/humanizeError';
import { connectAgentEvents } from '../../core/agentEvents';
import { eventStreamNative } from '../../core/native';
import { clientForHost, listingCache } from '../../services';
import { useResolvedView, useResolvedVisibility, useSettings } from '../../state/settings';
import { createExplorer, deriveDisplay, type Explorer, type ExplorerState } from './explorerStore';
import type { StoreApi } from 'zustand/vanilla';

/** How long after the last change hint the open folder is refetched. */
const EVENT_REFRESH_DEBOUNCE_MS = 400;

type Handle = StoreApi<ExplorerState> & Explorer;
const registry = new Map<string, Handle>();

/** One explorer per (host, root); kept alive while its screen is mounted and reused on remount so navigation state survives tab switches. */
export function explorerFor(host: Host, rootPath: string): Handle {
  const key = `${host.id}\u0000${rootPath}`;
  let h = registry.get(key);
  if (!h) {
    h = createExplorer({ hostId: host.id, rootPath, getClient: () => clientForHost(host), cache: listingCache, humanize: humanizeError });
    registry.set(key, h);
  }
  return h;
}

/** Subscribes a screen to an explorer plus its resolved view/visibility settings and derived display lists. */
export function useExplorer(host: Host, rootPath: string) {
  const ex = useMemo(() => explorerFor(host, rootPath), [host, rootPath]);
  const state = useStore(ex);
  const view = useResolvedView(host.id);
  const vis = useResolvedVisibility(host.id);
  const foldersFirst = useSettings((x) => x.state.app.foldersFirst);
  const derived = useMemo(() => deriveDisplay(state, view.sort, vis, foldersFirst), [state.entries, state.showHidden, view.sort, vis, foldersFirst]); // eslint-disable-line react-hooks/exhaustive-deps
  // The agent says when files changed (an upload finished, another phone renamed something), so the list refetches by itself.
  useEffect(() => {
    let live = true;
    let stop: (() => void) | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    void clientForHost(host)
      .then((cl) => {
        if (!live) return;
        let first = true;
        stop = connectAgentEvents(eventStreamNative, () => cl.eventsSpec(), (kind) => {
          // "hello" opens every connection; only a reconnect (not the first) needs a refresh on top of the initial load.
          // Unless that load failed or only the cache is showing: then the first hello means the agent is reachable now.
          if (kind === 'hello' && first) {
            first = false;
            const s = ex.getState();
            if (!s.error && !s.offline && !s.stale) return;
          }
          if (kind !== 'files' && kind !== 'hello') return;
          // A burst of changes (a batch upload) is one refetch, shortly after the last hint.
          if (timer) clearTimeout(timer);
          timer = setTimeout(() => {
            const s = ex.getState();
            // A failed or cache-only list needs the full load (spinner, errors); a fresh one is patched quietly.
            void (s.error || s.offline || s.stale ? ex.load() : ex.quietRefresh());
          }, EVENT_REFRESH_DEBOUNCE_MS);
        }, undefined, () => {
          // The agent's certificate no longer matches the pin: reload so the list call surfaces the identity-changed state.
          if (live) void ex.load();
        });
      })
      .catch(() => {});
    return () => {
      live = false;
      if (timer) clearTimeout(timer);
      stop?.();
    };
  }, [host, ex]);
  useEffect(() => {
    if (ex.getState().entries.length === 0 && !ex.getState().loading) void ex.load();
  }, [ex]);
  return { ex, state, view, ...derived };
}
