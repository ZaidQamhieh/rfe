import type { Listing } from '../../core/api/models';
import { collectFiles, computeWaste, defaultDeletions, groupDuplicates, scanForDuplicates, withoutPaths, type DupClient } from './dupFinder';

const file = (path: string, size: number) => ({ name: path.split('/').pop()!, path, isDir: false, isSymlink: false, size });
const dir = (path: string) => ({ name: path.split('/').pop()!, path, isDir: true, isSymlink: false });

function fakeClient(tree: Record<string, Listing[]>, hashes: Record<string, string>) {
  const calls = { list: [] as string[], sums: [] as string[][] };
  const client: DupClient = {
    async list(path, o = {}) {
      calls.list.push(`${path}@${o.cursor ?? ''}`);
      const pages = tree[path] ?? [{ path, entries: [] }];
      const i = o.cursor ? Number(o.cursor) : 0;
      return pages[i];
    },
    async batchChecksums(paths) {
      calls.sums.push(paths);
      return Object.fromEntries(paths.filter((p) => p in hashes).map((p) => [p, hashes[p]]));
    },
  };
  return { client, calls };
}

describe('collectFiles', () => {
  it('recurses and pages through every directory', async () => {
    const { client, calls } = fakeClient(
      {
        '/r': [
          { path: '/r', entries: [file('/r/a', 1), dir('/r/sub')], nextCursor: '1' },
          { path: '/r', entries: [file('/r/b', 2)] },
        ],
        '/r/sub': [{ path: '/r/sub', entries: [file('/r/sub/c', 3)] }],
      },
      {},
    );
    const seen: number[] = [];
    const out = await collectFiles(client, '/r', (n) => seen.push(n));
    expect(out.paths).toEqual(['/r/a', '/r/sub/c', '/r/b']);
    expect(out.sizes).toEqual({ '/r/a': 1, '/r/sub/c': 3, '/r/b': 2 });
    expect(calls.list).toEqual(['/r@', '/r/sub@', '/r@1']);
    expect(seen).toEqual([1, 2, 3]);
  });
});

describe('grouping', () => {
  const hashes = { '/a': 'h1', '/b': 'h1', '/c': 'h2', '/d': 'h2', '/e': 'h2', '/f': 'h3' };
  const sizes = { '/a': 10, '/b': 10, '/c': 100, '/d': 100, '/e': 100, '/f': 5 };
  it('keeps only real duplicates, largest first, in walk order', () => {
    const groups = groupDuplicates(hashes, sizes, ['/e', '/a', '/b', '/c', '/d', '/f']);
    expect(groups.map((g) => g.paths)).toEqual([['/e', '/c', '/d'], ['/a', '/b']]);
  });
  it('waste counts every copy but one', () => {
    const groups = groupDuplicates(hashes, sizes);
    expect(computeWaste(groups, sizes)).toBe(100 * 2 + 10);
  });
  it('marks all but the first copy and prunes after deletion', () => {
    const groups = groupDuplicates(hashes, sizes);
    const del = defaultDeletions(groups);
    expect([...del].sort()).toEqual(['/b', '/d', '/e']);
    expect(withoutPaths(groups, del)).toEqual([]);
    expect(withoutPaths(groups, new Set(['/e']))).toHaveLength(2);
    expect(withoutPaths(groups, new Set(['/c', '/d']))).toEqual([{ hash: 'h1', paths: ['/a', '/b'] }]);
  });
});

describe('scanForDuplicates', () => {
  it('hashes in chunks of 500 and ignores unhashed files', async () => {
    const entries = Array.from({ length: 1001 }, (_, i) => file(`/r/f${i}`, 1));
    const hashes: Record<string, string> = { '/r/f0': 'same', '/r/f900': 'same' };
    const { client, calls } = fakeClient({ '/r': [{ path: '/r', entries }] }, hashes);
    const { groups } = await scanForDuplicates(client, '/r');
    expect(calls.sums.map((c) => c.length)).toEqual([500, 500, 1]);
    expect(groups).toEqual([{ hash: 'same', paths: ['/r/f0', '/r/f900'] }]);
  });
});
