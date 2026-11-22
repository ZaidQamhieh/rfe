import type { Entry } from '../../core/api/models';
import { collectKeptFiles, type KeptFilesDeps } from './keptFiles';

const file = (path: string, isDir = false) => ({ path, name: path.split('/').pop()!, isDir }) as Entry;

/** Fake cache: one folder map, and a set of paths whose body is stored. */
const deps = (folders: Record<string, Entry[]>, kept: string[]): KeptFilesDeps => ({
  listing: async (_h, p) => (folders[p] ? { entries: folders[p] } : null),
  has: async (_h, p) => kept.includes(p),
});

describe('collectKeptFiles', () => {
  it('lists only files whose body is stored, from this computer\'s kept folders', async () => {
    const folders = { '/a': [file('/a/x.pdf'), file('/a/y.txt'), file('/a/sub', true)], '/b': [file('/b/z.dxf')] };
    const pins = [
      { hostId: 'h1', remotePath: '/a' },
      { hostId: 'h2', remotePath: '/b' },
    ];
    const out = await collectKeptFiles('h1', pins, deps(folders, ['/a/x.pdf', '/a/sub', '/b/z.dxf']));
    expect(out.map((e) => e.path)).toEqual(['/a/x.pdf']);
  });

  it('stops at the limit and skips a folder that was never cached', async () => {
    const folders = { '/a': [file('/a/1'), file('/a/2'), file('/a/3')] };
    const pins = [
      { hostId: 'h1', remotePath: '/gone' },
      { hostId: 'h1', remotePath: '/a' },
    ];
    const out = await collectKeptFiles('h1', pins, deps(folders, ['/a/1', '/a/2', '/a/3']), 2);
    expect(out.map((e) => e.path)).toEqual(['/a/1', '/a/2']);
  });

  it('leaves a file out when its lookup throws, and keeps the others', async () => {
    const d: KeptFilesDeps = {
      listing: async () => ({ entries: [file('/a/bad'), file('/a/ok')] }),
      has: async (_h, p) => {
        if (p === '/a/bad') throw new Error('vault');
        return true;
      },
    };
    expect((await collectKeptFiles('h1', [{ hostId: 'h1', remotePath: '/a' }], d)).map((e) => e.path)).toEqual(['/a/ok']);
  });
});
