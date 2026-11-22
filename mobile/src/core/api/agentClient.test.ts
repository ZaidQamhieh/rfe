import { AgentClient, ERR_CERT_PIN_MISMATCH, ERR_CONNECTION, ERR_PATH_NOT_FOUND, PAIR_NONCE_HEADER, PAIR_NONCE_QUERY, type Transport } from './agentClient';
import { canManageHost, hasAppCapabilities, hasFileCapabilities, parseAuditEntry, parseStatus } from './models';
import { CertPinMismatch, MissingCertPin } from './pin';
import type { Host } from '../models/host';

const PIN = 'ab'.repeat(32);
const host: Host = { id: 'h1', label: 'PC', address: '192.168.1.2:8765', tailscaleAddress: '100.1.2.3:8765' };

const ok = (body: unknown) => ({ status: 200, headers: {}, bodyText: JSON.stringify(body) });
const coded = (code: string) => Object.assign(new Error(code), { code });

function fake(handler: (url: string, method: string) => unknown) {
  const calls: { url: string; method: string; headers: Record<string, string>; pin: string | null }[] = [];
  const transport: Transport = {
    async request(a) {
      calls.push({ url: a.url, method: a.method, headers: a.headers, pin: a.pin });
      const r = handler(a.url, a.method);
      if (r instanceof Error) throw r;
      return r as never;
    },
  };
  return { transport, calls };
}

describe('AgentClient pin policy', () => {
  it('refuses a token without a pin at construction', () => {
    const { transport } = fake(() => ok({}));
    expect(() => new AgentClient(host, { transport, deviceToken: 't' })).toThrow(MissingCertPin);
  });

  it('refuses malformed pins', () => {
    const { transport } = fake(() => ok({}));
    expect(() => new AgentClient(host, { transport, pinnedFingerprint: 'zz' })).toThrow(MissingCertPin);
  });

  it("unpinned client may only call health and challenge, and never sends credentials", async () => {
    const { transport, calls } = fake(() =>
      ok({ status: "ok", nonce: "n", proof: "v2" }),
    );
    const c = new AgentClient(host, { transport });
    await c.health();
    await c.challenge();
    await expect(c.drives()).rejects.toBeInstanceOf(MissingCertPin);
    expect(calls).toHaveLength(2);
    expect(calls.every((x) => x.headers.Authorization === undefined && x.pin === null)).toBe(true);
  });

  it('sends bearer only when pinned and passes the normalized pin to the transport', async () => {
    const { transport, calls } = fake(() => ok([]));
    const c = new AgentClient(host, { transport, deviceToken: 'tok', pinnedFingerprint: PIN.toUpperCase() });
    await c.drives();
    expect(calls[0].headers.Authorization).toBe('Bearer tok');
    expect(calls[0].pin).toBe(PIN);
    expect(calls[0].url).toBe('https://192.168.1.2:8765/v1/system/drives');
  });

  it('surfaces a pin mismatch and never tries another route', async () => {
    const { transport, calls } = fake(() => coded(ERR_CERT_PIN_MISMATCH));
    const c = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN });
    await expect(c.list('/')).rejects.toBeInstanceOf(CertPinMismatch);
    expect(calls).toHaveLength(1);
  });
});

