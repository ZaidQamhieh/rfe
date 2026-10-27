import { useFocusEffect } from 'expo-router';
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { AppState } from 'react-native';

import type { AgentClient } from '../../core/api/agentClient';
import type { Drive, Health } from '../../core/api/models';
import { CertPinMismatch } from '../../core/api/pin';
import type { Host } from '../../core/models/host';
import { clientForHost, hostStore } from '../../services';

const STATUS_REFRESH_MS = 60_000;
const DRIVE_REFRESH_MS = 5 * 60_000;
const PING_TIMEOUT_MS = 8_000;

export type HostStatus = {
  checking: boolean;
  health: Health | null;
  online: boolean;
  /** The computer answered with a different certificate than the one this phone pinned. */
  untrusted: boolean;
  activeAddress: string | null;
  lastSeen: Date | null;
  drives: Drive[] | null;
  refresh(): void;
};

/**
 * Pings /health on mount and once a minute while the Devices tab is focused and the app is in the
 * foreground; drive usage refreshes every five minutes (older agents without /system/drives just
 * skip the gauges). Learns a Tailscale address / MAC the agent reports and stores it.
 */
export function useHostStatus(host: Host, onHostLearned?: () => void): HostStatus {
  const [state, setState] = useState<{ checking: boolean; health: Health | null; untrusted: boolean; activeAddress: string | null; lastSeen: Date | null; drives: Drive[] | null }>({
    checking: true,
    health: null,
    untrusted: false,
    activeAddress: null,
    lastSeen: null,
    drives: null,
  });
  const inFlight = useRef(false);
  const rerunForced = useRef(false);
  const lastDrives = useRef(0);
  const focused = useRef(false);
  const hostRef = useRef(host);
  useLayoutEffect(() => {
    hostRef.current = host;
  }, [host]);

  const ping = useCallback(async (forceDrives: boolean) => {
    if (inFlight.current) {
      // A forced ping (focus, resume, pull to refresh) must not be lost to one already running.
      if (forceDrives) rerunForced.current = true;
      return;
    }
    inFlight.current = true;
    setState((s) => ({ ...s, checking: true }));
    let client: AgentClient | null = null;
    try {
      const h = hostRef.current;
      client = await clientForHost(h, true, PING_TIMEOUT_MS);
      const health = await client.health();
      const now = new Date();
      const learnedTs = health.tailscaleAddress && health.tailscaleAddress !== h.tailscaleAddress && health.tailscaleAddress !== h.address;
      const learnedMac = health.macAddress && health.macAddress !== h.macAddress;
      if (learnedTs || learnedMac) {
        // Only the learned fields, and only if the host is still listed: a label or address edited (or the host forgotten)
        // while this ping was in flight must not be reverted or brought back.
        const stored = await hostStore.patchHost(h.id, { ...(learnedTs ? { tailscaleAddress: health.tailscaleAddress } : {}), ...(learnedMac ? { macAddress: health.macAddress } : {}) });
        if (stored) onHostLearned?.();
      }
      await hostStore.setLastSeen(h.id, now);
      setState((s) => ({ ...s, checking: false, health, untrusted: false, activeAddress: client!.activeAddress, lastSeen: now }));
      if (forceDrives || Date.now() - lastDrives.current >= DRIVE_REFRESH_MS) {
        lastDrives.current = Date.now();
        client.drives().then(
          (drives) => setState((s) => ({ ...s, drives })),
          () => setState((s) => ({ ...s, drives: [] })),
        );
      }
    } catch (e) {
      setState((s) => ({ ...s, checking: false, health: null, untrusted: e instanceof CertPinMismatch }));
    } finally {
      inFlight.current = false;
      if (rerunForced.current) {
        rerunForced.current = false;
        void ping(true);
      }
    }
  }, [onHostLearned]);

  useEffect(() => {
    let live = true;
    hostStore.getLastSeen(host.id).then((d) => live && setState((s) => ({ ...s, lastSeen: d })));
    return () => {
      live = false;
    };
  }, [host.id]);

  // Another host's status must not show under this one while its first ping is out.
  useEffect(() => {
    lastDrives.current = 0;
    setState((s) => (s.health || s.drives ? { checking: true, health: null, untrusted: false, activeAddress: null, lastSeen: null, drives: null } : s));
  }, [host.id]);

  useFocusEffect(
    useCallback(() => {
      focused.current = true;
      ping(true);
      const timer = setInterval(() => {
        if (focused.current && AppState.currentState === 'active') ping(false);
      }, STATUS_REFRESH_MS);
      const sub = AppState.addEventListener('change', (st) => {
        if (st === 'active' && focused.current) ping(true);
      });
      return () => {
        focused.current = false;
        clearInterval(timer);
        sub.remove();
      };
    }, [ping]),
  );

  return { ...state, online: state.health !== null, refresh: () => ping(true) };
}
