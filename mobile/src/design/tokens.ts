// Ported from app/lib/core/theme/{tokens,app_theme}.dart. Light values are the
// exact ColorScheme.fromSeed(seed #4C8DFF, secondary #9B87F5) output of the
// shipped Flutter app; dark is its hand-picked zinc scheme. Every value was
// dumped from the Flutter ThemeData, none inferred.

export const Brand = {
  seed: '#4C8DFF',
  seedDim: '#2C5FCC',
  accent: '#9B87F5',
  accentDim: '#7C6AE0',
  online: '#34D399',
  offline: '#9AA0A6',
  amber: '#F3A73F',
  red: '#F1596B',
  primaryGradient: ['#4C8DFF', '#2C5FCC'] as const,
  accentGradient: ['#9B87F5', '#7C6AE0'] as const,
} as const;

export const Spacing = { xs: 4, sm: 8, md2: 12, md: 16, md3: 20, lg: 24, xl: 32 } as const;

/** Mockup radius scale 8/14/20/28 plus stadium. */
export const Radii = { chip: 12, sm: 16, card: 18, lg: 28, sheet: 28, stadium: 999 } as const;

export const Motion = { short: 150, medium: 250, long: 350 } as const;

export type Scheme = {
  dark: boolean;
  primary: string;
  onPrimary: string;
  primaryContainer: string;
  onPrimaryContainer: string;
  secondary: string;
  onSecondary: string;
  secondaryContainer: string;
  onSecondaryContainer: string;
  tertiary: string;
  tertiaryContainer: string;
  onTertiaryContainer: string;
  error: string;
  onError: string;
  errorContainer: string;
  onErrorContainer: string;
  inverseSurface: string;
  onInverseSurface: string;
  surface: string;
  onSurface: string;
  onSurfaceVariant: string;
  outline: string;
  outlineVariant: string;
  surfaceContainerLowest: string;
  surfaceContainerLow: string;
  surfaceContainer: string;
  surfaceContainerHigh: string;
  surfaceContainerHighest: string;
};

/** Lumen white theme (design/mobile-redesign/lumen-variants.html, `.theme.white`). */
export const lightScheme: Scheme = {
  dark: false,
  primary: '#315FC4',
  onPrimary: '#FFFFFF',
  primaryContainer: '#DCE5F6',
  onPrimaryContainer: '#1F3F86',
  secondary: '#247E88',
  onSecondary: '#FFFFFF',
  secondaryContainer: '#DDEDEF',
  onSecondaryContainer: '#174F56',
  tertiary: '#267342',
  tertiaryContainer: '#DCEBE1',
  onTertiaryContainer: '#1A5030',
  error: '#BA1A1A',
  onError: '#FFFFFF',
  errorContainer: '#FFDAD6',
  onErrorContainer: '#93000A',
  inverseSurface: '#1D2934',
  onInverseSurface: '#F3F6F8',
  surface: '#FFFFFF',
  onSurface: '#1D2934',
  onSurfaceVariant: '#55646F',
  outline: '#86939D',
  outlineVariant: '#DDE3E8',
  surfaceContainerLowest: '#FFFFFF',
  surfaceContainerLow: '#F8FAFB',
  surfaceContainer: '#F3F6F8',
  surfaceContainerHigh: '#E9EEF2',
  surfaceContainerHighest: '#E1E7EC',
};

/** Lumen dark theme (`.theme`): graphite bg #171c22, cards #202831, raised #29333e. */
export const darkScheme: Scheme = {
  dark: true,
  primary: '#9BBCFF',
  onPrimary: '#142231',
  primaryContainer: '#2B3A56',
  onPrimaryContainer: '#DCE7FF',
  secondary: '#78C3CD',
  onSecondary: '#0E2A2E',
  secondaryContainer: '#27424A',
  onSecondaryContainer: '#DDF1F3',
  tertiary: '#94C69D',
  tertiaryContainer: '#2B3E3A',
  onTertiaryContainer: '#CFE8D4',
  error: Brand.red,
  onError: '#2E0A0A',
  errorContainer: '#4A2228',
  onErrorContainer: '#FFD9DD',
  inverseSurface: '#EEF3F7',
  onInverseSurface: '#171C22',
  surface: '#171C22',
  onSurface: '#EEF3F7',
  onSurfaceVariant: '#A5B1BC',
  outline: '#768491',
  outlineVariant: '#2C3742',
  surfaceContainerLowest: '#12161B',
  surfaceContainerLow: '#1B2128',
  surfaceContainer: '#202831',
  surfaceContainerHigh: '#29333E',
  surfaceContainerHighest: '#313C48',
};

