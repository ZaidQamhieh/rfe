// A Node implementation of the app's Transport for the live contract test: the same pin rule as the native
// client (compare the peer leaf certificate's SHA-256 before any request bytes are sent), over node:https.
import { createHash } from 'node:crypto';
import https from 'node:https';

import type { Transport } from '../src/core/api/agentClient';
import { ERR_CERT_PIN_MISMATCH, ERR_CONNECTION } from '../src/core/api/agentClient';

const codeError = (message: string, code: string) => Object.assign(new Error(message), { code });

export function nodeTransport(): Transport {
  return {
    request({ url, method, headers, bodyText, pin, timeoutMs }) {
      return new Promise((resolve, reject) => {
        const u = new URL(url);
        const req = https.request(
          {
            hostname: u.hostname,
            port: u.port,
            path: `${u.pathname}${u.search}`,
            method,
            // Node sends no body framing for DELETE unless the length is explicit.
            headers: bodyText !== null ? { ...headers, 'Content-Length': String(Buffer.byteLength(bodyText)) } : headers,
            agent: new https.Agent({ keepAlive: false }),
            // The agent's certificate is self-signed; trust is the pin alone, checked below.
            rejectUnauthorized: false,
            checkServerIdentity: () => undefined,
            timeout: timeoutMs ?? 30_000,
          },
          (res) => {
            const chunks: Buffer[] = [];
            res.on('data', (c: Buffer) => chunks.push(c));
            res.on('end', () => {
              const flat: Record<string, string> = {};
              for (const [k, v] of Object.entries(res.headers)) flat[k] = Array.isArray(v) ? v.join(', ') : (v ?? '');
              resolve({ status: res.statusCode ?? 0, headers: flat, bodyText: Buffer.concat(chunks).toString('utf8') });
            });
          },
        );
        req.on('socket', (socket) => {
          socket.once('secureConnect', () => {
            const cert = (socket as import('node:tls').TLSSocket).getPeerCertificate(true);
            const fp = createHash('sha256').update(cert.raw).digest('hex');
            if (pin !== null && fp !== pin) req.destroy(codeError(`certificate fingerprint mismatch (${fp})`, ERR_CERT_PIN_MISMATCH));
          });
        });
        req.on('timeout', () => req.destroy(codeError('timeout', ERR_CONNECTION)));
        req.on('error', (e: NodeJS.ErrnoException) => reject(e.code === ERR_CERT_PIN_MISMATCH ? e : codeError(e.message, ERR_CONNECTION)));
        if (bodyText !== null) req.write(bodyText);
        req.end();
      });
    },
  };
}
