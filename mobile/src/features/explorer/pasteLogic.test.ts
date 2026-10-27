import { destinationProblem, pasteSources, transferFullySucceeded } from './pasteLogic';

describe('pasteSources', () => {
  it('moves everything when nothing is already in the destination', () => {
    expect(pasteSources(['/a/x', '/b/y'], '/d', true)).toEqual(['/a/x', '/b/y']);
  });

  it('drops cut sources that already sit in the destination, so they cannot clash with themselves', () => {
    expect(pasteSources(['/d/x', '/b/y', '/d/z'], '/d', true)).toEqual(['/b/y']);
  });

  it('leaves nothing when every cut source is already there', () => {
    expect(pasteSources(['/d/x', '/d/z'], '/d', true)).toEqual([]);
  });

  it('keeps a same-folder copy, which is a legitimate duplicate', () => {
    expect(pasteSources(['/d/x'], '/d', false)).toEqual(['/d/x']);
  });

  it('works with Windows separators', () => {
    expect(pasteSources(['C:\\d\\x', 'C:\\b\\y'], 'C:\\d', true)).toEqual(['C:\\b\\y']);
  });
});

describe('destinationProblem', () => {
  it('refuses a folder moved or copied into itself or below itself', () => {
    expect(destinationProblem(['/a/dir'], '/a/dir')).toBe('inside-itself');
    expect(destinationProblem(['/a/dir'], '/a/dir/sub/deeper')).toBe('inside-itself');
    expect(destinationProblem(['/x', 'C:\\a\\dir'], 'C:\\a\\dir\\sub')).toBe('inside-itself');
  });
  it('allows siblings that only share a name prefix, and unrelated folders', () => {
    expect(destinationProblem(['/a/dir'], '/a/dir2')).toBeNull();
    expect(destinationProblem(['/a/dir', '/a/f.txt'], '/b')).toBeNull();
    expect(destinationProblem(['/a/dir'], '/')).toBeNull();
  });
});

describe('transferFullySucceeded (F-154)', () => {
  it('is true only when no item failed', () => {
    expect(transferFullySucceeded({ failed: [] })).toBe(true);
    expect(transferFullySucceeded({ failed: [{ path: '/a' }] })).toBe(false);
  });
});
