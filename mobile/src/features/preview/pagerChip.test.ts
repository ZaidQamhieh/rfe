import { PAGER_CHIP_GAP, PAGER_CHIP_HEIGHT, PREVIEW_BAR_HEIGHT, pagerChipTop, pagerChipTopClearance } from './pagerChip';

describe('pagerChipTopClearance', () => {
  it('clears the chip so page content starts below its bottom edge', () => {
    const top = pagerChipTop(24);
    const chipBottom = top + PAGER_CHIP_HEIGHT;
    // Page content starts at inset + bar height (it sits under the bar in the column), plus the clearance.
    expect(24 + PREVIEW_BAR_HEIGHT + pagerChipTopClearance(5, false)).toBeGreaterThan(chipBottom);
    expect(pagerChipTopClearance(5, false)).toBe(PAGER_CHIP_GAP * 2 + PAGER_CHIP_HEIGHT);
  });
  it('needs none for a single file or for media', () => {
    expect(pagerChipTopClearance(1, false)).toBe(0);
    expect(pagerChipTopClearance(5, true)).toBe(0);
  });
});
