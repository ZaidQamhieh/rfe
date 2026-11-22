import { widgetSnapshot } from './widgetLogic';

const GB = 1024 ** 3;

describe('widgetSnapshot', () => {
  it('sums the drives that report sizes and rounds the used share', () => {
    const s = widgetSnapshot('Office PC', [
      { path: 'C:', isOS: true, totalBytes: 100 * GB, freeBytes: 25 * GB },
      { path: 'D:', isOS: false, totalBytes: 100 * GB, freeBytes: 75 * GB },
      { path: 'E:', isOS: false },
    ]);
    expect(s?.title).toBe('Office PC');
    expect(s?.percent).toBe(50);
    expect(s?.detail).toContain('free of');
  });

  it('says so when drives report no size, is null with no drives, and clamps free above total', () => {
    expect(widgetSnapshot('x', [{ path: '/', isOS: true }])).toEqual({ title: 'x', detail: 'Storage size not reported', percent: 0 });
    expect(widgetSnapshot('x', [])).toBeNull();
    expect(widgetSnapshot('x', [{ path: '/', isOS: true, totalBytes: 10, freeBytes: 50 }])?.percent).toBe(0);
  });
});
