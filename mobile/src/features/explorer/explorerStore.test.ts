import type { AgentClient } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import { ListingCache, MemoryListingBackend } from '../../core/storage/listingCache';
import { defaultVisibility } from '../../core/visibility';
import { createExplorer, currentPath, deriveDisplay } from './explorerStore';
import type { UndoOp } from './undoPlan';

const e = (name: string, dir = false): Entry => ({ name, path: `/r/${name}`, isDir: dir, isSymlink: false });
const tick = () => new Promise((r) => setTimeout(r, 0));

function setup(client: Partial<Record<keyof AgentClient, jest.Mock>>) {
  const cache = new ListingCache(new MemoryListingBackend(), 3);
  const ex = createExplorer({ hostId: 'h', rootPath: '/r', getClient: async () => client as unknown as AgentClient, cache, humanize: (x) => (x as Error).message });
  return { ex, cache };
}

describe('load / cache / offline', () => {
  it('loads, caches, and falls back to cache with offline=true when the host is down', async () => {
    const list = jest.fn().mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c' }).mockRejectedValueOnce(new Error('down'));
    const { ex } = setup({ list });
    await ex.load();
    expect(ex.getState()).toMatchObject({ loading: false, stale: false, offline: false, nextCursor: 'c' });
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a']);
    await ex.load();
    expect(ex.getState()).toMatchObject({ offline: true, stale: true, error: null });
    expect(ex.getState().entries).toHaveLength(1);
  });

  it('a folder that fails with no cache does not show the previous folder\'s rows or offline flag', async () => {
    const list = jest.fn().mockResolvedValueOnce({ path: '/r', entries: [e('a')] }).mockRejectedValueOnce(new Error('down')).mockRejectedValueOnce(new Error('down'));
    const { ex } = setup({ list });
    await ex.load();
    await ex.load(); // the parent now comes from cache while the host is down
    expect(ex.getState().offline).toBe(true);
    ex.navigate('/r/sub');
    await ex.load();
    expect(ex.getState()).toMatchObject({ entries: [], offline: false, stale: false, error: 'down' });
  });

  it('keeps the agent\'s error code, and clears it on the next good listing', async () => {
    const forbidden = Object.assign(new Error('path is outside allowed root'), { code: 'FORBIDDEN' });
    const list = jest.fn().mockRejectedValueOnce(forbidden).mockResolvedValueOnce({ path: '/r', entries: [e('a')] });
    const { ex } = setup({ list });
    await ex.load();
    expect(ex.getState()).toMatchObject({ error: 'path is outside allowed root', errorCode: 'FORBIDDEN' });
    await ex.load();
    expect(ex.getState()).toMatchObject({ error: null, errorCode: null });
  });

  it('reports the error when there is no cache', async () => {
    const { ex } = setup({ list: jest.fn().mockRejectedValue(new Error('boom')) });
    await ex.load();
    expect(ex.getState()).toMatchObject({ error: 'boom', loading: false });
  });

  it('a stale overlapping load never clobbers the newer result (ABA)', async () => {
    let releaseFirst!: (v: unknown) => void;
    const list = jest
      .fn()
      .mockImplementationOnce(() => new Promise((r) => (releaseFirst = r)))
      .mockResolvedValueOnce({ path: '/r', entries: [e('fresh')] });
    const { ex } = setup({ list });
    const first = ex.load();
    await tick();
    const second = ex.load();
    await second;
    releaseFirst({ path: '/r', entries: [e('stale')] });
    await first;
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['fresh']);
  });

  it('paginates and appends, caching the merged list', async () => {
    const list = jest
      .fn()
      .mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c1' })
      .mockResolvedValueOnce({ path: '/r', entries: [e('b')] });
    const { ex, cache } = setup({ list });
    await ex.load();
    await ex.loadMore();
    expect(list).toHaveBeenLastCalledWith('/r', { cursor: 'c1' });
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a', 'b']);
    expect(ex.getState().nextCursor).toBeNull();
    expect((await cache.get('h', '/r'))!.entries).toHaveLength(2);
    await ex.loadMore(); // no cursor: no-op
    expect(list).toHaveBeenCalledTimes(2);
  });
});

