import { neighbourIndex, swipeDirection, swipeTarget } from './swipeNav';

describe('swipeDirection', () => {
  it('a long leftward drag goes to the next page', () => {
    expect(swipeDirection({ dx: -120, dy: 10, vx: -0.2 })).toBe(1);
  });
  it('a long rightward drag goes to the previous page', () => {
    expect(swipeDirection({ dx: 120, dy: -10, vx: 0.2 })).toBe(-1);
  });
  it('a short, slow drag is not a swipe', () => {
    expect(swipeDirection({ dx: -30, dy: 0, vx: -0.1 })).toBe(0);
  });
  it('a short but fast flick is a swipe', () => {
    expect(swipeDirection({ dx: -30, dy: 2, vx: -0.9 })).toBe(1);
  });
  it('a mostly vertical drag is not a swipe, however long', () => {
    expect(swipeDirection({ dx: -100, dy: 160, vx: -0.3 })).toBe(0);
  });
  it('a tap is not a swipe', () => {
    expect(swipeDirection({ dx: 0, dy: 0, vx: 0 })).toBe(0);
  });
});

describe('neighbourIndex', () => {
  it('steps within the list', () => {
    expect(neighbourIndex(2, 5, 1)).toBe(3);
    expect(neighbourIndex(2, 5, -1)).toBe(1);
  });
  it('stops at both ends', () => {
    expect(neighbourIndex(0, 5, -1)).toBeNull();
    expect(neighbourIndex(4, 5, 1)).toBeNull();
  });
  it('has no neighbour in a one-item list', () => {
    expect(neighbourIndex(0, 1, 1)).toBeNull();
    expect(neighbourIndex(0, 1, -1)).toBeNull();
  });
});

describe('swipeTarget', () => {
  it('maps a leftward swipe on the middle page to the next index', () => {
    expect(swipeTarget(1, 3, { dx: -150, dy: 5, vx: -0.4 })).toBe(2);
  });
  it('maps a rightward swipe on the middle page to the previous index', () => {
    expect(swipeTarget(1, 3, { dx: 150, dy: 5, vx: 0.4 })).toBe(0);
  });
  it('returns null on the last page for a next swipe', () => {
    expect(swipeTarget(2, 3, { dx: -150, dy: 5, vx: -0.4 })).toBeNull();
  });
  it('returns null for a non-swipe', () => {
    expect(swipeTarget(1, 3, { dx: 4, dy: 3, vx: 0 })).toBeNull();
  });
});
