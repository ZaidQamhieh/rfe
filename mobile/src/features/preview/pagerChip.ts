/** Height of the preview top bar below the status-bar inset: the bar's minimum height, shared by the bar and the chip below it. */
export const PREVIEW_BAR_HEIGHT = 56;
/** Gap between the top bar and the "n of m" chip. */
export const PAGER_CHIP_GAP = 8;
/** The chip's own height: one 16 dp text line plus 4 dp padding above and below. */
export const PAGER_CHIP_HEIGHT = 24;

/** Distance from the screen top to the chip's top edge: status-bar inset, the top bar, then the gap. */
export const pagerChipTop = (topInset: number): number => topInset + PREVIEW_BAR_HEIGHT + PAGER_CHIP_GAP;

/**
 * Empty space a page must leave under the top bar so the floating "n of m" chip does not cover its first line or tab
 * row. Only paged previews show the chip, and photos/videos float it over the picture on purpose, so they need none.
 */
export function pagerChipTopClearance(entryCount: number, overMedia: boolean): number {
  if (entryCount <= 1 || overMedia) return 0;
  return PAGER_CHIP_GAP + PAGER_CHIP_HEIGHT + PAGER_CHIP_GAP;
}
