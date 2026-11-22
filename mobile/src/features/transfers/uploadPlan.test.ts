import { cleanUploadName, planUploads } from './uploadPlan';

const pick = (...names: string[]) => names.map((name) => ({ name, uri: `file:///cache/${name}` }));

describe('upload plan', () => {
  it('uploads new names as they are', () => {
    expect(planUploads(pick('a.txt', 'b.txt'), new Set(), 'skip').map((i) => [i.targetName, i.overwrite])).toEqual([['a.txt', false], ['b.txt', false]]);
  });

  it('skips names that already exist when asked to skip', () => {
    expect(planUploads(pick('a.txt', 'b.txt'), new Set(['a.txt']), 'skip').map((i) => i.targetName)).toEqual(['b.txt']);
  });

  it('overwrites only the colliding names', () => {
    const plan = planUploads(pick('a.txt', 'b.txt'), new Set(['a.txt']), 'overwrite');
    expect(plan.map((i) => [i.targetName, i.overwrite])).toEqual([['a.txt', true], ['b.txt', false]]);
  });

  it('keeps both by numbering the new copy', () => {
    expect(planUploads(pick('a.txt'), new Set(['a.txt']), 'keepBoth').map((i) => i.targetName)).toEqual(['a (1).txt']);
  });

  it('never sends two picks to one target', () => {
    expect(planUploads(pick('a.txt', 'a.txt'), new Set(), 'skip').map((i) => i.targetName)).toEqual(['a.txt', 'a (1).txt']);
  });

  it('overwrites a repeated colliding name once and keeps the other picks apart', () => {
    const picked = [{ name: 'a.jpg', uri: 'file:///u1' }, { name: 'a.jpg', uri: 'file:///u2' }, { name: 'a.jpg', uri: 'file:///u3' }];
    const plan = planUploads(picked, new Set(['a.jpg']), 'overwrite');
    expect(plan.map((i) => [i.targetName, i.overwrite])).toEqual([['a.jpg', true], ['a (1).jpg', false], ['a (2).jpg', false]]);
  });

  it('strips directories and control characters from picked names', () => {
    expect(cleanUploadName('../../etc/passwd')).toBe('passwd');
    expect(cleanUploadName('C:\\x\\y.txt')).toBe('y.txt');
    expect(cleanUploadName('a\u0000b.txt')).toBe('ab.txt');
    expect(cleanUploadName('..')).toBe('file');
    expect(cleanUploadName('')).toBe('file');
  });
});
