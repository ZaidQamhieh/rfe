import { MemorySecureStore } from '../security/secureStore';
import { HostStore, MemoryKeyValueStore } from './hostStore';

const F1 = 'ab'.repeat(32);

describe('HostStore.mergeDuplicates', () => {
  it('drops the older copy of the same computer, keeps the newest token, and forgets the dropped one', async () => {
    const removed: string[] = [];
    const store = new HostStore(new MemoryKeyValueStore(), new MemorySecureStore(), async (id) => void removed.push(id));
    await store.commitPairing({ id: 'a', label: 'Studio PC', address: '10.0.0.1:8765' }, { token: 'tok-a', fingerprint: F1 });
    await store.commitPairing({ id: 'b', label: '10.0.0.7:8765', address: '10.0.0.7:8765' }, { token: 'tok-b', fingerprint: F1 });
    expect(await store.mergeDuplicates()).toBe(1);
    expect(await store.listHosts()).toMatchObject([{ id: 'b', label: 'Studio PC', address: '10.0.0.7:8765' }]);
    expect(await store.getToken('b')).toBe('tok-b');
    expect(await store.getToken('a')).toBeNull();
    expect(removed).toEqual(['a']);
  });

  it('uses the pin in the secure store when the saved record has no certificate mirror', async () => {
    const kv = new MemoryKeyValueStore();
    const secure = new MemorySecureStore();
    const store = new HostStore(kv, secure);
    await store.commitPairing({ id: 'a', label: 'A', address: '10.0.0.1:8765' }, { token: 'tok-a', fingerprint: F1 });
    await store.commitPairing({ id: 'b', label: 'B', address: '10.0.0.7:8765' }, { token: 'tok-b', fingerprint: F1 });
    // Simulate old saved records that never had the metadata mirror.
    await kv.set('rfe_hosts_v1', JSON.stringify([{ id: 'a', label: 'A', address: '10.0.0.1:8765' }, { id: 'b', label: 'B', address: '10.0.0.7:8765' }]));
    expect(await store.mergeDuplicates()).toBe(1);
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['b']);
  });

  it('does nothing when there are no duplicates', async () => {
    const store = new HostStore(new MemoryKeyValueStore(), new MemorySecureStore());
    await store.commitPairing({ id: 'a', label: 'A', address: '10.0.0.1:8765' }, { token: 't', fingerprint: F1 });
    await store.commitPairing({ id: 'b', label: 'B', address: '10.0.0.2:8765' }, { token: 't', fingerprint: 'cd'.repeat(32) });
    expect(await store.mergeDuplicates()).toBe(0);
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['a', 'b']);
  });
});

describe('HostStore refused flag', () => {
  it('remembers a computer that refused this phone until it is cleared, and pairing again clears it', async () => {
    const store = new HostStore(new MemoryKeyValueStore(), new MemorySecureStore());
    await store.commitPairing({ id: 'a', label: 'A', address: '10.0.0.1:8765' }, { token: 't', fingerprint: F1 });
    expect(await store.getRefused('a')).toBe(false);
    await store.setRefused('a', true);
    expect(await store.getRefused('a')).toBe(true);
    await store.setRefused('a', false);
    expect(await store.getRefused('a')).toBe(false);
    await store.setRefused('a', true);
    await store.commitPairing({ id: 'a', label: 'A', address: '10.0.0.1:8765' }, { token: 't2', fingerprint: F1 });
    expect(await store.getRefused('a')).toBe(false);
  });

  it('forgetting the computer forgets the flag, and a forgotten computer is not flagged', async () => {
    const kv = new MemoryKeyValueStore();
    const store = new HostStore(kv, new MemorySecureStore());
    await store.setRefused('ghost', true);
    expect(await store.getRefused('ghost')).toBe(false);
    await store.commitPairing({ id: 'a', label: 'A', address: '10.0.0.1:8765' }, { token: 't', fingerprint: F1 });
    await store.setRefused('a', true);
    await store.removeHost('a');
    expect([...kv.data.keys()].filter((k) => k.includes('refused'))).toEqual([]);
  });
});
