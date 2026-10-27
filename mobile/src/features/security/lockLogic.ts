import type { AuthOutcome } from '../../core/security/deviceAuth';

/** After leaving the app for less than this, coming back does not ask again (the system auth sheet itself backgrounds the app). */
export const RELOCK_GRACE_MS = 2000;

/** Whether resuming after [backgroundedAt] must show the lock again. Unknown background time locks. */
export function shouldRelockOnResume(enabled: boolean, backgroundedAt: number | null, now: number, graceMs = RELOCK_GRACE_MS): boolean {
  if (!enabled) return false;
  if (backgroundedAt === null) return true;
  return now - backgroundedAt >= graceMs;
}


/**
 * Device auth outcome to lock action. Only a successful check opens the app: no screen lock or no hardware
 * (`unavailable`) keeps it locked too, and the lock screen says to set a screen lock (audit F-19).
 */
export const unlocksApp = (o: AuthOutcome): boolean => o === 'success';

/** Why the switch cannot be turned on right now, or null when it can. */
export function lockEnableBlocker(hasHardware: boolean, enrolledLevel: number): 'noHardware' | 'noScreenLock' | null {
  if (!hasHardware) return 'noHardware';
  return enrolledLevel > 0 ? null : 'noScreenLock';
}
