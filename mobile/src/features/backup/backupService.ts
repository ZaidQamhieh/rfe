import { Directory, File, Paths } from 'expo-file-system';

import { backupDecrypt, backupEncrypt, shareFileExternal } from '../../core/native';
import type { SecureStore } from '../../core/security/secureStore';
import { buildPayload, isBackedUpPrefKey, isBackedUpSecureKey, parsePayload, prefsToKv } from './backupPayload';

/** What a backup reads and replaces. The SQLite implementation runs the replacement in one transaction. */
export interface BackupStorage {
  allRows(): Record<string, string>;
  /** Removes every row the backup owns (see [isBackedUpPrefKey]) and writes [rows], atomically. */
  replaceOwned(rows: Record<string, string>): void;
  secureAll(): Promise<Record<string, string>>;
  secure: SecureStore;
}

/** The encrypted envelope for the current state; the file is opened with the passphrase only. */
export async function exportEnvelope(storage: BackupStorage, passphrase: string): Promise<string> {
  const payload = buildPayload(storage.allRows(), await storage.secureAll());
  return backupEncrypt(JSON.stringify(payload), passphrase);
}

/**
 * Replaces this phone's hosts, tokens, pins and settings with the backup's. A wrong passphrase or damaged file throws
 * before anything is touched; a failure while writing puts the previous state back.
 */
export async function importEnvelope(storage: BackupStorage, envelope: string, passphrase: string): Promise<void> {
  const payload = parsePayload(await backupDecrypt(envelope, passphrase));
  const prevRows = Object.fromEntries(Object.entries(storage.allRows()).filter(([k]) => isBackedUpPrefKey(k)));
  const prevSecure = Object.fromEntries(Object.entries(await storage.secureAll()).filter(([k]) => isBackedUpSecureKey(k)));
  const nextSecure = Object.fromEntries(Object.entries(payload.secure).filter(([k]) => isBackedUpSecureKey(k)));
  try {
    storage.replaceOwned(prefsToKv(payload.prefs));
    for (const k of Object.keys(prevSecure)) if (!(k in nextSecure)) await storage.secure.delete(k);
    for (const [k, v] of Object.entries(nextSecure)) await storage.secure.write(k, v);
  } catch (e) {
    try {
      storage.replaceOwned(prevRows);
      for (const k of Object.keys(nextSecure)) if (!(k in prevSecure)) await storage.secure.delete(k);
      for (const [k, v] of Object.entries(prevSecure)) await storage.secure.write(k, v);
    } catch {
      // Best effort: the original failure is what the caller needs to see.
    }
    throw e;
  }
}

/** Writes the envelope to a file in the share folder and opens the system share sheet for it. */
export async function shareBackupFile(envelope: string, now = new Date()): Promise<boolean> {
  const dir = new Directory(Paths.cache, 'share');
  dir.create({ idempotent: true, intermediates: true });
  // Only the latest backup file stays on disk.
  for (const f of dir.list()) f.delete();
  const stamp = now.toISOString().replace(/[-:]/g, '').replace(/\..*/, '').replace('T', '-');
  const file = new File(dir, `rfe-backup-${stamp}.rfebackup`);
  file.create({ overwrite: true });
  file.write(envelope);
  return shareFileExternal(decodeURIComponent(file.uri.replace('file://', '')), 'application/octet-stream');
}
