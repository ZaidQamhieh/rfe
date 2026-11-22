import { factRows, formatDateTime, formatDimensions, formatDuration } from './previewFacts';

describe('formatDateTime', () => {
  it('writes day, month name, year and 24-hour time in local time', () => {
    expect(formatDateTime(new Date(2026, 10, 8, 17, 2).getTime())).toBe('8 Nov 2026, 17:02');
    expect(formatDateTime(new Date(2026, 0, 3, 9, 5).getTime())).toBe('3 Jan 2026, 09:05');
  });
  it('is empty for a missing or invalid date', () => {
    expect(formatDateTime(undefined)).toBe('');
    expect(formatDateTime(NaN)).toBe('');
  });
});

describe('formatDuration', () => {
  it('shows minutes and seconds, adding hours only from one hour', () => {
    expect(formatDuration(65_000)).toBe('1:05');
    expect(formatDuration(3_723_900)).toBe('1:02:03');
    expect(formatDuration(900)).toBe('0:00');
  });
  it('is empty when missing, zero or negative', () => {
    expect(formatDuration(undefined)).toBe('');
    expect(formatDuration(0)).toBe('');
    expect(formatDuration(-5)).toBe('');
  });
});

describe('formatDimensions', () => {
  it('needs both sides', () => {
    expect(formatDimensions(4032, 3024)).toBe('4032 × 3024');
    expect(formatDimensions(4032, undefined)).toBe('');
    expect(formatDimensions(0, 10)).toBe('');
  });
});

describe('factRows', () => {
  const modified = new Date(2026, 10, 8, 19, 40).toISOString();

  it('lists only size and modified for a plain file', () => {
    const rows = factRows({ size: 3_355_443, modified });
    expect(rows.map((r) => r.labelKey)).toEqual(['previewFactSize', 'previewFactModified']);
    expect(rows[1].value).toBe('8 Nov 2026, 19:40');
  });

  it('adds taken, dimensions and length from media info, in reading order', () => {
    const rows = factRows({ size: 100, modified }, { takenAt: new Date(2026, 10, 8, 17, 2).getTime(), width: 4032, height: 3024, durationMs: 12_000 });
    expect(rows.map((r) => r.labelKey)).toEqual(['previewFactSize', 'previewFactTaken', 'previewFactDimensions', 'previewFactLength', 'previewFactModified']);
    expect(rows[2].value).toBe('4032 × 3024');
    expect(rows[3].value).toBe('0:12');
  });

  it('leaves out every row that has no value', () => {
    expect(factRows({}, {})).toEqual([]);
    expect(factRows({ size: 5 }, { width: 10 }).map((r) => r.labelKey)).toEqual(['previewFactSize']);
  });
});
