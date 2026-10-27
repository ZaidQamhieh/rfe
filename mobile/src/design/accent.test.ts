import { ACCENT_PRESETS, accentLabel, schemeFromSeed } from './accent';
import { Brand, lightScheme } from './tokens';

describe('accent schemes', () => {
  it('keeps the hand-picked Lumen white scheme distinct from any derived one', () => {
    const derived = schemeFromSeed(0xff000000 | parseInt(Brand.seed.slice(1), 16), false);
    expect(lightScheme.surface).toBe('#FFFFFF');
    expect(derived.surface).not.toBe(lightScheme.surface);
  });

  it('gives a dark scheme with dark surfaces and a distinct primary per accent', () => {
    const teal = schemeFromSeed(0xff009688, true);
    const pink = schemeFromSeed(0xffe91e63, true);
    expect(teal.dark).toBe(true);
    expect(teal.primary).not.toBe(pink.primary);
    expect(teal.secondary).toBe(Brand.accent);
  });

  it('labels presets and unknown colors', () => {
    expect(accentLabel(null)).toBe('Default');
    expect(accentLabel(0xff009688)).toBe('Teal');
    expect(accentLabel(0xff123456)).toBe('Custom');
    expect(ACCENT_PRESETS[0].color).toBeNull();
  });
});
