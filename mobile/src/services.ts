import { Directory, Paths } from 'expo-file-system';
import { openDatabaseSync } from 'expo-sqlite';

import { AgentClient } from './core/api/agentClient';
import { DeviceIdentity } from './core/security/deviceIdentity';
import { HostStore, type KeyValueStore } from './core/storage/hostStore';
import { SettingsRepo } from './core/settings/settings';
import { importLegacyState, type ImportReport } from './core/storage/legacyImport';
import type { BackupStorage } from './features/backup/backupService';
import { isBackedUpPrefKey } from './features/backup/backupPayload';
import { secureReadAll, offlineBodies, nativeSecureStore, nativeTransport, readLegacyPrefs, secureRandomBytes, probeFingerprint, nativeTransport as _transport , deviceIdNative } from './core/native';
import type { Host } from './core/models/host';

import type { PairingDeps } from './features/pairing/pairingService';

import { forgetRoots } from './features/explorer/rootsCache';
import { ListingCache, type ListingBackend } from './core/storage/listingCache';

// Non-secret key/value storage in SQLite. Secrets never touch it.
const db = openDatabaseSync('rfe.db');
db.execSync('CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY NOT NULL, v TEXT NOT NULL)');

const kv: KeyValueStore = {
  async get(key) {
    return db.getFirstSync<{ v: string }>('SELECT v FROM kv WHERE k = ?', key)?.v ?? null;
  },
  async set(key, value) {
    db.runSync('INSERT OR REPLACE INTO kv (k, v) VALUES (?, ?)', key, value);
  },
  async remove(key) {
    db.runSync('DELETE FROM kv WHERE k = ?', key);
  },
};

/**
 * Cached file bytes are named by a hash of host and path, so one host's files cannot be picked out: forgetting a
 * host empties these regenerable caches (previews, thumbnails, PDF pages and the share/open hand-off copies).
 * The folders themselves stay, because their owners keep a handle to them (audit F-17).
 */
const CONTENT_CACHE_DIRS = ['preview', 'thumbs', 'pdfpages', 'share', 'open'];
function clearContentCaches() {
  for (const name of CONTENT_CACHE_DIRS) {
    try {
      const dir = new Directory(Paths.cache, name);
      if (dir.exists) for (const f of dir.list()) f.delete();
    } catch {
      // best effort: a file in use is evicted by the cache's own size cap later
    }
  }
}

export const hostStore = new HostStore(kv, nativeSecureStore, async (id) => {
  // Forgetting a host drops everything cached for it: listings, encrypted offline bodies, file caches, root paths.
  clearContentCaches();
  await Promise.allSettled([listingCache.evictHost(id), offlineBodies.evictHost(id), forgetRoots(kv, id)]);
});
/** Everything the encrypted backup reads and replaces: the app's own preference rows and its secure-storage entries. */
export const backupStorage: BackupStorage = {
  allRows: () => Object.fromEntries(db.getAllSync<{ k: string; v: string }>('SELECT k, v FROM kv').map((r) => [r.k, r.v])),
  replaceOwned(rows) {
    db.withTransactionSync(() => {
      for (const { k } of db.getAllSync<{ k: string }>('SELECT k FROM kv')) if (isBackedUpPrefKey(k)) db.runSync('DELETE FROM kv WHERE k = ?', k);
      for (const [k, v] of Object.entries(rows)) db.runSync('INSERT OR REPLACE INTO kv (k, v) VALUES (?, ?)', k, v);
    });
  },
  secureAll: () => secureReadAll(),
  secure: nativeSecureStore,
};
export const settingsRepo = new SettingsRepo(kv);
export const keyValue: KeyValueStore = kv;
export const identity = new DeviceIdentity(nativeSecureStore, secureRandomBytes);

let imported: Promise<ImportReport> | undefined;
/** Runs the one-time Flutter state import (idempotent). */
export function ensureLegacyImport(): Promise<ImportReport> {
  imported ??= readLegacyPrefs().then((prefs) => importLegacyState(prefs, kv, nativeSecureStore));
  return imported;
}

/** Builds a client for a paired host using only its secure-store pin and token. */
export async function clientForHost(host: Host, probeLanFirst = false, timeoutMs?: number): Promise<AgentClient> {
  const [pin, token] = await Promise.all([hostStore.getPin(host.id), hostStore.getToken(host.id)]);
  return new AgentClient(host, {
    transport: nativeTransport,
    deviceToken: token ?? undefined,
    pinnedFingerprint: pin,
    probeLanFirst,
    timeoutMs,
  });
}

export function unpinnedClient(host: Host): AgentClient {
  return new AgentClient(host, { transport: nativeTransport });
}

export const pairingDeps: PairingDeps = {
  transport: _transport,
  identity,
  store: hostStore,
  deviceId: deviceIdNative,
  probe: probeFingerprint,
  randomBytes: secureRandomBytes,
};

db.execSync('CREATE TABLE IF NOT EXISTS listing_cache (host TEXT NOT NULL, path TEXT NOT NULL, fetched_at INTEGER NOT NULL, json TEXT NOT NULL, PRIMARY KEY (host, path))');

const listingBackend: ListingBackend = {
  async get(host, path) {
    const r = db.getFirstSync<{ json: string; fetched_at: number }>('SELECT json, fetched_at FROM listing_cache WHERE host = ? AND path = ?', host, path);
    return r ? { json: r.json, fetchedAt: r.fetched_at } : null;
  },
  async put(host, path, json, fetchedAt) {
    db.runSync('INSERT OR REPLACE INTO listing_cache (host, path, fetched_at, json) VALUES (?, ?, ?, ?)', host, path, fetchedAt, json);
  },
  async list(host) {
    return db.getAllSync<{ path: string; fetched_at: number }>('SELECT path, fetched_at FROM listing_cache WHERE host = ? ORDER BY fetched_at ASC', host).map((r) => ({ path: r.path, fetchedAt: r.fetched_at }));
  },
  async remove(host, path) {
    db.runSync('DELETE FROM listing_cache WHERE host = ? AND path = ?', host, path);
  },
  async removeHost(host) {
    db.runSync('DELETE FROM listing_cache WHERE host = ?', host);
  },
};

export const listingCache = new ListingCache(listingBackend);