describe('AgentClient route fallback', () => {
  it('falls back to Tailscale for GET on connectivity failure and remembers it', async () => {
    const { transport, calls } = fake((url) => (url.includes('192.168') ? coded(ERR_CONNECTION) : ok([])));
    const c = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN, probeLanFirst: true });
    await c.drives();
    expect(calls.map((x) => x.url.split('/')[2])).toEqual(['192.168.1.2:8765', '100.1.2.3:8765']);
    expect(c.activeRoute).toBe('tailscale');
    const later = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN });
    expect(later.activeAddress).toBe('100.1.2.3:8765');
  });

  it('does not replay POST on another route', async () => {
    const { transport, calls } = fake(() => coded(ERR_CONNECTION));
    const c = new AgentClient(host, { transport, pinnedFingerprint: PIN, probeLanFirst: true });
    await expect(
      c.pair({ pairingCode: '1', deviceLabel: 'p', devicePublicKey: 'k', nonce: 'n', signature: 's' }),
    ).rejects.toMatchObject({ code: 'CONNECTION' });
    expect(calls).toHaveLength(1);
  });

  it('stops fallback when a pin mismatch appears on the second route', async () => {
    const { transport, calls } = fake((url) =>
      url.includes('192.168') ? coded(ERR_CONNECTION) : coded(ERR_CERT_PIN_MISMATCH),
    );
    const c = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN, probeLanFirst: true });
    await expect(c.drives()).rejects.toBeInstanceOf(CertPinMismatch);
    expect(calls).toHaveLength(2);
  });
});

describe('AgentClient responses', () => {
  it('maps API errors and parses listings', async () => {
    const { transport } = fake((url) =>
      url.includes('/fs?')
        ? ok({ path: '/', entries: [{ name: 'a', path: '/a', isDir: true }], nextCursor: 'c' })
        : { status: 403, headers: {}, bodyText: JSON.stringify({ code: 'READ_ONLY', message: 'nope' }) },
    );
    const c = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN });
    const l = await c.list('/');
    expect(l.entries[0]).toMatchObject({ name: 'a', isDir: true, isSymlink: false });
    expect(l.nextCursor).toBe('c');
    await expect(c.drives()).rejects.toMatchObject({ statusCode: 403, code: 'READ_ONLY' });
  });

  it('reads roots and access policy from /settings', async () => {
    const { transport, calls } = fake(() => ok({ roots: ['/srv/a', 7], accessDenied: true, effectiveScope: 'device', readOnly: true }));
    const c = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN });
    expect(await c.status()).toMatchObject({ roots: ['/srv/a'], accessDenied: true, effectiveScope: 'device', readOnly: true });
    expect(calls[0].url).toMatch(/\/v1\/settings$/);
  });
});

describe('AgentClient putContent', () => {
  it('PUTs raw text with baseModified and maps STALE_WRITE to a coded error', async () => {
    const seen: { url: string; body: string | null; ct?: string }[] = [];
    let status = 200;
    const transport: Transport = {
      async request(a) {
        seen.push({ url: a.url, body: a.bodyText, ct: a.headers['Content-Type'] });
        return status === 200
          ? { status, headers: {}, bodyText: JSON.stringify({ name: 'a.txt', path: '/a.txt', isDir: false, modified: 'm2' }) }
          : { status, headers: {}, bodyText: JSON.stringify({ code: 'STALE_WRITE', message: 'changed' }) };
      },
    };
    const client = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN });
    const e = await client.putContent('/a.txt', '{"k":1}\n', 'm1');
    expect(e.modified).toBe('m2');
    expect({ ...seen[0], url: seen[0].url.split('/v1')[1] }).toEqual({ url: '/content?path=%2Fa.txt&baseModified=m1', body: '{"k":1}\n', ct: 'application/octet-stream' });
    status = 409;
    await expect(client.putContent('/a.txt', 'x')).rejects.toMatchObject({ statusCode: 409, code: 'STALE_WRITE' });
    expect(seen[1].url.split('/v1')[1]).toBe('/content?path=%2Fa.txt');
  });
});

