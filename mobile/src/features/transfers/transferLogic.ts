import { archiveExtensions, audioExtensions, docExtensions, imageExtensions, videoExtensions, type EntryRole } from '../../core/entryCategory';
import type { TransferRecord } from '../../core/native';
import { basenameOf } from '../explorer/paths';
import { relativeLabel } from '../hosts/relative';

export const isUpload = (r: TransferRecord) => r.direction === 'UPLOAD';

/** The file name shown for a transfer: the target's name on the host for both directions. */
export const transferName = (r: TransferRecord) => basenameOf(r.remotePath) || r.remotePath;

/** 0..1, or null while the size is unknown. */
export function transferProgress(r: TransferRecord): number | null {
  if (r.state === 'DONE') return 1;
  if (r.total <= 0) return null;
  return Math.min(1, Math.max(0, r.received / r.total));
}

/** Where a finished transfer lives, for the row's status line. */
export function savedWhere(r: TransferRecord): string {
  if (isUpload(r)) return 'Uploaded';
  return r.publicUri ? 'Saved to Downloads' : 'Saved in app storage';
}

/** When a finished transfer completed, as a relative phrase ("3 minutes ago"); empty for journals that predate the timestamp. */
export function finishedWhen(r: TransferRecord, now: Date = new Date()): string {
  return r.updatedAt && r.updatedAt > 0 ? relativeLabel(new Date(r.updatedAt), now) : '';
}

export const isActive = (r: TransferRecord) => r.state === 'RUNNING' || r.state === 'QUEUED';
export const isFinished = (r: TransferRecord) => r.state === 'DONE' || r.state === 'CANCELLED';

/** Plain-language reason for a failed transfer, from the engine's error text (an agent API code or an ERR_ tag). */
export function transferErrorMessage(error: string | null): string {
  if (!error) return '';
  const code = error.split(':')[0].trim();
  switch (code) {
    case 'CONFLICT':
      return 'A file with this name already exists on the computer.';
    case 'FORBIDDEN':
    case 'READ_ONLY':
      return 'This device is not allowed to upload here.';
    case 'CAPABILITY_DENIED':
      return 'This device has not been given permission for this. Allow it on the computer, in the device’s access settings.';
    case 'HASH_MISMATCH':
    case 'CHUNK_HASH_MISMATCH':
      return 'The file changed or was damaged in transit. Try again.';
    case 'PAYLOAD_TOO_LARGE':
      return 'The file is too large for this computer to accept.';
    case 'RESOURCE_LIMIT':
      return 'The computer has too many uploads open. Wait for one to finish.';
    case 'TRANSFER_ACTIVE':
      return 'The computer is still finishing the previous attempt. Try again in a moment.';
    case 'NOT_FOUND':
      return 'The computer no longer has this file or upload. Try again.';
    case 'ERR_STORAGE_FULL':
      return 'This phone is out of storage. Free some space, then tap Retry.';
    case 'ERR_CERT_PIN_MISMATCH':
      return 'The computer’s identity changed. Check it in Devices.';
    case 'ERR_CONNECTION':
      return 'Could not reach the computer.';
    default:
      return error.startsWith('source file is missing') ? 'The file to upload is no longer on this phone.' : error;
  }
}

// Failures where trying again can work: the host was unreachable or busy, or the data was damaged in transit.
// Everything else (name clash, no permission, changed identity, full phone) fails the same way until something changes.
const RETRYABLE = new Set(['ERR_CONNECTION', 'TRANSFER_ACTIVE', 'HASH_MISMATCH', 'CHUNK_HASH_MISMATCH', 'NOT_FOUND', 'RESOURCE_LIMIT']);

/** Failed transfers worth a one-tap "Retry all". */
export const retryableFailures = (all: readonly TransferRecord[]): TransferRecord[] =>
  all.filter((r) => r.state === 'FAILED' && r.error !== null && RETRYABLE.has(r.error.split(':')[0].trim()));

export type VerifyOutcome = 'verified' | 'mismatch' | 'unavailable';

/** Compares the downloaded copy's SHA-256 with the host's. A missing hash is unavailable, never a match. */
export function verifyOutcome(localHash: string | null, hostHash: string | null): VerifyOutcome {
  if (!localHash || !hostHash) return 'unavailable';
  return localHash.toLowerCase() === hostHash.toLowerCase() ? 'verified' : 'mismatch';
}

/** Newest first. Ids are a kind letter then the enqueue time in base 36, so the time orders them across kinds. */
export function newestFirst(a: TransferRecord, b: TransferRecord): number {
  const at = parseInt(a.id.slice(1, 9), 36);
  const bt = parseInt(b.id.slice(1, 9), 36);
  if (Number.isNaN(at) || Number.isNaN(bt)) return b.id.localeCompare(a.id);
  return bt - at || b.id.localeCompare(a.id);
}

export type TransferGroups = { active: TransferRecord[]; failed: TransferRecord[]; finished: TransferRecord[] };

export function groupTransfers(all: TransferRecord[]): TransferGroups {
  const sorted = [...all].sort(newestFirst);
  return {
    active: sorted.filter((r) => isActive(r) || r.state === 'PAUSED'),
    failed: sorted.filter((r) => r.state === 'FAILED'),
    finished: sorted.filter(isFinished),
  };
}

export type TransferTone = 'transfer' | 'safe' | 'warn' | 'error' | 'muted';

/** Colour role of a transfer row by state (design `Roles`; errors use the scheme's error colour). */
export function transferTone(state: TransferRecord['state']): TransferTone {
  switch (state) {
    case 'RUNNING':
    case 'QUEUED':
      return 'transfer';
    case 'DONE':
      return 'safe';
    case 'PAUSED':
      return 'warn';
    case 'FAILED':
      return 'error';
    default:
      return 'muted';
  }
}

export type TransferKind = { glyph: 'image' | 'video' | 'audio' | 'archive' | 'doc' | 'file'; role: EntryRole | null };

/** Glyph and colour role of the file a transfer moves, from its extension (the record carries no MIME type). */
export function transferKind(name: string): TransferKind {
  const dot = name.lastIndexOf('.');
  const ext = dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
  if (imageExtensions.has(ext)) return { glyph: 'image', role: 'photo' };
  if (videoExtensions.has(ext)) return { glyph: 'video', role: 'photo' };
  if (audioExtensions.has(ext)) return { glyph: 'audio', role: 'route' };
  if (archiveExtensions.has(ext)) return { glyph: 'archive', role: 'warn' };
  if (docExtensions.has(ext)) return { glyph: 'doc', role: 'doc' };
  return { glyph: 'file', role: null };
}

/** The page subtitle: real counts only ("1 in progress · 2 complete · 1 failed"). */
export function transferSummary(g: TransferGroups): string {
  const parts: string[] = [];
  if (g.active.length > 0) parts.push(`${g.active.length} in progress`);
  if (g.finished.length > 0) parts.push(`${g.finished.length} complete`);
  if (g.failed.length > 0) parts.push(`${g.failed.length} failed`);
  return parts.length > 0 ? parts.join(' · ') : 'Nothing transferring';
}

/** Short state label for the row's pill: the percentage while it moves, else the state in words. */
export function transferPillLabel(r: TransferRecord): string {
  switch (r.state) {
    case 'DONE':
      return 'Done';
    case 'PAUSED':
      return 'Paused';
    case 'FAILED':
      return 'Failed';
    case 'CANCELLED':
      return 'Cancelled';
    case 'QUEUED':
      return 'Waiting';
    default: {
      const p = transferProgress(r);
      return p === null ? 'Active' : `${Math.floor(p * 100)}%`;
    }
  }
}
