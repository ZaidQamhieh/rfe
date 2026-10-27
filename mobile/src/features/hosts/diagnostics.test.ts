import { AgentApiError, ERR_CERT_PIN_MISMATCH, ERR_CONNECTION, type Transport } from '../../core/api/agentClient';
import type { Host } from '../../core/models/host';
import { classifyFailure, probeAll, probeRoute } from './diagnostics';
import { CertPinMismatch, MissingCertPin } from '../../core/api/pin';

const PIN = 'ab'.repeat(32);
const host: Host = { id: 'h', label: 'PC', address: '192.168.1.2:8765', tailscaleAddress: '100.1.2.3:8765' };
const coded = (code: string, message = code) => Object.assign(new Error(message), { code });
const ok = (body: unknown, status = 200) => ({ status, headers: {}, bodyText: JSON.stringify(body) });

function transport(handler: (url: string) => unknown): Transport {
  return {
    async request(a) {
      const r = handler(a.url);
      if (r instanceof Error) throw r;
      return r as never;
    },
  };
}

describe('classifyFailure', () => {
  it('separates pin, dns, reachability and everything else', () => {
    expect(classifyFailure(new CertPinMismatch())).toBe('pinMismatch');
    expect(classifyFailure(new MissingCertPin())).toBe('missingPin');
    expect(classifyFailure(new AgentApiError(0, 'CONNECTION', 'lost', new Error('Unable to resolve host "pc.local": No address associated with hostname')))).toBe('dns');
    expect(classifyFailure(new AgentApiError(0, 'CONNECTION', 'lost', new Error('failed to connect after 5000ms')))).toBe('unreachable');
    expect(classifyFailure(new AgentApiError(500, 'INTERNAL', 'boom'))).toBe('other');
    expect(classifyFailure(new Error('x'))).toBe('other');
  });
});

describe('probeRoute', () => {
  const deps = (t: Transport) => ({ transport: t, deviceToken: 'tok', fingerprint: PIN });

  it('reports a healthy, authenticated route with its latency', async () => {
    let clock = 1000;
    const r = await probeRoute({ ...deps(transport((u) => (u.endsWith('/health') ? ((clock += 42), ok({ status: 'ok', os: 'linux' })) : ok({ roots: [] })))), now: () => clock }, host, 'lan', host.address);
    expect(r).toMatchObject({ route: 'lan', failure: 'none', auth: 'accepted', latencyMs: 42 });
    expect(r.health?.os).toBe('linux');
  });

  it('marks a rejected token as denied while the host is still reachable', async () => {
    const r = await probeRoute(deps(transport((u) => (u.endsWith('/health') ? ok({ status: 'ok' }) : ok({ code: 'UNAUTHORIZED', message: 'no' }, 401)))), host, 'lan', host.address);
    expect(r).toMatchObject({ failure: 'none', auth: 'denied' });
  });

  it('keeps auth unknown when the auth check fails for another reason', async () => {
    const r = await probeRoute(deps(transport((u) => (u.endsWith('/health') ? ok({ status: 'ok' }) : ok({ code: 'X', message: 'x' }, 500)))), host, 'lan', host.address);
    expect(r).toMatchObject({ failure: 'none', auth: 'notChecked' });
  });

  it('surfaces a certificate mismatch as a failure of the whole route', async () => {
    const r = await probeRoute(deps(transport(() => coded(ERR_CERT_PIN_MISMATCH))), host, 'lan', host.address);
    expect(r).toMatchObject({ failure: 'pinMismatch', auth: 'notChecked' });
    expect(r.health).toBeUndefined();
  });

  it('does not even try without a pin', async () => {
    let calls = 0;
    const r = await probeRoute({ transport: transport(() => (calls++, ok({}))), deviceToken: 'tok', fingerprint: null }, host, 'lan', host.address);
    expect(r.failure).toBe('missingPin');
    expect(calls).toBe(0);
  });

  it('classifies a dead address', async () => {
    const r = await probeRoute(deps(transport(() => coded(ERR_CONNECTION, 'Unable to resolve host "x"'))), host, 'directHttps', 'x.example');
    expect(r.failure).toBe('dns');
  });
});

describe('probeAll', () => {
  it('checks each route against only its own address, in priority order', async () => {
    const seen: string[] = [];
    const results = await probeAll({ transport: transport((u) => (seen.push(new URL(u).host), ok({ status: 'ok' }))), deviceToken: 't', fingerprint: PIN }, { ...host, internetAddress: 'files.example.com' });
    expect(results.map((r) => r.route)).toEqual(['lan', 'tailscale', 'directHttps']);
    expect(new Set(seen)).toEqual(new Set(['192.168.1.2:8765', '100.1.2.3:8765', 'files.example.com']));
  });
});
