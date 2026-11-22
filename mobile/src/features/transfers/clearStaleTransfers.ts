import type { transfers } from '../../core/native';
import { staleAuthFailures } from './transferLogic';

/**
 * After computer [hostId] is paired again, forgets its transfer rows that failed with "no longer accepts this phone":
 * the phone is accepted now, so those rows are out of date. Every removal is tried; the first failure is rethrown after.
 */
export async function clearStaleTransfers(hostId: string, api: Pick<typeof transfers, 'list' | 'remove'>): Promise<void> {
  const stale = staleAuthFailures(await api.list(), hostId);
  const results = await Promise.allSettled(stale.map((r) => api.remove(r.id)));
  const failed = results.find((x): x is PromiseRejectedResult => x.status === 'rejected');
  if (failed) throw failed.reason;
}
