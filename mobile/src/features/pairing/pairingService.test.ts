import { AgentApiError, ERR_CERT_PIN_MISMATCH, statusError, type Transport } from '../../core/api/agentClient';
import { CertPinMismatch } from '../../core/api/pin';
import { DeviceIdentity } from '../../core/security/deviceIdentity';
import { MemorySecureStore } from '../../core/security/secureStore';
import { HostStore, MemoryKeyValueStore } from '../../core/storage/hostStore';
import { sha256 } from '@noble/hashes/sha2.js';

import { awaitPairing, checkKnownPin, humanizeError, loginWithAccount, PairExpired, pairWithCode, PairRejected, parsePairingQr, proofMessage, registerAccount, requestPairing, type PairingDeps } from './pairingService';

const PIN = 'ab'.repeat(32);
const rnd = (n: number) => Uint8Array.from({ length: n }, (_, i) => (i * 5 + 1) & 255);

function setup(handler: (url: string, method: string, body: unknown, pin: string | null) => unknown) {
  const calls: { url: string; method: string; body: unknown; pin: string | null; headers: Record<string, string> }[] = [];
  const transport: Transport = {
    async request(a) {
      const body = a.bodyText ? JSON.parse(a.bodyText) : null;
      calls.push({ url: a.url, method: a.method, body, pin: a.pin, headers: a.headers });
      const r = handler(a.url, a.method, body, a.pin);
      if (r instanceof Error) throw r;
      return { status: 200, headers: {}, bodyText: JSON.stringify(r) };
    },
  };
  const secure = new MemorySecureStore();
  const store = new HostStore(new MemoryKeyValueStore(), secure);
  const deps: PairingDeps = { transport, identity: new DeviceIdentity(secure, rnd), store, deviceId: async () => 'android-id-1', probe: async () => PIN, randomBytes: rnd };
  return { deps, calls, store, secure };
}

const okHandler = (url: string) =>
  url.endsWith("/auth/challenge")
    ? { nonce: "n-1", proof: "v2" }
    : {
        deviceToken: "tok",
        deviceId: "dev-9",
        agentName: "My PC",
        tailscaleAddress: "100.1.1.1:8765",
      };

describe('pairWithCode', () => {
  it('pins before every request, sends the proof and device id, and commits pin+token', async () => {
    const { deps, calls, store } = setup(okHandler);
    const host = await pairWithCode(deps, { address: ' 10.0.0.2:8765 ', fingerprint: PIN.toUpperCase() }, ' ABCD1234 ');
    expect(calls.map((c) => c.url.split('/v1')[1])).toEqual(['/auth/challenge', '/pair']);
    expect(calls.every((c) => c.pin === PIN)).toBe(true); // challenge is pinned too
    expect(calls[1].body).toMatchObject({ pairingCode: 'ABCD1234', nonce: 'n-1', deviceId: 'android-id-1', deviceLabel: 'Mobile App' });
    expect(calls[1].body).toHaveProperty('signature');
    expect(host).toMatchObject({ id: 'dev-9', label: 'My PC', address: '10.0.0.2:8765', tailscaleAddress: '100.1.1.1:8765' });
    expect(await store.getPin('dev-9')).toBe(PIN);
    expect(await store.getToken('dev-9')).toBe('tok');
  });

  it('a pin mismatch sends nothing further and stores nothing', async () => {
    const { deps, calls, store } = setup(() => Object.assign(new Error('x'), { code: ERR_CERT_PIN_MISMATCH }));
    await expect(pairWithCode(deps, { address: 'a:1', fingerprint: PIN }, 'CODE')).rejects.toBeInstanceOf(CertPinMismatch);
    expect(calls).toHaveLength(1);
    expect(await store.listHosts()).toEqual([]);
  });

  it('refuses to start without a valid fingerprint (no request at all)', async () => {
    const { deps, calls } = setup(okHandler);
    await expect(pairWithCode(deps, { address: 'a:1', fingerprint: 'nope' }, 'C')).rejects.toThrow();
    expect(calls).toHaveLength(0);
  });

  it("rejects an incomplete pairing response without committing", async () => {
    const { deps, store } = setup((url) =>
      url.endsWith("/auth/challenge")
        ? { nonce: "n", proof: "v2" }
        : { deviceToken: "", deviceId: "" },
    );
    await expect(
      pairWithCode(deps, { address: "a:1", fingerprint: PIN }, "C"),
    ).rejects.toMatchObject({ code: "BAD_RESPONSE" });
    expect(await store.listHosts()).toEqual([]);
  });
});

