import { bucketOf } from './recentBuckets';

const now = new Date(2026, 5, 15, 9, 30);
const at = (y: number, m: number, d: number, h = 12) => new Date(y, m, d, h).toISOString();

describe('recent buckets', () => {
  it('uses calendar days, not 24-hour windows', () => {
    expect(bucketOf(at(2026, 5, 15, 0), now)).toBe('Today');
    expect(bucketOf(at(2026, 5, 14, 23), now)).toBe('Yesterday');
    expect(bucketOf(at(2026, 5, 13, 23), now)).toBe('Earlier');
  });
  it('puts missing, invalid and future times sensibly', () => {
    expect(bucketOf(undefined, now)).toBe('Earlier');
    expect(bucketOf('not a date', now)).toBe('Earlier');
    expect(bucketOf(at(2026, 5, 16), now)).toBe('Today');
  });
});
