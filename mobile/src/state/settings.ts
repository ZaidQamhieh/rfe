import { useMemo } from 'react';
import { create } from 'zustand';

import { defaultAppDefaults, resolveView, resolveVisibility, type AppDefaults, type DeviceOverrides, type SettingsState } from '../core/settings/settings';
import { settingsRepo } from '../services';

type Store = {
  loaded: boolean;
  state: SettingsState;
  load(): Promise<void>;
  setApp<T extends keyof AppDefaults>(key: T, value: AppDefaults[T]): Promise<void>;
  setOverrides(next: Record<string, DeviceOverrides>): Promise<void>;
};

/** In-memory mirror of the persisted two-tier settings; every write persists first, then updates state. */
export const useSettings = create<Store>((set, get) => ({
  loaded: false,
  state: { app: defaultAppDefaults(), overrides: {} },
  async load() {
    set({ state: await settingsRepo.load(), loaded: true });
  },
  async setApp(key, value) {
    await settingsRepo.setApp(key, value);
    set((s) => ({ state: { ...s.state, app: { ...s.state.app, [key]: value } } }));
  },
  async setOverrides(next) {
    await settingsRepo.setOverrides(next);
    set((s) => ({ state: { ...s.state, overrides: Object.fromEntries(Object.entries(next).filter(([, o]) => Object.keys(o).length > 0)) } }));
  },
}));

// Selectors must return stable references (zustand 5 / useSyncExternalStore): select the settings
// object and derive the resolved value with useMemo instead of building a new object in the selector.
export function useResolvedView(hostId: string) {
  const state = useSettings((s) => s.state);
  return useMemo(() => resolveView(state, hostId), [state, hostId]);
}
export function useResolvedVisibility(hostId: string) {
  const state = useSettings((s) => s.state);
  return useMemo(() => resolveVisibility(state, hostId), [state, hostId]);
}
