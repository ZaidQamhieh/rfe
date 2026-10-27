import * as LocalAuthentication from 'expo-local-authentication';

export type AuthOutcome = 'success' | 'failed' | 'unavailable';

/** Hardware present and the enrolled security level (0 none, 1 PIN/pattern, higher biometric). */
export async function deviceAuthState(): Promise<{ hasHardware: boolean; level: number }> {
  try {
    return await readAuthState();
  } catch {
    return { hasHardware: false, level: 0 };
  }
}

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
  } catch {
    return 'failed';
  }
}
