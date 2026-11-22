import type { EntryDensity } from '../core/models/sort';

/** Row sizes for one density, in dp. */
export type RowDensity = {
  /** Least height of a list row (`Row`), and the vertical padding inside it. */
  rowMinHeight: number;
  rowPaddingY: number;
  /** The same two for a transfer row (`TransferFrame`), which has more lines in it. */
  frameMinHeight: number;
  framePaddingY: number;
};

const DENSITIES: Record<EntryDensity, RowDensity> = {
  comfortable: { rowMinHeight: 64, rowPaddingY: 8, frameMinHeight: 64, framePaddingY: 12 },
  // 48 high with 4 of padding still holds the 40 dp lead picture; transfer rows keep room for their two text lines.
  compact: { rowMinHeight: 48, rowPaddingY: 4, frameMinHeight: 48, framePaddingY: 6 },
};

/** The one place that turns the Comfortable / Compact setting into row height and padding, for every file and computer list. */
export function rowDensity(mode: EntryDensity): RowDensity {
  return DENSITIES[mode];
}
