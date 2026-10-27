import { DOUBLE_TAP_SCALE, MAX_SCALE, createZoomGesture } from './zoomGesture';

function setup() {
  const views: { scale: number; x: number; y: number }[] = [];
  const flips: boolean[] = [];
  const z = createZoomGesture((v, f) => {
    views.push(v);
    if (f !== null) flips.push(f);
  });
  const last = () => views[views.length - 1];
  return { z, views, flips, last };
}

test('double tap zooms in and a second double tap resets', () => {
  const { z, flips, last } = setup();
  const tap = (t: number) => {
    z.start(0);
    z.end(0, 0, t);
  };
  tap(1000);
  tap(1200);
  expect(last()).toEqual({ scale: DOUBLE_TAP_SCALE, x: 0, y: 0 });
  expect(z.zoomed()).toBe(true);
  tap(5000);
  tap(5250);
  expect(last().scale).toBe(1);
  expect(flips).toEqual([true, false]);
});

test('slow taps are not a double tap', () => {
  const { z, views } = setup();
  z.start(0);
  z.end(0, 0, 1000);
  z.start(0);
  z.end(0, 0, 1400);
  expect(views).toHaveLength(0);
});

test('pinch scales with the finger spread, clamped to 1..MAX', () => {
  const { z, last } = setup();
  z.start(100);
  z.move(200, 0, 0);
  expect(last().scale).toBe(2);
  z.move(10_000, 0, 0);
  expect(last().scale).toBe(MAX_SCALE);
  z.move(10, 0, 0);
  expect(last()).toEqual({ scale: 1, x: 0, y: 0 });
  z.end(40, 0, 2000); // a pinch release is never a tap
  expect(z.canRelease()).toBe(true);
});

test('one-finger drag pans only while zoomed; the pager keeps the gesture at 1x', () => {
  const { z, views, last } = setup();
  z.start(0);
  z.move(0, 50, 20);
  expect(views).toHaveLength(0);
  expect(z.canRelease()).toBe(true);
  z.start(100);
  z.move(300, 0, 0); // 3x
  z.end(0, 0, 10);
  z.start(0);
  z.move(0, 50, -20);
  expect(last()).toEqual({ scale: 3, x: 50, y: -20 });
  expect(z.canRelease()).toBe(false);
});

test('a second finger landing mid-drag starts the pinch from that spread', () => {
  const { z, last } = setup();
  z.start(0);
  z.move(120, 0, 0);
  expect(last().scale).toBe(1);
  z.move(240, 0, 0);
  expect(last().scale).toBe(2);
});
