import type { Entry } from '../../core/api/models';
import { compareVerdict, sizeVerdict } from './compareFiles';

const f = (name: string, size?: number, isDir = false): Entry => ({ name, path: `/r/${name}`, isDir, isSymlink: false, size });

describe('sizeVerdict', () => {
  it('says different without hashing when the sizes differ', () => {
    expect(sizeVerdict(f('a', 10), f('b', 11))).toBe('different');
  });
  it('needs the hashes when the sizes match or are unknown', () => {
    expect(sizeVerdict(f('a', 10), f('b', 10))).toBeNull();
    expect(sizeVerdict(f('a'), f('b', 10))).toBeNull();
  });
});

describe('compareVerdict', () => {
  const a = '/r/a';
  const b = '/r/b';
  it('same when both hashes are equal, ignoring case', () => {
    expect(compareVerdict({ [a]: 'ABC', [b]: 'abc' }, a, b)).toBe('same');
  });
  it('different when the hashes differ', () => {
    expect(compareVerdict({ [a]: 'abc', [b]: 'abd' }, a, b)).toBe('different');
  });
  it('unknown when either hash is missing: never claims a match it did not see', () => {
    expect(compareVerdict({ [a]: 'abc' }, a, b)).toBe('unknown');
    expect(compareVerdict({}, a, b)).toBe('unknown');
    expect(compareVerdict({ [a]: '', [b]: '' }, a, b)).toBe('unknown');
  });
});
