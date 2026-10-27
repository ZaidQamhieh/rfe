import { MemoryKeyValueStore } from '../../core/storage/hostStore';
import { albumsToScan, isAlreadyOnHost, assetIdFromTransferId, assetKey, backupRemotePath, backupTransferId, isFileStable, MAX_STAGED_PHOTOS, pendingIds, PhotoBackupStore, safeExtension, sanitizeSegment, singleFlight, stagingBudget } from './photoBackupLogic';

test('builds date folders, with and without a device segment or root destination', () => {
  const d = new Date(2026, 2, 9);
  expect(backupRemotePath('/srv/photos/', d, 'a.jpg')).toBe('/srv/photos/2026/2026-03/a.jpg');
  expect(backupRemotePath('/srv/photos', d, 'a.jpg', 'Zaid')).toBe('/srv/photos/Zaid/2026/2026-03/a.jpg');
  expect(backupRemotePath('/', d, 'a.jpg')).toBe('/2026/2026-03/a.jpg');
  expect(backupRemotePath('D:\\x', d, 'a.jpg')).toBe('D:\\x/2026/2026-03/a.jpg');
});

test('pending ids keep order; album selection drops missing albums and empty means all', () => {
  expect(pendingIds(['1', '2', '3'], new Set(['2']))).toEqual(['1', '3']);
  expect(albumsToScan(['a', 'b'], new Set())).toEqual(['a', 'b']);
  expect(albumsToScan(['a', 'b'], new Set(['b', 'gone']))).toEqual(['b']);
});

test('single flight shares the in-flight run and starts a new one after it ends', async () => {
  let calls = 0;
  let finish!: (n: number) => void;
  const run = singleFlight(() => {
    calls++;
    return new Promise<number>((r) => (finish = r));
  });
  const a = run();
  const b = run();
  expect(calls).toBe(1);
  finish(7);
  expect(await a).toBe(7);
  expect(await b).toBe(7);
  const c = run();
  expect(calls).toBe(2);
  finish(8);
  expect(await c).toBe(8);
});

test('single flight also frees the slot after a failure', async () => {
  let n = 0;
  const run = singleFlight(async () => {
    if (n++ === 0) throw new Error('first');
    return 'ok';
  });
  await expect(run()).rejects.toThrow('first');
  expect(await run()).toBe('ok');
});

test('staging budget counts held photo copies only', () => {
  const rec = (id: string, state: string, error: string | null = null) => ({ id, state, error });
  const records = [rec('pb-1', 'RUNNING'), rec('pb-2', 'FAILED', 'NETWORK'), rec('pb-3', 'DONE'), rec('pb-4', 'FAILED', 'CONFLICT'), rec('pb-5', 'CANCELLED'), rec('u123', 'RUNNING')];
  expect(stagingBudget(records, 10)).toBe(8);
  expect(stagingBudget(records, 2)).toBe(0);
  expect(stagingBudget([], MAX_STAGED_PHOTOS)).toBe(MAX_STAGED_PHOTOS);
});

test('a file counts as stable only when two reads agree and are non-zero', async () => {
  const wait = async () => {};
  expect(await isFileStable(() => 0, wait)).toBe(false);
  const grow = [10, 20];
  expect(await isFileStable(() => grow.shift() ?? 20, wait)).toBe(false);
  expect(await isFileStable(() => 10, wait)).toBe(true);
});

test('nickname and extension are made safe for a remote path', () => {
  expect(sanitizeSegment('  ../Zaid/phone: 1. ')).toBe('_Zaid_phone_ 1');
  expect(sanitizeSegment('...')).toBe('device');
  expect(sanitizeSegment('x'.repeat(100))).toHaveLength(64);
  expect(safeExtension('IMG_1.HEIC')).toBe('.HEIC');
  expect(safeExtension('../evil')).toBe('.jpg');
  expect(safeExtension('a.toolongext')).toBe('.jpg');
});

test('transfer ids round-trip an asset id and reject foreign ids', () => {
  expect(backupTransferId('123')).toBe('pb-123');
  expect(assetIdFromTransferId('pb-123')).toBe('123');
  expect(assetKey('content://media/external/images/media/20')).toBe('20');
  expect(backupTransferId('content://media/external/images/media/20')).toBe('pb-20');
  expect(assetKey('ph:/A-B/1')).toBe('1');
  expect(assetKey('a/b')).toBe('a_b');
  expect(assetIdFromTransferId('u1abc')).toBeNull();
  expect(assetIdFromTransferId('pb-1/2')).toBeNull();
});

test('prefs and the done record round-trip and read the Flutter encoding', async () => {
  const kv = new MemoryKeyValueStore();
  const store = new PhotoBackupStore(kv);
  expect(await store.load()).toEqual({ enabled: false, hostId: null, deviceName: null, wifiOnly: true, chargingOnly: false, albumIds: [], scheduled: false });
  await kv.set('rfe_photo_backup_enabled', 'true');
  await kv.set('rfe_photo_backup_host', '"h1"');
  await kv.set('rfe_photo_backup_done', '["1","2"]');
  const p = await store.load();
  expect(p).toMatchObject({ enabled: true, hostId: 'h1' });
  await store.save({ ...p, deviceName: 'Phone', albumIds: ['9'] });
  expect(await store.load()).toMatchObject({ deviceName: 'Phone', albumIds: ['9'], scheduled: false });
  await store.save({ ...p, scheduled: true });
  expect((await store.load()).scheduled).toBe(true);
  expect(await kv.get('rfe_photo_backup_scheduled')).toBe('true');
  await store.markDone(['2', '3']);
  expect([...(await store.doneIds())].sort()).toEqual(['1', '2', '3']);
  await store.clearDone();
  expect((await store.doneIds()).size).toBe(0);
});

test('a conflict on the host counts as already backed up, other failures do not', () => {
  expect(isAlreadyOnHost({ state: 'FAILED', error: 'CONFLICT' })).toBe(true);
  expect(isAlreadyOnHost({ state: 'FAILED', error: 'ERR_CONNECTION: x' })).toBe(false);
  expect(isAlreadyOnHost({ state: 'DONE', error: null })).toBe(false);
});
