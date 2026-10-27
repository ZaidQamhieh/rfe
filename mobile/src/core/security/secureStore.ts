// Secret storage interface. The Android implementation runs the vendored
// flutter_secure_storage code so keys written by the Flutter app (same package)
// stay readable after an in-place upgrade. Secrets never go through AsyncStorage,
// logs, or app state trees.

export interface SecureStore {
  read(key: string): Promise<string | null>;
  write(key: string, value: string): Promise<void>;
  delete(key: string): Promise<void>;
}

/** Key names are shared with the Flutter app and must not change. */
export const SecureKeys = {
  devicePrivate: 'rfe_device_identity_private_v1',
  devicePublic: 'rfe_device_identity_public_v1',
  token: (hostId: string) => `rfe_token_${hostId}`,
  fingerprint: (hostId: string) => `rfe_fp_${hostId}`,
} as const;

/** In-memory implementation for tests. */
export class MemorySecureStore implements SecureStore {
  readonly data = new Map<string, string>();
  async read(key: string) {
    return this.data.get(key) ?? null;
  }
  async write(key: string, value: string) {
    this.data.set(key, value);
  }
  async delete(key: string) {
    this.data.delete(key);
  }
}
