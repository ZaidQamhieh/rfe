import type { ShareLink } from '../../core/api/models';
import { liveLinks, withoutLink } from './activeShares';

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

describe('withoutLink', () => {
  it('removes only the revoked link', () => {
    expect(withoutLink([l('a', 1), l('b', 2)], 'a').map((x) => x.tokenHash)).toEqual(['b']);
  });
});
