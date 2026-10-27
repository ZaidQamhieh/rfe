import { lockEnableBlocker, RELOCK_GRACE_MS, shouldRelockOnResume, unlocksApp } from './lockLogic';

describe('shouldRelockOnResume', () => {
  it('never locks when the lock is off', () => {
    expect(shouldRelockOnResume(false, 0, 999999)).toBe(false);
  });
  it('keeps the app open inside the grace window', () => {
    expect(shouldRelockOnResume(true, 1000, 1000 + RELOCK_GRACE_MS - 1)).toBe(false);
  });
  it('locks at and after the grace window', () => {
    expect(shouldRelockOnResume(true, 1000, 1000 + RELOCK_GRACE_MS)).toBe(true);
    expect(shouldRelockOnResume(true, 1000, 60_000)).toBe(true);
  });
  it('locks when the background time is unknown', () => {
    expect(shouldRelockOnResume(true, null, 5)).toBe(true);
  });
});

describe('unlocksApp', () => {
  it('opens only on success; failure and an unavailable device lock stay locked', () => {
    expect(unlocksApp('success')).toBe(true);
    expect(unlocksApp('unavailable')).toBe(false);
    expect(unlocksApp('failed')).toBe(false);
  });
});

describe('lockEnableBlocker', () => {
  it('reports the first missing prerequisite', () => {
    expect(lockEnableBlocker(false, 0)).toBe('noHardware');
    expect(lockEnableBlocker(true, 0)).toBe('noScreenLock');
    expect(lockEnableBlocker(true, 1)).toBeNull();
    expect(lockEnableBlocker(true, 3)).toBeNull();
  });
});
