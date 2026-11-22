import type { TransferRecord } from '../../core/native';
import { t } from '../../i18n';
import { clashColumns, clashWhen, movingLine, pauseAllTarget, percentDone, planClashResolution, sectionTransfers, splitBackupDone } from './transferSections';

const rec = (id: string, over: Partial<TransferRecord> = {}): TransferRecord => ({
  id,
  hostId: 'h1',
  address: 'a',
  remotePath: '/Docs/Budget 2026.xlsx',
  destPath: '/data/uploads/x/Budget 2026.xlsx',
  state: 'RUNNING',
  received: 0,
  total: 100,
  error: null,
  direction: 'UPLOAD',
  ...over,
});

describe('sectionTransfers', () => {
  it('puts upload name clashes in waiting, other failures in failed, live ones in moving and the rest in done', () => {
    const s = sectionTransfers([
      rec('u0000001a', { state: 'FAILED', error: 'CONFLICT: exists' }),
      rec('u0000002a', { state: 'FAILED', error: 'ERR_CONNECTION' }),
      rec('u0000003a', { state: 'RUNNING' }),
      rec('u0000004a', { state: 'PAUSED' }),
      rec('u0000005a', { state: 'DONE' }),
      rec('d0000006a', { state: 'CANCELLED', direction: 'DOWNLOAD' }),
    ]);
    expect(s.waiting.map((r) => r.id)).toEqual(['u0000001a']);
    expect(s.failed.map((r) => r.id)).toEqual(['u0000002a']);
    expect(s.moving.map((r) => r.id).sort()).toEqual(['u0000003a', 'u0000004a']);
    expect(s.done.map((r) => r.id).sort()).toEqual(['d0000006a', 'u0000005a']);
  });

  it('does not treat a failed download with a conflict-like error as waiting', () => {
    const s = sectionTransfers([rec('d0000001a', { state: 'FAILED', error: 'CONFLICT: x', direction: 'DOWNLOAD' })]);
    expect(s.waiting).toEqual([]);
    expect(s.failed).toHaveLength(1);
  });

  it('returns four empty groups for no transfers', () => {
    expect(sectionTransfers([])).toEqual({ waiting: [], moving: [], failed: [], done: [] });
  });
});

describe('splitBackupDone', () => {
  it('takes only finished photo-backup uploads out of the finished list', () => {
    const s = splitBackupDone([
      rec('pb-1', { state: 'DONE' }),
      rec('pb-2', { state: 'DONE' }),
      rec('u0000005a', { state: 'DONE' }),
      rec('pb-3', { state: 'CANCELLED' }),
      rec('d0000006a', { state: 'DONE', direction: 'DOWNLOAD' }),
    ]);
    expect(s.backups.map((r) => r.id)).toEqual(['pb-1', 'pb-2']);
    expect(s.others.map((r) => r.id)).toEqual(['u0000005a', 'pb-3', 'd0000006a']);
  });

  it('leaves a non-backup id that only looks like the prefix in the other list', () => {
    const s = splitBackupDone([rec('pb-1/2', { state: 'DONE' })]);
    expect(s.backups).toEqual([]);
    expect(s.others).toHaveLength(1);
  });

  it('keeps failed and running photo-backup uploads as individual rows', () => {
    const s = sectionTransfers([rec('pb-1', { state: 'FAILED', error: 'ERR_CONNECTION' }), rec('pb-2', { state: 'RUNNING' })]);
    expect(s.failed.map((r) => r.id)).toEqual(['pb-1']);
    expect(s.moving.map((r) => r.id)).toEqual(['pb-2']);
  });

  it('pluralises the summary row title', () => {
    expect(t('trBackupSummary', { count: 14 })).toBe('Photo backup: 14 photos');
    expect(t('trBackupSummary', { count: 1 })).toBe('Photo backup: 1 photo');
  });
});

describe('movingLine and percentDone', () => {
  it('names the computer and the whole percent', () => {
    expect(movingLine(rec('u1', { received: 63, total: 100 }), 'Studio PC')).toBe('to Studio PC · 63%');
    expect(movingLine(rec('d1', { direction: 'DOWNLOAD', received: 50, total: 100 }), 'Studio PC')).toBe('from Studio PC · 50%');
  });

  it('leaves out the percent while the size is unknown and the name while the computer is unknown', () => {
    expect(percentDone(rec('u1', { total: -1 }))).toBeNull();
    expect(movingLine(rec('u1', { total: -1 }), undefined)).toBe('Sending');
    expect(movingLine(rec('d1', { direction: 'DOWNLOAD', total: -1 }), 'Studio PC')).toBe('from Studio PC');
  });
});

describe('pauseAllTarget', () => {
  it('pauses what runs; resumes the paused ones only when nothing runs', () => {
    expect(pauseAllTarget([rec('a', { state: 'RUNNING' }), rec('b', { state: 'PAUSED' })])).toEqual({ ids: ['a'], resume: false });
    expect(pauseAllTarget([rec('b', { state: 'PAUSED' })])).toEqual({ ids: ['b'], resume: true });
    expect(pauseAllTarget([])).toEqual({ ids: [], resume: true });
  });
});

describe('clashColumns', () => {
  const now = new Date(2026, 10, 10, 15, 0);
  it('shows size and when for both files', () => {
    const [a, b] = clashColumns({ size: 51 * 1024, modifiedAt: new Date(2026, 10, 10, 14, 20).getTime() }, { size: 48 * 1024, modifiedAt: new Date(2026, 10, 9, 9, 14).getTime() }, 'Studio PC', now);
    expect(a).toEqual({ heading: 'Sending', size: '51.0 KB', when: 'Today 14:20' });
    expect(b).toEqual({ heading: 'On Studio PC', size: '48.0 KB', when: 'Yesterday 09:14' });
  });

  it('shows unknown rather than zero when the other file is not read yet', () => {
    const [, b] = clashColumns({ size: 10 }, null, 'Studio PC', now);
    expect(b.size).toBe('Unknown');
    expect(b.when).toBe('');
  });

  it('writes an older day as a date', () => {
    expect(clashWhen(new Date(2026, 9, 1, 8, 5).getTime(), now)).toBe('2026-10-01 08:05');
    expect(clashWhen(undefined, now)).toBe('');
    expect(clashWhen(0, now)).toBe('');
  });
});

describe('planClashResolution', () => {
  it('replace keeps the name and overwrites', () => {
    expect(planClashResolution('Budget.xlsx', 'overwrite', new Set(['Budget.xlsx']))).toEqual({ targetName: 'Budget.xlsx', overwrite: true });
  });

  it('keep both picks the next free numbered name', () => {
    expect(planClashResolution('Budget.xlsx', 'keepBoth', new Set(['Budget.xlsx']))).toEqual({ targetName: 'Budget (1).xlsx', overwrite: false });
    expect(planClashResolution('Budget.xlsx', 'keepBoth', new Set(['Budget.xlsx', 'Budget (1).xlsx']))).toEqual({ targetName: 'Budget (2).xlsx', overwrite: false });
  });

  it('skip queues nothing', () => {
    expect(planClashResolution('Budget.xlsx', 'skip', new Set())).toBeNull();
  });
});
