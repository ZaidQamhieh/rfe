import { fullscreenOrientationFor } from './fullscreenOrientation';

describe('fullscreenOrientationFor', () => {
  it('turns a landscape video to landscape so the phone no longer has to be rotated by hand', () => {
    expect(fullscreenOrientationFor({ width: 1920, height: 1080 })).toBe('landscape');
  });
  it('treats a square video as landscape', () => {
    expect(fullscreenOrientationFor({ width: 720, height: 720 })).toBe('landscape');
  });
  it('lets a portrait video follow the phone', () => {
    expect(fullscreenOrientationFor({ width: 1080, height: 1920 })).toBe('default');
  });
  it('assumes landscape while the size is not known yet', () => {
    expect(fullscreenOrientationFor(null)).toBe('landscape');
    expect(fullscreenOrientationFor(undefined)).toBe('landscape');
    expect(fullscreenOrientationFor({ width: 0, height: 0 })).toBe('landscape');
  });
});
