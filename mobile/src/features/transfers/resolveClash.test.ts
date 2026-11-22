import type { Host } from '../../core/models/host';
import type { TransferRecord } from '../../core/native';
import { enqueueUploads } from './enqueueUploads';
import { resolveClash } from './resolveClash';
import { transfers } from '../../core/native';
import { clientForHost } from '../../services';

jest.mock('expo-file-system', () => {
  class File {
    uri: string;
    constructor(a: unknown, b?: string) {
      this.uri = typeof a === 'string' ? a : `file:///stage/${b}`;
    }
    async copy() {}
  }
  class Directory {
    create() {}
    delete() {}
  }
  return { File, Directory, Paths: { cache: '/cache', document: '/doc' } };
});
jest.mock('../../core/native', () => ({ transfers: { remove: jest.fn() } }));
jest.mock('../../services', () => ({ clientForHost: jest.fn() }));
jest.mock('./enqueueUploads', () => ({ enqueueUploads: jest.fn() }));

const host: Host = { id: 'h1', label: 'PC', address: '192.168.1.2:8765' };
const record = (id: string) => ({ id, remotePath: '/docs/a.txt', destPath: '/data/a.txt' }) as TransferRecord;
const remove = transfers.remove as jest.Mock;
const enqueue = enqueueUploads as jest.Mock;

beforeEach(() => {
  jest.resetAllMocks();
  (clientForHost as jest.Mock).mockResolvedValue({ list: jest.fn().mockResolvedValue({ entries: [{ name: 'a.txt' }] }) });
  enqueue.mockResolvedValue(1);
  remove.mockResolvedValue(undefined);
});

describe('resolveClash', () => {
  it('queues one upload and forgets the old record', async () => {
    await resolveClash(host, record('t1'), 'keepBoth');
    expect(enqueue).toHaveBeenCalledTimes(1);
    expect(remove).toHaveBeenCalledWith('t1');
  });

  it('a second call for the same record while the first runs queues nothing more', async () => {
    const first = resolveClash(host, record('t2'), 'overwrite');
    const second = resolveClash(host, record('t2'), 'overwrite');
    await Promise.all([first, second]);
    expect(enqueue).toHaveBeenCalledTimes(1);
    expect(remove).toHaveBeenCalledTimes(1);
  });

  it('a retry after the old record could not be removed does not queue a second copy', async () => {
    remove.mockRejectedValueOnce(new Error('db busy'));
    await expect(resolveClash(host, record('t3'), 'keepBoth')).rejects.toThrow('db busy');
    await resolveClash(host, record('t3'), 'keepBoth');
    expect(enqueue).toHaveBeenCalledTimes(1);
    expect(remove).toHaveBeenCalledTimes(2);
  });

  it('a failed enqueue leaves the original and a retry queues again', async () => {
    enqueue.mockRejectedValueOnce(new Error('disk full'));
    await expect(resolveClash(host, record('t4'), 'overwrite')).rejects.toThrow('disk full');
    expect(remove).not.toHaveBeenCalled();
    await resolveClash(host, record('t4'), 'overwrite');
    expect(enqueue).toHaveBeenCalledTimes(2);
    expect(remove).toHaveBeenCalledTimes(1);
  });
});
