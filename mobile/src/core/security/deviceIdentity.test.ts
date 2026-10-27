import { ed25519 } from '@noble/curves/ed25519.js';

import { DeviceIdentity, IdentityCorrupt, fromBase64, toBase64, tryParseKeyPair } from './deviceIdentity';
import { MemorySecureStore, SecureKeys } from './secureStore';

const rnd = (n: number) => Uint8Array.from({ length: n }, (_, i) => (i * 7 + 3) & 255);

describe('base64', () => {
  it('round-trips and matches Dart base64Encode', () => {
    for (const len of [0, 1, 2, 3, 31, 32, 33]) {
      const b = Uint8Array.from({ length: len }, (_, i) => (i * 37) & 255);
      expect(fromBase64(toBase64(b))).toEqual(b);
    }
    expect(toBase64(new TextEncoder().encode('Man'))).toBe('TWFu');
    expect(toBase64(new TextEncoder().encode('Ma'))).toBe('TWE=');
    expect(fromBase64('***')).toBeNull();
  });
});

describe('DeviceIdentity', () => {
  it('generates once, persists under the Flutter key names, and signs verifiably', async () => {
    const store = new MemorySecureStore();
    const id = new DeviceIdentity(store, rnd);
    const pub = await id.publicKeyBase64();
    expect(store.data.get(SecureKeys.devicePrivate)).toBeDefined();
    expect(store.data.get(SecureKeys.devicePublic)).toBe(pub);
    const sig = await id.signBase64('nonce-123');
    expect(ed25519.verify(fromBase64(sig)!, new TextEncoder().encode('nonce-123'), fromBase64(pub)!)).toBe(true);
  });

  it('reads an identity written by the Flutter app and keeps its public key', async () => {
    const seed = Uint8Array.from({ length: 32 }, (_, i) => 255 - i);
    const store = new MemorySecureStore();
    await store.write(SecureKeys.devicePrivate, toBase64(seed));
    await store.write(SecureKeys.devicePublic, toBase64(ed25519.getPublicKey(seed)));
    const id = new DeviceIdentity(store, () => {
      throw new Error('must not generate');
    });
    expect(await id.publicKeyBase64()).toBe(toBase64(ed25519.getPublicKey(seed)));
  });

  it('never silently replaces malformed, partial or mismatched material', async () => {
    const partial = new MemorySecureStore();
    await partial.write(SecureKeys.devicePrivate, toBase64(rnd(32)));
    await expect(new DeviceIdentity(partial, rnd).publicKeyBase64()).rejects.toBeInstanceOf(IdentityCorrupt);

    const bad = new MemorySecureStore();
    await bad.write(SecureKeys.devicePrivate, 'short');
    await bad.write(SecureKeys.devicePublic, 'short');
    await expect(new DeviceIdentity(bad, rnd).publicKeyBase64()).rejects.toBeInstanceOf(IdentityCorrupt);

    const mismatch = new MemorySecureStore();
    await mismatch.write(SecureKeys.devicePrivate, toBase64(rnd(32)));
    await mismatch.write(SecureKeys.devicePublic, toBase64(new Uint8Array(32)));
    await expect(new DeviceIdentity(mismatch, rnd).publicKeyBase64()).rejects.toBeInstanceOf(IdentityCorrupt);
    expect(bad.data.get(SecureKeys.devicePrivate)).toBe('short');
  });

  it('concurrent first callers get the same identity', async () => {
    const store = new MemorySecureStore();
    let n = 0;
    const id = new DeviceIdentity(store, (len) => Uint8Array.from({ length: len }, () => ++n & 255));
    const [a, b] = await Promise.all([id.publicKeyBase64(), id.publicKeyBase64()]);
    expect(a).toBe(b);
    expect(tryParseKeyPair(store.data.get(SecureKeys.devicePrivate)!, a)).not.toBeNull();
  });
});
