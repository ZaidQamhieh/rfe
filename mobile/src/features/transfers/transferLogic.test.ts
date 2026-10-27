import type { TransferRecord } from '../../core/native';
import { finishedWhen, transferKind, transferPillLabel, transferSummary, transferTone, groupTransfers, retryableFailures, savedWhere, verifyOutcome, isActive, isFinished, isUpload, transferErrorMessage, transferName, transferProgress } from './transferLogic';

const rec = (o: Partial<TransferRecord> = {}): TransferRecord => ({ id: 'x', hostId: 'h', address: 'a', remotePath: '/docs/a.txt', destPath: '/tmp/a.txt', state: 'RUNNING', received: 0, total: -1, error: null, ...o });

describe('transfer logic', () => {
  it('names a transfer by its host-side file', () => {
    expect(transferName(rec())).toBe('a.txt');
    expect(transferName(rec({ remotePath: 'C:\\x\\b.bin' }))).toBe('b.bin');
  });

  it('treats records without a direction as downloads', () => {
    expect(isUpload(rec())).toBe(false);
    expect(isUpload(rec({ direction: 'UPLOAD' }))).toBe(true);
  });

  it('reports progress only when the size is known', () => {
    expect(transferProgress(rec())).toBeNull();
    expect(transferProgress(rec({ received: 25, total: 100 }))).toBe(0.25);
    expect(transferProgress(rec({ received: 150, total: 100 }))).toBe(1);
    expect(transferProgress(rec({ state: 'DONE' }))).toBe(1);
  });

  it('classifies states', () => {
    expect(isActive(rec({ state: 'QUEUED' }))).toBe(true);
    expect(isActive(rec({ state: 'PAUSED' }))).toBe(false);
    expect(isFinished(rec({ state: 'DONE' }))).toBe(true);
    expect(isFinished(rec({ state: 'FAILED' }))).toBe(false);
  });

  it('explains agent and engine errors', () => {
    expect(transferErrorMessage('CONFLICT')).toMatch(/already exists/);
    expect(transferErrorMessage('READ_ONLY')).toMatch(/not allowed/);
    expect(transferErrorMessage('ERR_CONNECTION: timeout')).toMatch(/Could not reach/);
    expect(transferErrorMessage('ERR_STORAGE_FULL')).toMatch(/out of storage/);
    expect(transferErrorMessage('source file is missing')).toMatch(/no longer on this phone/);
    expect(transferErrorMessage('HTTP 500')).toBe('HTTP 500');
    expect(transferErrorMessage(null)).toBe('');
  });
});

describe('transfer grouping', () => {
  it('orders newest first across kinds and groups by state', () => {
    const all = [
      rec({ id: 'd0000000a', state: 'DONE' }),
      rec({ id: 'u0000000c', state: 'RUNNING' }),
      rec({ id: 'd0000000b', state: 'FAILED' }),
      rec({ id: 'u0000000d', state: 'PAUSED' }),
      rec({ id: 'd0000000e', state: 'CANCELLED' }),
    ];
    const g = groupTransfers(all);
    expect(g.active.map((r) => r.id)).toEqual(['u0000000d', 'u0000000c']);
    expect(g.failed.map((r) => r.id)).toEqual(['d0000000b']);
    expect(g.finished.map((r) => r.id)).toEqual(['d0000000e', 'd0000000a']);
  });
});

describe('saved location', () => {
  it('says where a finished transfer went', () => {
    expect(savedWhere(rec({ direction: 'UPLOAD' }))).toBe('Uploaded');
    expect(savedWhere(rec({ publicUri: 'content://media/external/downloads/1' }))).toBe('Saved to Downloads');
    expect(savedWhere(rec())).toBe('Saved in app storage');
  });
});

