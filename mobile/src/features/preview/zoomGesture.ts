// Pure zoom/pan state machine behind the image viewer's PanResponder (unit-testable, no React).

export const MAX_SCALE = 5;
export const DOUBLE_TAP_SCALE = 2.5;
/** Android's ViewConfiguration double-tap timeout. */
export const DOUBLE_TAP_MS = 300;
const TAP_SLOP = 6;
const ZOOMED = 1.01;

export type ZoomView = { scale: number; x: number; y: number };

/**
 * [render] receives each new view and, when the zoomed/unzoomed state flipped, the new state (else
 * null). Distances are the current two-finger spread in px (0 with one finger).
 */
export function createZoomGesture(render: (v: ZoomView, zoomedChanged: boolean | null) => void) {
  const g = { scale: 1, x: 0, y: 0, startScale: 1, startDist: 0, startX: 0, startY: 0, lastTap: 0, pinching: false };
  const apply = (s: number, x: number, y: number) => {
    const was = g.scale > ZOOMED;
    g.scale = s;
    g.x = s <= 1 ? 0 : x;
    g.y = s <= 1 ? 0 : y;
    const now = s > ZOOMED;
    render({ scale: g.scale, x: g.x, y: g.y }, now !== was ? now : null);
  };
  return {
    zoomed: () => g.scale > ZOOMED,
    /** The pager may take the gesture back only at 1x and outside a pinch. */
    canRelease: () => g.scale <= ZOOMED && !g.pinching,
    start(dist: number) {
      g.startScale = g.scale;
      g.startX = g.x;
      g.startY = g.y;
      g.startDist = dist;
      g.pinching = dist > 0;
    },
    move(dist: number, dx: number, dy: number) {
      if (dist > 0) {
        if (!g.pinching || g.startDist <= 0) {
          // second finger arrived mid-gesture: pinch from here
          g.pinching = true;
          g.startDist = dist;
          g.startScale = g.scale;
        }
        apply(Math.min(Math.max((g.startScale * dist) / g.startDist, 1), MAX_SCALE), g.startX + dx, g.startY + dy);
      } else if (g.scale > ZOOMED) {
        apply(g.scale, g.startX + dx, g.startY + dy);
      }
    },
    end(dx: number, dy: number, now: number) {
      const tap = Math.abs(dx) < TAP_SLOP && Math.abs(dy) < TAP_SLOP && !g.pinching;
      g.pinching = false;
      if (!tap) return;
      if (now - g.lastTap < DOUBLE_TAP_MS) {
        g.lastTap = 0;
        apply(g.scale > ZOOMED ? 1 : DOUBLE_TAP_SCALE, 0, 0);
      } else {
        g.lastTap = now;
      }
    },
  };
}