describe('AgentClient filesystem operations', () => {
  const pinned = (h: (url: string, method: string, body: unknown) => unknown) => {
    const calls: { url: string; method: string; body: unknown }[] = [];
    const transport: Transport = {
      async request(a) {
        const body = a.bodyText ? JSON.parse(a.bodyText) : null;
        calls.push({ url: a.url, method: a.method, body });
        return { status: 200, headers: {}, bodyText: JSON.stringify(h(a.url, a.method, body)) };
      },
    };
    return { client: new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN }), calls };
  };

  it('uses the exact verbs, paths and bodies of the Flutter client', async () => {
    const { client, calls } = pinned(() => ({ name: 'n', path: '/p', isDir: false, results: [], items: [] }));
    await client.createFolder('/a/b');
    await client.createFile('/a/f');
    await client.rename('/a/f', '/a/g');
    await client.chmod('/a/g', '0755');
    await client.copy(['/x'], '/d', { duplicate: true });
    await client.move(['/x'], '/d', { overwrite: true });
    await client.delete(['/x']);
    await client.delete(['/x'], { permanent: true });
    await client.restoreTrash(['1']);
    await client.emptyTrash();
    await client.emptyTrash(['1']);
    await client.compress(['/a'], '/a.zip');
    await client.extract('/a.zip', '/out');
    const shape = calls.map((c) => `${c.method} ${c.url.split('/v1')[1]} ${JSON.stringify(c.body)}`);
    expect(shape).toEqual([
      'POST /fs/folder {"path":"/a/b"}',
      'POST /fs/file {"path":"/a/f"}',
      'PATCH /fs/rename {"src":"/a/f","dst":"/a/g"}',
      'POST /fs/chmod {"path":"/a/g","mode":"0755"}',
      'POST /fs/copy {"sources":["/x"],"destDir":"/d","duplicate":true,"overwrite":false}',
      'POST /fs/move {"sources":["/x"],"destDir":"/d","duplicate":false,"overwrite":true}',
      'DELETE /fs {"paths":["/x"]}',
      'DELETE /fs?permanent=true {"paths":["/x"]}',
      'POST /trash/restore {"ids":["1"]}',
      'DELETE /trash null',
      'DELETE /trash {"ids":["1"]}',
      'POST /fs/compress {"sources":["/a"],"dest":"/a.zip"}',
      'POST /fs/extract {"archive":"/a.zip","destDir":"/out"}',
    ]);
  });

  it('parses batch results with per-item errors and lists trash/archives/checksums', async () => {
    const { client } = pinned((url) =>
      url.includes('/fs/copy')
        ? { results: [{ path: '/a', ok: true }, { path: '/b', ok: false, error: { code: 'CONFLICT', message: 'exists' } }] }
        : url.includes('/trash')
          ? { items: [{ id: '1', name: 'x', originalPath: '/x', isDir: true }] }
          : url.includes('/fs/archive')
            ? { entries: [{ path: 'a/b', size: 3, isDir: false }] }
            : { checksums: [{ path: '/a', hash: 'abc' }, { path: '/b', hash: '' }] },
    );
    const r = await client.copy(['/a', '/b'], '/d');
    expect(r.failed).toEqual([{ path: '/b', ok: false, errorCode: 'CONFLICT', errorMessage: 'exists' }]);
    expect((await client.listTrash())[0]).toMatchObject({ id: '1', isDir: true });
    expect((await client.archiveList('/a.zip'))[0]).toMatchObject({ path: 'a/b', size: 3 });
    expect(await client.batchChecksums(['/a', '/b'])).toEqual({ '/a': 'abc' });
  });
});

