import { useEffect, useRef } from 'react';

import { ignoreError } from '../../core/log';
import { transfers } from '../../core/native';
import { useTransfers } from '../hosts/useTransfers';
import { finishRelay, loadRelayJobs, updateRelayJobs } from './relayCopy';
import { planRelay } from './relayPlan';

const TICK_MS = 4000;

/** Starts the upload half of every phone-relayed copy whose download has finished. Mount once, at the root. */
export function useRelayCopy(): void {
  // A change in the transfers is only the cue to look again; the plan reads the list itself (see below).
  const items = useTransfers();
  const busy = useRef(false);
  useEffect(() => {
    const run = async () => {
      if (busy.current) return;
      busy.current = true;
      try {
        const jobs = await loadRelayJobs();
        if (jobs.length === 0) return;
        // Read after the jobs: a job is saved only once its download is queued, so a listing taken now has it. A copy
        // of the list taken earlier (the hook's own) can miss a download queued since and would get its job dropped.
        const records = await transfers.list();
        const step = planRelay(jobs, records);
        const done = new Set(step.drop);
        for (const s of step.start) {
          try {
            await finishRelay(s.job, s.localPath);
            done.add(s.job.downloadId);
          } catch {
            // Keep the job: the next tick tries again.
          }
        }
        if (done.size > 0) await updateRelayJobs((current) => current.filter((j) => !done.has(j.downloadId)));
      } finally {
        busy.current = false;
      }
    };
    const tick = () => void run().catch(ignoreError('relay copy: tick'));
    tick();
    const timer = setInterval(tick, TICK_MS);
    return () => clearInterval(timer);
  }, [items]);
}
