import { calendarDaysBetween, formatDate, formatRate, formatSize, relativeLabel } from './format';

test('formatSize thresholds match the Flutter formatter', () => {
  expect(formatSize(null)).toBe('');
  expect(formatSize(0)).toBe('0 B');
  expect(formatSize(1023)).toBe('1023 B');
  expect(formatSize(1024)).toBe('1.0 KB');
  expect(formatSize(1536)).toBe('1.5 KB');
  expect(formatSize(1048576)).toBe('1.0 MB');
  expect(formatSize(1073741824)).toBe('1.00 GB');
});

test('formatRate appends /s to the size', () => {
  expect(formatRate(0)).toBe('0 B/s');
  expect(formatRate(1536)).toBe('1.5 KB/s');
});

test('dates', () => {
  expect(formatDate(new Date(2026, 8, 5))).toBe('2026-09-05');
  expect(calendarDaysBetween(new Date(2026, 8, 28, 23, 0), new Date(2026, 8, 29, 1, 0))).toBe(1);
  const now = new Date(2026, 8, 29, 12, 0, 0);
  expect(relativeLabel(new Date(now.getTime() - 2_000), now)).toBe('just now');
  expect(relativeLabel(new Date(now.getTime() - 30_000), now)).toBe('30s ago');
  expect(relativeLabel(new Date(now.getTime() - 5 * 60_000), now)).toBe('5m ago');
  expect(relativeLabel(new Date(now.getTime() - 3 * 3600_000), now)).toBe('3h ago');
  expect(relativeLabel(new Date(now.getTime() - 2 * 86400_000), now)).toBe('2d ago');
  expect(relativeLabel(new Date(2026, 7, 1), now)).toBe('59d ago');
});