describe('AgentClient search, recent and share links', () => {
  const withHeaders = (body: unknown, headers: Record<string, string> = {}) => {
    const calls: { url: string; method: string }[] = [];
    const transport: Transport = {
      async request(a) {
        calls.push({ url: a.url, method: a.method });
        return { status: 200, headers, bodyText: JSON.stringify(body) };
      },
    };
    return { client: new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN }), calls };
  };

  it('sends only the filters that are set and reads truncation flags from headers, case-insensitively', async () => {
    const { client, calls } = withHeaders([{ name: 'a.jpg', path: '/a.jpg', isDir: false }], { 'X-Search-Truncated': '1' });
    const r = await client.search({ q: 'a*', root: '/r', types: ['image', 'video'], ext: ['jpg'], minSize: 5, modifiedAfter: new Date('2026-01-02T03:04:05Z') });
    expect(calls[0].url.split('/v1')[1]).toBe('/search?q=a*&root=%2Fr&limit=100&types=image%2Cvideo&ext=jpg&minSize=5&modifiedAfter=2026-01-02T03%3A04%3A05.000Z');
    expect(r.entries.map((e) => e.name)).toEqual(['a.jpg']);
    expect(r.truncated).toBe(true);
    expect(r.timeBudgetHit).toBe(false);
  });

  it('reports the time-budget flag independently and tolerates a non-array body', async () => {
    const { client } = withHeaders(null, { 'x-search-time-budget': '1' });
    expect(await client.recent()).toEqual({ entries: [], truncated: false, timeBudgetHit: true });
  });

  it('recent asks for the newest files under an optional root', async () => {
    const { client, calls } = withHeaders([]);
    await client.recent({ root: '/x', limit: 20 });
    expect(calls[0].url.split('/v1')[1]).toBe('/fs/recent?root=%2Fx&limit=20');
  });

  it('mints, lists and revokes share links by hash', async () => {
    const { client, calls } = withHeaders({ token: 'tok', tokenHash: 'h/1', path: '/f', expiresAt: 99, url: 'https://x/s/tok' });
    const link = await client.mintShareLink('/f', 60);
    expect(link).toEqual({ token: 'tok', tokenHash: 'h/1', path: '/f', expiresAt: 99, url: 'https://x/s/tok' });
    await client.revokeShareLink('h/1');
    expect(calls.map((c) => `${c.method} ${c.url.split('/v1')[1]}`)).toEqual(['POST /share/mint', 'DELETE /share/h%2F1']);
  });
});

describe('AgentClient settings, devices, bandwidth and audit', () => {
  it('treats removing an unknown device as done and still reports other failures', async () => {
    const reply = (status: number): AgentClient =>
      new AgentClient(host, {
        transport: { async request() { return { status, headers: {}, bodyText: JSON.stringify({ code: 'NOT_FOUND', message: 'x' }) }; } },
        deviceToken: 't',
        pinnedFingerprint: PIN,
      });
    await expect(reply(404).deleteDevice('gone')).resolves.toBeUndefined();
    await expect(reply(403).deleteDevice('other')).rejects.toBeDefined();
  });

  const rec = (body: unknown) => {
    const calls: { url: string; method: string; body: unknown }[] = [];
    const transport: Transport = {
      async request(a) {
        calls.push({ url: a.url, method: a.method, body: a.bodyText ? JSON.parse(a.bodyText) : null });
        return { status: 200, headers: {}, bodyText: JSON.stringify(body) };
      },
    };
    return { client: new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN }), calls };
  };
  const shape = (calls: { url: string; method: string; body: unknown }[]) => calls.map((c) => `${c.method} ${c.url.split('/v1')[1]} ${JSON.stringify(c.body)}`);

  it('uses the verbs, paths and bodies of the Flutter client', async () => {
    const { client, calls } = rec({ entries: [] });
    await client.updateSettings({ readOnly: true });
    await client.listDevices();
    await client.deleteDevice('d/1');
    await client.deleteDevice('d1', false);
    await client.updateDeviceAppCapabilities('d1', { viewApps: true, launchApps: false });
    await client.updateDeviceFileCapabilities('d1', { browse: true, download: true, upload: false, modify: false, delete: false, share: true });
    await client.getBandwidth();
    await client.setBandwidth({ maxDownloadBytesPerSec: 5 });
    await client.audit({ limit: 10, before: 7 });
    expect(shape(calls)).toEqual([
      'PATCH /settings {"readOnly":true}',
      'GET /devices null',
      'DELETE /devices/d%2F1?purge=true null',
      'DELETE /devices/d1 null',
      'PATCH /devices/d1 {"viewApps":true,"launchApps":false}',
      'PATCH /devices/d1 {"browse":true,"download":true,"upload":false,"modify":false,"delete":false,"share":true}',
      'GET /settings/bandwidth null',
      'PUT /settings/bandwidth {"maxDownloadBytesPerSec":5}',
      'GET /audit?limit=10&before=7 null',
    ]);
  });

  it('parses devices with and without capability flags, and audit entries', async () => {
    const { client } = rec([
      { id: 'a', label: 'Phone', created: 10, lastSeen: 20, current: true, viaLogin: true, lastAddress: '1.2.3.4', browse: true, download: false, upload: false, modify: false, delete: false, share: false, viewApps: true, launchApps: false },
      { id: 'b', label: 'Old', created: 1, lastSeen: 2, revoked: true },
    ]);
    const [a, b] = await client.listDevices();
    expect(a).toMatchObject({ id: 'a', created: 10_000, lastSeen: 20_000, current: true, viaLogin: true, browse: true, download: false });
    expect(hasFileCapabilities(a)).toBe(true);
    expect(hasAppCapabilities(a)).toBe(true);
    expect(hasFileCapabilities(b)).toBe(false);
    expect(hasAppCapabilities(b)).toBe(false);
    expect(b.revoked).toBe(true);
    expect(parseAuditEntry({ id: 3, at: '2026-01-02T03:04:05Z', action: 'login', actor: 'owner' })).toMatchObject({ id: 3, action: 'login', actor: 'owner', target: '' });
    expect(parseAuditEntry({ at: 'nope' }).at.getTime()).toBe(0);
  });

  it('treats an agent that omits isAdmin as one the owner may manage', () => {
    expect(canManageHost(parseStatus({}))).toBe(true);
    expect(canManageHost(parseStatus({ isAdmin: false }))).toBe(false);
    expect(canManageHost(parseStatus({ isAdmin: true }))).toBe(true);
  });
});

