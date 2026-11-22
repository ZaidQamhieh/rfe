import { phoneRemovedMessage } from '../../core/humanizeError';
import { t } from '../../i18n';
import { archiveExtensions, audioExtensions, docExtensions, imageExtensions, roleOf, videoExtensions, type EntryRole } from '../../core/entryCategory';
import type { TransferRecord } from '../../core/native';
import { basenameOf } from '../explorer/paths';
import { relativeLabel } from '../hosts/relative';

export const isUpload = (r: TransferRecord) => r.direction === 'UPLOAD';

/** The file name shown for a transfer: the name a download was saved under (a duplicate is "a (1).pdf"), else the target's name on the host. */
export const transferName = (r: TransferRecord) => r.savedName || basenameOf(r.remotePath) || r.remotePath;

/** 0..1, or null while the size is unknown. */
export function transferProgress(r: TransferRecord): number | null {
  if (r.state === 'DONE') return 1;
  if (r.total <= 0) return null;
  return Math.min(1, Math.max(0, r.received / r.total));
}

/** Where a finished transfer lives, for the row's status line. */
export function savedWhere(r: TransferRecord): string {
  if (isUpload(r)) return t('transferUploaded');
  return r.publicUri ? t('transferSavedToDownloads') : t('transferSavedInAppStorage');
}

/** When a finished transfer completed, as a relative phrase ("3 minutes ago"); empty for journals that predate the timestamp. */
export function finishedWhen(r: TransferRecord, now: Date = new Date()): string {
  return r.updatedAt && r.updatedAt > 0 ? relativeLabel(new Date(r.updatedAt), now) : '';
}

export const isActive = (r: TransferRecord) => r.state === 'RUNNING' || r.state === 'QUEUED';
export const isFinished = (r: TransferRecord) => r.state === 'DONE' || r.state === 'CANCELLED';

const DENIED_UPLOAD = t('transferDeniedUpload');
const DAMAGED_IN_TRANSIT = t('transferDamagedInTransit');

// One row per engine error code: the plain-language reason, and whether trying again can work. Retryable codes: the host
// was unreachable or busy, or the data was damaged in transit. Everything else (name clash, no permission, changed
// identity, full phone) fails the same way until something changes. `auto` marks the codes the app retries on its own (a
// bounded number of times, see autoRetry.ts): only "could not reach the computer". A Map, so codes like "constructor" are not inherited.
const FAILURES = new Map<string, { message: string; retryable: boolean; auto?: boolean }>([
  ['CONFLICT', { message: t('transferConflict'), retryable: false }],
  ['FORBIDDEN', { message: DENIED_UPLOAD, retryable: false }],
  ['READ_ONLY', { message: DENIED_UPLOAD, retryable: false }],
  ['CAPABILITY_DENIED', { message: t('transferCapabilityDenied'), retryable: false }],
  ['HASH_MISMATCH', { message: DAMAGED_IN_TRANSIT, retryable: true }],
  ['CHUNK_HASH_MISMATCH', { message: DAMAGED_IN_TRANSIT, retryable: true }],
  ['PAYLOAD_TOO_LARGE', { message: t('transferTooLarge'), retryable: false }],
  ['RESOURCE_LIMIT', { message: t('transferResourceLimit'), retryable: true }],
  ['TRANSFER_ACTIVE', { message: t('transferStillFinishing'), retryable: true }],
  ['NOT_FOUND', { message: t('transferNotFound'), retryable: true }],
  ['ERR_STORAGE_FULL', { message: t('transferStorageFull'), retryable: false }],
  ['ERR_CERT_PIN_MISMATCH', { message: t('transferIdentityChanged'), retryable: false }],
  ['ERR_CONNECTION', { message: t('transferCouldNotReach'), retryable: true, auto: true }],
  ['ERR_TLS', { message: t('transferTlsFailed'), retryable: false }],
  ['DEVICE_KEY_MISMATCH', { message: t('deviceKeyMismatch'), retryable: false }],
  ['UNAUTHORIZED', { get message() { return phoneRemovedMessage(); }, retryable: false }],
]);

/** The engine error code: the text before the first colon. */
const codeOf = (error: string): string => error.split(':')[0].trim();

/** Plain-language reason for a failed transfer, from the engine's error text (an agent API code or an ERR_ tag). */
export function transferErrorMessage(error: string | null): string {
  if (!error) return '';
  const known = FAILURES.get(codeOf(error));
  if (known) return known.message;
  return error.startsWith('source file is missing') ? t('transferSourceMissing') : error;
}

