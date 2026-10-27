import { wakeThenOpen } from './wakeThenOpen';

function deps(over: Partial<Parameters<typeof wakeThenOpen>[0]> = {}) {
  let t = 0;
  const calls: string[] = [];
  const base = {
    mac: 'AA:BB:CC:DD:EE:FF' as string | undefined,
    isUp: jest.fn().mockResolvedValue(false),
    wake: jest.fn().mockImplementation(async () => {
      calls.push('wake');
      return true;
    }),
    sleep: jest.fn().mockImplementation(async (ms: number) => {
      t += ms;
    }),
    now: () => t,
    timeoutMs: 10_000,
    intervalMs: 2_000,
  };
  return { d: { ...base, ...over }, calls };
}

describe('wakeThenOpen', () => {
  it('opens straight away when the host is already up, without sending a packet', async () => {
    const { d } = deps({ isUp: jest.fn().mockResolvedValue(true) });
    expect(await wakeThenOpen(d)).toBe('up');
    expect(d.wake).not.toHaveBeenCalled();
  });

  it('sends one packet, then polls until the host answers', async () => {
    const isUp = jest.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(false).mockResolvedValueOnce(false).mockResolvedValue(true);
    const onWaking = jest.fn();
    const { d } = deps({ isUp, onWaking });
    expect(await wakeThenOpen(d)).toBe('up');
    expect(d.wake).toHaveBeenCalledTimes(1);
    expect(onWaking).toHaveBeenCalledTimes(1);
    expect(isUp).toHaveBeenCalledTimes(4);
  });

  it('gives up after the timeout when the host never answers', async () => {
    const { d } = deps();
    expect(await wakeThenOpen(d)).toBe('timeout');
    expect(d.wake).toHaveBeenCalledTimes(1);
  });

  it('cannot wake a host with no MAC address', async () => {
    const { d } = deps({ mac: undefined });
    expect(await wakeThenOpen(d)).toBe('cannot-wake');
    expect(d.wake).not.toHaveBeenCalled();
  });

  it('reports a packet that could not be sent', async () => {
    const { d } = deps({ wake: jest.fn().mockResolvedValue(false) });
    expect(await wakeThenOpen(d)).toBe('send-failed');
  });

  it('stops polling when cancelled', async () => {
    let cancelled = false;
    const { d } = deps({ isCancelled: () => cancelled, sleep: jest.fn().mockImplementation(async () => { cancelled = true; }) });
    expect(await wakeThenOpen(d)).toBe('cancelled');
  });
});
