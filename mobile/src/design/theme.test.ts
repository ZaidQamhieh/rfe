import { resolveScheme } from './theme';
import { darkScheme, FontFamily, lightScheme, Radii } from './tokens';

describe('resolveScheme', () => {
  it('uses the chosen theme and follows the phone only for system', () => {
    expect(resolveScheme('light', true)).toBe(lightScheme);
    expect(resolveScheme('light', false)).toBe(lightScheme);
    expect(resolveScheme('dark', false)).toBe(darkScheme);
    expect(resolveScheme('dark', true)).toBe(darkScheme);
    expect(resolveScheme('system', true)).toBe(darkScheme);
    expect(resolveScheme('system', false)).toBe(lightScheme);
  });
});

describe('Nameplate tokens', () => {
  it('keeps the graphite accent fixed for light and dark', () => {
    expect([lightScheme.primary, lightScheme.onPrimary, lightScheme.primaryContainer]).toEqual(['#1A1A1A', '#FFFFFF', '#E6E6E2']);
    expect([darkScheme.primary, darkScheme.onPrimary, darkScheme.primaryContainer]).toEqual(['#F2F2F0', '#0E0E0E', '#2E2E2E']);
  });

  it('uses Figtree for text and JetBrains Mono only for figures', () => {
    expect(FontFamily.regular).toBe('Figtree_400Regular');
    expect(FontFamily.medium).toBe('Figtree_500Medium');
    expect(FontFamily.semibold).toBe('Figtree_600SemiBold');
    expect(FontFamily.bold).toBe(FontFamily.semibold);
    expect(FontFamily.mono).toMatch(/^JetBrainsMono/);
  });

  it('keeps radii to 16 sheet, 8 control and 4 file', () => {
    expect([Radii.sheet, Radii.control, Radii.file]).toEqual([16, 8, 4]);
  });
});
