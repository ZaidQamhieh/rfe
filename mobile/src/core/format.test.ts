import { formatDate, formatDuration, formatRelative, formatSize } from './format';
import { aggregateUsage, usedFraction } from './storage/usage';
import { magicPacket, parseMac } from './wol';

test('formatSize thresholds match the Flutter formatter', () => {
  expect(formatSize(null)).toBe('');
  expect(formatSize(0)).toBe('0 B');
  expect(formatSize(1023)).toBe('1023 B');
  expect(formatSize(1024)).toBe('1.0 KB');
  expect(formatSize(1536)).toBe('1.5 KB');
  expect(formatSize(1048576)).toBe('1.0 MB');
  expect(formatSize(1073741824)).toBe('1.00 GB');
});

test('formatDuration and dates', () => {
  expect(formatDuration(59)).toBe('0:59');
  expect(formatDuration(3661)).toBe('1:01:01');
  expect(formatDuration(-5)).toBe('0:00');
  expect(formatDate(new Date(2026, 8, 5))).toBe('2026-09-05');
  const now = new Date(2026, 8, 29, 12, 0, 0);
  expect(formatRelative(new Date(now.getTime() - 30_000), now)).toBe('just now');
  expect(formatRelative(new Date(now.getTime() - 5 * 60_000), now)).toBe('5m ago');
  expect(formatRelative(new Date(now.getTime() - 3 * 3600_000), now)).toBe('3h ago');
  expect(formatRelative(new Date(now.getTime() - 2 * 86400_000), now)).toBe('2d ago');
  expect(formatRelative(new Date(2026, 7, 1), now)).toBe('2026-08-01');
});

test('drive usage clamps bad data and skips unusable drives', () => {
  expect(usedFraction({ path: '/', totalBytes: 100, freeBytes: 25, isOS: false })).toBe(0.75);
  expect(usedFraction({ path: '/', totalBytes: 100, freeBytes: 500, isOS: false })).toBe(0);
  expect(usedFraction({ path: '/', totalBytes: 0, freeBytes: 0, isOS: false })).toBeNull();
  expect(usedFraction({ path: '/', freeBytes: 1, isOS: false })).toBeNull();
  const agg = aggregateUsage([
    { path: 'a', totalBytes: 100, freeBytes: 50, isOS: false },
    { path: 'b', totalBytes: 100, freeBytes: 900, isOS: false },
    { path: 'c', isOS: false },
  ]);
  expect(agg).toEqual({ totalBytes: 200, freeBytes: 150, usedFraction: 0.25 });
  expect(aggregateUsage([])).toBeNull();
});

test('wake-on-lan packet', () => {
  expect(parseMac('aa:bb:cc:dd:ee:0f')).toEqual([0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x0f]);
  expect(parseMac('aa:bb:cc')).toBeNull();
  expect(parseMac('zz:bb:cc:dd:ee:ff')).toBeNull();
  const p = magicPacket(parseMac('01:02:03:04:05:06')!);
  expect(p).toHaveLength(102);
  expect(p.slice(0, 6)).toEqual([255, 255, 255, 255, 255, 255]);
  expect(p.slice(96)).toEqual([1, 2, 3, 4, 5, 6]);
});
