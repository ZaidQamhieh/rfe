/** Native rejection codes that mean the person cancelled the work themselves. */
const CANCEL_CODES: ReadonlySet<string> = new Set(['ERR_CANCELLED']);

/**
 * True when [e] is the person's own cancel (an AbortError, the native ERR_CANCELLED code, or OkHttp's "Canceled" /
 * "cancelled" reason, also when wrapped in a native module's rejection text). Such a failure is not worth a bottom message.
 */
export function isUserCancel(e: unknown): boolean {
  if (typeof e !== 'object' || e === null) return false;
  const { name, code, message } = e as { name?: unknown; code?: unknown; message?: unknown };
  if (name === 'AbortError') return true;
  if (typeof code === 'string' && CANCEL_CODES.has(code)) return true;
  return typeof message === 'string' && /(^|[\s:>])cancell?ed\.?$/i.test(message.trim());
}
