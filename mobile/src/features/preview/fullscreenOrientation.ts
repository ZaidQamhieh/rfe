/** The width and height in pixels of a video's picture. */
export type VideoSize = { width: number; height: number };

/** The two fullscreen orientations the player is asked for: turned to landscape, or following the phone. */
export type FullscreenChoice = 'landscape' | 'default';

/**
 * Picks how fullscreen should be oriented for a video. A landscape (or square, or not yet known) picture is shown
 * in landscape, whichever way the phone is held and whether or not the system's auto-rotate is on; a portrait
 * picture follows the phone, so it is not forced sideways.
 */
export function fullscreenOrientationFor(size: VideoSize | null | undefined): FullscreenChoice {
  if (size && size.height > size.width) return 'default';
  return 'landscape';
}
