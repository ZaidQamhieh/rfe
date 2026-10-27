export type WakeResult = 'up' | 'timeout' | 'cannot-wake' | 'send-failed' | 'cancelled';

export type WakeDeps = {
  /** The host's MAC address, when known; without it nothing can be sent. */
  mac: string | undefined;
  /** One quick reachability check (the agent's /health answered). */
  isUp: () => Promise<boolean>;
  /** Sends the Wake-on-LAN magic packet; false when it could not be sent. */
  wake: (mac: string) => Promise<boolean>;
  sleep: (ms: number) => Promise<void>;
  now: () => number;
  timeoutMs: number;
  intervalMs: number;
  isCancelled?: () => boolean;
  /** Called once, right after the packet went out (the caller can say "waking…"). */
  onWaking?: () => void;
};

/**
 * Makes sure a host is awake before something opens on it: if it answers already, done; otherwise one magic packet,
 * then the host is polled until it answers or [timeoutMs] passes.
 */
export async function wakeThenOpen(d: WakeDeps): Promise<WakeResult> {
  if (await d.isUp()) return 'up';
  if (!d.mac) return 'cannot-wake';
  if (!(await d.wake(d.mac))) return 'send-failed';
  d.onWaking?.();
  const deadline = d.now() + d.timeoutMs;
  while (d.now() < deadline) {
    await d.sleep(d.intervalMs);
    if (d.isCancelled?.()) return 'cancelled';
    if (await d.isUp()) return 'up';
  }
  return 'timeout';
}
