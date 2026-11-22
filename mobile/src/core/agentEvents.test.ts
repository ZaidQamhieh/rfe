import { connectAgentEvents, EVENTS_PIN_MISMATCH, retryDelay, type EventsBackend } from './agentEvents';

const spec = () => ({ url: 'https://h/v1/events', headers: {}, pin: 'aa' });

describe('agent event stream', () => {
  it('backs off from 1 s to 30 s', () => {
    expect([0, 1, 2, 5, 20].map(retryDelay)).toEqual([1000, 2000, 4000, 30000, 30000]);
  });

  it('a connection that stayed up resets the backoff when it drops', async () => {
    const sleeps: number[] = [];
    let t = 0;
    const now = jest.spyOn(Date, 'now').mockImplementation(() => t);
    let n = 0;
    const backend: EventsBackend = {
      run: async () => {
        t += n++ === 0 ? 60_000 : 10;
        return n > 3 ? new Promise<number>(() => {}) : 0;
      },
      stop: jest.fn(async () => {}),
      onEvent: () => ({ remove: jest.fn() }),
    };
    const stop = connectAgentEvents(backend, spec, () => {}, async (ms) => void sleeps.push(ms));
    await new Promise((r) => setTimeout(r, 0));
    stop();
    now.mockRestore();
    expect(sleeps).toEqual([1000, 2000, 4000]);
  });

  it('a pin mismatch ends the loop at once: no backoff sleep, no reconnect, and the caller is told once', async () => {
    const run = jest.fn(async () => EVENTS_PIN_MISMATCH);
    const sleeps: number[] = [];
    const onPin = jest.fn();
    const backend: EventsBackend = { run, stop: jest.fn(async () => {}), onEvent: () => ({ remove: jest.fn() }) };
    const stop = connectAgentEvents(backend, spec, () => {}, async (ms) => void sleeps.push(ms), onPin);
    await new Promise((r) => setTimeout(r, 0));
    stop();
    expect(run).toHaveBeenCalledTimes(1);
    expect(sleeps).toEqual([]);
    expect(onPin).toHaveBeenCalledTimes(1);
  });

  it('a rejection with ERR_CERT_PIN_MISMATCH is treated like the -1 result', async () => {
    const run = jest.fn(async () => {
      throw Object.assign(new Error('pin'), { code: 'ERR_CERT_PIN_MISMATCH' });
    });
    const sleeps: number[] = [];
    const onPin = jest.fn();
    const backend: EventsBackend = { run, stop: jest.fn(async () => {}), onEvent: () => ({ remove: jest.fn() }) };
    const stop = connectAgentEvents(backend, spec, () => {}, async (ms) => void sleeps.push(ms), onPin);
    await new Promise((r) => setTimeout(r, 0));
    stop();
    expect(sleeps).toEqual([]);
    expect(run).toHaveBeenCalledTimes(1);
    expect(onPin).toHaveBeenCalledTimes(1);
  });

  it('a plain failure (0 or another error) still backs off and reconnects, never reporting a pin mismatch', async () => {
    let n = 0;
    const run = jest.fn(async () => {
      if (n++ === 0) throw new Error('boom');
      return n > 2 ? new Promise<number>(() => {}) : 0;
    });
    const sleeps: number[] = [];
    const onPin = jest.fn();
    const backend: EventsBackend = { run, stop: jest.fn(async () => {}), onEvent: () => ({ remove: jest.fn() }) };
    const stop = connectAgentEvents(backend, spec, () => {}, async (ms) => void sleeps.push(ms), onPin);
    await new Promise((r) => setTimeout(r, 0));
    stop();
    expect(sleeps).toEqual([2000, 4000]);
    expect(onPin).not.toHaveBeenCalled();
  });

  it('delivers kinds of the live connection only, reconnects after it ends, and stops', async () => {
    let listener: (id: string, kind: string) => void = () => {};
    const ids: string[] = [];
    let release: (s: number) => void = () => {};
    const backend: EventsBackend = {
      run: (id) => {
        ids.push(id);
        return new Promise<number>((r) => (release = r));
      },
      stop: jest.fn(async () => {}),
      onEvent: (cb) => {
        listener = cb;
        return { remove: jest.fn() };
      },
    };
    const got: string[] = [];
    const stop = connectAgentEvents(backend, spec, (k) => got.push(k), async () => {});
    await Promise.resolve();
    listener(ids[0], 'files');
    listener('other', 'transfers');
    release(200);
    await new Promise((r) => setTimeout(r, 0));
    expect(ids.length).toBe(2);
    listener(ids[1], 'settings');
    stop();
    listener(ids[1], 'files');
    expect(got).toEqual(['files', 'settings']);
    expect(backend.stop).toHaveBeenCalledWith(ids[1]);
  });
});
