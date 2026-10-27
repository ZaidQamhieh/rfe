// Port of _ThumbnailFetchQueue: bounded concurrency, one job per key shared by every subscriber,
// and a job is abandoned (queued) or cancelled (running) once its last subscriber goes away.

export type Request<T> = { promise: Promise<T>; cancel: () => void };

type Job<T> = {
  key: string;
  started: boolean;
  abandoned: boolean;
  aborter: { aborted: boolean; onAbort?: () => void };
  subs: Set<Sub<T>>;
  load: (signal: { readonly aborted: boolean; onAbort(cb: () => void): void }) => Promise<T>;
};

type Sub<T> = { job: Job<T>; done: boolean; resolve(v: T): void; reject(e: unknown): void };

export class FetchQueue<T> {
  private inFlight = new Map<string, Job<T>>();
  private pending: Job<T>[] = [];
  private active = 0;

  constructor(
    private readonly maxConcurrent: number,
    /** Value delivered to subscribers whose request was cancelled. */
    private readonly cancelledValue: T,
  ) {}

  request(key: string, load: Job<T>['load']): Request<T> {
    let job = this.inFlight.get(key);
    if (!job) {
      job = { key, started: false, abandoned: false, aborter: { aborted: false }, subs: new Set(), load };
      this.inFlight.set(key, job);
      this.pending.push(job);
    }
    let sub!: Sub<T>;
    const promise = new Promise<T>((resolve, reject) => {
      sub = { job: job!, done: false, resolve, reject };
    });
    job.subs.add(sub);
    this.pump();
    return { promise, cancel: () => this.cancel(sub) };
  }

  private cancel(sub: Sub<T>) {
    if (sub.done) return;
    const job = sub.job;
    sub.done = true;
    job.subs.delete(sub);
    sub.resolve(this.cancelledValue);
    if (job.subs.size > 0) return;
    job.abandoned = true;
    if (this.inFlight.get(job.key) === job) this.inFlight.delete(job.key);
    if (job.started) {
      job.aborter.aborted = true;
      job.aborter.onAbort?.();
    } else {
      this.pending = this.pending.filter((p) => p !== job);
    }
    this.pump();
  }

  private pump() {
    while (this.active < this.maxConcurrent && this.pending.length > 0) {
      const job = this.pending.shift()!;
      if (job.abandoned || job.subs.size === 0) continue;
      job.started = true;
      this.active++;
      void this.run(job);
    }
  }

  private async run(job: Job<T>) {
    const signal = {
      get aborted() {
        return job.aborter.aborted;
      },
      onAbort: (cb: () => void) => {
        job.aborter.onAbort = cb;
      },
    };
    try {
      const value = await job.load(signal);
      if (!job.abandoned) for (const s of [...job.subs]) this.finish(s, (x) => x.resolve(value));
    } catch (e) {
      if (!job.abandoned) for (const s of [...job.subs]) this.finish(s, (x) => x.reject(e));
    } finally {
      if (this.inFlight.get(job.key) === job) this.inFlight.delete(job.key);
      this.active--;
      this.pump();
    }
  }

  private finish(sub: Sub<T>, fn: (s: Sub<T>) => void) {
    if (sub.done) return;
    sub.done = true;
    sub.job.subs.delete(sub);
    fn(sub);
  }
}
