import { create } from 'zustand';

import type { Health } from '../core/api/models';
import type { Host } from '../core/models/host';

/** Port of home_state.dart ActiveHost: which host the Files tab shows, and where. */
export type ActiveHost = { host: Host; health: Health | null; rootPath?: string; initialPath?: string };

type State = {
  active: ActiveHost | null;
  setActive(a: ActiveHost | null): void;
};

export const useActiveHost = create<State>((set) => ({
  active: null,
  setActive: (active) => set({ active }),
}));