describe('AgentClient 2xx replies that are not JSON (F-139)', () => {
  const html = { status: 200, headers: {}, bodyText: '<html>captive portal</html>' };
  const make = (reply: unknown) => new AgentClient(host, { transport: fake(() => reply).transport, deviceToken: 't', pinnedFingerprint: PIN });

  it('throws BAD_RESPONSE instead of returning an empty body', async () => {
    await expect(make(html).drives()).rejects.toMatchObject({ statusCode: 200, code: 'BAD_RESPONSE' });
    await expect(make(html).list('/')).rejects.toMatchObject({ code: 'BAD_RESPONSE' });
  });

  it("a challenge without the v2 proof is UNSUPPORTED_BY_AGENT, not a later INVALID_SIGNATURE", async () => {
    await expect(make(ok({ nonce: "abc" })).challenge()).rejects.toMatchObject({
      code: "UNSUPPORTED_BY_AGENT",
    });
  });

  it("challenge never returns undefined: a non-JSON or nonce-less reply is BAD_RESPONSE", async () => {
    await expect(make(html).challenge()).rejects.toMatchObject({
      code: "BAD_RESPONSE",
    });
    await expect(make(ok({ nope: 1 })).challenge()).rejects.toMatchObject({
      code: "BAD_RESPONSE",
    });
    await expect(
      make(ok({ nonce: "abc", proof: "v2" })).challenge(),
    ).resolves.toBe("abc");
  });

  it('launchApp and drives reject an unexpected shape instead of a raw TypeError', async () => {
    await expect(make(ok(null)).launchApp('x')).rejects.toThrow('Unexpected app launch response.');
    await expect(make(ok({ not: 'an array' })).drives()).rejects.toMatchObject({ code: 'BAD_RESPONSE' });
  });

  it('an empty 2xx body is still a valid empty reply', async () => {
    const c = make({ status: 204, headers: {}, bodyText: '' });
    await expect(c.revealPair('id', 'n')).resolves.toBeUndefined();
  });

  it('an error status with a non-JSON body keeps its status code', async () => {
    await expect(make({ status: 502, headers: {}, bodyText: '<html>bad gateway</html>' }).drives()).rejects.toMatchObject({ statusCode: 502, code: 'UNKNOWN' });
  });
});

