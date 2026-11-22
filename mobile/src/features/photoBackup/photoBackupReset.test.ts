import { backupTransferId } from './photoBackupLogic';
import { photoBackupStore, resetPhotoBackupRecord } from './photoBackupService';

const mockRemove = jest.fn();
const mockList = jest.fn();
jest.mock('expo-battery', () => ({}));
jest.mock('expo-file-system', () => ({}));
jest.mock('expo-media-library', () => ({}));
jest.mock('../../core/native', () => ({ deviceIdNative: jest.fn(), networkTransports: jest.fn(), transfers: { list: (...a: unknown[]) => mockList(...a), remove: (...a: unknown[]) => mockRemove(...a) } }));
jest.mock('../../services', () => {
  const { MemoryKeyValueStore } = jest.requireActual('../../core/storage/hostStore');
  return { clientForHost: jest.fn(), hostStore: {}, keyValue: new MemoryKeyValueStore() };
});


it('forgetting the backup record also removes the finished and refused backup uploads, and nothing else', async () => {
  const rec = (id: string, state: string, error: string | null = null, direction = 'UPLOAD') => ({ id, state, error, direction });
  mockList.mockResolvedValue([
    rec(backupTransferId('1'), 'DONE'),
    rec(backupTransferId('2'), 'FAILED', 'CONFLICT'),
    rec(backupTransferId('3'), 'RUNNING'),
    rec(backupTransferId('5'), 'FAILED', 'CAPABILITY_DENIED: no'),
    rec(backupTransferId('6'), 'FAILED', 'ERR_CONNECTION: x'),
    rec('manual-upload', 'DONE'),
    rec(backupTransferId('4'), 'DONE', null, 'DOWNLOAD'),
  ]);
  const scope = { hostId: 'h1', root: '/srv' };
  await photoBackupStore.enterScope(scope);
  await photoBackupStore.markDone(scope, ['1', '2']);
  expect((await photoBackupStore.doneIds()).size).toBe(2);
  await resetPhotoBackupRecord();
  expect(mockRemove.mock.calls.map((c) => c[0]).sort()).toEqual([backupTransferId('1'), backupTransferId('2'), backupTransferId('5')].sort());
  expect((await photoBackupStore.doneIds()).size).toBe(0);
});