describe('navigation and selection', () => {
  it('navigates, pops, jumps within the root, and clears selection on every load', async () => {
    const list = jest.fn().mockResolvedValue({ path: '/r', entries: [e('a')] });
    const { ex } = setup({ list });
    await ex.load();
    ex.toggleSelect('/r/a');
    expect(ex.getState().selected.has('/r/a')).toBe(true);
    ex.navigate('/r/a');
    await tick();
    expect(currentPath(ex.getState())).toBe('/r/a');
    expect(ex.getState().selected.size).toBe(0);
    expect(ex.popDirectory()).toBe(true);
    expect(ex.popDirectory()).toBe(false);
    ex.jumpTo('/r/x/y');
    expect(ex.getState().pathStack).toEqual(['/r', '/r/x', '/r/x/y']);
    ex.jumpTo('/etc');
    expect(ex.getState().pathStack).toEqual(['/r']);
  });

  it('select all / invert operate on displayed (visible) entries only', async () => {
    const list = jest.fn().mockResolvedValue({ path: '/r', entries: [e('a'), e('.hidden'), e('b')] });
    const { ex } = setup({ list });
    await ex.load();
    const { display, hiddenCount } = deriveDisplay(ex.getState(), { field: 'name', ascending: true }, defaultVisibility());
    expect(hiddenCount).toBe(1);
    ex.selectAll(display);
    expect([...ex.getState().selected].sort()).toEqual(['/r/a', '/r/b']);
    ex.toggleSelect('/r/a');
    ex.invertSelection(display);
    expect([...ex.getState().selected]).toEqual(['/r/a']);
    ex.toggleShowHidden();
    expect(deriveDisplay(ex.getState(), { field: 'name', ascending: true }, defaultVisibility()).display).toHaveLength(3);
  });
});

describe('loadMore failures', () => {
  it('shows the error, stops asking on its own, and a retry loads the page', async () => {
    const list = jest
      .fn()
      .mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c1' })
      .mockRejectedValueOnce(new Error('timed out'))
      .mockResolvedValueOnce({ path: '/r', entries: [e('b')] });
    const { ex } = setup({ list });
    await ex.load();

    await ex.loadMore();
    expect(ex.getState()).toMatchObject({ loadingMore: false, loadMoreError: 'timed out', nextCursor: 'c1' });
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a']);

    // The list asks again when it is still at the end; that must not hammer a failing host.
    await ex.loadMore();
    expect(list).toHaveBeenCalledTimes(2);

    await ex.loadMore({ retry: true });
    expect(ex.getState()).toMatchObject({ loadMoreError: null, nextCursor: null });
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a', 'b']);
  });

  it('a refresh clears the error', async () => {
    const list = jest
      .fn()
      .mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c1' })
      .mockRejectedValueOnce(new Error('timed out'))
      .mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c1' });
    const { ex } = setup({ list });
    await ex.load();
    await ex.loadMore();
    expect(ex.getState().loadMoreError).toBe('timed out');
    await ex.refresh();
    expect(ex.getState().loadMoreError).toBeNull();
  });
});

