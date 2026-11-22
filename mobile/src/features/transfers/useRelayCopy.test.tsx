import { act } from 'react';
import { create } from 'react-test-renderer';

import type { Host } from '../../core/models/host';
import type { MemoryKeyValueStore } from '../../core/storage/hostStore';
import { transfers, type TransferRecord } from '../../core/native';
import { keyValue } from '../../services';
import { enqueueDownloads } from './enqueueDownloads';
import { startRelayCopy } from './relayCopy';
import type { RelayJob } from './relayPlan';
import { useRelayCopy } from './useRelayCopy';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

/** What useTransfers hands the hook: a snapshot that can be older than the native list. */
let mockSnapshot: TransferRecord[] | null = [];
let mockHosts: { id: string }[] = [];
let mockHostGate: Promise<void> = Promise.resolve();

jest.mock('../hosts/useTransfers', () => ({ useTransfers: () => mockSnapshot }));
jest.mock('expo-file-system', () => ({ Directory: class {}, File: class {}, Paths: { document: { uri: 'file:///doc' } } }));
jest.mock('./enqueueDownloads', () => ({ enqueueDownloads: jest.fn() }));
jest.mock('../../core/native', () => ({
  transfers: { list: jest.fn(), remove: jest.fn(), enqueueUpload: jest.fn() },
}));
jest.mock('../../services', () => {
  const { MemoryKeyValueStore: Memory } = jest.requireActual('../../core/storage/hostStore');
  return {
    keyValue: new Memory(),
    hostStore: { listHosts: async () => (await mockHostGate, mockHosts) },
    clientForHost: async () => ({ activeAddress: 'pc:8765' }),
  };
});

const list = transfers.list as jest.Mock;
const enqueueUpload = transfers.enqueueUpload as jest.Mock;
const enqueue = enqueueDownloads as jest.Mock;

const rec = (id: string, state: TransferRecord['state']): TransferRecord => ({ id, hostId: 'a', address: 'x', remotePath: '/f', destPath: '/d/' + id + '/f', state, received: 0, total: 0, error: null });
const job = (id: string): RelayJob => ({ downloadId: id, destHostId: 'b', destDir: '/in', name: 'f' });
const storedIds = async () => (JSON.parse((await keyValue.get('relay.jobs')) ?? '[]') as RelayJob[]).map((j) => j.downloadId);

function Mount() {
  useRelayCopy();
  return null;
}

/** Lets the hook's first run, and the promises it is waiting on, finish. */
const settle = () => act(async () => { for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 0)); });

beforeEach(() => {
  jest.clearAllMocks();
  (keyValue as MemoryKeyValueStore).data.clear();
  mockSnapshot = [];
  mockHosts = [{ id: 'b' }];
  mockHostGate = Promise.resolve();
});

it('keeps a job whose download the hook snapshot does not list yet', async () => {
  await keyValue.set('relay.jobs', JSON.stringify([job('d1')]));
  mockSnapshot = [];
  list.mockResolvedValue([rec('d1', 'RUNNING')]);
  let tree!: ReturnType<typeof create>;
  await act(async () => {
    tree = create(<Mount />);
  });
  await settle();
  expect(await storedIds()).toEqual(['d1']);
  expect(enqueueUpload).not.toHaveBeenCalled();
  await act(async () => tree.unmount());
});

it('does not lose a job added while an upload was being queued', async () => {
  await keyValue.set('relay.jobs', JSON.stringify([job('d1')]));
  mockSnapshot = [rec('d1', 'DONE')];
  list.mockResolvedValue([rec('d1', 'DONE')]);
  let open!: () => void;
  mockHostGate = new Promise<void>((r) => (open = r));
  let tree!: ReturnType<typeof create>;
  await act(async () => {
    tree = create(<Mount />);
  });
  await settle();
  // The hook is now waiting inside finishRelay; a new copy is queued meanwhile.
  enqueue.mockResolvedValue(['d2']);
  await startRelayCopy({ id: 'a' } as Host, ['/src/f'], { id: 'b' } as Host, '/in');
  open();
  await settle();
  expect(enqueueUpload).toHaveBeenCalledTimes(1);
  expect(await storedIds()).toEqual(['d2']);
  await act(async () => tree.unmount());
});
