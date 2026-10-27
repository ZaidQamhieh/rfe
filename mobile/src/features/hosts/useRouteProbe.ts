import { useCallback, useEffect, useRef, useState } from 'react';

import { routeAddresses, type Host } from '../../core/models/host';
import { nativeTransport } from '../../core/native';
import { clientForHost, hostStore } from '../../services';
import { pickCurrent } from './connectLogic';
import { probeAll, type ProbeResult } from './diagnostics';

export type RouteProbe = {
  /** null until the first probe finishes (and again while a re-check runs). */
  results: ProbeResult[] | null;
  /** The address requests are going through, when one route answered. */
  current: string | null;
  running: boolean;
  checkedAt: Date | null;
  rerun: () => void;
};

/**
 * Probes every route of [host] (reachability, pinned TLS, auth, latency) and finds which one the client is using. The Connect
 * screen and the diagnostics sheet share one of these so opening the sheet does not probe twice. [enabled] false keeps it idle.
 */
export function useRouteProbe(host: Host, enabled = true): RouteProbe {
  const [state, setState] = useState<{ results: ProbeResult[] | null; current: string | null; checkedAt: Date | null }>({ results: null, current: null, checkedAt: null });
  const [running, setRunning] = useState(enabled);
  const seq = useRef(0);

  const fetchOnce = useCallback(async () => {
    try {
      const [token, fingerprint] = await Promise.all([hostStore.getToken(host.id), hostStore.getPin(host.id)]);
      const client = await clientForHost(host, true, 5000);
      const [results, clientAddress] = await Promise.all([
        probeAll({ transport: nativeTransport, deviceToken: token, fingerprint }, host),
        client.health().then(() => client.activeAddress, () => null),
      ]);
      return { results, current: pickCurrent(results, clientAddress), checkedAt: new Date() };
    } catch {
      return { results: routeAddresses(host).map((r): ProbeResult => ({ ...r, failure: 'other', auth: 'notChecked' })), current: null, checkedAt: new Date() };
    }
  }, [host]);

  useEffect(() => {
    if (!enabled) return;
    let live = true;
    const mine = ++seq.current;
    void fetchOnce().then((r) => {
      if (!live || mine !== seq.current) return;
      setState(r);
      setRunning(false);
    });
    return () => {
      live = false;
    };
  }, [enabled, fetchOnce]);

  const rerun = () => {
    const mine = ++seq.current;
    setRunning(true);
    setState((s) => ({ ...s, results: null }));
    void fetchOnce().then((r) => {
      if (mine !== seq.current) return;
      setState(r);
      setRunning(false);
    });
  };

  return { ...state, running, rerun };
}
