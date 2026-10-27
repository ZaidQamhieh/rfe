import type { Drive } from '../../core/api/models';
import { aggregateByExtension, bucketBySize, categoryFor, extensionOf, ringSegments, sortedBySize } from './storageLogic';

describe('extensionOf', () => {
  it('keeps the dot and ignores dotfiles and trailing dots', () => {
    expect(extensionOf('a.tar.gz')).toBe('.gz');
    expect(extensionOf('.bashrc')).toBe('');
    expect(extensionOf('name.')).toBe('');
    expect(extensionOf('README')).toBe('');
  });
});

describe('aggregation', () => {
  const agg = aggregateByExtension([
    { ext: '.JPG', size: 100 },
    { ext: '.jpg', size: 50 },
    { ext: '', size: 5 },
    { ext: '.zip', size: 400 },
  ]);
  it('folds case and names the extensionless group', () => {
    expect(agg.sizeByExt.get('.jpg')).toBe(150);
    expect(agg.countByExt.get('.jpg')).toBe(2);
    expect(agg.sizeByExt.get('(no ext)')).toBe(5);
    expect(agg.totalSize).toBe(555);
    expect(agg.totalFiles).toBe(4);
  });
  it('sorts by size and buckets into the four map blocks', () => {
    const rows = sortedBySize(agg);
    expect(rows.map((r) => r.ext)).toEqual(['.zip', '.jpg', '(no ext)']);
    expect(bucketBySize(rows)).toEqual([
      { label: 'Archives', bytes: 400 },
      { label: 'Photos & Video', bytes: 150 },
      { label: 'Other', bytes: 5 },
    ]);
  });
  it('categorises by extension case-insensitively', () => {
    expect(categoryFor('.MKV')).toBe('video');
    expect(categoryFor('.rs')).toBe('code');
    expect(categoryFor('(no ext)')).toBe('other');
  });
});

describe('ringSegments', () => {
  const d = (path: string, totalBytes?: number, freeBytes?: number): Drive => ({ path, totalBytes, freeBytes, isOS: false });
  it('gives each capable drive its used share and a free remainder that sums to 1', () => {
    const segs = ringSegments([d('/', 100, 40), d('/data', 300, 300), d('/odd')]);
    expect(segs.map((s) => s.key)).toEqual(['/', '/data', 'free']);
    expect(segs.reduce((n, s) => n + s.fraction, 0)).toBeCloseTo(1);
    expect(segs[0].fraction).toBeCloseTo(60 / 400);
    expect(segs[2].fraction).toBeCloseTo(340 / 400);
  });
  it('is empty when nothing reports capacity, and caps bogus free space', () => {
    expect(ringSegments([d('/')])).toEqual([]);
    expect(ringSegments([d('/', 100, 500)])[0].fraction).toBe(0);
  });
});