describe('transferTone', () => {
  it.each([
    ['RUNNING', 'transfer'],
    ['QUEUED', 'transfer'],
    ['DONE', 'safe'],
    ['PAUSED', 'warn'],
    ['FAILED', 'error'],
    ['CANCELLED', 'muted'],
  ] as const)('%s -> %s', (state, tone) => {
    expect(transferTone(state)).toBe(tone);
  });
});

describe('transfer presentation', () => {
  it('picks a glyph and role from the extension', () => {
    expect(transferKind('Mountain.JPG')).toEqual({ glyph: 'image', role: 'photo' });
    expect(transferKind('a.zip')).toEqual({ glyph: 'archive', role: 'warn' });
    expect(transferKind('plan.pdf')).toEqual({ glyph: 'doc', role: 'doc' });
    expect(transferKind('noext')).toEqual({ glyph: 'file', role: null });
    expect(transferKind('.hidden')).toEqual({ glyph: 'file', role: null });
  });

  it('summarises only what exists', () => {
    expect(transferSummary({ active: [], failed: [], finished: [] })).toBe('Nothing transferring');
    const r = rec();
    expect(transferSummary({ active: [r], failed: [], finished: [r, r] })).toBe('1 in progress · 2 complete');
    expect(transferSummary({ active: [], failed: [r], finished: [] })).toBe('1 failed');
  });

  it('labels the pill', () => {
    expect(transferPillLabel(rec({ received: 72, total: 100 }))).toBe('72%');
    expect(transferPillLabel(rec())).toBe('Active');
    expect(transferPillLabel(rec({ state: 'PAUSED' }))).toBe('Paused');
    expect(transferPillLabel(rec({ state: 'DONE' }))).toBe('Done');
  });
});

describe('finishedWhen', () => {
  const now = new Date('2026-10-01T12:00:00Z');
  it('is a relative phrase for a stamped transfer and empty for an old journal', () => {
    expect(finishedWhen(rec({ updatedAt: now.getTime() - 3 * 60_000 }), now)).not.toBe('');
    expect(finishedWhen(rec({ updatedAt: now.getTime() - 3 * 60_000 }), now)).toContain('3');
    expect(finishedWhen(rec({ updatedAt: 0 }), now)).toBe('');
    expect(finishedWhen(rec(), now)).toBe('');
  });
});

describe('retryableFailures', () => {
  const failed = (id: string, error: string | null) => rec({ id, state: 'FAILED', error });
  it('keeps failures that are worth trying again and drops the ones that will fail the same way', () => {
    const all = [
      failed('a', 'ERR_CONNECTION'),
      failed('b', 'CONFLICT: exists'),
      failed('c', 'HASH_MISMATCH: x'),
      failed('d', 'ERR_CERT_PIN_MISMATCH'),
      failed('e', 'CAPABILITY_DENIED'),
      failed('f', 'RESOURCE_LIMIT'),
      failed('g', 'ERR_STORAGE_FULL'),
      failed('h', null),
    ];
    expect(retryableFailures(all).map((r) => r.id)).toEqual(['a', 'c', 'f']);
  });
  it('ignores records that did not fail', () => {
    expect(retryableFailures([rec({ id: 'r', state: 'RUNNING', error: 'ERR_CONNECTION' }), rec({ id: 'd', state: 'DONE' })])).toEqual([]);
  });
});

describe('verifyOutcome', () => {
  const h = 'a'.repeat(64);
  it('verifies when the phone copy and the host copy hash the same, ignoring case', () => {
    expect(verifyOutcome(h, h.toUpperCase())).toBe('verified');
  });
  it('flags a mismatch: one flipped digit is enough', () => {
    expect(verifyOutcome(h, `${h.slice(0, -1)}b`)).toBe('mismatch');
  });
  it('says unavailable, never verified, when either hash is missing', () => {
    expect(verifyOutcome(null, h)).toBe('unavailable');
    expect(verifyOutcome(h, null)).toBe('unavailable');
    expect(verifyOutcome('', '')).toBe('unavailable');
  });
});
