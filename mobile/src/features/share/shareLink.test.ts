import { formatRemaining, remainingMs } from './shareLink';

describe('share link countdown', () => {
  it('formats minutes, hours and clamps at zero', () => {
    expect(formatRemaining(0)).toBe('00:00');
    expect(formatRemaining(-5000)).toBe('00:00');
    expect(formatRemaining(61_000)).toBe('01:01');
    expect(formatRemaining(3_600_000)).toBe('1:00:00');
    expect(formatRemaining(86_399_000)).toBe('23:59:59');
  });
  it('computes remaining time from unix seconds', () => {
    expect(remainingMs(100, 90_000)).toBe(10_000);
    expect(remainingMs(100, 200_000)).toBe(0);
  });
});
