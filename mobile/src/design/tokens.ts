// "Nameplate" graphite design system: values map the `.scr` / `.scr.dark` custom properties of
// design/mobile-redesign/style.css (paper, sheet, ink, rule, fill, accent, go/wait/stop). The Scheme keeps the Material
// role names so every existing importer compiles; the comments say which mockup variable each role carries.

export const Brand = {
  seed: '#4C8DFF',
  accent: '#9B87F5',
  online: '#34D399',
  amber: '#F3A73F',
  red: '#F1596B',
} as const;

/** The dim behind every modal in light mode (`--scrim`); `Scheme.scrim` holds the per-theme value. */
export const SCRIM = 'rgba(18,19,17,0.38)';

/** 4px grid. */
export const Spacing = { xs: 4, sm: 8, md2: 12, md: 16, md3: 20, lg: 24, xl: 32 } as const;

/**
 * Nameplate radii: 16 for sheets, 8 for controls (buttons, fields, chips, menus, rows), 4 for file and photo
 * thumbnails and checkboxes. The older keys stay so existing call sites compile; they now map onto those three.
 */
export const Radii = { file: 4, control: 8, chip: 8, sm: 8, card: 8, lg: 16, sheet: 16, stadium: 999 } as const;

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
  /** Success snackbar fill and its text. */
  success: string;
  onSuccess: string;
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
  /** `--go`: connected, done, safe. */
  go: string;
  /** `--wait`: waiting, paused, needs attention. */
  wait: string;
  /** `--stop`: failed, destructive. Same colour as `error`. */
  stop: string;
  /** `--sel-bd`: border of a selected row or segment. */
  selBd: string;
  /** `--scrim`: dim behind modals. */
  scrim: string;
  /** `--rule`: hairline between rows (translucent ink). */
  rule: string;
  /** `--rule-2`: stronger hairline, the 1px border of outlined buttons, chips and fields. */
  rule2: string;
  /** `--fill-2`: translucent ink for tracks (switch off, progress bar, skeletons). */
  fill2: string;
  /** `--raise`: box-shadow of the raised primary button (outer edge plus inner highlight). */
  raise: string;
};

/**
 * Light "Nameplate": surface = `--paper` (screen), surfaceContainerLowest = `--sheet` (bars, sheets, menus, fields),
 * surfaceContainer = `--fill`, primary = `--ac`, primaryContainer = `--ac-soft`, onSurface = `--ink`,
 * onSurfaceVariant = `--ink-2`, outline = `--ink-3`, outlineVariant = `--rule` flattened onto paper.
 */
export const lightScheme: Scheme = {
  dark: false,
  primary: '#1A1A1A',
  onPrimary: '#FFFFFF',
  primaryContainer: '#E6E6E2',
  onPrimaryContainer: '#1A1A1A',
  secondary: '#51534C',
  onSecondary: '#FFFFFF',
  secondaryContainer: '#E6E6E2',
  onSecondaryContainer: '#1C1D1A',
  tertiary: '#3F7D32',
  tertiaryContainer: '#DDE5D9',
  onTertiaryContainer: '#2A5A20',
  error: '#B3261E',
  onError: '#FFFFFF',
  errorContainer: '#EBDAD7',
  onErrorContainer: '#7A1A14',
  success: '#1B5E20',
  onSuccess: '#FFFFFF',
  inverseSurface: '#1C1D1A',
  onInverseSurface: '#F3F3F0',
  surface: '#F3F3F0',
  onSurface: '#1C1D1A',
  onSurfaceVariant: '#51534C',
  outline: '#75776F',
  outlineVariant: '#E0E0DD',
  surfaceContainerLowest: '#FFFFFF',
  surfaceContainerLow: '#F3F3F0',
  surfaceContainer: '#F0F0EC',
  surfaceContainerHigh: '#E2E2DF',
  surfaceContainerHighest: '#D8D8D4',
  go: '#3F7D32',
  wait: '#9A5B0B',
  stop: '#B3261E',
  selBd: '#BDBEB7',
  scrim: 'rgba(18,19,17,0.38)',
  rule: 'rgba(28,29,26,0.09)',
  rule2: 'rgba(28,29,26,0.18)',
  fill2: 'rgba(28,29,26,0.08)',
  raise: '0 1px 2px rgba(0,0,0,0.25), 0 0 0 1px #000000, inset 0 1px 0 rgba(255,255,255,0.18), inset 0 -6px 12px rgba(0,0,0,0.12)',
};

