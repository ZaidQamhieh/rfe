// Keeps the agent's change-hint stream (GET /v1/events) open and calls onKind for each event; reconnects with backoff.
// An event carries only a kind ("files", "transfers", ...), so the receiver refetches through the normal pinned client.
// A certificate that no longer matches the pin ends the loop (no retry) and is reported once through onPinMismatch.

import { ERR_CERT_PIN_MISMATCH } from './api/pin';

export type EventsBackend = {
  run: (id: string, url: string, headers: Record<string, string>, pin: string | null) => Promise<number>;
  stop: (id: string) => Promise<void>;
  onEvent: (cb: (id: string, kind: string) => void) => { remove: () => void };
};
export type EventsSpec = { url: string; headers: Record<string, string>; pin: string };

/** What the native stream returns when the host's certificate did not match the pin (see EventStream.PIN_MISMATCH in Kotlin). */
export const EVENTS_PIN_MISMATCH = -1;

export const RETRY_MIN_MS = 1000;
export const RETRY_MAX_MS = 30000;
/** Reconnect delay after the n-th failure in a row (0-based): doubles from 1 s up to 30 s. */
export const retryDelay = (failures: number): number => Math.min(RETRY_MAX_MS, RETRY_MIN_MS * 2 ** Math.min(failures, 10));

let counter = 0;

/** A connection older than this counts as healthy when it ends. */
const HEALTHY_MS = 30_000;

/**
 * Starts the stream for `getSpec()` (called again on every reconnect, so a changed address is picked up). Returns stop().
 * A pin mismatch (a -1 result or a rejection with ERR_CERT_PIN_MISMATCH) stops the loop for good and calls
 * `onPinMismatch`; the caller shows the changed-identity message instead of an offline host.
 */
export function connectAgentEvents(
  backend: EventsBackend,
  getSpec: () => EventsSpec,
  onKind: (kind: string) => void,
  sleep: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms)),
  onPinMismatch?: () => void,
): () => void {
  let stopped = false;
  let current: string | null = null;
  const sub = backend.onEvent((id, kind) => {
    if (id === current && !stopped) onKind(kind);
  });
  void (async () => {
    let failures = 0;
    while (!stopped) {
      const id = `ev${++counter}`;
      current = id;
      let status = 0;
      const startedAt = Date.now();
      try {
        const s = getSpec();
        status = await backend.run(id, s.url, s.headers, s.pin);
      } catch (e) {
        status = (e as { code?: unknown } | null)?.code === ERR_CERT_PIN_MISMATCH ? EVENTS_PIN_MISMATCH : 0;
      }
      if (stopped) return;
      if (status === EVENTS_PIN_MISMATCH) {
        onPinMismatch?.();
        return;
      }
      // A stream that stayed up for a while was healthy: a later drop starts the backoff over.
      failures = status === 200 || Date.now() - startedAt >= HEALTHY_MS ? 0 : failures + 1;
      await sleep(retryDelay(failures));
    }
  })();
  return () => {
    stopped = true;
    sub.remove();
    if (current) void backend.stop(current);
  };
}
