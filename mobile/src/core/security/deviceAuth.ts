import * as LocalAuthentication from 'expo-local-authentication';

import { ignoreError } from '../log';

export type AuthOutcome = 'success' | 'failed' | 'unavailable';

async function readAuthState(): Promise<{ hasHardware: boolean; level: number }> {
  const [hasHardware, level] = await Promise.all([LocalAuthentication.hasHardwareAsync(), LocalAuthentication.getEnrolledLevelAsync()]);
  return { hasHardware, level };
}

/**
 * Biometric prompt with the device PIN/pattern as fallback. Only a device that affirmatively reports no hardware or
 * no enrolment is `unavailable` (the lock then cannot be used); a native error counts as `failed`, so a transient
 * fault never opens a locked app (audit F-136).
 */
export async function authenticate(reason: string): Promise<AuthOutcome> {
  try {
    const { hasHardware, level } = await readAuthState();
    if (!hasHardware || level === 0) return 'unavailable';
    const r = await LocalAuthentication.authenticateAsync({ promptMessage: reason, disableDeviceFallback: false });
    return r.success ? 'success' : 'failed';
  } catch (e) {
    ignoreError('device auth')(e);
    return 'failed';
  }
}
