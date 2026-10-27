import { MemoryKeyValueStore } from '../../core/storage/hostStore';
import { isOnboarded, markOnboarded, ONBOARDING_KEY } from './onboardingState';

describe('onboarding state', () => {
  it('is not done on a fresh install', async () => {
    expect(await isOnboarded(new MemoryKeyValueStore(), async () => 0)).toBe(false);
  });

  it('is done once marked, and the flag is persisted', async () => {
    const kv = new MemoryKeyValueStore();
    await markOnboarded(kv);
    expect(kv.data.get(ONBOARDING_KEY)).toBe('true');
    expect(await isOnboarded(kv, async () => 0)).toBe(true);
  });

  it('is implied by an existing paired computer', async () => {
    expect(await isOnboarded(new MemoryKeyValueStore(), async () => 2)).toBe(true);
  });

  it('ignores any other stored value', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(ONBOARDING_KEY, 'false');
    expect(await isOnboarded(kv, async () => 0)).toBe(false);
  });
});
