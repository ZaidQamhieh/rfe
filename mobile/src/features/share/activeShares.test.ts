import type { ShareLink } from '../../core/api/models';
import { liveLinkForPath, liveLinks, rememberResolvedPath, withoutLink } from './activeShares';

const l = (hash: string, expiresAt: number, path = `/f/${hash}.txt`): ShareLink => ({ token: '', tokenHash: hash, path, expiresAt, url: '' });

describe('liveLinks', () => {
  it('drops expired links and lists the soonest-to-expire first', () => {
    const now = 1_000_000; // ms
    const out = liveLinks([l('late', 2000), l('gone', 500), l('soon', 1200)], now);
    expect(out.map((x) => x.tokenHash)).toEqual(['soon', 'late']);
  });
  it('treats a link expiring exactly now as expired', () => {
    expect(liveLinks([l('edge', 1000)], 1_000_000)).toEqual([]);
  });
});

describe('liveLinkForPath', () => {
  const now = 1_000_000;
  it('returns the soonest-expiring live link for the same path', () => {
    const links = [l('later', 5000, '/a.txt'), l('sooner', 2000, '/a.txt')];
    expect(liveLinkForPath(links, '/a.txt', now)?.tokenHash).toBe('sooner');
  });
  it('ignores expired links and links for other files', () => {
    const links = [l('gone', 500, '/a.txt'), l('other', 5000, '/b.txt')];
    expect(liveLinkForPath(links, '/a.txt', now)).toBeUndefined();
  });
});

describe('withoutLink', () => {
  it('removes only the revoked link', () => {
    expect(withoutLink([l('a', 1), l('b', 2)], 'a').map((x) => x.tokenHash)).toEqual(['b']);
  });
});

describe('a symlinked file', () => {
  it('finds the link recorded under its target once the pair is remembered', () => {
    const links = [{ tokenHash: 'x', path: '/real/a.txt', expiresAt: Math.floor(Date.now() / 1000) + 600 }] as never;
    expect(liveLinkForPath(links, '/link/a.txt')).toBeUndefined();
    rememberResolvedPath('/link/a.txt', '/real/a.txt');
    expect(liveLinkForPath(links, '/link/a.txt')?.tokenHash).toBe('x');
  });
});
