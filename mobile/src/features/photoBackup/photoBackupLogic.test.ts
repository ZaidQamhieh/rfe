import { MemoryKeyValueStore } from '../../core/storage/hostStore';
import { retryableFailures } from '../transfers/transferLogic';
import { albumsToScan, backupRootPrefix, classifyPrior, isAlreadyOnHost, isInScope, isRefused, REFUSAL_CODES, assetIdFromTransferId, assetKey, backupRemotePath, backupTransferId, isFileStable, MAX_STAGED_PHOTOS, PhotoBackupStore, safeExtension, sanitizeSegment, singleFlight, stagingBudget } from './photoBackupLogic';

test('builds date folders, with and without a device segment or root destination', () => {
  const d = new Date(2026, 2, 9);
  expect(backupRemotePath('/srv/photos/', d, 'a.jpg')).toBe('/srv/photos/2026/2026-03/a.jpg');
  expect(backupRemotePath('/srv/photos', d, 'a.jpg', 'Zaid')).toBe('/srv/photos/Zaid/2026/2026-03/a.jpg');
  expect(backupRemotePath('/', d, 'a.jpg')).toBe('/2026/2026-03/a.jpg');
  expect(backupRemotePath('D:\\x', d, 'a.jpg')).toBe('D:\\x/2026/2026-03/a.jpg');
});

