import { InFlightGate } from './inFlight';

describe('InFlightGate', () => {
  it('ignores a call made while another is still running', async () => {
    const gate = new InFlightGate();
    let release!: () => void;
    const first = gate.run(() => new Promise<string>((resolve) => { release = () => resolve('first'); }));
    const action = jest.fn(async () => 'second');
    expect(await gate.run(action)).toBeUndefined();
    expect(action).not.toHaveBeenCalled();
    release();
    expect(await first).toBe('first');
  });

  it('runs again once the previous call has finished, even if it failed', async () => {
    const gate = new InFlightGate();
    await expect(gate.run(async () => { throw new Error('boom'); })).rejects.toThrow('boom');
    expect(await gate.run(async () => 'ok')).toBe('ok');
  });
});
