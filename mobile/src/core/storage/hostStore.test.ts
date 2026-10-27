import { HostStore, MemoryKeyValueStore, HOSTS_KEY } from './hostStore';
import { MemorySecureStore, SecureKeys } from '../security/secureStore';

const PIN = 'cd'.repeat(32);
const host = { id: 'h1', label: 'PC', address: '10.0.0.2:8765' };

function make() {
  const kv = new MemoryKeyValueStore();
  const secure = new MemorySecureStore();
  return { kv, secure, store: new HostStore(kv, secure) };
}

describe('HostStore.commitPairing', () => {
  it('stores pin and token in secure storage and only the mirror in metadata', async () => {
    const { store, secure } = make();
    await store.commitPairing(host, { token: 'tok', fingerprint: PIN.toUpperCase() });
    expect(await store.getPin('h1')).toBe(PIN);
    expect(await store.getToken('h1')).toBe('tok');
    expect(secure.data.get(SecureKeys.fingerprint('h1'))).toBe(PIN);
    expect((await store.listHosts())[0].certFingerprint).toBe(PIN);
  });

  it('refuses a missing or malformed pin and writes nothing', async () => {
    const { store, secure, kv } = make();
    await expect(store.commitPairing(host, { token: 't', fingerprint: 'nope' })).rejects.toThrow();
    expect(secure.data.size).toBe(0);
    expect(kv.data.size).toBe(0);
  });

  it('rolls back secrets if the host record cannot be written', async () => {
    const { store, secure, kv } = make();
    kv.set = async () => {
      throw new Error('disk full');
    };
    await expect(store.commitPairing(host, { token: 't', fingerprint: PIN })).rejects.toThrow('disk full');
    expect(secure.data.size).toBe(0);
  });

  it('rolls back when the token write fails, leaving no orphan pin', async () => {
    const { store, secure } = make();
    const write = secure.write.bind(secure);
    secure.write = async (k, v) => {
      if (k.startsWith('rfe_token_')) throw new Error('keystore');
      return write(k, v);
    };
    await expect(store.commitPairing(host, { token: 't', fingerprint: PIN })).rejects.toThrow('keystore');
    expect(secure.data.size).toBe(0);
    expect(await store.listHosts()).toEqual([]);
  });
});

describe('HostStore list handling', () => {
  it('skips corrupt records, accepts Flutter string-encoded entries, and moves touched host first', async () => {
    const { store, kv } = make();
    await kv.set(HOSTS_KEY, JSON.stringify([JSON.stringify(host), 'not json', { id: 'x' }, { id: 'h2', label: 'B', address: 'b:1' }]));
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['h1', 'h2']);
    await store.touchHost('h2');
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['h2', 'h1']);
  });

  it('removeHost runs the cache cleanup for that host and survives a failing cleanup', async () => {
    const secure = new MemorySecureStore();
    const removed: string[] = [];
    const store = new HostStore(new MemoryKeyValueStore(), secure, async (id) => void removed.push(id));
    await store.removeHost('h1');
    expect(removed).toEqual(['h1']);
    const failing = new HostStore(new MemoryKeyValueStore(), secure, async () => Promise.reject(new Error('disk')));
    await expect(failing.removeHost('h1')).resolves.toBeUndefined();
  });

  it('removeHost keeps the host listed when a keystore delete fails (F-75)', async () => {
    const { store, secure } = make();
    await store.commitPairing(host, { token: 'tok', fingerprint: PIN });
    const del = secure.delete.bind(secure);
    secure.delete = async () => Promise.reject(new Error('keystore'));
    await expect(store.removeHost('h1')).rejects.toThrow('keystore');
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['h1']);
    secure.delete = del;
    await store.removeHost('h1');
    expect(await store.listHosts()).toEqual([]);
    expect(await store.getToken('h1')).toBeNull();
  });

  it('updateHost and patchHost never bring back a forgotten host, and setLastSeen leaves no row for it (F-28)', async () => {
    const { store, kv } = make();
    await store.commitPairing(host, { token: 't', fingerprint: PIN });
    await store.removeHost('h1');
    await store.updateHost({ ...host, label: 'old snapshot' });
    expect(await store.patchHost('h1', { macAddress: 'aa:bb' })).toBe(false);
    await store.setLastSeen('h1');
    expect(await store.listHosts()).toEqual([]);
    expect(await store.getLastSeen('h1')).toBeNull();
    expect([...kv.data.keys()]).toEqual([HOSTS_KEY]);
  });

  it('a ping that learns an address does not revert an edit made meanwhile (F-29)', async () => {
    const { store } = make();
    await store.addHost(host);
    // The edit and the learn start together; both are applied, in order, whatever the async timing.
    await Promise.all([store.updateHost({ ...host, label: 'Renamed', address: '10.0.0.9:8765' }), store.patchHost('h1', { tailscaleAddress: '100.64.0.1:8765' })]);
    const [h] = await store.listHosts();
    expect(h.label).toBe('Renamed');
    expect(h.address).toBe('10.0.0.9:8765');
    expect(h.tailscaleAddress).toBe('100.64.0.1:8765');
  });

  it('serialises overlapping mutations of a slow store so none is lost (F-29)', async () => {
    const { store, kv } = make();
    const set = kv.set.bind(kv);
    kv.set = async (k, v) => {
      await new Promise((r) => setTimeout(r, 5));
      return set(k, v);
    };
    await Promise.all([store.addHost(host), store.addHost({ id: 'h2', label: 'B', address: 'b:1' }), store.addHost({ id: 'h3', label: 'C', address: 'c:1' })]);
    expect((await store.listHosts()).map((h) => h.id).sort()).toEqual(['h1', 'h2', 'h3']);
  });

  it('a failed mutation does not block the ones queued after it', async () => {
    const { store, kv } = make();
    const set = kv.set.bind(kv);
    kv.set = async () => {
      throw new Error('disk');
    };
    await expect(store.addHost(host)).rejects.toThrow('disk');
    kv.set = set;
    await store.addHost(host);
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['h1']);
  });

  it('removeHost clears its secrets', async () => {
    const { store, secure } = make();
    await store.commitPairing(host, { token: 't', fingerprint: PIN });
    await store.removeHost('h1');
    expect(secure.data.size).toBe(0);
    expect(await store.listHosts()).toEqual([]);
  });

  it('a failed re-pairing of a listed host restores its previous pin and token', async () => {
    const { store, secure, kv } = make();
    await store.commitPairing(host, { token: 'old-token', fingerprint: PIN });
    const set = kv.set.bind(kv);
    kv.set = async () => {
      throw new Error('disk full');
    };
    const NEW = 'ab'.repeat(32);
    await expect(store.commitPairing(host, { token: 'new-token', fingerprint: NEW })).rejects.toThrow('disk full');
    kv.set = set;
    expect(await store.getToken('h1')).toBe('old-token');
    expect(await store.getPin('h1')).toBe(PIN);
    expect(secure.data.get(SecureKeys.token('h1'))).toBe('old-token');
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['h1']);
  });
});
