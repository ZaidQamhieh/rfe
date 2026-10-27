import { useCallback, useRef } from 'react';

/**
 * Component form of `guardedAction`: a call made while a previous one is still running is ignored, and a failure goes
 * to [onError]. The running flag lives in a ref, so it survives re-renders.
 */
export function useGuardedAction<A extends unknown[]>(fn: (...args: A) => Promise<unknown>, onError: (e: unknown) => void): (...args: A) => Promise<boolean> {
  const busy = useRef(false);
  return useCallback(
    async (...args: A) => {
      if (busy.current) return false;
      busy.current = true;
      try {
        await fn(...args);
        return true;
      } catch (e) {
        onError(e);
        return false;
      } finally {
        busy.current = false;
      }
    },
    [fn, onError],
  );
}