describe('login and register', () => {
  it('login trims the username but never the password, and posts to /login', async () => {
    const { deps, calls } = setup(okHandler);
    await loginWithAccount(deps, { address: 'a:1', fingerprint: PIN }, { username: ' bob ', password: ' pw ' });
    expect(calls[1].url.endsWith('/v1/login')).toBe(true);
    expect(calls[1].body).toMatchObject({ username: 'bob', password: ' pw ' });
  });
  it('register posts the pairing code with the account', async () => {
    const { deps, calls } = setup(okHandler);
    await registerAccount(deps, { address: 'a:1', fingerprint: PIN }, { pairingCode: 'C0DE', username: 'u', password: 'longpassword' });
    expect(calls[1].url.endsWith('/v1/register')).toBe(true);
    expect(calls[1].body).toMatchObject({ pairingCode: 'C0DE', username: 'u' });
  });
});

describe('parsePairingQr', () => {
  it('accepts the agent QR and normalizes the fingerprint', () => {
    const r = parsePairingQr(JSON.stringify({ address: '1.2.3.4:8765', certFingerprint: PIN.toUpperCase().match(/../g)!.join(':'), pairingCode: 'ABCD1234' }));
    expect(r).toEqual({ ok: true, qr: { address: '1.2.3.4:8765', certFingerprint: PIN, pairingCode: 'ABCD1234' } });
  });
  it.each([
    ['not json', 'invalidQrFormat'],
    ['[1]', 'invalidQrFormat'],
    [JSON.stringify({ address: 'a' }), 'qrMissingFields'],
    [JSON.stringify({ address: 'a', certFingerprint: 'zz', pairingCode: 'c' }), 'qrInvalidFingerprint'],
  ])('rejects %s', (raw, error) => expect(parsePairingQr(raw)).toEqual({ ok: false, error }));
});

describe('humanizeError', () => {
  it('unwraps a native module rejection to its reason', () => {
    const e = new Error("Call to function 'RfeTransport.backupDecrypt' has been rejected.\n→ Caused by: Incorrect passphrase");
    expect(humanizeError(e)).toBe('Incorrect passphrase');
  });
  it('leaves other messages alone', () => expect(humanizeError(new Error('boom'))).toBe('boom'));
  it('explains a raw native connection failure instead of echoing the host name', () => {
    const e = Object.assign(new Error('not'), { code: 'ERR_CONNECTION' });
    expect(humanizeError(e)).toMatch(/^Could not reach the computer\. Check the address and port/);
    expect(humanizeError(Object.assign(new Error('handshake'), { code: 'ERR_TLS' }))).toMatch(/secure connection/);
  });
  it('says what a bare 403 from a download means instead of showing the status', () => {
    for (const e of [statusError(403), new AgentApiError(403, 'CAPABILITY_DENIED', 'device lacks download permission')]) {
      expect(humanizeError(e)).toMatch(/has not allowed this phone to do that/);
    }
  });
  it('keeps the agent’s own wording for other API errors, and plain statuses for non-403', () => {
    expect(humanizeError(new AgentApiError(403, 'FORBIDDEN', 'answering pairing requests requires an admin session'))).toBe('answering pairing requests requires an admin session');
    expect(humanizeError(statusError(500))).toBe('HTTP 500');
  });
  it('says a bare 401 means this computer no longer accepts the phone', () => {
    expect(humanizeError(new AgentApiError(401, 'UNKNOWN', 'HTTP 401'))).toMatch(/no longer accepts this phone/);
    expect(humanizeError(new AgentApiError(401, 'UNAUTHORIZED', 'token expired'))).toBe('token expired');
  });
  it('explains a certificate mismatch as a changed computer identity', () => {
    const m = humanizeError(new CertPinMismatch());
    expect(m).toMatch(/identity has changed/);
    expect(m).not.toMatch(/fingerprint/i);
  });
});

