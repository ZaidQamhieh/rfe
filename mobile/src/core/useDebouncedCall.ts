import { useEffect, useRef } from 'react';

function clearPending(timer: { current: ReturnType<typeof setTimeout> | null }): void {
  if (timer.current) clearTimeout(timer.current);
}

/**
 * Debounced calls for one screen: `schedule(fn)` runs fn once [delayMs] passes with no newer call, replacing a call still
 * waiting; `cancel()` drops the waiting call. The pending timer is cleared on unmount.
 */
export function useDebouncedCall(delayMs: number): { schedule: (fn: () => void) => void; cancel: () => void } {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => clearPending(timer), []);
  const cancel = () => clearPending(timer);
  const schedule = (fn: () => void) => {
    cancel();
    timer.current = setTimeout(fn, delayMs);
  };
  return { schedule, cancel };
}