describe('AgentClient v2 and v3 agents (compat C1, C3)', () => {
  const rec = (body: unknown) => {
    const calls: { url: string; method: string; headers: Record<string, string> }[] = [];
    const transport: Transport = {
      async request(a) {
        calls.push({ url: a.url, method: a.method, headers: a.headers });
        return { status: 200, headers: {}, bodyText: JSON.stringify(body) };
      },
    };
    return { client: new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN }), calls };
  };

  it('C3: the pair poll sends the nonce in the header and the query, so a v2 and a v3 agent both read it', async () => {
    const { client, calls } = rec({ status: 'pending' });
    await client.pairRequestStatus('r1', 'abc 1');
    expect(calls[0].url.split('/v1')[1]).toBe(`/pair/request/r1?${PAIR_NONCE_QUERY}=abc%201`);
    expect(calls[0].headers[PAIR_NONCE_HEADER]).toBe('abc 1');
  });

  it('C3: other calls carry no nonce header', async () => {
    const { client, calls } = rec([]);
    await client.drives();
    expect(calls[0].headers[PAIR_NONCE_HEADER]).toBeUndefined();
  });

  it('C1: a path already gone (v3 per-item PATH_NOT_FOUND) is a success, not a failed item', async () => {
    const { client } = rec({
      results: [
        { path: '/a', ok: true },
        { path: '/gone', ok: false, error: { code: ERR_PATH_NOT_FOUND, message: 'no such path' } },
        { path: '/b', ok: false, error: { code: 'PERMISSION_DENIED', message: 'nope' } },
      ],
    });
    for (const permanent of [true, false]) {
      const res = await client.delete(['/a', '/gone', '/b'], { permanent });
      expect(res.results.map((r) => [r.path, r.ok])).toEqual([['/a', true], ['/gone', true], ['/b', false]]);
      expect(res.failed.map((r) => r.path)).toEqual(['/b']);
    }
  });

  it('C1: a v2 reply (no entry for a missing path) is unchanged', async () => {
    const { client } = rec({ results: [{ path: '/a', ok: true }] });
    const res = await client.delete(['/a', '/gone']);
    expect(res.failed).toEqual([]);
  });

  it('C1: PATH_NOT_FOUND from a copy or move is still a failure', async () => {
    const { client } = rec({ results: [{ path: '/s', ok: false, error: { code: ERR_PATH_NOT_FOUND, message: 'x' } }] });
    expect((await client.copy(['/s'], '/d')).failed).toHaveLength(1);
    expect((await client.move(['/s'], '/d')).failed).toHaveLength(1);
  });
});

