/**
 * Fixed colours drawn over a photo, video or camera canvas (`onDark`), whatever the theme.
 * `ink` is white; the others are white or black at a set opacity.
 */
export const ON_DARK = {
  /** Foreground text and icons on the dark canvas. */
  ink: '#FFFFFF',
  /** Secondary text on the dark canvas (0.7 white). */
  inkDim: 'rgba(255,255,255,0.7)',
  /** Message text under a spinner on the dark canvas (0.8 white). */
  inkSoft: 'rgba(255,255,255,0.8)',
  /** Hairline rule on the dark canvas (0.18 white). */
  hairline: 'rgba(255,255,255,0.18)',
  /** The solid canvas behind the media. */
  canvas: '#000',
  /** Translucent black behind the top bar over the media (0.45 black). */
  bar: 'rgba(0,0,0,0.45)',
  /** Translucent black pill behind the "1 of 3" page indicator (0.54 black). */
  pill: 'rgba(0,0,0,0.54)',
} as const;
