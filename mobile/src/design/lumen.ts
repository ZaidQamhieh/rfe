import { FontFamily } from './tokens';

/**
 * Nameplate metrics, in real dp straight from design/mobile-redesign/style.css (the mockup phone is 412 px wide for a
 * 412 dp screen, so one mockup px is one dp). The file keeps its Lumen-era name and export names for the importers.
 */

/** Text presets named after the roles the old Lumen file had, now carrying the Nameplate sizes. */
export const LumenType = {
  /** `.empty h5`, `.plate .name`: 22/28 500 */
  pageTitle: { fontSize: 22, lineHeight: 28, fontFamily: FontFamily.medium },
  /** `.note`, `.sheet p.lede`: 14/20 */
  caption: { fontSize: 14, lineHeight: 20, fontFamily: FontFamily.regular },
  /** `.plate .name`: 22/28 500 */
  hostName: { fontSize: 22, lineHeight: 28, fontFamily: FontFamily.medium },
  /** `.bar h3`: 20/28 500 */
  appbar: { fontSize: 20, lineHeight: 28, fontFamily: FontFamily.medium },
  /** `.sec`: 14/20 500, sentence case */
  sectionLabel: { fontSize: 14, lineHeight: 20, fontFamily: FontFamily.medium },
  /** `.sheet h4`: 18/24 500 */
  title: { fontSize: 18, lineHeight: 24, fontFamily: FontFamily.medium },
  /** `.t1.b`: 16/22 500 */
  rowTitle: { fontSize: 16, lineHeight: 22, fontFamily: FontFamily.medium },
  /** `.t2`: 14/20 */
  meta: { fontSize: 14, lineHeight: 20, fontFamily: FontFamily.regular },
  /** `.t1`: 16/22 400 */
  name: { fontSize: 16, lineHeight: 22, fontFamily: FontFamily.regular },
  /** `.chip`: 14/20 500 */
  pill: { fontSize: 14, lineHeight: 20, fontFamily: FontFamily.medium },
  /** `.btn`: 15/20 500 */
  action: { fontSize: 15, lineHeight: 20, fontFamily: FontFamily.medium },
  /** `.nav a`: 13/16 500 */
  dock: { fontSize: 13, lineHeight: 16, fontFamily: FontFamily.medium },
  /** `.keycap`, `.nav .n`, `.dur`: 12/16 mono 500 */
  monoSmall: { fontSize: 12, lineHeight: 16, fontFamily: FontFamily.monoMedium },
  /** `.code i`: 24/28 mono 500 */
  monoCode: { fontSize: 24, lineHeight: 28, fontFamily: FontFamily.monoMedium },
} as const;

/** Radii and sizes from the same CSS, in dp. */
export const LumenSize = {
  cardRadius: 8,
  tileRadius: 8,
  buttonRadius: 8,
  pillRadius: 8,
  dockRadius: 8,
  dockItemRadius: 8,
  /** `.nav`: 76 high. */
  dockHeight: 76,
  /** `.btn`: 48 high (`.btn.s` is 36). */
  actionHeight: 48,
  actionHeightSmall: 36,
  footerActionHeight: 48,
  /** `.row .lead`: 40x40. */
  appIconTile: 40,
  cardPadding: 16,
  /** `.row`: min height 64, 16 side padding, 16 gap. */
  rowMinHeight: 64,
} as const;
