import { ed25519 } from '@noble/curves/ed25519.js';

import { DeviceIdentity, IdentityCorrupt, toBase64 } from './deviceIdentity';
import { MemorySecureStore, SecureKeys } from './secureStore';

const rnd = (n: number) => Uint8Array.from({ length: n }, (_, i) => (i * 7 + 3) & 255);
const seed = rnd(32);
const pubB64 = toBase64(ed25519.getPublicKey(seed));

/** A store whose reads or writes fail on demand, to stand in for a keystore error or a crash mid-write. */
class FlakyStore extends MemorySecureStore {
  failReads = 0;
  failWriteOf: string | null = null;
  async read(key: string) {
    if (this.failReads > 0) {
      this.failReads--;
      throw new Error('keystore unavailable');
    }
    return super.read(key);
  }
  async write(key: string, value: string) {
    if (this.failWriteOf === key) throw new Error('process died');
    return super.write(key, value);
  }
}

describe('DeviceIdentity retry after a failed load', () => {
  it('a later call retries instead of replaying the first failure forever', async () => {
    const store = new FlakyStore();
    await store.write(SecureKeys.devicePrivate, toBase64(seed));
    await store.write(SecureKeys.devicePublic, pubB64);
    store.failReads = 1;
    const id = new DeviceIdentity(store, rnd);
    await expect(id.publicKeyBase64()).rejects.toThrow('keystore unavailable');
    await expect(id.publicKeyBase64()).resolves.toBe(pubB64);
  });
});

describe('DeviceIdentity repairs a half-written identity', () => {
  it('derives the missing public key from the stored private key and keeps the identity', async () => {
    const store = new MemorySecureStore();
    await store.write(SecureKeys.devicePrivate, toBase64(seed));
    const id = new DeviceIdentity(store, () => {
      throw new Error('must not generate a second identity');
    });
    await expect(id.publicKeyBase64()).resolves.toBe(pubB64);
    expect(store.data.get(SecureKeys.devicePublic)).toBe(pubB64);
    expect(store.data.get(SecureKeys.devicePrivate)).toBe(toBase64(seed));
  });

  it('recovers after a crash between writing the two keys', async () => {
    const store = new FlakyStore();
    store.failWriteOf = SecureKeys.devicePublic;
    const first = new DeviceIdentity(store, rnd);
    await expect(first.publicKeyBase64()).rejects.toThrow('process died');
    store.failWriteOf = null;
    const restarted = new DeviceIdentity(store, () => {
      throw new Error('must not generate a second identity');
    });
    await expect(restarted.publicKeyBase64()).resolves.toBe(pubB64);
    expect(store.data.get(SecureKeys.devicePublic)).toBe(pubB64);
  });

  it('still refuses a malformed private key with no public key, and writes nothing', async () => {
    const store = new MemorySecureStore();
    await store.write(SecureKeys.devicePrivate, 'short');
    await expect(new DeviceIdentity(store, rnd).publicKeyBase64()).rejects.toBeInstanceOf(IdentityCorrupt);
    expect(store.data.has(SecureKeys.devicePublic)).toBe(false);
  });

  it('still refuses a public key with no private key (nothing to derive it from)', async () => {
    const store = new MemorySecureStore();
    await store.write(SecureKeys.devicePublic, pubB64);
    await expect(new DeviceIdentity(store, rnd).publicKeyBase64()).rejects.toBeInstanceOf(IdentityCorrupt);
    expect(store.data.has(SecureKeys.devicePrivate)).toBe(false);
  });
});
