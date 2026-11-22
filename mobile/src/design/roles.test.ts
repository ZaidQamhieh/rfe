import { contrast, mix } from './color';
import { darkRoles, darkScheme, lightRoles, lightScheme, type Roles, type Scheme } from './tokens';

/** Tinted chip behind a role glyph: the role over the surface at 14% (light) or 17% (dark). */
const roleTint = (role: string, scheme: Scheme): string => mix(role, scheme.surface, scheme.dark ? 0.17 : 0.14);

/** Roles that carry state colour (`--go`, `--wait`); the rest are plain ink. */
const STATE_ROLES = ['safe', 'warn'];

const cases: [string, Scheme, Roles][] = [
  ['light', lightScheme, lightRoles],
  ['dark', darkScheme, darkRoles],
];

describe.each(cases)('%s roles', (_name, scheme, roles) => {
  const ink = Object.entries(roles).filter(([role]) => !STATE_ROLES.includes(role));
  const state = Object.entries(roles).filter(([role]) => STATE_ROLES.includes(role));

  it.each(ink)('%s (ink) clears 4.5:1 on its tint and on the surface', (_role, hex) => {
    expect(contrast(hex, roleTint(hex, scheme))).toBeGreaterThanOrEqual(4.5);
    expect(contrast(hex, scheme.surface)).toBeGreaterThanOrEqual(4.5);
  });

  // The approved mockup fixes --go and --wait; they are status words next to a lamp, so 4:1 on the plain surface is the floor.
  it.each(state)('%s (state colour) stays readable on the surface and its tint', (_role, hex) => {
    expect(contrast(hex, scheme.surface)).toBeGreaterThanOrEqual(4);
    expect(contrast(hex, roleTint(hex, scheme))).toBeGreaterThanOrEqual(3.5);
  });

  it('uses the scheme go and wait colours for safe and warn', () => {
    expect(roles.safe).toBe(scheme.go);
    expect(roles.warn).toBe(scheme.wait);
  });

  it('keeps ink readable on every container', () => {
    for (const bg of [scheme.surface, scheme.surfaceContainerLowest, scheme.surfaceContainer, scheme.surfaceContainerHighest]) {
      expect(contrast(scheme.onSurface, bg)).toBeGreaterThanOrEqual(7);
      expect(contrast(scheme.onSurfaceVariant, bg)).toBeGreaterThanOrEqual(4.5);
    }
  });

  it('keeps the button label readable on the accent and the stop colour on the paper', () => {
    expect(contrast(scheme.onPrimary, scheme.primary)).toBeGreaterThanOrEqual(7);
    expect(contrast(scheme.stop, scheme.surface)).toBeGreaterThanOrEqual(4);
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
