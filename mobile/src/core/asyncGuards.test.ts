import { guardedAction } from './asyncGuards';

describe('guardedAction', () => {
  it('ignores a second call while the first is running, then allows another', async () => {
    let release!: () => void;
    const fn = jest.fn(() => new Promise<void>((r) => (release = r)));
    const act = guardedAction(fn, jest.fn());
    const first = act();
    expect(await act()).toBe(false);
    expect(fn).toHaveBeenCalledTimes(1);
    release();
    expect(await first).toBe(true);
    const again = act();
    release();
    expect(await again).toBe(true);
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it('reports a failure to onError, resolves false, and frees the guard', async () => {
    const onError = jest.fn();
    let n = 0;
    const act = guardedAction(async (x: string) => {
      if (n++ === 0) throw new Error(`bad ${x}`);
    }, onError);
    expect(await act('a')).toBe(false);
    expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: 'bad a' }));
    expect(await act('b')).toBe(true);
    expect(onError).toHaveBeenCalledTimes(1);
  });
});
