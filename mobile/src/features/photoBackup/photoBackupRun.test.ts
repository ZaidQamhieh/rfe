import type { MemoryKeyValueStore } from '../../core/storage/hostStore';
import { transfers } from '../../core/native';
import { keyValue } from '../../services';
import { backupTransferId } from './photoBackupLogic';
import { photoBackupStore, resetPhotoBackupRecord, runPhotoBackup, watchPhotoBackup } from './photoBackupService';

let mockAssets: { id: string }[] = [];
let mockAlbums: { id: string }[] = [];
/** File lengths the next reads return, then a steady 10; two different ones make a file look like it is still being written. */
let mockSizes: number[] = [];
let mockHosts: { id: string; label: string; address: string }[] = [];
let mockStatus = { photoBackupRoot: '/srv/pics', photoBackupConfigured: true, photoBackupAvailable: true };

let mockBattery = 2;
/** What the photo permission dialog and the silent permission check answer. */
let mockDialogGranted = true;
let mockHasAccess = true;
const mockRequestPermissions = jest.fn(async () => ({ granted: mockDialogGranted }));
jest.mock('expo-battery', () => ({ BatteryState: { CHARGING: 2, FULL: 3 }, getBatteryStateAsync: async () => mockBattery }));
jest.mock('../../core/sleep', () => ({ sleep: async () => {} }));
jest.mock('expo-media-library', () => {
  class Query {
    album() {
      return this;
    }
    eq() {
      return this;
    }
    exe() {
      return Promise.resolve(mockAssets.map((a) => ({ ...a, getUri: async () => 'file:///src/x.jpg', getFilename: async () => 'x.jpg', getCreationTime: async () => Date.UTC(2026, 2, 9, 12) })));
    }
  }
  return {
    Query,
    AssetField: { MEDIA_TYPE: 'mediaType' },
    MediaType: { IMAGE: 'image' },
    requestPermissionsAsync: () => mockRequestPermissions(),
    getPermissionsAsync: async () => ({ granted: mockHasAccess }),
    Album: { getAll: () => Promise.resolve(mockAlbums) },
  };
});
jest.mock('expo-file-system', () => {
  const join = (parts: unknown[]) => parts.map((p) => (typeof p === 'string' ? p : (p as { uri: string }).uri)).join('/');
  class File {
    uri: string;
    constructor(...parts: unknown[]) {
      this.uri = join(parts);
    }
    get size() {
      return mockSizes.shift() ?? 10;
    }
    get exists() {
      return true;
    }
    copy() {}
  }
  class Directory {
    uri: string;
    constructor(...parts: unknown[]) {
      this.uri = join(parts);
    }
    create() {}
    delete() {}
  }
  return { File, Directory, Paths: { document: { uri: 'file:///doc' } } };
});
jest.mock('../../core/native', () => ({
  deviceIdNative: jest.fn(async () => 'abcdefgh1234'),
  networkTransports: jest.fn(async () => ['wifi']),
  transfers: { list: jest.fn(), remove: jest.fn(), resume: jest.fn(), pause: jest.fn(), enqueueUpload: jest.fn(), subscribe: jest.fn() },
}));
jest.mock('../../services', () => {
  const { MemoryKeyValueStore: Memory } = jest.requireActual('../../core/storage/hostStore');
  return {
    keyValue: new Memory(),
    hostStore: { listHosts: async () => mockHosts },
    clientForHost: async () => ({ status: async () => mockStatus, activeAddress: 'pc:8765' }),
  };
});

const list = transfers.list as jest.Mock;
const enqueue = transfers.enqueueUpload as jest.Mock;
const resume = transfers.resume as jest.Mock;
const remove = transfers.remove as jest.Mock;
const pause = transfers.pause as jest.Mock;
const subscribe = transfers.subscribe as jest.Mock;

const asset = (n: number) => ({ id: `content://media/external/images/media/${n}` });
const prefs = (hostId: string, albumIds: string[] = []) => ({ enabled: true, hostId, deviceName: 'Zaid', wifiOnly: false, chargingOnly: false, albumIds, scheduled: false });
/** An upload record the way the transfer engine reports it, made for [hostId] under /srv/pics. */
const rec = (n: number, state: string, error: string | null = null, hostId = 'h1', root = '/srv/pics') => ({
  id: backupTransferId(`${n}`),
  hostId,
  address: 'pc:8765',
  remotePath: `${root}/Zaid/2026/2026-03/${n}.jpg`,
  destPath: `/doc/uploads/pb-${n}/${n}.jpg`,
  state,
  error,
  direction: 'UPLOAD',
});

