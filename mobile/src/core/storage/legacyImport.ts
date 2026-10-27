import { hostFromJson, hostToJson, type Host } from '../models/host';
import { SecureKeys, type SecureStore } from '../security/secureStore';
import { normalizeFingerprint } from '../api/pin';
import { HOSTS_KEY, type KeyValueStore } from './hostStore';

export const IMPORT_DONE_KEY = 'rn_legacy_import_v1';

/** Prefixes/keys of the Flutter app's own SharedPreferences (a superset of the backup's list: bookmarks, pins and digest state live outside `rfe_`). */
export const OWNED_PREFIXES = ['rfe_', 'app.', 'host.', 'settings.', 'bookmarks_v1', 'offline_pins_v1', 'digest.'];

export type ImportReport = {
  status: 'imported' | 'already-done' | 'nothing-to-import';
  hosts: number;
  /** Hosts that lack a usable secure-store pin or token and therefore need re-pairing. */
  needRepair: string[];
  skippedRecords: number;
};

/**
 * One-time import of the Flutter app's non-secret state. Secrets are NOT
 * copied: the RN app reads the same secure-storage entries in place (same
 * package, vendored plugin code), so tokens, pins and the Ed25519 identity keep
 * their original protection. Hosts whose pin/token can't be read are reported
 * for explicit re-pairing instead of being trusted from the metadata mirror.
 * Idempotent, and never overwrites hosts already created by the RN app.
 */
export async function importLegacyState(
  legacyPrefs: Record<string, string>,
  kv: KeyValueStore,
  secure: SecureStore,
): Promise<ImportReport> {
  if ((await kv.get(IMPORT_DONE_KEY)) !== null) return { status: 'already-done', hosts: 0, needRepair: [], skippedRecords: 0 };

  const raw = legacyPrefs[HOSTS_KEY];
  if (raw === undefined) {
    // No computers saved, but the settings, favorites and bookmarks of an earlier install still carry over.
    const copied = await copyOwnedPrefs(legacyPrefs, kv);
    await kv.set(IMPORT_DONE_KEY, '1');
    return { status: copied > 0 ? 'imported' : 'nothing-to-import', hosts: 0, needRepair: [], skippedRecords: 0 };
  }

  let entries: unknown[] = [];
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) entries = parsed;
  } catch {
    // unreadable list: treated as no records
  }

  const hosts: Host[] = [];
  let skipped = 0;
  for (const e of entries) {
    let obj: unknown = e;
    if (typeof e === 'string') {
      try {
        obj = JSON.parse(e);
      } catch {
        obj = null;
      }
    }
    const h = hostFromJson(obj);
    if (h) hosts.push(h);
    else skipped++;
  }

  const needRepair: string[] = [];
  for (const h of hosts) {
    const pin = normalizeFingerprint(await secure.read(SecureKeys.fingerprint(h.id)).catch(() => null));
    const token = await secure.read(SecureKeys.token(h.id)).catch(() => null);
    if (pin === null || !token) needRepair.push(h.id);
  }

  const existingRaw = await kv.get(HOSTS_KEY);
  // A corrupt or non-list value already stored reads as no hosts (as `HostStore.listHosts` does); throwing here would
  // skip the done marker below and retry the import on every start.
  let existing: unknown[] = [];
  try {
    const parsed: unknown = existingRaw ? JSON.parse(existingRaw) : [];
    if (Array.isArray(parsed)) existing = parsed;
  } catch {
    // unreadable: replaced by the imported list
  }
  const existingIds = new Set(existing.map((x) => (x as { id?: string } | null)?.id));
  const merged = [...existing, ...hosts.filter((h) => !existingIds.has(h.id)).map(hostToJson)];
  await kv.set(HOSTS_KEY, JSON.stringify(merged));
  await copyOwnedPrefs(legacyPrefs, kv);
  await kv.set(IMPORT_DONE_KEY, '1');
  return { status: 'imported', hosts: hosts.length, needRepair, skippedRecords: skipped };
}

/**
 * Non-secret state under the app's own key prefixes (settings, favorites, bookmarks, pins, sync rules, ...).
 * Values arrive JSON-encoded from the native reader and keep their Flutter key names, so the RN
 * settings/feature stores read them unchanged. The host list has its own merge. Never overwrites a
 * key the RN app already has; returns how many keys it copied.
 */
async function copyOwnedPrefs(legacyPrefs: Record<string, string>, kv: KeyValueStore): Promise<number> {
  let copied = 0;
  for (const [k, v] of Object.entries(legacyPrefs)) {
    if (k === HOSTS_KEY || !OWNED_PREFIXES.some((p) => k.startsWith(p))) continue;
    if (k.startsWith('rfe_last_seen_') && !/^[0-9]+$/.test(v)) continue;
    if ((await kv.get(k)) === null) {
      await kv.set(k, v);
      copied++;
    }
  }
  return copied;
}
