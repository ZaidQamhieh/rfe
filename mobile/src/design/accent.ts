import { argbFromHex, Hct, hexFromArgb, MaterialDynamicColors, SchemeTonalSpot, type DynamicColor } from '@material/material-color-utilities';

import { Brand, type Scheme } from './tokens';

/** The accent presets of the Flutter Appearance screen, in the same order; `null` is the default blue. */
export const ACCENT_PRESETS: { label: string; color: number | null }[] = [
  { label: 'Default', color: null },
  { label: 'Blue', color: 0xff2196f3 },
  { label: 'Green', color: 0xff4caf50 },
  { label: 'Deep orange', color: 0xffff5722 },
  { label: 'Purple', color: 0xff9c27b0 },
  { label: 'Orange', color: 0xffff9800 },
  { label: 'Pink', color: 0xffe91e63 },
  { label: 'Teal', color: 0xff009688 },
];

export const accentLabel = (seed: number | null): string => ACCENT_PRESETS.find((p) => p.color === seed)?.label ?? 'Custom';

/** '#RRGGBB' for a 0xAARRGGBB value. */
export const hexOfColor = (argb: number): string => hexFromArgb(argb >>> 0).toUpperCase();

/**
 * The Material 3 tonal-spot scheme for [seed], the same derivation as Flutter's `ColorScheme.fromSeed`, with the
 * app's violet as the secondary role exactly like the Flutter theme. The default dark theme is hand-picked and never
 * comes through here; a custom accent in dark mode does, as in the Flutter app.
 */
export function schemeFromSeed(seed: number, dark: boolean): Scheme {
  const s = new SchemeTonalSpot(Hct.fromInt(seed >>> 0), dark, 0);
  const m = new MaterialDynamicColors();
  const c = (role: DynamicColor) => hexOfColor(role.getArgb(s));
  return {
    dark,
    primary: c(m.primary()),
    onPrimary: c(m.onPrimary()),
    primaryContainer: c(m.primaryContainer()),
    onPrimaryContainer: c(m.onPrimaryContainer()),
    secondary: Brand.accent,
    onSecondary: c(m.onSecondary()),
    secondaryContainer: c(m.secondaryContainer()),
    onSecondaryContainer: c(m.onSecondaryContainer()),
    tertiary: c(m.tertiary()),
    tertiaryContainer: c(m.tertiaryContainer()),
    onTertiaryContainer: c(m.onTertiaryContainer()),
    error: c(m.error()),
    onError: c(m.onError()),
    errorContainer: c(m.errorContainer()),
    onErrorContainer: c(m.onErrorContainer()),
    inverseSurface: c(m.inverseSurface()),
    onInverseSurface: c(m.inverseOnSurface()),
    surface: c(m.surface()),
    onSurface: c(m.onSurface()),
    onSurfaceVariant: c(m.onSurfaceVariant()),
    outline: c(m.outline()),
    outlineVariant: c(m.outlineVariant()),
    surfaceContainerLowest: c(m.surfaceContainerLowest()),
    surfaceContainerLow: c(m.surfaceContainerLow()),
    surfaceContainer: c(m.surfaceContainer()),
    surfaceContainerHigh: c(m.surfaceContainerHigh()),
    surfaceContainerHighest: c(m.surfaceContainerHighest()),
  };
}

export { argbFromHex };
