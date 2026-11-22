import { useFocusEffect } from 'expo-router';
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { AppState } from 'react-native';

import { AgentApiError, HTTP_UNAUTHORIZED, type AgentClient } from '../../core/api/agentClient';
import type { Drive, Health } from '../../core/api/models';
import { CertPinMismatch } from '../../core/api/pin';
import type { Host } from '../../core/models/host';
import { ignoreError } from '../../core/log';
import { clientForHost, hostStore } from '../../services';
import { widgetSet } from '../../core/native';
import { widgetSnapshot } from './widgetLogic';
import { nextRefused, type AccessOutcome } from './accessState';

const STATUS_REFRESH_MS = 60_000;
/** The drive list is fetched on every status check: a computer that removed this phone answers it with 401, so the phone learns within one check. */
const DRIVE_REFRESH_MS = STATUS_REFRESH_MS / 2; // half a check interval, so timer jitter never skips one
const PING_TIMEOUT_MS = 8_000;

type StatusState = { checking: boolean; health: Health | null; untrusted: boolean; refused: boolean; activeAddress: string | null; lastSeen: Date | null; drives: Drive[] | null };
const EMPTY_STATUS: StatusState = { checking: true, health: null, untrusted: false, refused: false, activeAddress: null, lastSeen: null, drives: null };

export type HostStatus = {
  checking: boolean;
  health: Health | null;
  online: boolean;
  /** The computer answered with a different certificate than the one this phone pinned. */
  untrusted: boolean;
  /** The computer answered, but its sign-in check refused this phone's token (it was removed there). */
  refused: boolean;
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
  const [state, setState] = useState<StatusState>({ ...EMPTY_STATUS });
  const inFlight = useRef(false);
  const rerunForced = useRef(false);
  const lastDrives = useRef(0);
  const focused = useRef(false);
  const hostRef = useRef(host);
  // What is known about the computer refusing this phone; kept across checks and restarts (see [nextRefused]).
  const refusedRef = useRef(false);
  useLayoutEffect(() => {
    hostRef.current = host;
  }, [host]);

  /** Folds one check's result into the refused state, shows it and saves it when it changed. */
  const learnAccess = useCallback((id: string, outcome: AccessOutcome) => {
    const next = nextRefused(refusedRef.current, outcome);
    if (next !== refusedRef.current) {
      refusedRef.current = next;
      void hostStore.setRefused(id, next).catch(ignoreError('remember refused state'));
    }
    setState((s) => (s.refused === next ? s : { ...s, refused: next }));
  }, []);

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
          (drives) => {
            setState((s) => ({ ...s, drives }));
            learnAccess(h.id, 'accepted');
            const snap = widgetSnapshot(h.label, drives);
            if (snap) void widgetSet(snap.title, snap.detail, snap.percent).catch(ignoreError('storage widget'));
          },
          // /health needs no token, so a 401 here is the first sign the computer removed this phone.
          (e) => {
            setState((s) => ({ ...s, drives: [] }));
            learnAccess(h.id, e instanceof AgentApiError && e.statusCode === HTTP_UNAUTHORIZED ? 'unauthorized' : 'error');
          },
        );
      }
    } catch (e) {
      // Not answering says nothing about the login: a computer known to have removed this phone stays marked.
      setState((s) => ({ ...s, checking: false, health: null, untrusted: e instanceof CertPinMismatch }));
    } finally {
      inFlight.current = false;
      if (rerunForced.current) {
        rerunForced.current = false;
        void ping(true);
      }
    }
  }, [onHostLearned, learnAccess]);

  useEffect(() => {
    let live = true;
    hostStore.getLastSeen(host.id).then((d) => live && setState((s) => ({ ...s, lastSeen: d })));
    // A removal learned in an earlier session is still shown before the first check answers.
    hostStore.getRefused(host.id).then(
      (r) => {
        if (!live) return;
        refusedRef.current = r;
        setState((s) => ({ ...s, refused: r }));
      },
      ignoreError('read refused state'),
    );
    return () => {
      live = false;
    };
  }, [host.id]);

  // Another host's status must not show under this one while its first ping is out.
  useEffect(() => {
    lastDrives.current = 0;
    refusedRef.current = false;
    setState((s) => (s.health || s.drives ? { ...EMPTY_STATUS } : s));
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