test('album selection drops missing albums and empty means all', () => {
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

test('a nickname cut to its length limit never ends in half an emoji', () => {
  const out = sanitizeSegment('a' + '😀'.repeat(70));
  expect(out).toBe('a' + '😀'.repeat(63));
  expect(out).not.toMatch(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/);
});

test('nickname and extension are made safe for a remote path', () => {
  expect(sanitizeSegment('  ../Zaid/phone: 1. ')).toBe('_Zaid_phone_ 1');
  expect(sanitizeSegment('...')).toBe('device');
  expect(sanitizeSegment('x'.repeat(100))).toHaveLength(64);
  expect(safeExtension('IMG_1.HEIC')).toBe('.HEIC');
  expect(safeExtension('../evil')).toBe('.jpg');
  expect(safeExtension('a.toolongext')).toBe('.jpg');
});

test('a nickname that Windows reserves as a device name is prefixed, with or without an extension', () => {
  expect(sanitizeSegment('CON')).toBe('_CON');
  expect(sanitizeSegment('nul')).toBe('_nul');
  expect(sanitizeSegment('Com1.txt')).toBe('_Com1.txt');
  expect(sanitizeSegment('LPT9.tar.gz')).toBe('_LPT9.tar.gz');
  expect(sanitizeSegment('aux ')).toBe('_aux');
  expect(sanitizeSegment('PRN. ')).toBe('_PRN');
  expect(sanitizeSegment('COM10')).toBe('COM10');
  expect(sanitizeSegment('CONSOLE')).toBe('CONSOLE');
});

test('a nickname never ends in a dot or a space, including after the length cap', () => {
  expect(sanitizeSegment('Zaid. ')).toBe('Zaid');
  expect(sanitizeSegment('a'.repeat(63) + ' x')).toBe('a'.repeat(63));
  expect(sanitizeSegment('a'.repeat(63) + '.x')).toBe('a'.repeat(63));
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
  await store.enterScope({ hostId: 'h1', root: '/srv' });
  expect([...(await store.doneIds())].sort()).toEqual(['1', '2']);
  await store.markDone({ hostId: 'h1', root: '/srv' }, ['2', '3']);
  expect([...(await store.doneIds())].sort()).toEqual(['1', '2', '3']);
  await store.clearDone();
  expect((await store.doneIds()).size).toBe(0);
});

test('a conflict on the host counts as already backed up, other failures do not', () => {
  expect(isAlreadyOnHost({ state: 'FAILED', error: 'CONFLICT' })).toBe(true);
  expect(isAlreadyOnHost({ state: 'FAILED', error: 'ERR_CONNECTION: x' })).toBe(false);
  expect(isAlreadyOnHost({ state: 'DONE', error: null })).toBe(false);
});

describe('refused uploads', () => {
  const failed = (error: string | null) => ({ id: 'pb-1', state: 'FAILED', error });

  test('only a failure the computer refused is refused, and the transfer list agrees it cannot be retried', () => {
    for (const code of REFUSAL_CODES) {
      expect(isRefused(failed(`${code}: no`))).toBe(true);
      expect(retryableFailures([{ ...failed(code) } as never])).toEqual([]);
    }
    expect(isRefused(failed('ERR_CONNECTION: x'))).toBe(false);
    expect(isRefused(failed(null))).toBe(false);
    expect(isRefused({ state: 'PAUSED', error: 'FORBIDDEN' })).toBe(false);
  });

  test('a refused upload does not use up the staging budget', () => {
    const records = [failed('CAPABILITY_DENIED: x'), { id: 'pb-2', state: 'FAILED', error: 'READ_ONLY' }, { id: 'pb-3', state: 'QUEUED', error: null }];
    expect(stagingBudget(records, 10)).toBe(9);
    const many = Array.from({ length: MAX_STAGED_PHOTOS }, (_, i) => ({ id: `pb-${i}`, state: 'FAILED', error: 'FORBIDDEN' }));
    expect(stagingBudget(many)).toBe(MAX_STAGED_PHOTOS);
  });
});

describe('classifyPrior', () => {
  const where = { hostId: 'h1', address: '10.0.0.2:8765', root: '/srv/photos/' };
  const rec = (o: Partial<{ state: string; error: string | null; hostId: string; address: string; remotePath: string }> = {}) => ({ state: 'DONE', error: null, hostId: 'h1', address: '10.0.0.2:8765', remotePath: '/srv/photos/Zaid/2026/2026-03/1.jpg', ...o });
  const staged = () => true;

  test('no record means copy the photo', () => expect(classifyPrior(undefined, where, staged)).toBe('copy'));
  test('queued and running uploads are waiting, not done', () => {
    expect(classifyPrior(rec({ state: 'QUEUED' }), where, staged)).toBe('waiting');
    expect(classifyPrior(rec({ state: 'RUNNING' }), where, staged)).toBe('waiting');
  });
  test('a finished upload or a name clash on this computer and folder is done', () => {
    expect(classifyPrior(rec(), where, staged)).toBe('done');
    expect(classifyPrior(rec({ state: 'FAILED', error: 'CONFLICT' }), where, staged)).toBe('done');
  });
  test('a finished upload that reached another computer or folder is not done', () => {
    expect(classifyPrior(rec({ hostId: 'h2' }), where, staged)).toBe('copy');
    expect(classifyPrior(rec({ remotePath: '/other/Zaid/2026/2026-03/1.jpg' }), where, staged)).toBe('copy');
    expect(classifyPrior(rec({ remotePath: '/srv/photos-old/1.jpg' }), where, staged)).toBe('copy');
  });
  test('an in-flight upload to another computer or folder is busy, not waiting for this target', () => {
    expect(classifyPrior(rec({ state: 'RUNNING', hostId: 'h2' }), where, staged)).toBe('busy');
    expect(classifyPrior(rec({ state: 'QUEUED', remotePath: '/other/1.jpg' }), where, staged)).toBe('busy');
    expect(classifyPrior(rec({ state: 'FAILED', error: 'FORBIDDEN', hostId: 'h2' }), where, staged)).toBe('copy');
  });
  test('a refused upload is left alone', () => expect(classifyPrior(rec({ state: 'FAILED', error: 'CAPABILITY_DENIED: x' }), where, staged)).toBe('refused'));
  test('a failed or paused upload resumes only with its staged copy and on the same address', () => {
    expect(classifyPrior(rec({ state: 'FAILED', error: 'ERR_CONNECTION' }), where, staged)).toBe('resume');
    expect(classifyPrior(rec({ state: 'PAUSED' }), where, staged)).toBe('resume');
    expect(classifyPrior(rec({ state: 'PAUSED' }), where, () => false)).toBe('copy');
    expect(classifyPrior(rec({ state: 'PAUSED', address: '10.0.0.9:8765' }), where, staged)).toBe('copy');
    expect(classifyPrior(rec({ state: 'CANCELLED' }), where, staged)).toBe('copy');
  });
  test('a scope matches on host and folder prefix', () => {
    expect(backupRootPrefix('/')).toBe('/');
    expect(backupRootPrefix('/a//')).toBe('/a/');
    expect(isInScope({ hostId: 'h1', remotePath: '/a/x' }, { hostId: 'h1', root: '/a' })).toBe(true);
    expect(isInScope({ hostId: 'h1', remotePath: '/ab/x' }, { hostId: 'h1', root: '/a' })).toBe(false);
  });
});

describe('done record scope', () => {
  const scopeA = { hostId: 'h1', root: '/srv' };

  test('an old record without a scope is kept and adopted by the first scope', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set('rfe_photo_backup_done', '["1","2"]');
    const store = new PhotoBackupStore(kv);
    expect(await store.activeScope()).toBeNull();
    await store.enterScope(scopeA);
    expect([...(await store.doneIds())]).toEqual(['1', '2']);
    expect(await store.activeScope()).toEqual(scopeA);
  });

  test('another computer or folder starts empty, the same one keeps the record', async () => {
    const store = new PhotoBackupStore(new MemoryKeyValueStore());
    await store.enterScope(scopeA);
    await store.markDone(scopeA, ['1']);
    await store.enterScope({ hostId: 'h1', root: '/srv/' });
    expect((await store.doneIds()).size).toBe(1);
    await store.enterScope({ hostId: 'h1', root: '/other' });
    expect((await store.doneIds()).size).toBe(0);
    await store.markDone({ hostId: 'h1', root: '/other' }, ['2']);
    await store.enterScope({ hostId: 'h2', root: '/other' });
    expect((await store.doneIds()).size).toBe(0);
  });

  test('a late finish for the old scope is dropped, and the count follows the chosen computer', async () => {
    const store = new PhotoBackupStore(new MemoryKeyValueStore());
    await store.enterScope(scopeA);
    await store.markDone(scopeA, ['1']);
    expect(await store.doneCountFor('h1')).toBe(1);
    expect(await store.doneCountFor('h2')).toBe(0);
    await store.enterScope({ hostId: 'h2', root: '/srv' });
    await store.markDone(scopeA, ['9']);
    expect((await store.doneIds()).size).toBe(0);
  });

  test('an unscoped old record is dropped when another computer is chosen before the first run, and counts only for the chosen one', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set('rfe_photo_backup_host', '"h1"');
    await kv.set('rfe_photo_backup_done', '["1","2"]');
    const store = new PhotoBackupStore(kv);
    const prefs = await store.load();
    expect(await store.doneCountFor('h1')).toBe(2);
    expect(await store.doneCountFor(null)).toBe(0);
    await store.save({ ...prefs, wifiOnly: false });
    expect(await store.doneCountFor('h1')).toBe(2);
    await store.save({ ...prefs, hostId: 'h2' });
    expect(await store.doneCountFor('h2')).toBe(0);
    expect((await store.doneIds()).size).toBe(0);
  });

  test('a clear is not undone by a mark that was already reading the record', async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    class Gated extends MemoryKeyValueStore {
      gated = false;
      async get(key: string) {
        if (this.gated && key === 'rfe_photo_backup_done') await gate;
        return super.get(key);
      }
    }
    const kv = new Gated();
    const store = new PhotoBackupStore(kv);
    await store.enterScope(scopeA);
    kv.gated = true;
    const mark = store.markDone(scopeA, ['1']);
    const clear = store.clearDone();
    release();
    await Promise.all([mark, clear]);
    kv.gated = false;
    expect((await store.doneIds()).size).toBe(0);
  });
});
