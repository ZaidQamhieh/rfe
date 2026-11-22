import type { TransferRecord } from '../../core/native';

/** One file being copied between computers through this phone: first downloaded from the source, then uploaded to the destination. */
export type RelayJob = { downloadId: string; destHostId: string; destDir: string; name: string };

export type RelayStep = {
  /** Downloads that finished: start their upload. */
  start: { job: RelayJob; localPath: string }[];
  /** Jobs to forget: the download failed or was cancelled, or its record is gone (removed by the user). */
  drop: string[];
};

/**
 * Decides what to do for each job from the current transfer records. Pure; the caller performs the steps. A job
 * whose download is not in [records] counts as removed, so [records] must be listed after the jobs were read.
 */
export function planRelay(jobs: RelayJob[], records: TransferRecord[]): RelayStep {
  const byId = new Map(records.map((r) => [r.id, r]));
  const step: RelayStep = { start: [], drop: [] };
  for (const job of jobs) {
    const r = byId.get(job.downloadId);
    if (!r) step.drop.push(job.downloadId);
    else if (r.state === 'DONE') step.start.push({ job, localPath: r.destPath });
    else if (r.state === 'FAILED' || r.state === 'CANCELLED') step.drop.push(job.downloadId);
  }
  return step;
}

export const parseJobs = (raw: string | null): RelayJob[] => {
  try {
    const v: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(v) ? v.filter((j): j is RelayJob => !!j && typeof j.downloadId === 'string' && typeof j.destHostId === 'string' && typeof j.destDir === 'string' && typeof j.name === 'string') : [];
  } catch {
    return [];
  }
};
