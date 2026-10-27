import { FontFamily } from './tokens';

/**
 * Lumen mockup metrics (design/mobile-redesign/lumen-variants.html). The mockup phone screen is about 174 px wide and
 * stands for a 360 dp phone, so every mockup px becomes `px * 2` dp: a 9 px card radius is 18 dp, 8 px text is 16 sp.
 */
export const K = 2;
export const px = (n: number): number => n * K;


/** Text presets named after the mockup classes, already in dp. */
export const LumenType = {
  /** `.pagehead h3`: 20 px / 600, -0.02em */
  pageTitle: { fontSize: 40, lineHeight: 46, fontFamily: FontFamily.bold, letterSpacing: -0.8 },
  /** `.pagehead p`, `.subtitle`, `.route`: 8 px */
  caption: { fontSize: 16, lineHeight: 22, fontFamily: FontFamily.regular },
  /** `.host`: 18 px / 600, -0.02em */
  hostName: { fontSize: 36, lineHeight: 42, fontFamily: FontFamily.bold, letterSpacing: -0.7 },
  /** `.appbar`, `.sys`: 10 px / 600 */
  appbar: { fontSize: 20, lineHeight: 26, fontFamily: FontFamily.bold },
  /** `.section-label`: 9 px / 600, sentence case */
  sectionLabel: { fontSize: 18, lineHeight: 24, fontFamily: FontFamily.bold },
  /** `.feature-copy b`: 10 px / 700 */
  title: { fontSize: 20, lineHeight: 26, fontFamily: FontFamily.bold },
  /** `.route-option .feature-copy b`, `.transfer-head b`: 9 px / 700 */
  rowTitle: { fontSize: 18, lineHeight: 24, fontFamily: FontFamily.bold },
  /** `.feature-copy small`, `.filemeta`, `.app-meta`: 8 px */
  meta: { fontSize: 16, lineHeight: 21, fontFamily: FontFamily.regular },
  /** `.filename`, `.app-title`: 8 px / 700 */
  name: { fontSize: 16, lineHeight: 21, fontFamily: FontFamily.bold },
  /** `.state-pill`, `.run`: 8 px / 700 */
  pill: { fontSize: 16, lineHeight: 18, fontFamily: FontFamily.bold },
  /** `.action-footer .action`: 9 px / 700 */
  action: { fontSize: 18, lineHeight: 22, fontFamily: FontFamily.bold },
  /** `.bottom`: 7 px */
  dock: { fontSize: 14, lineHeight: 16, fontFamily: FontFamily.regular },
} as const;

/** Radii and sizes from the same CSS, in dp. */
export const LumenSize = {
  cardRadius: px(9),
  tileRadius: px(8),
  buttonRadius: px(7),
  footerButtonRadius: px(9),
  pillRadius: px(12),
  dockRadius: px(12),
  dockItemRadius: px(9),
  dockHeight: px(38),
  dockInsetX: px(8),
  dockInsetBottom: px(6),
  actionHeight: px(25),
  footerActionHeight: px(36),
  iconTile: px(19),
  appIconTile: px(23),
  cardPadding: px(7),
} as const;
