import { create } from 'zustand';

/** Whether this app session has passed device auth. Starts locked; the lock only matters while app lock is enabled. */
export const useLock = create<{ unlocked: boolean; setUnlocked(v: boolean): void }>((set) => ({
  unlocked: false,
  setUnlocked: (unlocked) => set({ unlocked }),
}));
