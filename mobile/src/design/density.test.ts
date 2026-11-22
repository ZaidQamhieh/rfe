import { rowDensity } from './density';

describe('rowDensity', () => {
  it('compact rows are shorter and tighter than comfortable ones, in every number', () => {
    const c = rowDensity('compact');
    const f = rowDensity('comfortable');
    expect(c.rowMinHeight).toBeLessThan(f.rowMinHeight);
    expect(c.rowPaddingY).toBeLessThan(f.rowPaddingY);
    expect(c.frameMinHeight).toBeLessThan(f.frameMinHeight);
    expect(c.framePaddingY).toBeLessThan(f.framePaddingY);
  });
  it('comfortable keeps the sizes the rows always had', () => {
    expect(rowDensity('comfortable')).toMatchObject({ rowMinHeight: 64, rowPaddingY: 8, frameMinHeight: 64, framePaddingY: 12 });
  });
  it('compact still fits the 40 dp lead picture', () => {
    const c = rowDensity('compact');
    expect(c.rowMinHeight - 2 * c.rowPaddingY).toBeGreaterThanOrEqual(40);
  });
});
