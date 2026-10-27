import type { KeyValueStore } from '../../core/storage/hostStore';

export const ONBOARDING_KEY = 'onboarding_complete';

/**
 * Whether the first-run pager should be skipped. A user who already has a paired computer (an upgrade from the Flutter
 * app, or a restored install) has seen the pitch, so the flag is implied by having a host.
 */
export async function isOnboarded(kv: KeyValueStore, hostCount: () => Promise<number>): Promise<boolean> {
  if ((await kv.get(ONBOARDING_KEY)) === 'true') return true;
  return (await hostCount()) > 0;
}

export async function markOnboarded(kv: KeyValueStore): Promise<void> {
  await kv.set(ONBOARDING_KEY, 'true');
}
