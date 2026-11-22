import { transfers } from '../../core/native';
import { enqueueUploads, PartialUploadError } from './enqueueUploads';
import type { UploadItem } from './uploadPlan';

const mockPresent = new Set<string>();
const mockFailMove = new Set<string>();

jest.mock('expo-file-system', () => {
  class File {
    uri: string;
    constructor(a: unknown, b?: string) {
      this.uri = typeof a === 'string' ? a : `file:///stage/${(a as { name: string }).name}/${b}`;
    }
    get exists() {
      return mockPresent.has(this.uri);
    }
    async move(dest: File) {
      if (mockFailMove.has(this.uri)) throw new Error('disk full');
      mockPresent.delete(this.uri);
      mockPresent.add(dest.uri);
    }
  }
  class Directory {
    name: string;
    constructor(_base: unknown, ...parts: string[]) {
      this.name = parts.join('/');
    }
    create() {}
    delete() {
      for (const u of [...mockPresent]) if (u.startsWith(`file:///stage/${this.name}/`)) mockPresent.delete(u);
    }
  }
  return { File, Directory, Paths: { document: '/doc' } };
});
jest.mock('../../core/native', () => ({ transfers: { enqueueUpload: jest.fn() } }));
jest.mock('../../services', () => ({ clientForHost: jest.fn(async () => ({ activeAddress: 'pc:8765' })) }));

const host = { id: 'h1', label: 'PC', address: 'pc:8765' };
const enqueueUpload = transfers.enqueueUpload as jest.Mock;
const item = (n: string): UploadItem => ({ source: { name: n, uri: `file:///cache/${n}` }, targetName: n, overwrite: false });
const items = [item('a'), item('b'), item('c')];

beforeEach(() => {
  jest.clearAllMocks();
  enqueueUpload.mockReset();
  mockPresent.clear();
  mockFailMove.clear();
  for (const i of items) mockPresent.add(i.source.uri);
  enqueueUpload.mockResolvedValue(undefined);
});

describe('enqueueUploads partial failure', () => {
  it('says how many files are queued and which are still to do, so a retry does not queue the first ones again', async () => {
    mockFailMove.add('file:///cache/b');
    const err = await enqueueUploads(host, '/in', items).catch((e) => e);
    expect(err).toBeInstanceOf(PartialUploadError);
    expect(err.queued).toBe(1);
    expect(err.remaining.map((i: UploadItem) => i.targetName)).toEqual(['b', 'c']);
    expect((err.failure as Error).message).toBe('disk full');
    // A retry of just the remainder queues only those two.
    mockFailMove.clear();
    enqueueUpload.mockClear();
    await expect(enqueueUploads(host, '/in', err.remaining)).resolves.toBe(2);
    expect(enqueueUpload).toHaveBeenCalledTimes(2);
  });

  it('leaves out a file whose staged copy was removed by the failed enqueue (nothing left to retry)', async () => {
    enqueueUpload.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('engine refused'));
    const err = await enqueueUploads(host, '/in', items).catch((e) => e);
    expect(err).toBeInstanceOf(PartialUploadError);
    expect(err.queued).toBe(1);
    expect(err.remaining.map((i: UploadItem) => i.targetName)).toEqual(['c']);
  });

  it('throws the original error when nothing was queued yet', async () => {
    mockFailMove.add('file:///cache/a');
    const err = await enqueueUploads(host, '/in', items).catch((e) => e);
    expect(err).not.toBeInstanceOf(PartialUploadError);
    expect((err as Error).message).toBe('disk full');
  });
});