describe('operations', () => {
  it('batchRename does two-phase renames and rejects duplicate targets before touching anything', async () => {
    const rename = jest.fn().mockResolvedValue({});
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }), rename });
    const res = await ex.batchRename([
      { path: '/r/a', newName: 'b' },
      { path: '/r/b', newName: 'a' },
      { path: '/r/c', newName: 'dup' },
      { path: '/r/d', newName: 'dup' },
      { path: '/r/e', newName: 'e' },
    ]);
    expect(res.failed.map((f) => f.errorCode)).toEqual(['DUPLICATE_TARGET', 'DUPLICATE_TARGET']);
    const calls = rename.mock.calls.map((c) => c.join('>'));
    expect(calls).toEqual(['/r/a>/r/.rfe-rn-0-b', '/r/b>/r/.rfe-rn-1-a', '/r/.rfe-rn-0-b>/r/b', '/r/.rfe-rn-1-a>/r/a']);
    expect(res.results.find((r) => r.path === '/r/e')!.ok).toBe(true);
  });

  it('batchRename puts a file back under its own name when phase 2 fails', async () => {
    // The agent now answers 409 when a final name is taken by an item that was not selected.
    const rename = jest.fn().mockImplementation(async (from: string, to: string) => {
      if (from === '/r/.rfe-rn-0-b' && to === '/r/b') throw new Error('destination already exists');
    });
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }), rename });
    const res = await ex.batchRename([
      { path: '/r/a', newName: 'b' },
      { path: '/r/c', newName: 'd' },
    ]);
    expect(rename.mock.calls.map((c) => c.join('>'))).toEqual([
      '/r/a>/r/.rfe-rn-0-b',
      '/r/c>/r/.rfe-rn-1-d',
      '/r/.rfe-rn-0-b>/r/b',
      '/r/.rfe-rn-0-b>/r/a',
      '/r/.rfe-rn-1-d>/r/d',
    ]);
    expect(res.failed).toHaveLength(1);
    expect(res.failed[0]).toMatchObject({ path: '/r/a', ok: false, errorCode: 'RENAME_FAILED' });
    expect(res.results.find((r) => r.path === '/r/d')!.ok).toBe(true);
  });

  it('batchRename reports a file it could not put back, with the name it was left under', async () => {
    const rename = jest.fn().mockImplementation(async (from: string) => {
      if (from === '/r/.rfe-rn-0-b') throw new Error('nope');
    });
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }), rename });
    const res = await ex.batchRename([{ path: '/r/a', newName: 'b' }]);
    expect(res.failed).toHaveLength(1);
    expect(res.failed[0]).toMatchObject({ path: '/r/.rfe-rn-0-b', ok: false, errorCode: 'RENAME_STRANDED' });
    expect(res.failed[0].errorMessage).toContain('/r/a');
  });

  it('batchRename refuses a name that is taken by an unselected item before moving anything, and still allows a chain', async () => {
    const files = new Set(['/r/a', '/r/b', '/r/c', '/r/d']);
    const rename = jest.fn().mockImplementation(async (from: string, to: string) => {
      if (files.has(to)) throw new Error('destination already exists');
      files.delete(from);
      files.add(to);
    });
    const list = jest.fn().mockImplementation(async () => ({ path: '/r', entries: [...files].map((p) => e(p.slice(3))) }));
    const { ex } = setup({ list, rename });
    await ex.load();
    // a>b then b>c is a chain through b, and c exists unselected: b>c is refused up front, a>b (which needed the name b to be vacated) is refused up front too
    // and put back, and nothing is left under a temp name.
    const res = await ex.batchRename([
      { path: '/r/a', newName: 'b' },
      { path: '/r/b', newName: 'c' },
      { path: '/r/d', newName: 'e' },
    ]);
    expect(res.failed.map((r) => [r.path, r.errorCode])).toEqual([['/r/a', 'TARGET_EXISTS'], ['/r/b', 'TARGET_EXISTS']]);
    expect([...files].sort()).toEqual(['/r/a', '/r/b', '/r/c', '/r/e']);
    expect([...files].some((p) => p.includes('.rfe-rn-'))).toBe(false);
  });

  it('batchRename refuses a target that is only in an unloaded page, so nothing is stranded (F-138)', async () => {
    const files = new Set(['/r/x', '/r/a', '/r/b', '/r/c']);
    const rename = jest.fn().mockImplementation(async (from: string, to: string) => {
      if (files.has(to)) throw new Error('destination already exists');
      files.delete(from);
      files.add(to);
    });
    // The screen holds only the first page (x, a, b); c sits on the next page. Listing without a cursor returns page one.
    const list = jest.fn().mockImplementation(async (_dir: string, o?: { cursor?: string }) => {
      const all = [...files].sort().map((p) => e(p.slice(3)));
      return o?.cursor ? { path: '/r', entries: all.slice(3) } : { path: '/r', entries: all.slice(0, 3), nextCursor: 'p2' };
    });
    const { ex } = setup({ list, rename });
    await ex.load();
    const res = await ex.batchRename([
      { path: '/r/x', newName: 'a' },
      { path: '/r/a', newName: 'b' },
      { path: '/r/b', newName: 'c' },
    ]);
    expect(res.failed.find((r) => r.errorCode === 'RENAME_STRANDED')).toBeUndefined();
    expect(res.failed.map((r) => [r.path, r.errorCode])).toEqual([['/r/x', 'TARGET_EXISTS'], ['/r/a', 'TARGET_EXISTS'], ['/r/b', 'TARGET_EXISTS']]);
    expect([...files].some((p) => p.includes('.rfe-rn-'))).toBe(false);
    expect(files.has('/r/b')).toBe(true);
  });

  it('a loadMore that a refresh superseded does not leave the list unable to page', async () => {
    let releaseMore!: (v: unknown) => void;
    const list = jest
      .fn()
      .mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c1' })
      .mockImplementationOnce(() => new Promise((r) => (releaseMore = r)))
      .mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c1' })
      .mockResolvedValueOnce({ path: '/r', entries: [e('b')] });
    const { ex } = setup({ list });
    await ex.load();
    const more = ex.loadMore();
    await tick();
    await ex.refresh();
    releaseMore({ path: '/r', entries: [e('late')] });
    await more;
    expect(ex.getState().loadingMore).toBe(false);
    await ex.loadMore();
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a', 'b']);
  });

  it('duplicateSelected copies the selection into the folder on screen as keep-both', async () => {
    const res = { results: [], failed: [] };
    const copy = jest.fn().mockResolvedValue(res);
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }), copy });
    ex.toggleSelect('/r/a');
    ex.toggleSelect('/r/b');
    expect(await ex.duplicateSelected()).toBe(res);
    expect(copy).toHaveBeenCalledWith(['/r/a', '/r/b'], '/r', { duplicate: true });
  });

  it('undo of a rename renames back; undo of a move moves back without ever overwriting', async () => {
    const rename = jest.fn().mockResolvedValue({});
    const move = jest.fn().mockResolvedValue({ results: [{ path: '/d/x', ok: true }], failed: [] });
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }), rename, move });
    await ex.undo({ kind: 'rename', from: '/r/b', to: '/r/a' });
    expect(rename).toHaveBeenCalledWith('/r/b', '/r/a');
    const op: UndoOp = { kind: 'move', groups: [{ paths: ['/d/x'], destDir: '/a' }] };
    await ex.undo(op);
    expect(move).toHaveBeenCalledWith(['/d/x'], '/a', {});
  });

  it('undo reports a move that the host refused, so it is never shown as undone', async () => {
    const move = jest.fn().mockResolvedValue({ results: [{ path: '/d/x', ok: false, errorCode: 'CONFLICT' }], failed: [{ path: '/d/x', ok: false, errorCode: 'CONFLICT' }] });
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }), move });
    await expect(ex.undo({ kind: 'move', groups: [{ paths: ['/d/x'], destDir: '/a' }] })).rejects.toThrow(/already exists|could not/i);
  });

  it('selectRange extends from the last toggled item to the target, on the list as displayed', async () => {
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [e('a'), e('b'), e('c'), e('d'), e('e')] }) });
    await ex.load();
    const shown = ex.getState().entries;
    ex.toggleSelect('/r/b');
    ex.selectRange(shown, '/r/d');
    expect([...ex.getState().selected].sort()).toEqual(['/r/b', '/r/c', '/r/d']);
    // The anchor moves to the target, so the next range starts there and keeps what is already selected.
    ex.selectRange(shown, '/r/e');
    expect([...ex.getState().selected].sort()).toEqual(['/r/b', '/r/c', '/r/d', '/r/e']);
  });

  it('selectRange with nothing selected selects just the target; clearing forgets the anchor', async () => {
    const { ex } = setup({ list: jest.fn().mockResolvedValue({ path: '/r', entries: [e('a'), e('b'), e('c')] }) });
    await ex.load();
    const shown = ex.getState().entries;
    ex.selectRange(shown, '/r/c');
    expect([...ex.getState().selected]).toEqual(['/r/c']);
    ex.clearSelection();
    ex.selectRange(shown, '/r/a');
    expect([...ex.getState().selected]).toEqual(['/r/a']);
  });

  it('createFolder can open the new folder with a single listing', async () => {
    const list = jest.fn().mockResolvedValue({ path: '/r/new', entries: [] });
    const createFolder = jest.fn().mockResolvedValue({});
    const { ex } = setup({ list, createFolder });
    await ex.createFolder('new', { open: true });
    expect(createFolder).toHaveBeenCalledWith('/r/new');
    expect(currentPath(ex.getState())).toBe('/r/new');
    expect(list).toHaveBeenCalledTimes(1);
    expect(list.mock.calls[0][0]).toBe('/r/new');
  });

  it('collidingBasenames pages the whole destination', async () => {
    const list = jest
      .fn()
      .mockResolvedValueOnce({ path: '/d', entries: [e('x')], nextCursor: 'n' })
      .mockResolvedValueOnce({ path: '/d', entries: [e('y')] });
    const { ex } = setup({ list });
    expect([...(await ex.collidingBasenames('/d', ['/s/y', '/s/z']))]).toEqual(['y']);
  });

  it('copy/move/delete pass the selection and options to the client and refresh', async () => {
    const res = { results: [], failed: [] };
    const client = { list: jest.fn().mockResolvedValue({ path: '/r', entries: [] }), copy: jest.fn().mockResolvedValue(res), move: jest.fn().mockResolvedValue(res), delete: jest.fn().mockResolvedValue(res), createFolder: jest.fn().mockResolvedValue({}) };
    const { ex } = setup(client);
    ex.toggleSelect('/r/a');
    await ex.copySelected('/d', { duplicate: true });
    await ex.moveSelected('/d', { sources: ['/r/z'], overwrite: true });
    ex.toggleSelect('/r/a');
    ex.toggleSelect('/r/b');
    await ex.deleteSelected({ permanent: true });
    await ex.createFolder('new');
    expect(client.copy).toHaveBeenCalledWith(['/r/a'], '/d', { duplicate: true });
    expect(client.move).toHaveBeenCalledWith(['/r/z'], '/d', { sources: ['/r/z'], overwrite: true });
    expect(client.delete).toHaveBeenCalledWith(['/r/a', '/r/b'], { permanent: true });
    expect(client.createFolder).toHaveBeenCalledWith('/r/new');
  });
});

