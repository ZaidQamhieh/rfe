import type { TransferRecord } from '../../core/native';
import { stripModel } from '../hosts/transferStrip';
import { scheduleAutoRetries } from './autoRetry';
import { groupTransfers, isAutoRetryable, isNameClash, retryableFailures, transferPillLabel, transferTone } from './transferLogic';
import { sectionTransfers } from './transferSections';

// A transfer the person cancelled must read as finished, never as a failure: no error colour, no failed group, no Retry
// and no automatic resume. (A cancel can leave an old error text on the record, so the state alone must decide.)
const cancelled = (over: Partial<TransferRecord> = {}): TransferRecord => ({
  id: 'd1',
  hostId: 'h1',
  address: 'a',
  remotePath: '/Docs/a.txt',
  destPath: 'content://a',
  state: 'CANCELLED',
  direction: 'UPLOAD',
  received: 10,
  total: 100,
  error: 'CONFLICT',
  ...over,
});

describe('a cancelled transfer', () => {
  it('is not a failure in any grouping, even with a stale error text', () => {
    const r = cancelled();
    expect(groupTransfers([r]).failed).toEqual([]);
    expect(groupTransfers([r]).finished).toEqual([r]);
    const s = sectionTransfers([r]);
    expect([s.waiting, s.failed, s.moving]).toEqual([[], [], []]);
    expect(s.done).toEqual([r]);
    expect(isNameClash(r)).toBe(false);
    expect(retryableFailures([r])).toEqual([]);
    expect(isAutoRetryable(cancelled({ error: 'ERR_CONNECTION: timeout' }))).toBe(false);
  });
  it('has the muted look and the Cancelled label, not the error tone', () => {
    expect(transferTone('CANCELLED')).toBe('muted');
    expect(transferPillLabel(cancelled())).toBe('Cancelled');
  });
  it('is never resumed on its own and keeps the bottom strip away', () => {
    const r = cancelled({ error: 'ERR_CONNECTION: timeout' });
    expect(scheduleAutoRetries([r], new Map(), 1_000_000, true).resume).toEqual([]);
    expect(stripModel([r], {}, String)).toBeNull();
  });
});
