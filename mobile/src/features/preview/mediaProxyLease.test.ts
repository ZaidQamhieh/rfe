import { leaseMediaProxy, type MediaProxyApi } from './mediaProxyLease';

const spec = { url: 'https://h/x', headers: { Authorization: 'Bearer t' }, pin: null };

function fakeProxy(startImpl?: () => Promise<string>) {
  const calls: string[] = [];
  const proxy: MediaProxyApi = {
    start: jest.fn(async (id: string) => {
      calls.push(`start:${id}`);
      return startImpl ? startImpl() : `http://127.0.0.1:1/${id}`;
    }),
    stop: jest.fn(async (id: string) => {
      calls.push(`stop:${id}`);
    }),
  };
  return { proxy, calls };
}

const flush = () => new Promise((r) => setImmediate(r));

describe('leaseMediaProxy', () => {
  it('starts once the spec is known and stops on release', async () => {
    const { proxy, calls } = fakeProxy();
    const lease = leaseMediaProxy(proxy, 'a', async () => spec);
    await expect(lease.uri).resolves.toBe('http://127.0.0.1:1/a');
    lease.release();
    await flush();
    expect(calls).toEqual(['start:a', 'stop:a']);
  });

  it('never starts when released before the spec resolves', async () => {
    const { proxy, calls } = fakeProxy();
    let resolveSpec!: (s: typeof spec) => void;
    const lease = leaseMediaProxy(proxy, 'b', () => new Promise((r) => (resolveSpec = r)));
    lease.release();
    resolveSpec(spec);
    await expect(lease.uri).rejects.toThrow('released');
    await flush();
    expect(calls).toEqual([]);
  });

  it('stops the listener when released while start is in flight', async () => {
    let finishStart!: (uri: string) => void;
    const { proxy, calls } = fakeProxy(() => new Promise((r) => (finishStart = r)));
    const lease = leaseMediaProxy(proxy, 'c', async () => spec);
    await flush();
    expect(calls).toEqual(['start:c']);
    lease.release();
    await flush();
    expect(calls).toEqual(['start:c']);
    finishStart('http://127.0.0.1:2/c');
    await lease.uri;
    await flush();
    expect(calls).toEqual(['start:c', 'stop:c']);
  });

  it('stops a start that failed after release (it may have bound the port)', async () => {
    let failStart!: (e: Error) => void;
    const { proxy, calls } = fakeProxy(() => new Promise((_, rej) => (failStart = rej)));
    const lease = leaseMediaProxy(proxy, 'd', async () => spec);
    await flush();
    lease.release();
    failStart(new Error('boom'));
    await expect(lease.uri).rejects.toThrow('boom');
    await flush();
    expect(calls).toEqual(['start:d', 'stop:d']);
  });

  it('releasing twice stops once, and a failed spec never stops', async () => {
    const { proxy, calls } = fakeProxy();
    const lease = leaseMediaProxy(proxy, 'e', async () => spec);
    await lease.uri;
    lease.release();
    lease.release();
    await flush();
    expect(calls).toEqual(['start:e', 'stop:e']);

    const second = fakeProxy();
    const bad = leaseMediaProxy(second.proxy, 'f', async () => {
      throw new Error('no client');
    });
    await expect(bad.uri).rejects.toThrow('no client');
    bad.release();
    await flush();
    expect(second.calls).toEqual([]);
  });
});