beforeEach(async () => {
  jest.clearAllMocks();
  (keyValue as MemoryKeyValueStore).data.clear();
  mockAssets = [asset(1)];
  mockAlbums = [{ id: 'a1' }];
  mockSizes = [];
  mockHosts = [{ id: 'h1', label: 'PC one', address: 'pc:8765' }, { id: 'h2', label: 'PC two', address: 'pc2:8765' }];
  mockStatus = { photoBackupRoot: '/srv/pics', photoBackupConfigured: true, photoBackupAvailable: true };
  list.mockResolvedValue([]);
  mockDialogGranted = true;
  mockHasAccess = true;
  await photoBackupStore.save(prefs('h1'));
});

it('does not report up to date while photos are still queued or uploading', async () => {
  mockAssets = [asset(1), asset(2)];
  list.mockResolvedValue([rec(1, 'QUEUED'), rec(2, 'RUNNING')]);
  expect(await runPhotoBackup()).toEqual({ kind: 'waiting', count: 2 });
  expect(enqueue).not.toHaveBeenCalled();
});

it('reports refused photos with the reason and neither resumes nor counts them as backed up', async () => {
  list.mockResolvedValue([rec(1, 'FAILED', 'CAPABILITY_DENIED: no')]);
  const r = await runPhotoBackup();
  expect(r).toMatchObject({ kind: 'refused', count: 1 });
  expect((r as { reason: string }).reason).toMatch(/permission/);
  expect(resume).not.toHaveBeenCalled();
  expect(enqueue).not.toHaveBeenCalled();
});

it('still backs up new photos when 100 earlier ones were refused', async () => {
  mockAssets = [asset(500)];
  list.mockResolvedValue(Array.from({ length: 100 }, (_, i) => rec(i + 1, 'FAILED', 'FORBIDDEN')));
  expect(await runPhotoBackup()).toEqual({ kind: 'enqueued', count: 1 });
  expect(enqueue).toHaveBeenCalledTimes(1);
});

