/** Which way a horizontal swipe turns the pager: -1 to the previous page, 1 to the next, 0 when the drag is not a swipe. */
export type SwipeDirection = -1 | 0 | 1;

/** A finished drag: distance moved in px since the touch began (dx right, dy down) and the horizontal speed in px/ms. */
export type SwipeGesture = { dx: number; dy: number; vx: number };

/** A slow drag must travel at least this far (px) to count as a swipe. */
const SWIPE_MIN_DISTANCE = 48;
/** A quick flick may be shorter than the distance above, but not shorter than this (px). */
const SWIPE_FLICK_DISTANCE = 20;
/** The horizontal speed (px/ms) at which a short drag counts as a flick. */
const SWIPE_FLICK_VELOCITY = 0.5;
/** The drag must be at least this many times wider than tall, so a vertical scroll never turns the page. */
const SWIPE_AXIS_RATIO = 1.5;

/**
 * Reads a finished drag as a swipe. Dragging left (negative dx) brings in the next page and dragging right the
 * previous one, like a paged list. Mostly vertical or tiny drags are not swipes.
 */
export function swipeDirection(g: SwipeGesture): SwipeDirection {
  const w = Math.abs(g.dx);
  if (w < SWIPE_AXIS_RATIO * Math.abs(g.dy)) return 0;
  const flick = w >= SWIPE_FLICK_DISTANCE && Math.abs(g.vx) >= SWIPE_FLICK_VELOCITY;
  if (w < SWIPE_MIN_DISTANCE && !flick) return 0;
  return g.dx < 0 ? 1 : -1;
}

/** The index one step from [index] in a list of [count], or null at either end (and for a one-item list). */
export function neighbourIndex(index: number, count: number, dir: SwipeDirection): number | null {
  if (dir === 0) return null;
  const next = index + dir;
  return next >= 0 && next < count ? next : null;
}

/** The page a drag should land on, or null when it is not a swipe or there is nothing further that way. */
export function swipeTarget(index: number, count: number, g: SwipeGesture): number | null {
  return neighbourIndex(index, count, swipeDirection(g));
}
