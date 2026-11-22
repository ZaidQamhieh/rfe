import { busyDelayMs, hashKey, thumbnailKey, thumbnails } from './thumbnails';

const mockFetchToFile = jest.fn();
const mockListHosts = jest.fn();
const mockClientForHost = jest.fn();

jest.mock('expo-file-system', () => ({
  Directory: class {
    create() {}
    list() {
      return [];
    }
  },
  File: class {
    exists = false;
    uri = 'file:///cache/thumb.jpg';
  },
  Paths: { cache: '/cache' },
}));
jest.mock('../../core/native', () => ({
  fetchCancelNative: jest.fn(),
  fetchToFileNative: (...args: unknown[]) => mockFetchToFile(...args),
}));
jest.mock('../../services', () => ({
  hostStore: { listHosts: (...args: unknown[]) => mockListHosts(...args) },
  clientForHost: (...args: unknown[]) => mockClientForHost(...args),
}));

describe('thumbnail helpers', () => {
  it('cache key includes host, path, size and version', () => {
    expect(thumbnailKey('h', '/a.png', 256, 5)).toBe('h@/a.png@256@5');
    expect(thumbnailKey('h', '/a.png', 256, 6)).not.toBe(thumbnailKey('h', '/a.png', 256, 5));
  });
  it('file names are stable, distinct and filesystem-safe', () => {
    expect(hashKey('k')).toBe(hashKey('k'));
    expect(hashKey('k1')).not.toBe(hashKey('k2'));
    expect(hashKey('h@/weird path/../x?.png@256@1')).toMatch(/^[0-9a-f]{16}$/);
  });
  it('busy retries honour a bounded Retry-After, else back off linearly', () => {
    expect(busyDelayMs(1, null)).toBe(200);
    expect(busyDelayMs(3, null)).toBe(600);
    expect(busyDelayMs(1, 0)).toBe(1000);
    expect(busyDelayMs(1, 30)).toBe(2000);
  });
});

describe('thumbnails after a missing download grant (403)', () => {
  beforeEach(() => {
    mockFetchToFile.mockReset();
    mockListHosts.mockResolvedValue([{ id: 'deny-host' }]);
    mockClientForHost.mockResolvedValue({ thumbnailSpec: (path: string) => ({ url: `https://agent/v1/thumb${path}`, headers: {}, pin: null }) });
  });

  it('one 403 stops further thumbnail requests for that computer, and forgetDenied asks again', async () => {
    mockFetchToFile.mockResolvedValue({ status: 403, headers: {}, bodyText: '', retryAfter: null });

    await expect(thumbnails.request('deny-host', '/a.png', 1, 256).promise).resolves.toBeNull();
    expect(thumbnails.isDenied('deny-host')).toBe(true);
    expect(thumbnails.isDenied('other-host')).toBe(false);
    const callsAfterDenial = mockFetchToFile.mock.calls.length;

    await expect(thumbnails.request('deny-host', '/b.png', 1, 256).promise).resolves.toBeNull();
    expect(mockFetchToFile).toHaveBeenCalledTimes(callsAfterDenial);

    thumbnails.forgetDenied('deny-host');
    expect(thumbnails.isDenied('deny-host')).toBe(false);
  });
});