it('starts again for another computer or another folder instead of skipping every photo', async () => {
  expect(await runPhotoBackup()).toEqual({ kind: 'enqueued', count: 1 });
  expect(enqueue).toHaveBeenCalledTimes(1);

  // The upload finished on the first computer: the same computer and folder is now up to date.
  list.mockResolvedValue([rec(1, 'DONE')]);
  expect(await runPhotoBackup()).toEqual({ kind: 'upToDate' });
  expect(await photoBackupStore.doneCountFor('h1')).toBe(1);
  expect(await photoBackupStore.doneCountFor('h2')).toBe(0);
  enqueue.mockClear();

  // Another computer: the finished upload went elsewhere, so the photo is uploaded again.
  await photoBackupStore.save(prefs('h2'));
  expect(await runPhotoBackup()).toEqual({ kind: 'enqueued', count: 1 });
  expect(remove).toHaveBeenCalledWith(backupTransferId('1'));
  expect(enqueue.mock.calls[0][1]).toBe('h2');
  enqueue.mockClear();

  // Same computer, another destination folder.
  await photoBackupStore.save(prefs('h1'));
  list.mockResolvedValue([rec(1, 'DONE', null, 'h1', '/srv/pics')]);
  mockStatus = { ...mockStatus, photoBackupRoot: '/srv/other' };
  expect(await runPhotoBackup()).toEqual({ kind: 'enqueued', count: 1 });
  expect(enqueue.mock.calls[0][4]).toMatch(/^\/srv\/other\//);
});

it('does not call a photo waiting, or let it take staging places, when it is still uploading to another computer', async () => {
  list.mockResolvedValue([rec(1, 'RUNNING', null, 'h2')]);
  expect(await runPhotoBackup()).toEqual({ kind: 'incomplete', count: 1 });
  expect(enqueue).not.toHaveBeenCalled();
  expect(remove).not.toHaveBeenCalled();

  mockAssets = [asset(500)];
  list.mockResolvedValue(Array.from({ length: 100 }, (_, i) => rec(i + 1, 'QUEUED', null, 'h2')));
  expect(await runPhotoBackup()).toEqual({ kind: 'enqueued', count: 1 });
});

it('does not report up to date for a photo whose file is still being written', async () => {
  mockSizes = [10, 20];
  expect(await runPhotoBackup()).toEqual({ kind: 'incomplete', count: 1 });
  expect(enqueue).not.toHaveBeenCalled();
});

it('sends a refused photo again once the record was reset', async () => {
  list.mockResolvedValue([rec(1, 'FAILED', 'CAPABILITY_DENIED: no')]);
  expect(await runPhotoBackup()).toMatchObject({ kind: 'refused' });
  await resetPhotoBackupRecord();
  expect(remove).toHaveBeenCalledWith(backupTransferId('1'));
  list.mockResolvedValue([]);
  expect(await runPhotoBackup()).toEqual({ kind: 'enqueued', count: 1 });
});

it('says so when every chosen album is gone instead of reporting up to date', async () => {
  await photoBackupStore.save(prefs('h1', ['deleted-album']));
  expect(await runPhotoBackup()).toEqual({ kind: 'albumsGone' });
  expect(enqueue).not.toHaveBeenCalled();
});

it('reports the computer as not set up when its status carries no backup folder at all', async () => {
  mockStatus = {} as typeof mockStatus;
  expect(await runPhotoBackup()).toEqual({ kind: 'serverNotConfigured' });
  expect(enqueue).not.toHaveBeenCalled();
});

describe('photo permission', () => {
  it('asks once and stays denied when the owner refuses the dialog', async () => {
    mockDialogGranted = false;
    expect(await runPhotoBackup()).toEqual({ kind: 'permissionDenied' });
    expect(mockRequestPermissions).toHaveBeenCalledTimes(1);
  });

  it('asks on screen when it joined a background run that could not ask', async () => {
    mockHasAccess = false;
    const background = runPhotoBackup({ interactive: false });
    const interactive = runPhotoBackup();
    expect(await background).toEqual({ kind: 'permissionDenied' });
    expect(await interactive).toEqual({ kind: 'enqueued', count: 1 });
    expect(mockRequestPermissions).toHaveBeenCalledTimes(1);
  });
});

describe('constraints after the photos are queued', () => {
  const watchAndEmit = async (r: ReturnType<typeof rec>) => {
    watchPhotoBackup();
    const listener = subscribe.mock.calls[0][0] as (r: unknown) => void;
    listener(r);
    await new Promise((res) => setTimeout(res, 0));
  };

  it('pauses running photo uploads once charging-only stops holding', async () => {
    await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
    mockBattery = 1;
    list.mockResolvedValue([rec(1, 'RUNNING'), rec(2, 'RUNNING')]);
    await watchAndEmit(rec(1, 'RUNNING'));
    expect(pause).toHaveBeenCalledTimes(2);
    mockBattery = 2;
  });

  it('leaves uploads running while the constraint holds', async () => {
    await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
    mockBattery = 2;
    list.mockResolvedValue([rec(1, 'RUNNING')]);
    await watchAndEmit(rec(3, 'RUNNING'));
    expect(pause).not.toHaveBeenCalled();
  });

  it('lets uploads it paused go on at the next run that finds the constraint holding', async () => {
    await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
    mockBattery = 1;
    list.mockResolvedValue([rec(1, 'RUNNING')]);
    await watchAndEmit(rec(1, 'RUNNING'));
    expect(pause).toHaveBeenCalledWith(backupTransferId('1'));
    mockBattery = 2;
    list.mockResolvedValue([rec(1, 'PAUSED')]);
    mockAssets = [asset(1)];
    await runPhotoBackup();
    expect(resume).toHaveBeenCalledWith(backupTransferId('1'));
  });

  it('leaves an upload the owner paused by hand paused', async () => {
    list.mockResolvedValue([rec(1, 'PAUSED'), rec(2, 'PAUSED')]);
    mockAssets = [asset(1), asset(2)];
    expect(await runPhotoBackup()).toEqual({ kind: 'waiting', count: 2 });
    expect(resume).not.toHaveBeenCalled();
  });

  it('leaves an upload paused by hand again after the owner resumed one the app had paused', async () => {
    await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
    mockBattery = 1;
    list.mockResolvedValue([rec(1, 'RUNNING')]);
    await watchAndEmit(rec(1, 'RUNNING'));
    expect(pause).toHaveBeenCalledWith(backupTransferId('1'));
    const listener = subscribe.mock.calls[0][0] as (r: unknown) => void;
    const emit = async (r: ReturnType<typeof rec>) => {
      listener(r);
      await new Promise((res) => setTimeout(res, 0));
    };
    // The engine reports the app's pause, then the owner resumes the upload by hand and pauses it again.
    await emit(rec(1, 'PAUSED'));
    await emit(rec(1, 'RUNNING'));
    await emit(rec(1, 'PAUSED'));
    mockBattery = 2;
    mockAssets = [asset(1)];
    list.mockResolvedValue([rec(1, 'PAUSED')]);
    expect(await runPhotoBackup()).toEqual({ kind: 'waiting', count: 1 });
    expect(resume).not.toHaveBeenCalled();
  });

  /** Subscribes, and returns an emitter that delivers one engine event and lets the listener's async work settle. */
  const subscribeAndEmitter = () => {
    watchPhotoBackup();
    const listener = subscribe.mock.calls[0][0] as (r: unknown) => void;
    return async (r: ReturnType<typeof rec>) => {
      listener(r);
      await new Promise((res) => setTimeout(res, 0));
    };
  };

  it('does not let a startup snapshot that predates a pause undo that pause', async () => {
    await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
    mockBattery = 1;
    let deliverSnapshot!: (records: unknown[]) => void;
    // The first list is the one the watcher takes at startup; it is slow to arrive.
    list.mockReturnValueOnce(new Promise((res) => (deliverSnapshot = res)));
    list.mockResolvedValue([rec(1, 'RUNNING'), rec(2, 'RUNNING')]);
    const emit = subscribeAndEmitter();
    // The constraint stops holding: both uploads are paused by the app while the snapshot is still on its way.
    await emit(rec(2, 'RUNNING'));
    expect(pause).toHaveBeenCalledTimes(2);
    // The late snapshot shows upload 1 paused (as it was before), then the engine's in-flight progress for it arrives.
    deliverSnapshot([rec(1, 'PAUSED')]);
    await new Promise((res) => setTimeout(res, 0));
    await emit(rec(1, 'RUNNING'));
    mockBattery = 2;
    mockAssets = [asset(1), asset(2)];
    list.mockResolvedValue([rec(1, 'PAUSED'), rec(2, 'PAUSED')]);
    await runPhotoBackup();
    expect(resume.mock.calls.map((c) => c[0])).toEqual([backupTransferId('1'), backupTransferId('2')]);
  });

  it('resumes an upload the app paused a second time after the owner had resumed it by hand', async () => {
    const now = jest.spyOn(Date, 'now');
    try {
      await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
      mockBattery = 1;
      list.mockResolvedValue([rec(1, 'RUNNING')]);
      const emit = subscribeAndEmitter();
      // First pause by the app, confirmed by the engine.
      await emit(rec(1, 'RUNNING'));
      await emit(rec(1, 'PAUSED'));
      expect(pause).toHaveBeenCalledTimes(1);
      // The owner resumes upload 1 by hand; before that event reaches the app, the next constraint check
      // (triggered by upload 2) finds it running and pauses it again.
      now.mockReturnValue(Date.now() + 60_000);
      list.mockResolvedValue([rec(1, 'RUNNING'), rec(2, 'RUNNING')]);
      await emit(rec(2, 'RUNNING'));
      expect(pause).toHaveBeenCalledTimes(3);
      // The owner's resume event arrives late; it must not cancel the app's newer pause.
      await emit(rec(1, 'RUNNING'));
      mockBattery = 2;
      mockAssets = [asset(1), asset(2)];
      list.mockResolvedValue([rec(1, 'PAUSED'), rec(2, 'PAUSED')]);
      await runPhotoBackup();
      expect(resume.mock.calls.map((c) => c[0])).toEqual([backupTransferId('1'), backupTransferId('2')]);
    } finally {
      now.mockRestore();
    }
  });

  it('keeps held an upload that a system pause re-held while the resume loop was running', async () => {
    await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
    mockBattery = 1;
    list.mockResolvedValue([rec(1, 'RUNNING'), rec(2, 'RUNNING')]);
    await watchAndEmit(rec(1, 'RUNNING'));
    mockBattery = 2;
    mockAssets = [asset(1), asset(2)];
    list.mockResolvedValue([rec(1, 'PAUSED'), rec(2, 'PAUSED')]);
    // Resuming upload 1 races a new system pause: the constraint fails again and the app re-holds it.
    resume.mockImplementationOnce(async (id: string) => {
      await photoBackupStore.holdUploads([id]);
    });
    await runPhotoBackup();
    expect([...(await photoBackupStore.heldUploads())]).toEqual([backupTransferId('1')]);
  });

  it('resumes only the paused uploads it paused, and forgets them afterwards', async () => {
    await photoBackupStore.save({ ...prefs('h1'), chargingOnly: true });
    mockBattery = 1;
    list.mockResolvedValue([rec(1, 'RUNNING')]);
    await watchAndEmit(rec(1, 'RUNNING'));
    mockBattery = 2;
    mockAssets = [asset(1), asset(2)];
    list.mockResolvedValue([rec(1, 'PAUSED'), rec(2, 'PAUSED')]);
    await runPhotoBackup();
    expect(resume.mock.calls.map((c) => c[0])).toEqual([backupTransferId('1')]);
    resume.mockClear();
    await runPhotoBackup();
    expect(resume).not.toHaveBeenCalled();
  });
});
