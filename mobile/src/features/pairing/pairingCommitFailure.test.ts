import type { Transport } from '../../core/api/agentClient';
import { DeviceIdentity } from '../../core/security/deviceIdentity';
import { MemorySecureStore } from '../../core/security/secureStore';
import { HostStore, MemoryKeyValueStore } from '../../core/storage/hostStore';
import { awaitPairing, PairExpired, requestPairing, type PairingDeps } from './pairingService';

const PIN = 'ab'.repeat(32);
const rnd = (n: number) => Uint8Array.from({ length: n }, (_, i) => (i * 5 + 1) & 255);
const APPROVED = { status: 'approved', deviceToken: 'tok', deviceId: 'dev-9', agentName: 'My PC' };

/** Deps whose agent answers every approval poll with "approved" and counts the polls. */
function setup() {
  const polls = { n: 0 };
  const transport: Transport = {
    async request(a) {
      let body: unknown = {};
      if (a.url.endsWith('/auth/challenge')) body = { nonce: 'n-1', proof: 'v2' };
      else if (a.url.endsWith('/pair/request')) body = { requestId: 'req-1', expiresInSeconds: 60 };
      else if (a.url.includes('/pair/request/')) {
        if (a.method === 'GET' || a.url.includes('status')) polls.n++;
        body = a.url.endsWith('/reveal') ? {} : APPROVED;
      }
      return { status: 200, headers: {}, bodyText: JSON.stringify(body) };
    },
  };
  const secure = new MemorySecureStore();
  const store = new HostStore(new MemoryKeyValueStore(), secure);
  const deps: PairingDeps = { transport, identity: new DeviceIdentity(secure, rnd), store, deviceId: async () => 'android-id-1', probe: async () => PIN, randomBytes: rnd };
  return { deps, store, polls };
}

describe('awaitPairing when saving the approved host fails', () => {
  it('surfaces the save error at once instead of treating it as a dropped poll', async () => {
    const { deps, store, polls } = setup();
    const boom = new Error('disk full');
    const commit = jest.spyOn(store, 'commitPairing').mockRejectedValue(boom);
    const h = await requestPairing(deps, '10.0.0.2:8765');
    polls.n = 0;
    await expect(awaitPairing(deps, h, { sleep: async () => {} })).rejects.toBe(boom);
    expect(polls.n).toBe(1);
    expect(commit).toHaveBeenCalledTimes(1);
  });

  it('does not run out the clock and report the request as expired', async () => {
    const { deps, store } = setup();
    const boom = new Error('keystore locked');
    jest.spyOn(store, 'commitPairing').mockRejectedValue(boom);
    const h = await requestPairing(deps, '10.0.0.2:8765');
    let t = 0;
    const p = awaitPairing(deps, h, { sleep: async () => { t += 30_000; }, now: () => t });
    await expect(p).rejects.toBe(boom);
    await expect(p).rejects.not.toBeInstanceOf(PairExpired);
  });
});

describe('awaitPairing cancellation without a global DOMException (React Native)', () => {
  const g = globalThis as { DOMException?: unknown };
  let saved: unknown;
  beforeEach(() => {
    saved = g.DOMException;
    delete g.DOMException;
  });
  afterEach(() => {
    g.DOMException = saved;
  });

  it('an approval that arrives after cancelling rejects with an AbortError and saves nothing', async () => {
    const { deps, store } = setup();
    const h = await requestPairing(deps, '10.0.0.2:8765');
    const ctl = new AbortController();
    ctl.abort();
    await expect(awaitPairing(deps, h, { signal: ctl.signal })).rejects.toMatchObject({ name: 'AbortError' });
    expect(await store.listHosts()).toEqual([]);
  });

  it('cancelling while waiting between polls rejects with an AbortError', async () => {
    const { deps } = setup();
    jest.spyOn(deps.store, 'commitPairing');
    const h = await requestPairing(deps, '10.0.0.2:8765');
    const ctl = new AbortController();
    // The status poll answers "pending" so the loop reaches its sleep, where the person cancels.
    const pending: Transport = { request: async () => ({ status: 200, headers: {}, bodyText: JSON.stringify({ status: 'pending' }) }) };
    const p = awaitPairing({ ...deps, transport: pending }, h, { signal: ctl.signal, sleep: async () => ctl.abort() });
    await expect(p).rejects.toMatchObject({ name: 'AbortError' });
  });
});
