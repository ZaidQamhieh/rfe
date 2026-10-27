import type { BatchResult } from '../../core/api/models';
import { basenameOf } from './paths';

export type RestoreOutcome = { ok: true; name: string; renamed: boolean } | { ok: false; error: string };

/**
 * What a single-item trash restore did, from the agent's BatchResult. The agent renames on a collision and returns
 * the path it really used, so the name shown is that path's, and any failed item is an error, never a success.
 */
export function restoreOutcome(itemName: string, result: BatchResult): RestoreOutcome {
  const failed = result.failed[0];
  if (failed) return { ok: false, error: failed.errorMessage ?? failed.errorCode ?? 'Restore failed' };
  const done = result.results[0];
  if (!done) return { ok: false, error: 'Restore failed' };
  const name = basenameOf(done.path) || itemName;
  return { ok: true, name, renamed: name !== itemName };
}
