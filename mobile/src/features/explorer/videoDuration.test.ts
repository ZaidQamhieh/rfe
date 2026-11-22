import { clientForHost, hostStore } from '../../services';
import { createDurationSource, isVideoEntry, videoDurations } from './videoDuration';

jest.mock('../../services', () => ({ clientForHost: jest.fn(), hostStore: { listHosts: jest.fn() } }));

const deferred = <T,>() => {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
};

describe('isVideoEntry', () => {
  it('recognises a video by MIME type or by extension, never a folder', () => {
    expect(isVideoEntry({ isDir: false, mimeType: 'video/mp4', name: 'a' })).toBe(true);
    expect(isVideoEntry({ isDir: false, mimeType: 'application/octet-stream', name: 'Clip.MKV' })).toBe(true);
    expect(isVideoEntry({ isDir: false, mimeType: 'image/jpeg', name: 'a.jpg' })).toBe(false);
    expect(isVideoEntry({ isDir: true, mimeType: undefined, name: 'movies.mp4' })).toBe(false);
  });
});

describe('createDurationSource', () => {
  it('runs at most the given number of lookups at once and starts the next as one finishes', async () => {
    const gates = [deferred<number | null>(), deferred<number | null>(), deferred<number | null>()];
    let running = 0;
    let peak = 0;
    let n = 0;
    const src = createDurationSource(async () => {
      const gate = gates[n++];
      running++;
      peak = Math.max(peak, running);
      const v = await gate.promise;
      running--;
      return v;
    }, 2);
    const reqs = ['/a', '/b', '/c'].map((p) => src.request('h', p, 1));
    await Promise.resolve();
    expect(n).toBe(2);
    gates[0].resolve(1000);
    await expect(reqs[0].promise).resolves.toBe(1000);
    await Promise.resolve();
    expect(n).toBe(3);
    gates[1].resolve(null);
    gates[2].resolve(3000);
    await expect(reqs[1].promise).resolves.toBeNull();
    await expect(reqs[2].promise).resolves.toBe(3000);
    expect(peak).toBe(2);
  });

  it('remembers an answer per file version, but not a failure', async () => {
    const fetch = jest.fn().mockRejectedValueOnce(new Error('boom')).mockResolvedValue(5000);
    const src = createDurationSource(fetch);
    await expect(src.request('h', '/v.mp4', 'm1').promise).rejects.toThrow('boom');
    await expect(src.request('h', '/v.mp4', 'm1').promise).resolves.toBe(5000);
    await expect(src.request('h', '/v.mp4', 'm1').promise).resolves.toBe(5000);
    expect(fetch).toHaveBeenCalledTimes(2);
    await expect(src.request('h', '/v.mp4', 'm2').promise).resolves.toBe(5000);
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it('a cancelled queued request never reaches the network and learns nothing', async () => {
    const gate = deferred<number | null>();
    const fetch = jest.fn().mockReturnValue(gate.promise);
    const src = createDurationSource(fetch, 1);
    const first = src.request('h', '/a.mp4', 1);
    const second = src.request('h', '/b.mp4', 1);
    second.cancel();
    await expect(second.promise).resolves.toBeUndefined();
    gate.resolve(1);
    await first.promise;
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

describe('videoDurations (the app source)', () => {
  const abortError = () => Object.assign(new Error('aborted'), { name: 'AbortError' });
  const setup = (mediaInfo: jest.Mock) => {
    (hostStore.listHosts as jest.Mock).mockResolvedValue([{ id: 'h' }]);
    (clientForHost as jest.Mock).mockResolvedValue({ mediaInfo });
  };

  it('asks for the length and passes an abort signal along', async () => {
    const mediaInfo = jest.fn().mockResolvedValue({ durationMs: 4200 });
    setup(mediaInfo);
    await expect(videoDurations.request('h', '/ok.mp4', 1).promise).resolves.toBe(4200);
    expect(mediaInfo).toHaveBeenCalledWith('/ok.mp4', expect.objectContaining({ aborted: false }));
  });

  it('aborts the call when the request is cancelled, and nothing is learned or thrown', async () => {
    let seen: AbortSignal | undefined;
    const mediaInfo = jest.fn((_p: string, s: AbortSignal) => {
      seen = s;
      return new Promise((_res, rej) => s.addEventListener('abort', () => rej(abortError())));
    });
    setup(mediaInfo);
    const req = videoDurations.request('h', '/cancel.mp4', 1);
    await new Promise((r) => setImmediate(r));
    expect(seen?.aborted).toBe(false);
    req.cancel();
    expect(seen?.aborted).toBe(true);
    await expect(req.promise).resolves.toBeUndefined();
    await new Promise((r) => setImmediate(r));
    // Nothing was remembered: asking again calls the computer again.
    const again = jest.fn().mockResolvedValue({ durationMs: 9 });
    setup(again);
    await expect(videoDurations.request('h', '/cancel.mp4', 1).promise).resolves.toBe(9);
  });

  it('turns an AbortError raised after the cancel into no answer rather than an error', async () => {
    const mediaInfo = jest.fn().mockRejectedValue(abortError());
    setup(mediaInfo);
    await expect(videoDurations.request('h', '/abort.mp4', 1).promise).resolves.toBeNull();
  });
});