/** The failed transfers of computer [hostId] that it refused for authorization ("no longer accepts this phone"): stale once the phone is paired again. */
export const staleAuthFailures = (all: readonly TransferRecord[], hostId: string): TransferRecord[] =>
  all.filter((r) => r.hostId === hostId && r.state === 'FAILED' && r.error !== null && codeOf(r.error) === 'UNAUTHORIZED');

/** An upload the computer refused because a file with this name is already there: it waits for the owner's choice. */
export const isNameClash = (r: TransferRecord): boolean => r.direction === 'UPLOAD' && r.state === 'FAILED' && r.error !== null && codeOf(r.error) === 'CONFLICT';

/** Failed transfers worth a one-tap "Retry all". */
export const retryableFailures = (all: readonly TransferRecord[]): TransferRecord[] =>
  all.filter((r) => r.state === 'FAILED' && r.error !== null && FAILURES.get(codeOf(r.error))?.retryable === true);

/** A failed transfer the app may resume on its own, within the limits in autoRetry.ts. */
export const isAutoRetryable = (r: TransferRecord): boolean =>
  r.state === 'FAILED' && r.error !== null && FAILURES.get(codeOf(r.error))?.auto === true;

export type VerifyOutcome = 'verified' | 'mismatch' | 'unavailable';

/** Compares the downloaded copy's SHA-256 with the host's. A missing hash is unavailable, never a match. */
export function verifyOutcome(localHash: string | null, hostHash: string | null): VerifyOutcome {
  if (!localHash || !hostHash) return 'unavailable';
  return localHash.toLowerCase() === hostHash.toLowerCase() ? 'verified' : 'mismatch';
}

/** Id made by newTransferId: a kind letter, then the time in base 36 (8 digits), then a random tail. */
const ID_TIME = /^[a-z]([0-9a-z]{8})/;

/** When a transfer was queued: the engine's own time, else the one in the id (a kind letter then the time in base 36), else NaN (photo backup ids carry none). */
const queuedAt = (r: TransferRecord): number => {
  if (r.enqueuedAt && r.enqueuedAt > 0) return r.enqueuedAt;
  const m = ID_TIME.exec(r.id);
  return m ? parseInt(m[1], 36) : NaN;
};

/**
 * Newest first, by queue time across kinds. A row with no known queue time sorts before every row that has one (by id,
 * highest first, among themselves), so the comparison stays a consistent total order.
 */
export function newestFirst(a: TransferRecord, b: TransferRecord): number {
  const at = queuedAt(a);
  const bt = queuedAt(b);
  const aKnown = !Number.isNaN(at);
  if (aKnown !== !Number.isNaN(bt)) return aKnown ? 1 : -1;
  return (aKnown ? bt - at : 0) || b.id.localeCompare(a.id);
}

/** The list the native engine returned merged with the events that arrived meanwhile: an event is newer than the snapshot row with its id. */
export function mergeSnapshot(snapshot: TransferRecord[], live: TransferRecord[] | null): TransferRecord[] {
  if (!live) return snapshot;
  const seen = new Set(live.map((r) => r.id));
  return [...snapshot.filter((r) => !seen.has(r.id)), ...live];
}

/**
 * The list with one engine update folded in: the update replaces the row with its id (or is added). Only a FAILED
 * record keeps an error text, so a retried transfer that is queued or running never shows its earlier failure.
 */
export function applyTransferUpdate(list: readonly TransferRecord[] | null, r: TransferRecord): TransferRecord[] {
  const row = r.state !== 'FAILED' && r.error !== null ? { ...r, error: null } : r;
  return [...(list ?? []).filter((x) => x.id !== r.id), row];
}

/** The list after the owner taps Retry on `id`: a failed row is queued at once with its failure cleared, until the engine's own update arrives. Any other row is left as it is. */
export function markRetrying(list: readonly TransferRecord[], id: string): TransferRecord[] {
  return list.map((r) => (r.id === id && r.state === 'FAILED' ? { ...r, state: 'QUEUED', error: null } : r));
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
  const role = roleOf({ isDir: false, name });
  if (imageExtensions.has(ext)) return { glyph: 'image', role };
  if (videoExtensions.has(ext)) return { glyph: 'video', role };
  if (audioExtensions.has(ext)) return { glyph: 'audio', role };
  if (archiveExtensions.has(ext)) return { glyph: 'archive', role };
  if (docExtensions.has(ext)) return { glyph: 'doc', role };
  return { glyph: 'file', role };
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
