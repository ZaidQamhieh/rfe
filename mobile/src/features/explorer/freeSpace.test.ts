import type { Drive } from '../../core/api/models';
import { driveFor, uploadSpaceShortfall } from './freeSpace';

const d = (path: string, freeBytes?: number, totalBytes?: number): Drive => ({ path, freeBytes, totalBytes, isOS: false });

describe('driveFor', () => {
  it('picks the drive with the longest matching mount path', () => {
    const drives = [d('/', 100), d('/home', 50), d('/home/x/big', 10)];
    expect(driveFor(drives, '/home/x/big/in')?.path).toBe('/home/x/big');
    expect(driveFor(drives, '/home/x')?.path).toBe('/home');
    expect(driveFor(drives, '/etc')?.path).toBe('/');
  });
  it('does not match a sibling that only shares a name prefix', () => {
    expect(driveFor([d('/mnt/a', 1), d('/', 2)], '/mnt/ab/x')?.path).toBe('/');
  });
  it('handles Windows drives, ignoring case and separators', () => {
    expect(driveFor([d('C:\\', 5), d('D:\\', 7)], 'd:\\Photos')?.path).toBe('D:\\');
  });
  it('is undefined when nothing covers the path', () => {
    expect(driveFor([d('/mnt/a', 1)], '/etc')).toBeUndefined();
  });
});

describe('uploadSpaceShortfall', () => {
  it('reports the free space when the files will not fit', () => {
    expect(uploadSpaceShortfall([d('/', 1000)], '/x', 1500)).toEqual({ needed: 1500, free: 1000 });
  });
  it('is null when they fit or when free space is unknown', () => {
    expect(uploadSpaceShortfall([d('/', 1000)], '/x', 1000)).toBeNull();
    expect(uploadSpaceShortfall([d('/')], '/x', 10 ** 12)).toBeNull();
    expect(uploadSpaceShortfall([], '/x', 10)).toBeNull();
    // The agent reports 0 and 0 when it could not read the drive: that is unknown, not full.
    expect(uploadSpaceShortfall([d('/', 0, 0)], '/x', 10)).toBeNull();
    expect(uploadSpaceShortfall([d('/', 0, 500)], '/x', 10)).toEqual({ needed: 10, free: 0 });
    expect(uploadSpaceShortfall([d('/', 5)], '/x', 0)).toBeNull();
  });
});
