import type { transfers, TransferRecord } from '../../core/native';

/**
 * Removes [rs] from the engine, then reads the list again (a row native refused to remove stays visible instead of
 * vanishing while it runs). A failed removal or a failed re-read is handed to [onError] and never rejects; the result is
 * the fresh list, or null when it could not be read.
 */
export async function forgetTransfers(rs: TransferRecord[], api: Pick<typeof transfers, 'remove' | 'list'>, onError: (e: unknown) => void): Promise<TransferRecord[] | null> {
  const results = await Promise.allSettled(rs.map((r) => api.remove(r.id)));
  const failed = results.find((x): x is PromiseRejectedResult => x.status === 'rejected');
  if (failed) onError(failed.reason);
  try {
    return await api.list();
  } catch (e) {
    onError(e);
    return null;
  }
}
