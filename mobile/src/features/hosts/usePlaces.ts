import { useEffect, useState } from 'react';

import type { Drive, Health } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { ignoreError } from '../../core/log';
import { clientForHost, keyValue } from '../../services';
import { resolveRoots, type RootResolution } from '../explorer/rootResolution';
import { loadRoots, saveRoots } from '../explorer/rootsCache';
import { placesFor, type Place } from './nameplateLogic';

/**
 * The places this phone may start browsing on [host]: its shared folders or drives. Asks the computer when it is online
 * and otherwise (or if asking fails) falls back to what it last allowed, so the list survives being offline. Null until
 * known; an empty list means there is nothing to list (denied, or drives not loaded yet).
 */
export function usePlaces(host: Host, health: Health | null, online: boolean, drives: Drive[] | null): Place[] | null {
  const [res, setRes] = useState<{ res: RootResolution; drives: Drive[] | null } | null>(null);
  const os = health?.os;
  useEffect(() => {
    let live = true;
    (async () => {
      if (online) {
        try {
          const status = await (await clientForHost(host)).status();
          if (!status.accessDenied) await saveRoots(keyValue, host.id, { roots: status.roots, os, drives: drives ?? undefined }).catch(ignoreError('save roots'));
          return { res: resolveRoots(status, os ? { os } : null), drives };
        } catch (e) {
          // Logged, then the cached answer below stands in.
          ignoreError('load places')(e);
        }
      }
      const cached = await loadRoots(keyValue, host.id);
      if (!cached) return null;
      return { res: resolveRoots({ roots: cached.roots, accessDenied: false }, os ? { os } : cached.os ? { os: cached.os } : null), drives: drives ?? cached.drives ?? null };
    })().then(
      (r) => live && setRes(r),
      () => live && setRes(null),
    );
    return () => {
      live = false;
    };
  }, [host, os, online, drives]);
  return res ? placesFor(res.res, res.drives) : null;
}
