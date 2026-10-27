import type { Entry } from '../../core/api/models';
import { runSync, type LocalInfo, type SyncDeps } from './syncRunner';

const file = (name: string, size: number, modified = '2026-09-30T10:00:00Z'): Entry => ({ name, path: `/d/${name}`, isDir: false, isSymlink: false, size, modified });
const t0 = Date.parse('2026-09-30T10:00:00Z');

function setup(remote: Entry[], local: Record<string, LocalInfo>, failing: string[] = []) {
  const put: string[] = [];
  const discarded: string[] = [];
  const fetched: string[] = [];
  const deps: SyncDeps = {
    listRemote: async () => remote,
    fetch: async (e, dest) => {
      fetched.push(e.name);
      if (failing.includes(e.name)) throw new Error('network');
      expect(dest).toBe(`stage/${e.name}`);
    },
    staging: (n) => `stage/${n}`,
    discard: (p) => void discarded.push(p),
    local: { list: async () => new Map(Object.entries(local)), put: async (n) => void put.push(n) },
  };
  return { deps, put, discarded, fetched };
}

test('downloads only what is missing or changed, and skips folders', async () => {
  const s = setup(
    [file('new.txt', 5), file('same.txt', 5), file('grown.txt', 9), { ...file('sub', 0), isDir: true }, file('edited.txt', 5, '2026-09-30T12:00:00Z')],
    { 'same.txt': { size: 5, modified: t0 + 5000 }, 'grown.txt': { size: 5, modified: t0 + 5000 }, 'edited.txt': { size: 5, modified: t0 } },
  );
  const r = await runSync('/d', s.deps);
  expect(s.fetched).toEqual(['new.txt', 'grown.txt', 'edited.txt']);
  expect(s.put).toEqual(['new.txt', 'grown.txt', 'edited.txt']);
  expect(r).toEqual({ downloaded: 3, failed: [], skipped: 0 });
});

test('a failed file is reported, its staging file discarded, and the rest still sync', async () => {
  const s = setup([file('a', 1), file('b', 1), file('c', 1)], {}, ['b']);
  const r = await runSync('/d', s.deps);
  expect(r).toEqual({ downloaded: 2, failed: ['b'], skipped: 0, firstError: 'network' });
  expect(s.put).toEqual(['a', 'c']);
  expect(s.discarded).toEqual(['stage/a', 'stage/b', 'stage/c']);
});

test('unsafe remote names are skipped, progress is reported, and cancelling stops the pass', async () => {
  const s = setup([file('ok', 1), file('../evil', 1), file('later', 1)], {});
  const seen: string[] = [];
  let calls = 0;
  const r = await runSync('/d', s.deps, (p) => seen.push(`${p.current}/${p.total} ${p.name}`), () => ++calls > 2);
  expect(seen).toEqual(['1/3 ok', '2/3 ../evil']);
  expect(r).toEqual({ downloaded: 1, failed: [], skipped: 1 });
  expect(s.fetched).toEqual(['ok']);
});

test('names that are not one path segment never reach staging, including "." and ".." (F-31)', async () => {
  const s = setup([file('..', 1), file('.', 1), file('a/b', 1), file('ok', 1)], {});
  const staged: string[] = [];
  const staging = s.deps.staging;
  s.deps.staging = (n) => (staged.push(n), staging(n));
  const r = await runSync('/d', s.deps);
  expect(staged).toEqual(['ok']);
  expect(r).toEqual({ downloaded: 1, failed: [], skipped: 3 });
});
