import { insetPadding, paletteMargins } from './safeInsets';

const insets = { top: 48, bottom: 34, left: 0, right: 0 };

describe('insetPadding', () => {
  it('adds the gap to every system inset', () => {
    expect(insetPadding({ top: 48, bottom: 34, left: 12, right: 8 }, 16)).toEqual({ paddingTop: 64, paddingBottom: 50, paddingLeft: 28, paddingRight: 24 });
  });
  it('is just the gap when there are no insets', () => {
    expect(insetPadding({ top: 0, bottom: 0, left: 0, right: 0 }, 24)).toEqual({ paddingTop: 24, paddingBottom: 24, paddingLeft: 24, paddingRight: 24 });
  });
});

describe('paletteMargins', () => {
  it('keeps the base margin when the bars are small', () => {
    expect(paletteMargins({ top: 24, bottom: 0, left: 0, right: 0 }, 80, 16)).toEqual({ paddingTop: 80, paddingBottom: 80 });
  });
  it('moves clear of a tall status bar or cutout and a tall gesture area', () => {
    expect(paletteMargins({ top: 90, bottom: 70, left: 0, right: 0 }, 80, 16)).toEqual({ paddingTop: 106, paddingBottom: 86 });
  });
  it('uses the real insets', () => {
    expect(paletteMargins(insets, 40, 16)).toEqual({ paddingTop: 64, paddingBottom: 50 });
  });
});
