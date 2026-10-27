// Ed25519 device identity for the embedded browser companion. The private
// CryptoKey is persisted in IndexedDB as non-extractable; only the public key
// can be exported for the pairing/login/register proof-of-possession flow.

const DB_NAME = 'rfe-device-identity';
const DB_VERSION = 1;
const STORE_NAME = 'identity';
const IDENTITY_KEY = 'browser-device';
const LEGACY_PRIV_KEY = 'rfe_device_privkey';
const LEGACY_PUB_KEY = 'rfe_device_pubkey';

interface StoredIdentity {
  privateKey: CryptoKey;
  publicKey: CryptoKey;
}

function toBase64(buf: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(buf)));
}

function fromBase64(b64: string): ArrayBuffer {
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0)).buffer;
}

function openIdentityDB(): Promise<IDBDatabase> {
  if (!globalThis.indexedDB) {
    return Promise.reject(new Error('This browser does not support secure device identity storage.'));
  }
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(STORE_NAME)) {
        request.result.createObjectStore(STORE_NAME);
      }
    };
    request.onsuccess = () => {
      request.result.onversionchange = () => request.result.close();
      resolve(request.result);
    };
    request.onerror = () => reject(request.error ?? new Error('Could not open secure device identity storage.'));
    request.onblocked = () => reject(new Error('Secure device identity storage is busy in another tab.'));
  });
}

async function readIdentity(): Promise<StoredIdentity | undefined> {
  const db = await openIdentityDB();
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(STORE_NAME, 'readonly');
    const request = transaction.objectStore(STORE_NAME).get(IDENTITY_KEY);
    let identity: StoredIdentity | undefined;
    request.onsuccess = () => { identity = request.result as StoredIdentity | undefined; };
    request.onerror = () => reject(request.error ?? new Error('Could not read device identity.'));
    transaction.oncomplete = () => { db.close(); resolve(identity); };
    transaction.onerror = () => { db.close(); reject(transaction.error ?? new Error('Could not read device identity.')); };
    transaction.onabort = () => { db.close(); reject(transaction.error ?? new Error('Could not read device identity.')); };
  });
}

async function writeIdentity(identity: StoredIdentity): Promise<void> {
  const db = await openIdentityDB();
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(STORE_NAME, 'readwrite');
    transaction.objectStore(STORE_NAME).put(identity, IDENTITY_KEY);
    transaction.oncomplete = () => { db.close(); resolve(); };
    transaction.onerror = () => { db.close(); reject(transaction.error ?? new Error('Could not save device identity.')); };
    transaction.onabort = () => { db.close(); reject(transaction.error ?? new Error('Could not save device identity.')); };
  });
}

async function validateIdentity(identity: StoredIdentity): Promise<void> {
  if (identity.privateKey.extractable || identity.privateKey.algorithm.name !== 'Ed25519') {
    throw new Error('Stored browser identity is not a non-extractable Ed25519 key. Clear this site’s data and pair again.');
  }
  const challenge = crypto.getRandomValues(new Uint8Array(32));
  const signature = await crypto.subtle.sign('Ed25519', identity.privateKey, challenge);
  const valid = await crypto.subtle.verify('Ed25519', identity.publicKey, signature, challenge);
  if (!valid) throw new Error('Stored browser device identity is invalid. Clear this site’s data and pair again.');
}

