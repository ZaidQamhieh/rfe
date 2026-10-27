import { contrast, mix } from './color';
import { roleTint } from './theme';
import { amoledScheme, darkRoles, darkScheme, lightRoles, lightScheme, type Roles, type Scheme } from './tokens';

const cases: [string, Scheme, Roles][] = [
  ['light', lightScheme, lightRoles],
  ['dark', darkScheme, darkRoles],
  ['amoled', amoledScheme, darkRoles],
];

describe.each(cases)('%s roles', (_name, scheme, roles) => {
  it.each(Object.entries(roles))('%s clears 4.5:1 on its tint and on the surface', (_role, hex) => {
    expect(contrast(hex, roleTint(hex, scheme))).toBeGreaterThanOrEqual(4.5);
    expect(contrast(hex, scheme.surface)).toBeGreaterThanOrEqual(4.5);
  });

  it('keeps secondary text readable on every container', () => {
    for (const bg of [scheme.surface, scheme.surfaceContainer, scheme.surfaceContainerHighest]) {
      expect(contrast(scheme.onSurfaceVariant, bg)).toBeGreaterThanOrEqual(4.5);
    }
  });
});

describe('colour maths', () => {
  it('mix returns the endpoints at alpha 0 and 1', () => {
    expect(mix('#FF0000', '#0000FF', 0)).toBe('#0000FF');
    expect(mix('#FF0000', '#0000FF', 1)).toBe('#FF0000');
    expect(mix('#FFFFFF', '#000000', 0.5)).toBe('#808080');
  });

  it('contrast is 21 for black on white and symmetric', () => {
    expect(contrast('#000000', '#FFFFFF')).toBeCloseTo(21, 5);
    expect(contrast('#FFFFFF', '#000000')).toBeCloseTo(21, 5);
  });
});
