import type { Host } from '../../core/models/host';
import { MemoryKeyValueStore } from '../../core/storage/hostStore';
import { LAST_HOST_KEY, pickHomeHost, readLastHostId, writeLastHostId } from './activeSelection';

const h = (id: string): Host => ({ id, label: id, address: '10.0.0.1:8765' });
const hosts = [h('a'), h('b'), h('c')];

describe('pickHomeHost', () => {
  it('prefers the active host', () => expect(pickHomeHost(hosts, 'b', 'c')?.id).toBe('b'));
  it('falls back to the last opened host when the active one is gone', () => expect(pickHomeHost(hosts, 'zz', 'c')?.id).toBe('c'));
  it('falls back to the first host', () => expect(pickHomeHost(hosts, null, 'gone')?.id).toBe('a'));
  it('is null when nothing is paired', () => expect(pickHomeHost([], 'a', 'b')).toBeNull());
});

describe('last host persistence', () => {
  it('round-trips the id', async () => {
    const kv = new MemoryKeyValueStore();
    expect(await readLastHostId(kv)).toBeNull();
    await writeLastHostId(kv, 'b');
    expect(kv.data.get(LAST_HOST_KEY)).toBe('b');
    expect(await readLastHostId(kv)).toBe('b');
  });
});