describe('AgentClient pairing requests, phone folders and media info', () => {
  const rec = (body: unknown) => {
    const calls: { url: string; method: string; body: unknown }[] = [];
    const transport: Transport = {
      async request(a) {
        calls.push({ url: a.url, method: a.method, body: a.bodyText ? JSON.parse(a.bodyText) : null });
        return { status: 200, headers: {}, bodyText: JSON.stringify(body) };
      },
    };
    return { client: new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN }), calls };
  };
  const shape = (calls: { url: string; method: string; body: unknown }[]) => calls.map((c) => `${c.method} ${c.url.split('/v1')[1]} ${JSON.stringify(c.body)}`);

  it('lists waiting phones, reading the expiry as a time and tolerating missing fields', async () => {
    const { client, calls } = rec({ requests: [{ id: 'r/1', label: 'Mum', matchCode: '6150 2283', remoteIp: '10.0.0.5', replaces: 'Old', expiresAt: '2026-11-10T12:00:00Z' }, { id: 'r2' }] });
    const list = await client.listPairRequests();
    expect(shape(calls)).toEqual(['GET /pair/requests null']);
    expect(list[0]).toEqual({ id: 'r/1', label: 'Mum', matchCode: '6150 2283', remoteIp: '10.0.0.5', replaces: 'Old', expiresAt: Date.parse('2026-11-10T12:00:00Z') });
    expect(list[1]).toEqual({ id: 'r2', label: '', matchCode: '', remoteIp: '', replaces: '', expiresAt: 0 });
  });

  it('an answer without a requests list is an empty list', async () => {
    expect(await rec({}).client.listPairRequests()).toEqual([]);
    expect(await rec(null).client.listPairRequests()).toEqual([]);
  });

  it('allows and refuses a waiting phone by its encoded id', async () => {
    const { client, calls } = rec({});
    await client.approvePairRequest('r/1');
    await client.rejectPairRequest('r 2');
    expect(shape(calls)).toEqual(['POST /pair/requests/r%2F1/approve null', 'POST /pair/requests/r%202/reject null']);
  });

  it('keeps a phone inside a folder, and an empty string is sent to clear it', async () => {
    const { client, calls } = rec({});
    await client.updateDeviceJailRoot('d1', '/srv/leila');
    await client.updateDeviceJailRoot('d1', '');
    expect(shape(calls)).toEqual(['PATCH /devices/d1 {"jailRoot":"/srv/leila"}', 'PATCH /devices/d1 {"jailRoot":""}']);
  });

  it('reads media info, and an empty answer gives an empty object', async () => {
    const full = rec({ width: 4000, height: 3000, takenAt: '2026-05-01T10:00:00Z', durationMs: 1500 });
    expect(await full.client.mediaInfo('/p/a.jpg')).toEqual({ width: 4000, height: 3000, takenAt: Date.parse('2026-05-01T10:00:00Z'), durationMs: 1500 });
    expect(shape(full.calls)).toEqual(['GET /fs/media-info?path=%2Fp%2Fa.jpg null']);
    expect(await rec({}).client.mediaInfo('/folder')).toEqual({});
    expect(await rec({ width: 'x', takenAt: 'not a date' }).client.mediaInfo('/f')).toEqual({});
  });

  describe('media info abort signal', () => {
    const hanging = () => {
      const seen: (AbortSignal | undefined)[] = [];
      const transport: Transport = {
        request(a) {
          seen.push(a.signal);
          return new Promise(() => undefined);
        },
      };
      return { client: new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN }), seen };
    };

    it('hands the signal to the transport', async () => {
      const seen: (AbortSignal | undefined)[] = [];
      const transport: Transport = {
        async request(a) {
          seen.push(a.signal);
          return { status: 200, headers: {}, bodyText: '{}' };
        },
      };
      const client = new AgentClient(host, { transport, deviceToken: 't', pinnedFingerprint: PIN });
      const ctl = new AbortController();
      await client.mediaInfo('/a.mp4', ctl.signal);
      expect(seen).toEqual([ctl.signal]);
    });

    it('rejects with an AbortError when aborted while the request is waiting', async () => {
      const { client, seen } = hanging();
      const ctl = new AbortController();
      const p = client.mediaInfo('/a.mp4', ctl.signal);
      await Promise.resolve();
      expect(seen).toHaveLength(1);
      ctl.abort();
      await expect(p).rejects.toMatchObject({ name: 'AbortError' });
    });

    it('rejects at once, sending nothing, when the signal is already aborted', async () => {
      const { client, seen } = hanging();
      const ctl = new AbortController();
      ctl.abort();
      await expect(client.mediaInfo('/a.mp4', ctl.signal)).rejects.toMatchObject({ name: 'AbortError' });
      expect(seen).toHaveLength(0);
    });
  });

  it('mints a share link of one hour unless told otherwise', async () => {
    const { client, calls } = rec({ token: 't' });
    await client.mintShareLink('/f');
    await client.mintShareLink('/f', 604800);
    expect(calls.map((c) => c.body)).toEqual([{ path: '/f', expiresInSeconds: 3600 }, { path: '/f', expiresInSeconds: 604800 }]);
  });
});

