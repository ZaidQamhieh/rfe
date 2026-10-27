/**
 * Wraps a UI action so a second call made before the first finishes is ignored (a double tap), and a failure is handed
 * to [onError] instead of becoming an unhandled rejection. Resolves to true when the action ran and succeeded.
 */
export function guardedAction<A extends unknown[]>(fn: (...args: A) => Promise<unknown>, onError: (e: unknown) => void): (...args: A) => Promise<boolean> {
  let busy = false;
  return async (...args) => {
    if (busy) return false;
    busy = true;
    try {
      await fn(...args);
      return true;
    } catch (e) {
      onError(e);
      return false;
    } finally {
      busy = false;
    }
  };
}
