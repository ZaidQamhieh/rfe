import { createStore, type StoreApi } from 'zustand/vanilla';

import type { AgentClient } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import { isEntryHiddenInPicker, type VisibilityPrefs } from '../../core/visibility';
import { buildPathStack, joinRemotePath } from './paths';

export type PickerState = {
  pathStack: string[];
  /** Folders of the current directory in server order (the picker offers no sorting). */
  folders: Entry[];
  loading: boolean;
  loadingMore: boolean;
  error: string | null;
  nextCursor: string | null;
};

export type PickerDeps = {
  hostId: string;
  startPath: string;
  getClient: () => Promise<Pick<AgentClient, 'list' | 'createFolder'>>;
  visibility: () => VisibilityPrefs;
  humanize: (e: unknown) => string;
};

export interface Picker {
  load(): Promise<void>;
  loadMore(): Promise<void>;
  navigate(path: string): void;
  navigateTo(index: number): void;
  createFolder(name: string): Promise<void>;
}

export const pickerPath = (s: PickerState) => s.pathStack[s.pathStack.length - 1];

/**
 * Folder-only browser used as a Move/Copy/Save-to destination. Every load takes a generation number so a
 * slow response for a folder the user already left (A -> B -> A) cannot overwrite the current listing (PR-34).
 */
export function createDestinationPicker(deps: PickerDeps): StoreApi<PickerState> & Picker {
  const store = createStore<PickerState>(() => ({ pathStack: buildPathStack(deps.startPath), folders: [], loading: false, loadingMore: false, error: null, nextCursor: null }));
  let generation = 0;
  const get = store.getState;
  const set = (p: Partial<PickerState>) => store.setState(p);
  const keep = (e: Entry) => e.isDir && !isEntryHiddenInPicker(e, deps.visibility());

  const api: Picker = {
    async load() {
      const path = pickerPath(get());
      const gen = ++generation;
      set({ loading: true, error: null, nextCursor: null });
      try {
        const listing = await (await deps.getClient()).list(path);
        if (gen !== generation) return;
        set({ loading: false, folders: listing.entries.filter(keep), error: null, nextCursor: listing.nextCursor ?? null });
      } catch (e) {
        if (gen === generation) set({ loading: false, error: deps.humanize(e) });
      }
    },
    async loadMore() {
      const s = get();
      if (s.loading || s.loadingMore || s.nextCursor === null) return;
      const gen = ++generation;
      set({ loadingMore: true });
      try {
        const listing = await (await deps.getClient()).list(pickerPath(s), { cursor: s.nextCursor });
        if (gen !== generation) return;
        set({ folders: [...get().folders, ...listing.entries.filter(keep)], loadingMore: false, nextCursor: listing.nextCursor ?? null });
      } catch {
        // Keep what is shown; scrolling again retries.
        if (gen === generation) set({ loadingMore: false });
      }
    },
    navigate(path) {
      set({ pathStack: [...get().pathStack, path] });
      void api.load();
    },
    navigateTo(index) {
      const s = get();
      if (index >= s.pathStack.length) return;
      set({ pathStack: s.pathStack.slice(0, index + 1) });
      void api.load();
    },
    async createFolder(name) {
      await (await deps.getClient()).createFolder(joinRemotePath(pickerPath(get()), name));
      await api.load();
    },
  };
  return Object.assign(store, api);
}
