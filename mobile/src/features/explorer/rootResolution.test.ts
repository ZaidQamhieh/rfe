import { allowedRootForPath, resolveRoots } from './rootResolution';

describe('allowedRootForPath', () => {
  it('picks the deepest containing root and never a sibling with a shared prefix', () => {
    const roots = ['/srv', '/srv/share', '/srv/share2'];
    expect(allowedRootForPath(roots, '/srv/share/a/b')).toBe('/srv/share');
    expect(allowedRootForPath(roots, '/srv/share2')).toBe('/srv/share2');
    expect(allowedRootForPath(roots, '/srv/shared')).toBe('/srv');
    expect(allowedRootForPath(roots, '/etc')).toBeNull();
    expect(allowedRootForPath(['/'], '/anything')).toBe('/');
  });
  it('handles Windows separators and case', () => {
    expect(allowedRootForPath(['C:\\Users'], 'c:\\users\\me\\x', true)).toBe('C:\\Users');
    expect(allowedRootForPath(['C:\\'], 'C:\\a', true)).toBe('C:\\');
    expect(allowedRootForPath(['C:\\Users'], 'c:\\users\\me', false)).toBeNull();
  });
});

describe('resolveRoots', () => {
  it('denied access wins over everything', () => {
    expect(resolveRoots({ roots: ['/a'], accessDenied: true }, null, '/a/x')).toEqual({ kind: 'denied' });
  });
  it('a saved path inside a root selects that root with the path', () => {
    expect(resolveRoots({ roots: ['/a', '/b'], accessDenied: false }, null, '/b/x')).toEqual({ kind: 'select', rootPath: '/b', initialPath: '/b/x' });
  });
  it('a saved path outside every root falls back to the picker and reports it', () => {
    expect(resolveRoots({ roots: ['/a'], accessDenied: false }, null, '/z')).toEqual({ kind: 'roots', roots: ['/a'], unavailablePath: '/z' });
  });
  it('no roots: Windows shows drives, POSIX opens /', () => {
    expect(resolveRoots({ roots: [], accessDenied: false }, { os: 'windows' })).toEqual({ kind: 'drives' });
    expect(resolveRoots({ roots: [], accessDenied: false }, { os: 'linux' })).toEqual({ kind: 'select', rootPath: '/', initialPath: undefined });
    expect(resolveRoots({ roots: [], accessDenied: false }, null, 'D:\\x')).toEqual({ kind: 'select', rootPath: 'D:\\', initialPath: 'D:\\x' });
  });
});
