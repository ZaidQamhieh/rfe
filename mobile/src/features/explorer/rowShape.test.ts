import { rowShape } from './rowShape';

const d = (isDir: boolean) => ({ isDir });

describe('rowShape', () => {
  it('folders are standalone cards', () => {
    expect(rowShape([d(true), d(true)], 0)).toEqual({ first: true, last: true });
    expect(rowShape([d(true), d(true)], 1)).toEqual({ first: true, last: true });
  });

  it('consecutive files share one card: first, middle, last', () => {
    const list = [d(true), d(false), d(false), d(false)];
    expect(rowShape(list, 1)).toEqual({ first: true, last: false });
    expect(rowShape(list, 2)).toEqual({ first: false, last: false });
    expect(rowShape(list, 3)).toEqual({ first: false, last: true });
  });

  it('a lone file is its own card', () => {
    expect(rowShape([d(true), d(false)], 1)).toEqual({ first: true, last: true });
    expect(rowShape([d(false), d(true)], 0)).toEqual({ first: true, last: true });
  });
});