describe('approve-on-computer pairing', () => {
  const requestHandler = (status: () => unknown) => (url: string) =>
    url.endsWith("/auth/challenge")
      ? { nonce: "n-1", proof: "v2" }
      : url.endsWith("/pair/request")
        ? { requestId: "rid1", expiresInSeconds: 120 }
        : status();

  it('probes the certificate, pins every call to it, and never sends a code or a fingerprint', async () => {
    const { deps, calls } = setup(requestHandler(() => ({ status: 'pending' })));
    const h = await requestPairing(deps, ' https://10.0.0.2:8765/ ');
    expect(h.target).toEqual({ address: '10.0.0.2:8765', fingerprint: PIN });
    expect(calls.map((c) => c.url.split('/v1')[1])).toEqual(['/auth/challenge', '/pair/request', '/pair/request/rid1/reveal']);
    expect(calls.every((c) => c.pin === PIN)).toBe(true);
    expect(calls[1].body).toMatchObject({ deviceId: 'android-id-1', nonce: 'n-1' });
    expect(JSON.stringify(calls[1].body)).not.toContain(PIN);
    // The nonce is committed before the request id exists and revealed after; the commitment is its SHA-256.
    expect(calls[1].body).toHaveProperty('clientNonceCommit');
    expect(calls[1].body).not.toHaveProperty('clientNonce');
    const revealed = (calls[2].body as { clientNonce: string }).clientNonce;
    expect(revealed).toBe(h.clientNonce);
    const commit = Array.from(sha256(Uint8Array.from(revealed.match(/../g)!, (b) => parseInt(b, 16))), (x) => x.toString(16).padStart(2, '0')).join('');
    expect((calls[1].body as { clientNonceCommit: string }).clientNonceCommit).toBe(commit);
    // The device proof names the certificate this phone pinned (v2), not the bare nonce.
    expect((calls[1].body as { signature: string }).signature).toBe(await deps.identity.signBase64(proofMessage(PIN, 'n-1')));
    expect((calls[1].body as { signature: string }).signature).not.toBe(await deps.identity.signBase64('n-1'));
    expect(h.matchCode).toMatch(/^\d{4} \d{4}$/);
  });

  it('polls until approved, then commits the pin and token', async () => {
    let n = 0;
    const { deps, store } = setup(requestHandler(() => (++n < 3 ? { status: 'pending' } : { status: 'approved', deviceToken: 'tok', deviceId: 'dev-9', agentName: 'My PC' })));
    const h = await requestPairing(deps, '10.0.0.2:8765');
    const host = await awaitPairing(deps, h, { sleep: async () => {} });
    expect(n).toBe(3);
    expect(host).toMatchObject({ id: 'dev-9', label: 'My PC', address: '10.0.0.2:8765' });
    expect(await store.getPin('dev-9')).toBe(PIN);
    expect(await store.getToken('dev-9')).toBe('tok');
  });

  it('a rejection stores nothing', async () => {
    const { deps, store } = setup(requestHandler(() => ({ status: 'rejected' })));
    const h = await requestPairing(deps, '10.0.0.2:8765');
    await expect(awaitPairing(deps, h, { sleep: async () => {} })).rejects.toBeInstanceOf(PairRejected);
    expect(await store.listHosts()).toEqual([]);
  });

  it('gives up when the request expires', async () => {
    let t = 0;
    const { deps } = setup(requestHandler(() => ({ status: 'pending' })));
    const h = await requestPairing(deps, '10.0.0.2:8765');
    await expect(awaitPairing(deps, h, { sleep: async () => { t += 60_000; }, now: () => t })).rejects.toBeInstanceOf(PairExpired);
  });

  it('stops polling when aborted', async () => {
    const { deps, calls } = setup(requestHandler(() => ({ status: 'pending' })));
    const h = await requestPairing(deps, '10.0.0.2:8765');
    const ctl = new AbortController();
    const before = calls.length;
    const p = awaitPairing(deps, h, { signal: ctl.signal, sleep: async () => ctl.abort() });
    await expect(p).rejects.toMatchObject({ name: 'AbortError' });
    expect(calls.length - before).toBe(1);
  });

  it('an approval that arrives after cancelling is not committed (F-151)', async () => {
    const ctl = new AbortController();
    // The person cancels while the status request is in flight; the answer that comes back is "approved".
    const { deps, store } = setup(
      requestHandler(() => {
        ctl.abort();
        return { status: 'approved', deviceToken: 'tok', deviceId: 'dev-9', agentName: 'My PC' };
      }),
    );
    const h = await requestPairing(deps, '10.0.0.2:8765');
    await expect(awaitPairing(deps, h, { signal: ctl.signal, sleep: async () => {} })).rejects.toMatchObject({ name: 'AbortError' });
    expect(await store.listHosts()).toEqual([]);
    expect(await store.getToken('dev-9')).toBeNull();
  });
});

describe("credentials are only sent to a computer this phone trusts", () => {
  const okLogin = (url: string) =>
    url.endsWith("/auth/challenge")
      ? { nonce: "n-1", proof: "v2" }
      : { deviceToken: "tok", deviceId: "dev-9", agentName: "My PC" };
  const host = (fp: string) => ({
    id: "h1",
    label: "PC",
    address: "10.0.0.2:8765",
    certFingerprint: fp,
  });

  it('refuses to log in when the address is saved under a different certificate, before any request', async () => {
    const { deps, calls, store } = setup(okLogin);
    await store.commitPairing(host('cd'.repeat(32)), { token: 't', fingerprint: 'cd'.repeat(32) });
    await expect(loginWithAccount(deps, { address: '10.0.0.2:8765', fingerprint: PIN }, { username: 'u', password: 'secret-password' })).rejects.toBeInstanceOf(CertPinMismatch);
    await expect(registerAccount(deps, { address: '10.0.0.2:8765', fingerprint: PIN }, { pairingCode: 'ABCD1234', username: 'u', password: 'secret-password' })).rejects.toBeInstanceOf(CertPinMismatch);
    expect(calls).toEqual([]);
  });

  it('knows a saved certificate, and treats an unseen address as new', async () => {
    const { deps, store } = setup(okLogin);
    expect(await checkKnownPin(deps, { address: '10.0.0.2:8765', fingerprint: PIN })).toBe('new');
    await store.commitPairing(host(PIN), { token: 't', fingerprint: PIN });
    expect(await checkKnownPin(deps, { address: '10.0.0.2:8765', fingerprint: PIN })).toBe('known');
  });
});
