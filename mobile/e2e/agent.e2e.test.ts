/**
 * Live contract test against a real rfe. Skipped unless RFE_E2E_ADDR,
 * RFE_E2E_PIN and RFE_E2E_CODE are set:
 *   RFE_E2E_ADDR=127.0.0.1:18765 RFE_E2E_PIN=<sha256> RFE_E2E_CODE=<code> npx jest e2e
 * The Node transport enforces the same policy as the Kotlin one (empty trust
 * store, exact leaf SHA-256 checked in the handshake, no redirects).
 */
import { createHash, randomBytes } from 'node:crypto';
import https from 'node:https';
import type { TLSSocket } from 'node:tls';

import { AgentClient, type Transport } from '../src/core/api/agentClient';
import { CertPinMismatch } from '../src/core/api/pin';
import { DeviceIdentity } from '../src/core/security/deviceIdentity';
import { MemorySecureStore } from '../src/core/security/secureStore';

const ADDR = process.env.RFE_E2E_ADDR;
const PIN = process.env.RFE_E2E_PIN;
const CODE = process.env.RFE_E2E_CODE;
const run = ADDR && PIN && CODE ? describe : describe.skip;

function nodeTransport(): Transport {
  return {
    request: ({ url, method, headers, bodyText, pin }) =>
      new Promise((resolve, reject) => {
        const u = new URL(url);
        const req = https.request(
          {
            host: u.hostname,
            port: u.port,
            path: u.pathname + u.search,
            method,
            headers,
            ca: [], // no system roots: only the pin decides
            rejectUnauthorized: false,
            checkServerIdentity: () => undefined,
            // Do not send anything until the leaf matches the pin.
            createConnection: undefined,
          },
          (res) => {
            const chunks: Buffer[] = [];
            res.on('data', (c: Buffer) => chunks.push(c));
            res.on('end', () =>
              resolve({
                status: res.statusCode ?? 0,
                headers: res.headers as Record<string, string>,
                bodyText: Buffer.concat(chunks).toString('utf8'),
              }),
            );
          },
        );
        req.on('socket', (sock) => {
          (sock as TLSSocket).on('secureConnect', () => {
            const der = (sock as TLSSocket).getPeerCertificate().raw;
            const fp = createHash('sha256').update(der).digest('hex');
            if (pin !== null && fp !== pin) {
              req.destroy(Object.assign(new Error('pin mismatch'), { code: 'ERR_CERT_PIN_MISMATCH' }));
            }
          });
        });
        req.on('error', (e) =>
          reject(
            (e as { code?: string }).code === 'ERR_CERT_PIN_MISMATCH'
              ? e
              : Object.assign(e, { code: 'ERR_CONNECTION' }),
          ),
        );
        if (bodyText !== null) req.write(bodyText);
        req.end();
      }),
  };
}

run('live agent contract', () => {
  const host = { id: 'e2e', label: 'e2e', address: ADDR! };
  const identity = new DeviceIdentity(new MemorySecureStore(), (n) => new Uint8Array(randomBytes(n)));

  it('health works unpinned; pair with device-proof; list root and files', async () => {
    const anon = new AgentClient(host, { transport: nodeTransport() });
    const health = await anon.health();
    expect(health.status).toBe('ok');

    const nonce = await anon.challenge();
    const paired = await new AgentClient(host, { transport: nodeTransport(), pinnedFingerprint: PIN }).pair({
      pairingCode: CODE!,
      deviceLabel: 'rn-e2e',
      devicePublicKey: await identity.publicKeyBase64(),
      nonce,
      signature: await identity.signBase64(nonce),
    });
    expect(paired.deviceToken.length).toBeGreaterThan(10);

    const c = new AgentClient(host, { transport: nodeTransport(), deviceToken: paired.deviceToken, pinnedFingerprint: PIN });
    // With configured roots the agent may report no OS drives; the roots are
    // then browsed from '/' (or the drive path when drives exist).
    const drives = await c.drives();
    const listing = await c.list(drives[0]?.path ?? process.env.RFE_E2E_ROOT ?? '/');
    expect(listing.entries.map((e) => e.name)).toEqual(expect.arrayContaining(['hello.txt', 'docs']));
  });

  it('a wrong pin is rejected as CertPinMismatch', async () => {
    const c = new AgentClient(host, { transport: nodeTransport(), deviceToken: 'x', pinnedFingerprint: '0'.repeat(64) });
    await expect(c.drives()).rejects.toBeInstanceOf(CertPinMismatch);
  });
});
