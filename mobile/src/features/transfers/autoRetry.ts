import type { TransferRecord } from '../../core/native';
import { ignoreError } from '../../core/log';
import { isAutoRetryable } from './transferLogic';

const SECOND_MS = 1000;
const MINUTE_MS = 60 * SECOND_MS;

/**
 * The wait before each automatic attempt, counted from the failure. One entry per attempt, so the list's length is the cap:
 * 10 s, then 1 min, then 5 min. After the last one the row keeps only its Retry button.
 */
export const AUTO_RETRY_DELAYS_MS: readonly number[] = [10 * SECOND_MS, MINUTE_MS, 5 * MINUTE_MS];

/** Asks the engine to resume each of [ids]; a resume that fails is logged and never left as an unhandled rejection. */
export async function resumeQuietly(ids: string[], resume: (id: string) => Promise<void>): Promise<void> {
  await Promise.all(ids.map((id) => resume(id).catch(ignoreError('auto-retry resume'))));
}

/** How often the open screen checks whether an automatic attempt is due. */
export const AUTO_RETRY_TICK_MS = SECOND_MS;

/** Per transfer: the automatic attempts used so far, and when the next one is due (null until the failure is seen). */
type AutoRetryTrack = { attempts: number; dueAt: number | null };
export type AutoRetryTracks = ReadonlyMap<string, AutoRetryTrack>;
export type AutoRetryStep = { tracks: AutoRetryTracks; resume: string[] };

/**
 * Decides which failed transfers to resume now. Pure: the caller passes the records, the tracks from the previous call,
 * the current time and whether the app is in the foreground, then resumes the ids it gets back.
 * - A connection failure is due after its wait; once due (in the foreground) it is resumed and its attempt is counted.
 * - A running transfer keeps its count; a success or a cancel drops it; a pause keeps it but does not resume.
 * - Other failures are never resumed. Once every wait is used, the transfer stays failed for the owner's Retry.
 */
export function scheduleAutoRetries(records: readonly TransferRecord[], tracks: AutoRetryTracks, now: number, foreground: boolean): AutoRetryStep {
  if (!foreground) return { tracks, resume: [] };
  const next = new Map<string, AutoRetryTrack>();
  const resume: string[] = [];
  for (const r of records) {
    const track = tracks.get(r.id);
    if (r.state === 'DONE' || r.state === 'CANCELLED') continue;
    if (r.state === 'PAUSED') {
      if (track) next.set(r.id, track);
      continue;
    }
    if (r.state === 'RUNNING' || r.state === 'QUEUED') {
      if (track) next.set(r.id, { attempts: track.attempts, dueAt: null });
      continue;
    }
    if (!isAutoRetryable(r)) continue;
    const attempts = track?.attempts ?? 0;
    if (attempts >= AUTO_RETRY_DELAYS_MS.length) {
      next.set(r.id, { attempts, dueAt: null });
      continue;
    }
    const dueAt = track?.dueAt ?? now + AUTO_RETRY_DELAYS_MS[attempts];
    if (now >= dueAt) {
      resume.push(r.id);
      next.set(r.id, { attempts: attempts + 1, dueAt: null });
    } else {
      next.set(r.id, { attempts, dueAt });
    }
  }
  return { tracks: next, resume };
}

/** The owner tapped Retry: these transfers get their automatic attempts back. */
export function resetAutoRetry(tracks: AutoRetryTracks, ids: readonly string[]): AutoRetryTracks {
  const next = new Map(tracks);
  for (const id of ids) next.delete(id);
  return next;
}