async function importLegacyIdentity(): Promise<StoredIdentity | null> {
  let privateKeyBase64: string | null;
  let publicKeyBase64: string | null;
  try {
    privateKeyBase64 = localStorage.getItem(LEGACY_PRIV_KEY);
    publicKeyBase64 = localStorage.getItem(LEGACY_PUB_KEY);
  } catch {
    // Storage may be disabled. We can still create a new IndexedDB identity;
    // users with an inaccessible legacy identity will need to pair again.
    return null;
  }
  if (!privateKeyBase64 || !publicKeyBase64) {
    clearLegacyIdentity();
    return null;
  }

  let identity: StoredIdentity;
  try {
    identity = {
      privateKey: await crypto.subtle.importKey(
        'pkcs8', fromBase64(privateKeyBase64), { name: 'Ed25519' }, false, ['sign'],
      ),
      publicKey: await crypto.subtle.importKey(
        'raw', fromBase64(publicKeyBase64), { name: 'Ed25519' }, true, ['verify'],
      ),
    };
    await validateIdentity(identity);
  } catch {
    // A malformed legacy key can never work; drop it so the next load pairs afresh
    // instead of failing every time with a raw DOMException.
    clearLegacyIdentity();
    return null;
  }
  await writeIdentity(identity);
  clearLegacyIdentity();
  return identity;
}

function clearLegacyIdentity(): void {
  try {
    localStorage.removeItem(LEGACY_PRIV_KEY);
    localStorage.removeItem(LEGACY_PUB_KEY);
  } catch {
    // The non-extractable IndexedDB key is already persisted. A restricted
    // browser may prevent cleanup of its obsolete localStorage copy.
  }
}

async function generateIdentity(): Promise<StoredIdentity> {
  // Export once in memory to re-import the private half as non-extractable.
  // WebCrypto uses the same extractability flag for the generated pair.
  const generated = await crypto.subtle.generateKey(
    { name: 'Ed25519' }, true, ['sign', 'verify'],
  ) as CryptoKeyPair;
  const [privateBytes, publicBytes] = await Promise.all([
    crypto.subtle.exportKey('pkcs8', generated.privateKey),
    crypto.subtle.exportKey('raw', generated.publicKey),
  ]);
  const identity: StoredIdentity = {
    privateKey: await crypto.subtle.importKey(
      'pkcs8', privateBytes, { name: 'Ed25519' }, false, ['sign'],
    ),
    publicKey: await crypto.subtle.importKey(
      'raw', publicBytes, { name: 'Ed25519' }, true, ['verify'],
    ),
  };
  await validateIdentity(identity);
  await writeIdentity(identity);
  return identity;
}

let cached: StoredIdentity | null = null;
let loading: Promise<StoredIdentity> | null = null;

async function loadIdentity(): Promise<StoredIdentity> {
  const stored = await readIdentity();
  if (stored) {
    await validateIdentity(stored);
    return stored;
  }
  // Malformed legacy keys must not wedge sign-in for good: drop them and start a fresh identity.
  let legacy: StoredIdentity | null = null;
  try {
    legacy = await importLegacyIdentity();
  } catch {
    clearLegacyIdentity();
  }
  return legacy ?? generateIdentity();
}

/** The browser's persistent Ed25519 identity, creating it once when needed. */
export async function getDeviceKeyPair(): Promise<CryptoKeyPair> {
  if (cached) return cached;
  loading ??= loadIdentity();
  try {
    cached = await loading;
    return cached;
  } finally {
    loading = null;
  }
}

/** devicePublicKey field: standard base64 of the 32 raw public-key bytes. */
export async function getDevicePublicKeyB64(): Promise<string> {
  const { publicKey } = await getDeviceKeyPair();
  return toBase64(await crypto.subtle.exportKey('raw', publicKey));
}

/** What the agent verifies for /pair and /register: the nonce bound to its certificate fingerprint. */
export function deviceProofMessageV2(certFingerprint: string, nonce: string): string {
  return `rfe-device-proof-v2\n${certFingerprint.toLowerCase()}\n${nonce}`;
}

/** signature field: message (the bound v2 proof, or the bare nonce for /login) signed with the non-extractable key. */
export async function signNonce(message: string): Promise<string> {
  const { privateKey } = await getDeviceKeyPair();
  const signature = await crypto.subtle.sign(
    'Ed25519', privateKey, new TextEncoder().encode(message),
  );
  return toBase64(signature);
}
