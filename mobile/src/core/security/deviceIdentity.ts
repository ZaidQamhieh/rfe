import { ed25519 } from '@noble/curves/ed25519.js';

import { SecureKeys, type SecureStore } from './secureStore';

// Standard base64 (matches Dart base64Encode/Decode used by the Flutter app).
const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

export function toBase64(bytes: Uint8Array): string {
  let out = '';
  for (let i = 0; i < bytes.length; i += 3) {
    const b0 = bytes[i];
    const b1 = bytes[i + 1];
    const b2 = bytes[i + 2];
    out += B64[b0 >> 2] + B64[((b0 & 3) << 4) | ((b1 ?? 0) >> 4)];
    out += i + 1 < bytes.length ? B64[((b1 & 15) << 2) | ((b2 ?? 0) >> 6)] : '=';
    out += i + 2 < bytes.length ? B64[b2 & 63] : '=';
  }
  return out;
}

export function fromBase64(s: string): Uint8Array | null {
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(s) || s.length % 4 !== 0) return null;
  const pad = s.endsWith('==') ? 2 : s.endsWith('=') ? 1 : 0;
  const out = new Uint8Array((s.length / 4) * 3 - pad);
  let o = 0;
  for (let i = 0; i < s.length; i += 4) {
    const n = [0, 1, 2, 3].map((k) => (s[i + k] === '=' ? 0 : B64.indexOf(s[i + k])));
    const v = (n[0] << 18) | (n[1] << 12) | (n[2] << 6) | n[3];
    if (o < out.length) out[o++] = (v >> 16) & 255;
    if (o < out.length) out[o++] = (v >> 8) & 255;
    if (o < out.length) out[o++] = v & 255;
  }
  return out;
}

/** Validates stored key material is a 32-byte seed + 32-byte public key; null on anything else (PR-55). */
export function tryParseKeyPair(priv: string, pub: string): { seed: Uint8Array; publicKey: Uint8Array } | null {
  const seed = fromBase64(priv);
  const publicKey = fromBase64(pub);
  if (!seed || !publicKey || seed.length !== 32 || publicKey.length !== 32) return null;
  return { seed, publicKey };
}

/**
 * The device's permanent Ed25519 identity, stored under the same secure-store
 * keys as the Flutter app so an in-place upgrade keeps the identity every host
 * has pinned. A new identity is generated ONLY when no key material exists;
 * malformed material is surfaced (not silently replaced) so the caller can run
 * the explicit re-pair recovery instead of creating a second identity behind
 * the user's back.
 */
export class DeviceIdentity {
  private cached?: { seed: Uint8Array; publicKey: Uint8Array };
  private pending?: Promise<{ seed: Uint8Array; publicKey: Uint8Array }>;

  constructor(
    private readonly store: SecureStore,
    private readonly randomBytes: (n: number) => Uint8Array,
  ) {}

  private load() {
    if (this.cached) return Promise.resolve(this.cached);
    // concurrent first callers share one load/generate (PR-55)
    this.pending ??= this.loadOrCreate().then((kp) => (this.cached = kp));
    return this.pending;
  }

  private async loadOrCreate() {
    const priv = await this.store.read(SecureKeys.devicePrivate);
    const pub = await this.store.read(SecureKeys.devicePublic);
    if (priv === null && pub === null) return this.generate();
    if (priv === null || pub === null) throw new IdentityCorrupt('partial device identity');
    const parsed = tryParseKeyPair(priv, pub);
    if (!parsed) throw new IdentityCorrupt('malformed device identity');
    const derived = ed25519.getPublicKey(parsed.seed);
    if (toBase64(derived) !== toBase64(parsed.publicKey)) throw new IdentityCorrupt('public key does not match seed');
    return parsed;
  }

  private async generate() {
    const seed = this.randomBytes(32);
    const publicKey = ed25519.getPublicKey(seed);
    await this.store.write(SecureKeys.devicePrivate, toBase64(seed));
    await this.store.write(SecureKeys.devicePublic, toBase64(publicKey));
    return { seed, publicKey };
  }

  async publicKeyBase64(): Promise<string> {
    return toBase64((await this.load()).publicKey);
  }

  /** Signs the UTF-8 bytes of [message] (a challenge nonce); standard base64. */
  async signBase64(message: string): Promise<string> {
    const { seed } = await this.load();
    return toBase64(ed25519.sign(new TextEncoder().encode(message), seed));
  }
}

export class IdentityCorrupt extends Error {
  constructor(message: string) {
    super(`${message}; re-pair the device instead of regenerating silently`);
    this.name = 'IdentityCorrupt';
  }
}
