import { FetchQueue } from './fetchQueue';

const deferred = <T,>() => {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
};
const tick = () => new Promise((r) => setTimeout(r, 0));

describe('FetchQueue', () => {
  it('bounds concurrency and starts queued jobs as slots free up', async () => {
    const q = new FetchQueue<string | null>(2, null);
    const gates = [deferred<string>(), deferred<string>(), deferred<string>()];
    let running = 0;
    let peak = 0;
    const reqs = gates.map((g, i) =>
      q.request(`k${i}`, async () => {
        running++;
        peak = Math.max(peak, running);
        const v = await g.promise;
        running--;
        return v;
      }),
    );
    await tick();
    expect(running).toBe(2);
    gates[0].resolve('a');
    await tick();
    expect(running).toBe(2); // third started
    gates[1].resolve('b');
    gates[2].resolve('c');
    expect(await Promise.all(reqs.map((r) => r.promise))).toEqual(['a', 'b', 'c']);
    expect(peak).toBe(2);
  });

  it('shares one job per key; it survives until the last subscriber cancels', async () => {
    const q = new FetchQueue<string | null>(1, null);
    const gate = deferred<string>();
    let loads = 0;
    let aborted = false;
    const load = async (s: { onAbort(cb: () => void): void }) => {
      loads++;
      s.onAbort(() => (aborted = true));
      return gate.promise;
    };
    const a = q.request('same', load);
    const b = q.request('same', load);
    await tick();
    expect(loads).toBe(1);
    a.cancel();
    expect(await a.promise).toBeNull();
    expect(aborted).toBe(false); // b still waiting
    b.cancel();
    expect(aborted).toBe(true);
    gate.resolve('late');
    expect(await b.promise).toBeNull();
  });

  it('a queued job whose subscribers all cancel never runs', async () => {
    const q = new FetchQueue<string | null>(1, null);
    const gate = deferred<string>();
    q.request('first', () => gate.promise);
    let ran = false;
    const second = q.request('second', async () => ((ran = true), 'x'));
    second.cancel();
    gate.resolve('done');
    await tick();
    await tick();
    expect(ran).toBe(false);
  });

  it('errors reach every subscriber and free the slot', async () => {
    const q = new FetchQueue<string | null>(1, null);
    const a = q.request('e', async () => {
      throw new Error('boom');
    });
    const b = q.request('e', async () => 'unused');
    await expect(a.promise).rejects.toThrow('boom');
    await expect(b.promise).rejects.toThrow('boom');
    expect(await q.request('ok', async () => 'fine').promise).toBe('fine');
  });
});
