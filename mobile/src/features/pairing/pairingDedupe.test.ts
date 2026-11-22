import type { Transport } from '../../core/api/agentClient';
import { DeviceIdentity } from '../../core/security/deviceIdentity';
import { MemorySecureStore, SecureKeys } from '../../core/security/secureStore';
import { HostStore, MemoryKeyValueStore } from '../../core/storage/hostStore';
import { pairWithCode, type PairingDeps } from './pairingService';

const PIN = 'ab'.repeat(32);
const rnd = (n: number) => Uint8Array.from({ length: n }, (_, i) => (i * 5 + 1) & 255);

function setup() {
  const transport: Transport = {
    async request(a) {
      const body = a.url.endsWith('/auth/challenge') ? { nonce: 'n-1', proof: 'v2' } : { deviceToken: 'tok', deviceId: 'dev-9', agentName: 'My PC' };
      return { status: 200, headers: {}, bodyText: JSON.stringify(body) };
    },
  };
  const secure = new MemorySecureStore();
  const kv = new MemoryKeyValueStore();
  const store = new HostStore(kv, secure);
  const deps: PairingDeps = { transport, identity: new DeviceIdentity(secure, rnd), store, deviceId: async () => 'android-id-1', probe: async () => PIN, randomBytes: rnd };
  return { deps, store, secure, kv };
}

describe('pairing never adds a second copy of a computer already saved', () => {
  it('matches a saved computer whose record has no certificate mirror by the pin in the secure store', async () => {
    const { deps, store, secure, kv } = setup();
    await secure.write(SecureKeys.fingerprint('old-id'), PIN);
    await secure.write(SecureKeys.token('old-id'), 'old-tok');
    await kv.set('rfe_hosts_v1', JSON.stringify([{ id: 'old-id', label: 'Desk', address: '10.0.0.9:8765' }]));
    const host = await pairWithCode(deps, { address: '10.0.0.2:8765', fingerprint: PIN }, 'CODE');
    expect(host).toMatchObject({ id: 'old-id', label: 'Desk', address: '10.0.0.2:8765' });
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['old-id']);
  });

  it('matches by the id the computer hands out when it is the same entry', async () => {
    const { deps, store } = setup();
    await store.commitPairing({ id: 'dev-9', label: 'Desk', address: '10.0.0.9:8765', note: 'Home' }, { token: 'old', fingerprint: 'cd'.repeat(32) });
    const host = await pairWithCode(deps, { address: '10.0.0.2:8765', fingerprint: PIN }, 'CODE');
    expect(host).toMatchObject({ id: 'dev-9', label: 'Desk', note: 'Home' });
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['dev-9']);
  });

  it('a second pairing of the same certificate leaves one saved host', async () => {
    const { deps, store } = setup();
    await pairWithCode(deps, { address: '10.0.0.2:8765', fingerprint: PIN }, 'CODE');
    await pairWithCode(deps, { address: '10.0.0.3:8765', fingerprint: PIN }, 'CODE-2');
    const hosts = await store.listHosts();
    expect(hosts).toHaveLength(1);
    expect(hosts[0]).toMatchObject({ id: 'dev-9', address: '10.0.0.3:8765' });
  });

  it('pairing again folds copies of the same computer that were already saved, without waiting for an app start', async () => {
    const { deps, store, secure } = setup();
    await store.commitPairing({ id: 'first', label: 'Desk', address: '10.0.0.1:8765' }, { token: 'tok-first', fingerprint: PIN });
    await store.commitPairing({ id: 'second', label: '10.0.0.7:8765', address: '10.0.0.7:8765' }, { token: 'tok-second', fingerprint: PIN });
    const host = await pairWithCode(deps, { address: '10.0.0.2:8765', fingerprint: PIN }, 'CODE');
    const hosts = await store.listHosts();
    expect(hosts).toHaveLength(1);
    expect(hosts[0]).toMatchObject({ id: host.id, label: 'Desk', address: '10.0.0.2:8765' });
    expect(await store.getToken(host.id)).toBe('tok');
    const dropped = ['first', 'second'].filter((id) => id !== host.id);
    for (const id of dropped) expect(await secure.read(SecureKeys.token(id))).toBeNull();
  });

  it('a failing merge does not fail the pairing', async () => {
    const { deps, store } = setup();
    jest.spyOn(store, 'mergeDuplicates').mockRejectedValue(new Error('disk full'));
    const host = await pairWithCode(deps, { address: '10.0.0.2:8765', fingerprint: PIN }, 'CODE');
    expect(host.id).toBe('dev-9');
    expect((await store.listHosts()).map((h) => h.id)).toEqual(['dev-9']);
  });
});
