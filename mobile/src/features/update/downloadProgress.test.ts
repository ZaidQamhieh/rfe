import { downloadUpdate } from './updateService';
import { publicDownload, sha256File } from '../../core/native';
import type { AppRelease } from './updateLogic';

const mockFs = { exists: false, size: 0 };

jest.mock('expo-file-system', () => ({
  Directory: class {
    create() {}
  },
  File: class {
    uri = 'file:///cache/updates/update-7.apk';
    get exists() {
      return mockFs.exists;
    }
    get size() {
      return mockFs.size;
    }
    delete() {
      mockFs.exists = false;
      mockFs.size = 0;
    }
  },
  Paths: { cache: 'cache' },
}));
jest.mock('../../core/native', () => ({
  canInstallPackages: jest.fn(),
  installApk: jest.fn(),
  openInstallSettings: jest.fn(),
  publicDownload: jest.fn(),
  sha256File: jest.fn(),
  appBuild: jest.fn(),
}));
jest.mock('../../services', () => ({ keyValue: { get: jest.fn(), set: jest.fn() } }));

const SHA = 'cd'.repeat(32);
const release: AppRelease = { versionName: '9.9.9', versionCode: 7, size: 1000, url: 'https://example.invalid/app.apk', sha256: SHA };

/** Lets queued promise callbacks run (timers are faked, promises are not). */
const flush = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve();
};

describe('downloadUpdate progress', () => {
  let finish: () => void;
  beforeEach(() => {
    jest.clearAllMocks();
    jest.useFakeTimers();
    mockFs.exists = false;
    mockFs.size = 0;
    (sha256File as jest.Mock).mockResolvedValue(SHA);
    (publicDownload as jest.Mock).mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = () => {
            mockFs.exists = true;
            mockFs.size = 1000;
            resolve();
          };
        }),
    );
  });
  afterEach(() => jest.useRealTimers());

  it('reports the share downloaded so far, and 100 once verified', async () => {
    const seen: number[] = [];
    const job = downloadUpdate(release, (p) => seen.push(p));
    await flush();
    mockFs.exists = true;
    mockFs.size = 500;
    jest.advanceTimersByTime(300);
    expect(seen).toEqual([50]);
    finish();
    await job;
    expect(seen[seen.length - 1]).toBe(100);
  });

  it('a caller that joins a running download (started without a listener) gets progress too', async () => {
    const first = downloadUpdate(release);
    await flush();
    const seen: number[] = [];
    const joined = downloadUpdate(release, (p) => seen.push(p));
    expect(joined).toBe(first);
    mockFs.exists = true;
    mockFs.size = 500;
    jest.advanceTimersByTime(300);
    expect(seen).toEqual([50]);
    finish();
    await joined;
    expect(seen[seen.length - 1]).toBe(100);
    expect(publicDownload).toHaveBeenCalledTimes(1);
  });

  it('every caller sharing the download hears the same progress', async () => {
    const a: number[] = [];
    const b: number[] = [];
    const first = downloadUpdate(release, (p) => a.push(p));
    await flush();
    const second = downloadUpdate(release, (p) => b.push(p));
    mockFs.exists = true;
    mockFs.size = 250;
    jest.advanceTimersByTime(300);
    finish();
    await Promise.all([first, second]);
    expect(a).toEqual(b);
    expect(a[0]).toBe(25);
    expect(a[a.length - 1]).toBe(100);
  });

  it('stops reporting once the download has ended', async () => {
    const seen: number[] = [];
    const job = downloadUpdate(release, (p) => seen.push(p));
    await flush();
    finish();
    await job;
    const n = seen.length;
    jest.advanceTimersByTime(2000);
    expect(seen).toHaveLength(n);
  });
});
