import { OWNED_PREFIXES } from '../../core/storage/legacyImport';

/** One preference as the Flutter backup file typed it. */
export type PrefEntry = { t: 'bool' | 'int' | 'double' | 'string' | 'stringList'; v: boolean | number | string | string[] };

/** The decrypted contents of a backup (same JSON as the Flutter app's `BackupPayload`). */
export type BackupPayload = { version: number; createdAt: string; prefs: Record<string, PrefEntry>; secure: Record<string, string> };

/** Shortest passphrase a backup accepts (enforced natively too). */
export const MIN_PASSPHRASE = 12;

export class BackupFormatError extends Error {}

/**
 * Secure-storage entries that travel in a backup: the per-computer token and pinned certificate fingerprint.
 * Everything else stays put. The device identity is bound to this phone (a backup must not clone it) and the
 * offline-cache key only opens this phone's cache, so neither is exported or restored.
 */
export const isBackedUpSecureKey = (key: string) => key.startsWith('rfe_token_') || key.startsWith('rfe_fp_');

/** Only the app's own preference keys are read from or written to a backup, so a crafted file cannot touch internal state. */
export const isBackedUpPrefKey = (key: string) => OWNED_PREFIXES.some((p) => key.startsWith(p));

/**
 * The app stores each preference as the JSON text of its Flutter value (a bool as `true`, a string with quotes, a
 * list as a JSON array). The host list is kept as objects here and as a list of JSON strings in the file.
 */
export function kvToPrefs(rows: Record<string, string>): Record<string, PrefEntry> {
  const out: Record<string, PrefEntry> = {};
  for (const [key, raw] of Object.entries(rows)) {
    if (!isBackedUpPrefKey(key)) continue;
    let v: unknown;
    try {
      v = JSON.parse(raw);
    } catch {
      v = raw;
    }
    if (typeof v === 'boolean') out[key] = { t: 'bool', v };
    else if (typeof v === 'number' && Number.isFinite(v)) out[key] = { t: Number.isInteger(v) ? 'int' : 'double', v };
    else if (typeof v === 'string') out[key] = { t: 'string', v };
    else if (Array.isArray(v)) out[key] = { t: 'stringList', v: v.map((e) => (typeof e === 'string' ? e : JSON.stringify(e))) };
  }
  return out;
}

/** Inverse of [kvToPrefs]: the rows to write for a backup's preferences. */
export function prefsToKv(prefs: Record<string, PrefEntry>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, e] of Object.entries(prefs)) {
    if (!isBackedUpPrefKey(key)) continue;
    // A list of JSON strings is stored as is: the host and rule readers accept strings or objects.
    out[key] = JSON.stringify(e.v);
  }
  return out;
}

export function buildPayload(rows: Record<string, string>, secureAll: Record<string, string>, now = new Date()): BackupPayload {
  const secure: Record<string, string> = {};
  for (const [k, v] of Object.entries(secureAll)) if (isBackedUpSecureKey(k)) secure[k] = v;
  return { version: 1, createdAt: now.toISOString(), prefs: kvToPrefs(rows), secure };
}

const isStringList = (v: unknown): v is string[] => Array.isArray(v) && v.every((x) => typeof x === 'string');

/** Validates decrypted JSON; throws [BackupFormatError] for anything the format does not allow. */
export function parsePayload(json: string): BackupPayload {
  let d: unknown;
  try {
    d = JSON.parse(json);
  } catch {
    throw new BackupFormatError('This backup file is corrupted.');
  }
  const o = d as { version?: unknown; createdAt?: unknown; prefs?: unknown; secure?: unknown };
  if (typeof d !== 'object' || d === null || typeof o.prefs !== 'object' || o.prefs === null || typeof o.secure !== 'object' || o.secure === null) {
    throw new BackupFormatError('Malformed backup payload.');
  }
  const prefs: Record<string, PrefEntry> = {};
  for (const [k, e] of Object.entries(o.prefs as Record<string, { t?: unknown; v?: unknown }>)) {
    const ok =
      (e?.t === 'bool' && typeof e.v === 'boolean') ||
      ((e?.t === 'int' || e?.t === 'double') && typeof e.v === 'number' && Number.isFinite(e.v)) ||
      (e?.t === 'string' && typeof e.v === 'string') ||
      (e?.t === 'stringList' && isStringList(e.v));
    if (!ok) {
      if (typeof e?.t === 'string' && !['bool', 'int', 'double', 'string', 'stringList'].includes(e.t)) throw new BackupFormatError(`Unknown preference type "${e.t}" in backup.`);
      throw new BackupFormatError('This backup file is corrupted.');
    }
    prefs[k] = e as PrefEntry;
  }
  const secure: Record<string, string> = {};
  for (const [k, v] of Object.entries(o.secure as Record<string, unknown>)) {
    if (typeof v !== 'string') throw new BackupFormatError('This backup file is corrupted.');
    secure[k] = v;
  }
  return { version: typeof o.version === 'number' ? o.version : 1, createdAt: typeof o.createdAt === 'string' ? o.createdAt : '', prefs, secure };
}