/** AMOLED variant of the Lumen dark theme: true-black background, cards lifted just enough to read. */
export const amoledScheme: Scheme = {
  ...darkScheme,
  surface: '#000000',
  surfaceContainerLowest: '#000000',
  surfaceContainerLow: '#0A0D10',
  surfaceContainer: '#12171C',
  surfaceContainerHigh: '#1B222A',
  surfaceContainerHighest: '#252D36',
};

/**
 * Category colours for icons, tints and small status text (never body text). Every role clears 4.5:1 on its own
 * tint (`mix(role, surface, 0.14|0.17)`) and on the plain surface; roles.test.ts asserts it for light, dark and AMOLED.
 */
export type Roles = { folder: string; doc: string; photo: string; route: string; transfer: string; safe: string; warn: string };

export const lightRoles: Roles = { folder: '#7D530B', doc: '#A3432A', photo: '#2F6B3F', route: '#0E6A77', transfer: '#2A57B8', safe: '#2F6B3F', warn: '#9A4A16' };
export const darkRoles: Roles = { folder: '#E4BD73', doc: '#E59B85', photo: '#9AC6A1', route: '#78C3CD', transfer: '#9BBCFF', safe: '#94C69D', warn: '#E8A16D' };

export const FontFamily = {
  regular: 'Lato_400Regular',
  medium: 'Lato_400Regular',
  semibold: 'Lato_700Bold',
  bold: 'Lato_700Bold',
  black: 'Lato_900Black',
  mono: 'JetBrainsMono-Regular',
  monoMedium: 'JetBrainsMono-Medium',
} as const;

/** Material 3 type roles as used by the Flutter theme (Inter, weights 400/500/600). */
export const TypeScale = {
  headlineSmall: { fontSize: 24, lineHeight: 32, fontFamily: FontFamily.regular },
  titleLarge: { fontSize: 22, lineHeight: 28, fontFamily: FontFamily.semibold },
  titleMedium: { fontSize: 16, lineHeight: 24, letterSpacing: 0.15, fontFamily: FontFamily.medium },
  titleSmall: { fontSize: 14, lineHeight: 20, letterSpacing: 0.1, fontFamily: FontFamily.medium },
  bodyLarge: { fontSize: 16, lineHeight: 24, letterSpacing: 0.5, fontFamily: FontFamily.regular },
  bodyMedium: { fontSize: 14, lineHeight: 20, letterSpacing: 0.25, fontFamily: FontFamily.regular },
  bodySmall: { fontSize: 12, lineHeight: 16, letterSpacing: 0.4, fontFamily: FontFamily.regular },
  labelLarge: { fontSize: 14, lineHeight: 20, letterSpacing: 0.1, fontFamily: FontFamily.medium },
  labelMedium: { fontSize: 12, lineHeight: 16, letterSpacing: 0.5, fontFamily: FontFamily.medium },
  /** Mockup `.section-label`, raised to the 12 sp floor: 12/600 uppercase, .06em tracking. */
  sectionLabel: { fontSize: 12, lineHeight: 16, letterSpacing: 0.72, fontFamily: FontFamily.semibold },
  /** ScreenHeader title: 19/700, -0.19 tracking. */
  screenTitle: { fontSize: 19, lineHeight: 24, letterSpacing: -0.19, fontFamily: FontFamily.semibold },
  screenSubtitle: { fontSize: 12.5, lineHeight: 16, fontFamily: FontFamily.regular },
} as const;

export type TypeRole = keyof typeof TypeScale;
