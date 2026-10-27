import { MemoryKeyValueStore } from '../../core/storage/hostStore';
import { forgetRoots, loadRoots, saveRoots } from './rootsCache';

describe('roots cache', () => {
  it('round-trips roots, os and drives per host', async () => {
    const kv = new MemoryKeyValueStore();
    await saveRoots(kv, 'a', { roots: ['/srv'], os: 'linux' });
    await saveRoots(kv, 'b', { roots: [], os: 'windows', drives: [{ path: 'C:\\', isOS: true }] });
    expect(await loadRoots(kv, 'a')).toEqual({ roots: ['/srv'], os: 'linux', drives: undefined });
    expect((await loadRoots(kv, 'b'))?.drives).toEqual([{ path: 'C:\\', isOS: true }]);
    expect(await loadRoots(kv, 'c')).toBeNull();
  });
  it('rejects corrupt records', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set('rfe_roots_v1_x', 'not json');
    expect(await loadRoots(kv, 'x')).toBeNull();
    await kv.set('rfe_roots_v1_y', JSON.stringify({ roots: [1, 2] }));
    expect(await loadRoots(kv, 'y')).toBeNull();
  });
  it('can be forgotten', async () => {
    const kv = new MemoryKeyValueStore();
    await saveRoots(kv, 'a', { roots: ['/'] });
    await forgetRoots(kv, 'a');
    expect(await loadRoots(kv, 'a')).toBeNull();
  });
});
