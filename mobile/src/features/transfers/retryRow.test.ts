import type { TransferRecord } from '../../core/native';
import { applyTransferUpdate, markRetrying, transferErrorMessage } from './transferLogic';
import { movingLine } from './transferSections';

const rec = (id: string, over: Partial<TransferRecord> = {}): TransferRecord => ({
  id,
  hostId: 'h',
  address: 'a',
  remotePath: '/f',
  destPath: '/d',
  state: 'FAILED',
  received: 0,
  total: 100,
  error: 'ERR_CONNECTION: timeout',
  ...over,
});

describe('a retried transfer no longer shows its earlier failure', () => {
  it('an update that is no longer FAILED replaces the failed row and carries no error text', () => {
    const failed = rec('t1');
    const next = applyTransferUpdate([failed, rec('t2', { state: 'DONE', error: null })], rec('t1', { state: 'RUNNING', received: 40 }));
    const row = next.find((r) => r.id === 't1')!;
    expect(row.state).toBe('RUNNING');
    expect(row.error).toBeNull();
    expect(transferErrorMessage(row.error)).toBe('');
    expect(movingLine(row, 'Studio PC')).toMatch(/40%/);
    expect(next).toHaveLength(2);
  });

  it('drops an error a non-failed record still carries (a running row never shows a failure)', () => {
    const [row] = applyTransferUpdate(null, rec('t1', { state: 'RUNNING', received: 10, error: 'ERR_TLS' }));
    expect(row.error).toBeNull();
  });

  it('keeps the error of a record that is FAILED', () => {
    const [row] = applyTransferUpdate([], rec('t1'));
    expect(row.error).toBe('ERR_CONNECTION: timeout');
  });

  it('Retry queues the failed row at once with its failure cleared, before the engine answers', () => {
    const list = [rec('t1', { received: 20 }), rec('t2')];
    const next = markRetrying(list, 't1');
    expect(next.find((r) => r.id === 't1')).toMatchObject({ state: 'QUEUED', error: null, received: 20 });
    expect(next.find((r) => r.id === 't2')).toMatchObject({ state: 'FAILED', error: 'ERR_CONNECTION: timeout' });
  });

  it('Retry leaves a row that is not failed as it is', () => {
    const running = rec('t1', { state: 'RUNNING', error: null });
    expect(markRetrying([running], 't1')).toEqual([running]);
    expect(markRetrying([running], 'missing')).toEqual([running]);
  });
});
