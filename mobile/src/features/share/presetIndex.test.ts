import { presetIndexFor, SHARE_EXPIRY_PRESETS } from './shareLogic';

describe('presetIndexFor', () => {
  it('picks the shortest length that covers the time left', () => {
    expect(presetIndexFor(10)).toBe(0);
    expect(presetIndexFor(3600)).toBe(0);
    expect(presetIndexFor(3601)).toBe(1);
    expect(presetIndexFor(86_400)).toBe(1);
    expect(presetIndexFor(86_401)).toBe(2);
  });
  it('falls back to the longest length and to the shortest for an expired link', () => {
    expect(presetIndexFor(10 * 86_400)).toBe(SHARE_EXPIRY_PRESETS.length - 1);
    expect(presetIndexFor(0)).toBe(0);
  });
});
