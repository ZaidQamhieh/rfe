import type { TransferRecord } from '../../core/native';
import { clearStaleTransfers } from './clearStaleTransfers';
import { staleAuthFailures } from './transferLogic';

const rec = (o: Partial<TransferRecord> = {}): TransferRecord => ({ id: 'x', hostId: 'h', address: 'a', remotePath: '/a.txt', destPath: '/tmp/a.txt', state: 'FAILED', received: 0, total: -1, error: 'UNAUTHORIZED', ...o });

describe('staleAuthFailures', () => {
  it('picks only failed rows of that computer that the computer refused for authorization', () => {
    const all = [
      rec({ id: 'stale' }),
      rec({ id: 'stale-detail', error: 'UNAUTHORIZED: device revoked' }),
      rec({ id: 'other-host', hostId: 'h2' }),
      rec({ id: 'other-error', error: 'ERR_CONNECTION: timeout' }),
      rec({ id: 'running', state: 'RUNNING', error: null }),
      rec({ id: 'done', state: 'DONE', error: null }),
    ];
    expect(staleAuthFailures(all, 'h').map((r) => r.id)).toEqual(['stale', 'stale-detail']);
  });
});

describe('clearStaleTransfers', () => {
  it('removes the refused rows of the re-paired computer and nothing else', async () => {
    const rows = [rec({ id: 'a' }), rec({ id: 'b', hostId: 'h2' }), rec({ id: 'c', error: 'ERR_TLS' })];
    const removed: string[] = [];
    await clearStaleTransfers('h', { list: async () => rows, remove: async (id: string) => void removed.push(id) });
    expect(removed).toEqual(['a']);
  });

  it('keeps going when one removal fails and reports it by rejecting', async () => {
    const rows = [rec({ id: 'a' }), rec({ id: 'b' })];
    const removed: string[] = [];
    const api = { list: async () => rows, remove: async (id: string) => { if (id === 'a') throw new Error('busy'); removed.push(id); } };
    await expect(clearStaleTransfers('h', api)).rejects.toThrow('busy');
    expect(removed).toEqual(['b']);
  });
});