/** Dark "Nameplate" (`.scr.dark`): paper #121413, sheet #1a1c1b, light graphite accent with dark text on it. */
export const darkScheme: Scheme = {
  dark: true,
  primary: '#F2F2F0',
  onPrimary: '#0E0E0E',
  primaryContainer: '#2E2E2E',
  onPrimaryContainer: '#F2F2F0',
  secondary: '#ADB0AE',
  onSecondary: '#121413',
  secondaryContainer: '#2E2E2E',
  onSecondaryContainer: '#ECECEA',
  tertiary: '#8FCF7A',
  tertiaryContainer: '#283626',
  onTertiaryContainer: '#C5E8BA',
  error: '#F0716B',
  onError: '#0E0E0E',
  errorContainer: '#3A2523',
  onErrorContainer: '#F7B8B4',
  success: '#2E7D32',
  onSuccess: '#FFFFFF',
  inverseSurface: '#ECECEA',
  onInverseSurface: '#121413',
  surface: '#121413',
  onSurface: '#ECECEA',
  onSurfaceVariant: '#ADB0AE',
  outline: '#8B8E8C',
  outlineVariant: '#272928',
  surfaceContainerLowest: '#1A1C1B',
  surfaceContainerLow: '#121413',
  surfaceContainer: '#242726',
  surfaceContainerHigh: '#2C2F2E',
  surfaceContainerHighest: '#353837',
  go: '#8FCF7A',
  wait: '#E2A65C',
  stop: '#F0716B',
  selBd: '#4A4D4B',
  scrim: 'rgba(0,0,0,0.55)',
  rule: 'rgba(255,255,255,0.09)',
  rule2: 'rgba(255,255,255,0.18)',
  fill2: 'rgba(255,255,255,0.08)',
  raise: '0 0 0 1px #A9A9A8, inset 0 1px 0 rgba(255,255,255,0.35), inset 0 -6px 12px rgba(0,0,0,0.10)',
};

/**
 * Glyph colours by kind. Nameplate draws things in ink, so folder, doc, photo, route and transfer are all `--ink-2`;
 * only safe and warn carry colour (`--go`, `--wait`) because they say something about state. roles.test.ts asserts
 * contrast for light and dark.
 */
export type Roles = { folder: string; doc: string; photo: string; route: string; transfer: string; safe: string; warn: string };

export const lightRoles: Roles = { folder: '#51534C', doc: '#51534C', photo: '#51534C', route: '#51534C', transfer: '#51534C', safe: '#3F7D32', warn: '#9A5B0B' };
export const darkRoles: Roles = { folder: '#ADB0AE', doc: '#ADB0AE', photo: '#ADB0AE', route: '#ADB0AE', transfer: '#ADB0AE', safe: '#8FCF7A', warn: '#E2A65C' };

/** Figtree for everything (the mockup uses 400, 500 and 600); JetBrains Mono only for figures (codes, sizes, counts). */
export const FontFamily = {
  regular: 'Figtree_400Regular',
  medium: 'Figtree_500Medium',
  semibold: 'Figtree_600SemiBold',
  bold: 'Figtree_600SemiBold',
  mono: 'JetBrainsMono-Regular',
  monoMedium: 'JetBrainsMono-Medium',
} as const;

/** Type roles in real dp from style.css: body 16/22, meta 14/20, titles weight 500, no letter-spacing. */
export const TypeScale = {
  headlineSmall: { fontSize: 24, lineHeight: 32, fontFamily: FontFamily.regular },
  /** `.plate .name`: 22/28 500. */
  titleLarge: { fontSize: 22, lineHeight: 28, fontFamily: FontFamily.medium },
  titleMedium: { fontSize: 16, lineHeight: 22, fontFamily: FontFamily.medium },
  bodyLarge: { fontSize: 16, lineHeight: 22, fontFamily: FontFamily.regular },
  bodyMedium: { fontSize: 14, lineHeight: 20, fontFamily: FontFamily.regular },
  bodySmall: { fontSize: 12, lineHeight: 16, fontFamily: FontFamily.regular },
  labelLarge: { fontSize: 14, lineHeight: 20, fontFamily: FontFamily.medium },
  labelMedium: { fontSize: 12, lineHeight: 16, fontFamily: FontFamily.medium },
  /** `.sec`: 14/20 500, sentence case. */
  sectionLabel: { fontSize: 14, lineHeight: 20, fontFamily: FontFamily.medium },
  /** `.bar h3`: 20/28 500. */
  screenTitle: { fontSize: 20, lineHeight: 28, fontFamily: FontFamily.medium },
  /** `.bar h3 small`: 14/18. */
  screenSubtitle: { fontSize: 14, lineHeight: 18, fontFamily: FontFamily.regular },
} as const;

/** Explanatory paragraph under an empty-state or status title: 16/24, capped so it wraps like a sentence. */
export const ExplainerText = { fontSize: 16, lineHeight: 24, maxWidth: 336 } as const;

export type TypeRole = keyof typeof TypeScale;
