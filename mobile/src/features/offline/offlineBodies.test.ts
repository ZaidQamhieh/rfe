import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { keepOfflineCopy, MAX_OFFLINE_CACHE_BYTES, MAX_OFFLINE_FILE_BYTES, precachePinnedFolder, restoreOfflineCopy, wantsOfflineCopy, type OfflineDeps } from './offlineBodies';

const host = { id: 'h', label: 'PC', address: 'a' } as Host;
const file = (path: string, size = 10): Entry => ({ name: path.split('/').pop()!, path, isDir: false, isSymlink: false, size });

function deps(o: { pinned?: string[]; stored?: string[]; total?: number } = {}) {
  const stored = new Set(o.stored ?? []);
  const puts: string[] = [];
  const d: OfflineDeps = {
    isPinnedFolder: (_h, folder) => (o.pinned ?? ['/p']).includes(folder),
    vault: {
      has: async (_h, p) => stored.has(p),
      put: async (_h, p) => void (stored.add(p), puts.push(p)),
      restore: async (_h, p) => stored.has(p),
      totalBytes: async () => o.total ?? 0,
    },
  };
  return { d, puts, stored };
}

describe('wantsOfflineCopy', () => {
  it('needs a file in a pinned folder under the size cap', () => {
    const { d } = deps();
    expect(wantsOfflineCopy(d, host, file('/p/a'))).toBe(true);
    expect(wantsOfflineCopy(d, host, file('/other/a'))).toBe(false);
    expect(wantsOfflineCopy(d, host, file('/p/big', MAX_OFFLINE_FILE_BYTES + 1))).toBe(false);
    expect(wantsOfflineCopy(d, host, { ...file('/p/dir'), isDir: true })).toBe(false);
  });
});

describe('keepOfflineCopy', () => {
  it('stores a fresh fetch, replacing an older body', async () => {
    const { d, puts } = deps({ stored: ['/p/a'] });
    expect(await keepOfflineCopy(d, host, file('/p/a'), '/tmp/x', true)).toBe(true);
    expect(puts).toEqual(['/p/a']);
  });
  it('leaves an existing copy alone when not replacing', async () => {
    const { d, puts } = deps({ stored: ['/p/a'] });
    expect(await keepOfflineCopy(d, host, file('/p/a'), '/tmp/x', false)).toBe(false);
    expect(puts).toEqual([]);
  });
  it('respects the whole-cache budget and never throws', async () => {
    expect(await keepOfflineCopy(deps({ total: MAX_OFFLINE_CACHE_BYTES }).d, host, file('/p/a'), '/x', true)).toBe(false);
    const { d } = deps();
    d.vault.put = async () => Promise.reject(new Error('disk full'));
    await expect(keepOfflineCopy(d, host, file('/p/a'), '/x', true)).resolves.toBe(false);
  });
  it('ignores files outside pinned folders', async () => {
    const { d, puts } = deps();
    expect(await keepOfflineCopy(d, host, file('/q/a'), '/x', true)).toBe(false);
    expect(puts).toEqual([]);
  });
});

describe('restoreOfflineCopy', () => {
  it('reports presence, and treats an integrity failure as absent', async () => {
    const { d } = deps({ stored: ['/p/a'] });
    expect(await restoreOfflineCopy(d, host, file('/p/a'), '/dest')).toBe(true);
    expect(await restoreOfflineCopy(d, host, file('/p/none'), '/dest')).toBe(false);
    d.vault.restore = async () => Promise.reject(Object.assign(new Error('bad'), { code: 'ERR_INTEGRITY' }));
    expect(await restoreOfflineCopy(d, host, file('/p/a'), '/dest')).toBe(false);
  });
});

describe('precachePinnedFolder', () => {
  it('fetches only missing eligible files', async () => {
    const { d } = deps({ stored: ['/p/have'] });
    const entries = [file('/p/have'), file('/p/new'), { ...file('/p/sub'), isDir: true }, file('/p/huge', MAX_OFFLINE_FILE_BYTES + 1)];
    const fetched: string[] = [];
    const n = await precachePinnedFolder(d, host, entries, async (_h, e) => void fetched.push(e.path));
    expect(n).toBe(1);
    expect(fetched).toEqual(['/p/new']);
  });
  it('never overshoots the budget, and skips failures', async () => {
    const { d } = deps({ total: MAX_OFFLINE_CACHE_BYTES - 15 });
    const fetched: string[] = [];
    const n = await precachePinnedFolder(d, host, [file('/p/a', 10), file('/p/b', 10), file('/p/c', 10)], async (_h, e) => {
      if (e.path === '/p/a') throw new Error('unreadable');
      fetched.push(e.path);
    });
    // a fails (no bytes counted), b fits (5 bytes left afterwards), c would overshoot and is skipped.
    expect(fetched).toEqual(['/p/b']);
    expect(n).toBe(1);
  });
  it('one failing lookup does not stop the folder, and an unmeasurable store adds nothing', async () => {
    const { d } = deps({});
    const fetched: string[] = [];
    d.vault.has = async (_h, path) => {
      if (path === '/p/a') throw new Error('native failure');
      return false;
    };
    expect(await precachePinnedFolder(d, host, [file('/p/a', 1), file('/p/b', 1)], async (_h, e) => void fetched.push(e.path))).toBe(1);
    expect(fetched).toEqual(['/p/b']);
    d.vault.totalBytes = async () => Promise.reject(new Error('native failure'));
    expect(await precachePinnedFolder(d, host, [file('/p/c', 1)], async () => fetched.push('/p/c'))).toBe(0);
    expect(fetched).toEqual(['/p/b']);
  });
});
