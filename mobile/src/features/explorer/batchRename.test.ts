import { computeBatchRenames, splitNameExt } from './batchRename';

describe('splitNameExt', () => {
  it('keeps dotfiles and trailing dots in the stem', () => {
    expect(splitNameExt('a.txt')).toEqual({ stem: 'a', ext: '.txt' });
    expect(splitNameExt('a.tar.gz')).toEqual({ stem: 'a.tar', ext: '.gz' });
    expect(splitNameExt('.bashrc')).toEqual({ stem: '.bashrc', ext: '' });
    expect(splitNameExt('name.')).toEqual({ stem: 'name.', ext: '' });
    expect(splitNameExt('README')).toEqual({ stem: 'README', ext: '' });
  });
});

describe('computeBatchRenames', () => {
  it('numbers with the base name, padding to the widest index and keeping extensions', () => {
    const names = Array.from({ length: 12 }, (_, i) => `img${i}.jpg`);
    const out = computeBatchRenames({ names, mode: 'pattern', base: 'Trip' });
    expect(out[0]).toBe('Trip 01.jpg');
    expect(out[11]).toBe('Trip 12.jpg');
  });
  it('places the number at the {n} token and honours the start number', () => {
    expect(computeBatchRenames({ names: ['a.png', 'b'], mode: 'pattern', base: 'pic_{n}_x', startNumber: 9 })).toEqual(['pic_09_x.png', 'pic_10_x']);
  });
  it('falls back to "file" for an empty base', () => {
    expect(computeBatchRenames({ names: ['a.txt'], mode: 'pattern', base: '' })).toEqual(['file 1.txt']);
  });
  it('find/replace hits every occurrence including the extension, and empty find changes nothing', () => {
    expect(computeBatchRenames({ names: ['aa.aa', 'b'], mode: 'findReplace', find: 'a', replace: 'x' })).toEqual(['xx.xx', 'b']);
    const names = ['keep.txt'];
    const out = computeBatchRenames({ names, mode: 'findReplace', find: '', replace: 'x' });
    expect(out).toEqual(names);
    expect(out).not.toBe(names);
  });
  it('treats regex characters in find literally', () => {
    expect(computeBatchRenames({ names: ['a.b (1).txt'], mode: 'findReplace', find: ' (1)', replace: '' })).toEqual(['a.b.txt']);
  });
});