describe('listing cache', () => {
  it('evicts the oldest non-pinned listing beyond capacity and serialises writes', async () => {
    let t = 0;
    const cache = new ListingCache(new MemoryListingBackend(), 2, () => ++t);
    cache.setPinned('h:/a', true);
    await Promise.all([cache.put('h', '/a', []), cache.put('h', '/b', []), cache.put('h', '/c', [])]);
    expect(await cache.get('h', '/a')).not.toBeNull();
    expect(await cache.get('h', '/b')).toBeNull();
    expect(await cache.get('h', '/c')).not.toBeNull();
    await cache.evictHost('h');
    expect(await cache.get('h', '/a')).toBeNull();
  });
});

describe('local cache failures never decide the result (F-163, F-164)', () => {
  const brokenCache = (o: { get?: boolean; put?: boolean }) =>
    ({
      get: jest.fn(async () => {
        if (o.get) throw new Error('db read');
        return null;
      }),
      put: jest.fn(async () => {
        if (o.put) throw new Error('db write');
      }),
    }) as unknown as ListingCache;
  const make = (cache: ListingCache, list: jest.Mock) => createExplorer({ hostId: 'h', rootPath: '/r', getClient: async () => ({ list }) as unknown as AgentClient, cache, humanize: (x) => (x as Error).message });

  it('a good listing is shown although the cache write fails', async () => {
    const ex = make(brokenCache({ put: true }), jest.fn().mockResolvedValue({ path: '/r', entries: [e('a')] }));
    await ex.load();
    expect(ex.getState()).toMatchObject({ loading: false, error: null, offline: false });
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a']);
  });

  it('a failed cache write while paging keeps the page and sets no loadMoreError', async () => {
    const list = jest.fn().mockResolvedValueOnce({ path: '/r', entries: [e('a')], nextCursor: 'c' }).mockResolvedValueOnce({ path: '/r', entries: [e('b')] });
    const ex = make(brokenCache({ put: true }), list);
    await ex.load();
    await ex.loadMore();
    expect(ex.getState().loadMoreError).toBeNull();
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a', 'b']);
  });

  it('a failed cache read is a miss: load does not reject and still lists the folder', async () => {
    const ex = make(brokenCache({ get: true }), jest.fn().mockResolvedValue({ path: '/r', entries: [e('a')] }));
    await expect(ex.load()).resolves.toBeUndefined();
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a']);
    expect(ex.getState().loading).toBe(false);
  });
});
