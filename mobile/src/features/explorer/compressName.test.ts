import { compressDefaultStem, freeZipName, ROOT_ARCHIVE_STEM, zipFileName } from './compressName';

describe('zipFileName', () => {
  it('adds .zip once', () => {
    expect(zipFileName('photos')).toBe('photos.zip');
    expect(zipFileName('a.tar')).toBe('a.tar.zip');
    expect(zipFileName('photos.zip')).toBe('photos.zip');
    expect(zipFileName('photos.ZIP')).toBe('photos.ZIP');
  });
});

describe('compressDefaultStem', () => {
  it('uses a single file without its extension and a single folder whole', () => {
    expect(compressDefaultStem({ name: 'report.pdf', isDir: false }, 'Docs')).toBe('report');
    expect(compressDefaultStem({ name: 'Photos.v2', isDir: true }, 'Docs')).toBe('Photos.v2');
    expect(compressDefaultStem({ name: '.bashrc', isDir: false }, 'Docs')).toBe('.bashrc');
  });

  it('uses the folder name for several items, or the archive name in a root', () => {
    expect(compressDefaultStem(null, 'Trip')).toBe('Trip');
    expect(compressDefaultStem(null, null)).toBe(ROOT_ARCHIVE_STEM);
  });
});

describe('freeZipName', () => {
  it('keeps a name nothing else uses', () => {
    expect(freeZipName('a.zip', [])).toBe('a.zip');
    expect(freeZipName('a.zip', ['a (1).zip', 'b.zip'])).toBe('a.zip');
  });

  it('suffixes the first free number when the name is taken', () => {
    expect(freeZipName('a.zip', ['a.zip'])).toBe('a (1).zip');
    expect(freeZipName('a.zip', ['a.zip', 'a (1).zip'])).toBe('a (2).zip');
  });

  it('counts a folder of the same name and ignores case', () => {
    expect(freeZipName('a.zip', ['A.ZIP'])).toBe('a (1).zip');
    expect(freeZipName('a.zip', ['a.zip'])).not.toBe('a.zip');
  });

  it('never returns a name that is already taken', () => {
    const taken = ['x.zip', 'x (1).zip', 'x (2).zip', 'X (3).ZIP'];
    const picked = freeZipName('x.zip', taken);
    expect(taken.map((n) => n.toLowerCase())).not.toContain(picked.toLowerCase());
    expect(picked).toBe('x (4).zip');
  });
});
