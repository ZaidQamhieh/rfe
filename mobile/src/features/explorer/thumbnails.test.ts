import { busyDelayMs, hashKey, thumbnailKey } from './thumbnails';

jest.mock('expo-file-system', () => ({ Directory: class {}, File: class {}, Paths: {} }));
jest.mock('../../core/native', () => ({}));
jest.mock('../../services', () => ({}));

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
