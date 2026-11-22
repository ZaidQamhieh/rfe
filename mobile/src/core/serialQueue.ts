/**
 * Returns a runner that starts each job only after the previous one has settled. The caller gets its own job's result
 * or error, and a failed job does not block the jobs queued behind it.
 */
export function serialQueue() {
  let tail: Promise<unknown> = Promise.resolve();
  return <T>(job: () => Promise<T>): Promise<T> => {
    const run = tail.then(job, job);
    tail = run.catch(() => undefined);
    return run;
  };
}