describe('account server calls', () => {
  const FP = 'cd'.repeat(32);
  function recorded(handler: (path: string) => unknown) {
    const calls: { url: string; method: string; headers: Record<string, string>; body: unknown; pin: string | null }[] = [];
    const transport: Transport = {
      async request(a) {
        calls.push({ url: a.url, method: a.method, headers: a.headers, body: a.bodyText ? JSON.parse(a.bodyText) : null, pin: a.pin });
        return ok(handler(a.url.split('/v1')[1])) as never;
      },
    };
    return { transport, calls };
  }

  it('accountKey reads and validates GET /accounts/key', async () => {
    const { transport, calls } = recorded(() => ({ name: 'Home', publicKey: 'AAAA', keyFingerprint: FP.toUpperCase(), algorithm: 'Ed25519' }));
    const k = await new AgentClient(host, { transport, pinnedFingerprint: PIN }).accountKey();
    expect(k).toEqual({ name: 'Home', publicKey: 'AAAA', keyFingerprint: FP });
    expect(calls[0]).toMatchObject({ method: 'GET', pin: PIN });
  });

  it('accountKey rejects a malformed key reply', async () => {
    for (const bad of [{}, { publicKey: 'A', keyFingerprint: 'short', algorithm: 'Ed25519' }, { publicKey: 'A', keyFingerprint: FP, algorithm: 'RSA' }]) {
      const { transport } = recorded(() => bad);
      await expect(new AgentClient(host, { transport, pinnedFingerprint: PIN }).accountKey()).rejects.toThrow();
    }
  });

  it('accountAssertion posts the audience, key and nonce with the bearer token', async () => {
    const { transport, calls } = recorded(() => ({ assertion: 'p.s', exp: 1700000000 }));
    const c = new AgentClient(host, { transport, deviceToken: 'tok', pinnedFingerprint: PIN });
    expect(await c.accountAssertion({ audience: FP, devicePublicKey: 'pk', memberNonce: 'n' })).toEqual({ assertion: 'p.s', exp: 1700000000 });
    expect(calls[0]).toMatchObject({ method: 'POST', body: { audience: FP, devicePublicKey: 'pk', memberNonce: 'n' } });
    expect(calls[0].url.endsWith('/v1/accounts/assertion')).toBe(true);
    expect(calls[0].headers.Authorization).toBe('Bearer tok');
  });

  it('accountAssertion rejects a reply without an assertion', async () => {
    const { transport } = recorded(() => ({ exp: 1 }));
    const c = new AgentClient(host, { transport, deviceToken: 'tok', pinnedFingerprint: PIN });
    await expect(c.accountAssertion({ audience: FP, devicePublicKey: 'pk', memberNonce: 'n' })).rejects.toThrow();
  });

  it('login sends the assertion and no username or password', async () => {
    const { transport, calls } = recorded(() => ({ deviceToken: 't', deviceId: 'd', agentName: 'A' }));
    await new AgentClient(host, { transport, pinnedFingerprint: PIN }).login({ assertion: 'p.s', deviceLabel: 'L', devicePublicKey: 'pk', nonce: 'n', signature: 'sig' });
    expect(calls[0].body).toEqual({ assertion: 'p.s', deviceLabel: 'L', devicePublicKey: 'pk', nonce: 'n', signature: 'sig' });
  });

  it('health exposes the account-server hint only for a valid fingerprint', async () => {
    const good = recorded(() => ({ status: 'ok', accountServer: { name: 'Home', keyFingerprint: FP } }));
    expect((await new AgentClient(host, { transport: good.transport }).health()).accountServer).toEqual({ name: 'Home', keyFingerprint: FP });
    const bad = recorded(() => ({ status: 'ok', accountServer: { name: 'x', keyFingerprint: 'zz' } }));
    expect((await new AgentClient(host, { transport: bad.transport }).health()).accountServer).toBeUndefined();
  });
});
