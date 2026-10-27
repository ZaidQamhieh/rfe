import type { Entry } from '../../core/api/models';
import { markPinSynced, pinSyncPending, refreshPinnedFolders, resetPinSyncSession } from './pinSync';

const file = (path: string): Entry => ({ name: path.split('/').pop()!, path, isDir: false, isSymlink: false, size: 1 });

describe('refreshPinnedFolders', () => {
  it('lists, stores and precaches each folder and totals the fetched files', async () => {
    const stored: string[] = [];
    const r = await refreshPinnedFolders(['/a', '/b'], {
      listAll: async (p) => [file(`${p}/x`), file(`${p}/y`)],
      storeListing: async (p) => void stored.push(p),
      precache: async (es) => es.length - 1,
    });
    expect(stored).toEqual(['/a', '/b']);
    expect(r).toEqual({ folders: 2, failed: 0, filesFetched: 2 });
  });
  it('skips a folder that cannot be listed and carries on', async () => {
    const r = await refreshPinnedFolders(['/gone', '/ok'], {
      listAll: async (p) => {
        if (p === '/gone') throw new Error('NOT_FOUND');
        return [file('/ok/x')];
      },
      storeListing: async () => {},
      precache: async () => 1,
    });
    expect(r).toEqual({ folders: 1, failed: 1, filesFetched: 1 });
  });
  it('counts a failing precache as a failed folder without throwing', async () => {
    const r = await refreshPinnedFolders(['/a'], { listAll: async () => [], storeListing: async () => {}, precache: async () => { throw new Error('boom'); } });
    expect(r.failed).toBe(1);
  });
});

describe('session guard', () => {
  it('stays pending until a host is marked, per host', () => {
    resetPinSyncSession();
    expect(pinSyncPending('h1')).toBe(true);
    markPinSynced('h1');
    expect(pinSyncPending('h1')).toBe(false);
    expect(pinSyncPending('h2')).toBe(true);
  });
});
