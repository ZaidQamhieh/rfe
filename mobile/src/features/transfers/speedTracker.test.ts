import { etaSeconds, formatEta, formatSpeed, SpeedTracker } from './speedTracker';

function tracker() {
  let now = 0;
  const tr = new SpeedTracker(() => now, 0.5, 500);
  return { tr, at: (ms: number) => (now = ms) };
}

describe('SpeedTracker', () => {
  it('needs two samples and then reports bytes per second', () => {
    const { tr, at } = tracker();
    at(0);
    expect(tr.sample('a', 0, true)).toBeNull();
    at(1000);
    expect(tr.sample('a', 1000, true)).toBe(1000);
  });
  it('smooths later samples', () => {
    const { tr, at } = tracker();
    at(0);
    tr.sample('a', 0, true);
    at(1000);
    tr.sample('a', 1000, true); // 1000 B/s
    at(2000);
    expect(tr.sample('a', 4000, true)).toBe(2000); // 0.5 * 3000 + 0.5 * 1000
  });
  it('ignores samples closer than the minimum gap', () => {
    const { tr, at } = tracker();
    at(0);
    tr.sample('a', 0, true);
    at(1000);
    tr.sample('a', 1000, true);
    at(1100);
    expect(tr.sample('a', 9999, true)).toBe(1000);
  });
  it('restarts when bytes go backwards and forgets ids that stop running', () => {
    const { tr, at } = tracker();
    at(0);
    tr.sample('a', 5000, true);
    at(1000);
    tr.sample('a', 6000, true);
    at(2000);
    expect(tr.sample('a', 100, true)).toBeNull();
    expect(tr.sample('a', 100, false)).toBeNull();
    at(3000);
    expect(tr.sample('a', 200, true)).toBeNull();
  });
});

describe('eta and formatting', () => {
  it('computes seconds left and refuses unknowns', () => {
    expect(etaSeconds(1000, 500, 100)).toBe(5);
    expect(etaSeconds(0, 0, 100)).toBeNull();
    expect(etaSeconds(1000, 500, null)).toBeNull();
    expect(etaSeconds(1000, 500, 0)).toBeNull();
    expect(etaSeconds(1000, 1000, 100)).toBeNull();
  });
  it('formats short, minute and hour spans', () => {
    expect(formatEta(0.2)).toBe('1 s');
    expect(formatEta(42)).toBe('42 s');
    expect(formatEta(150)).toBe('3 min');
    expect(formatEta(3600)).toBe('1 h');
    expect(formatEta(3900)).toBe('1 h 5 min');
    expect(formatSpeed(3 * 1024 * 1024)).toBe('3.0 MB/s');
  });
});
