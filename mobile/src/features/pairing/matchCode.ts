import { hmac } from '@noble/hashes/hmac.js';
import { sha256 } from '@noble/hashes/sha2.js';

const hexToBytes = (h: string) => Uint8Array.from(h.match(/../g) ?? [], (b) => parseInt(b, 16));

/**
 * The short code the computer shows next to its approval prompt (agent: pairing.SAS). Keyed by the certificate
 * this phone actually pinned, so a machine relaying the connection makes the two codes differ. The fingerprint
 * itself is never shown. 8 digits, "1234 5678".
 */
export function matchCode(fingerprintHex: string, clientNonceHex: string, requestId: string): string {
  const mac = hmac.create(sha256, hexToBytes(fingerprintHex));
  mac.update(new TextEncoder().encode('rfe-pair-sas\n'));
  mac.update(hexToBytes(clientNonceHex));
  mac.update(new TextEncoder().encode(requestId));
  const d = mac.digest();
  const n = (BigInt(d[0]) << 56n) | (BigInt(d[1]) << 48n) | (BigInt(d[2]) << 40n) | (BigInt(d[3]) << 32n) | (BigInt(d[4]) << 24n) | (BigInt(d[5]) << 16n) | (BigInt(d[6]) << 8n) | BigInt(d[7]);
  const v = Number(n % 100_000_000n);
  const s = String(v).padStart(8, '0');
  return `${s.slice(0, 4)} ${s.slice(4)}`;
}
