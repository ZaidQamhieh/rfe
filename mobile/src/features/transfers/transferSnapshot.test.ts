import type { TransferRecord } from '../../core/native';
import { forgetTransfers } from './forgetTransfers';
import { mergeSnapshot } from './transferLogic';

const rec = (id: string, state: TransferRecord['state'] = 'RUNNING', received = 0): TransferRecord => ({ id, hostId: 'h', address: 'a', remotePath: '/f', destPath: '/d', state, received, total: 10, error: null });

describe('mergeSnapshot', () => {
  it('keeps a live event that arrived before the list answered, instead of the older snapshot row', () => {
    const merged = mergeSnapshot([rec('t1', 'RUNNING', 1), rec('t2', 'QUEUED')], [rec('t1', 'DONE', 10)]);
    expect(merged.find((r) => r.id === 't1')).toMatchObject({ state: 'DONE', received: 10 });
    expect(merged.map((r) => r.id).sort()).toEqual(['t1', 't2']);
  });

  it('is the snapshot when no event arrived yet', () => {
    expect(mergeSnapshot([rec('t1')], null)).toEqual([rec('t1')]);
  });
});

describe('forgetTransfers', () => {
  it('reports a failed removal, still re-reads the list, and does not reject', async () => {
    const remove = jest.fn(async (id: string) => {
      if (id === 't1') throw new Error('busy');
    });
    const list = jest.fn(async () => [rec('t1')]);
    const onError = jest.fn();
    const items = await forgetTransfers([rec('t1'), rec('t2')], { remove, list }, onError);
    expect(remove).toHaveBeenCalledTimes(2);
    expect(onError).toHaveBeenCalledTimes(1);
    expect((onError.mock.calls[0][0] as Error).message).toBe('busy');
    expect(items?.map((r) => r.id)).toEqual(['t1']);
  });

  it('reports a failed re-read and returns null', async () => {
    const onError = jest.fn();
    const items = await forgetTransfers([rec('t1')], { remove: async () => {}, list: async () => Promise.reject(new Error('down')) }, onError);
    expect(items).toBeNull();
    expect(onError).toHaveBeenCalledTimes(1);
  });
});
