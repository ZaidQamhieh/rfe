import { createStore, type StoreApi } from 'zustand/vanilla';

import type { AgentClient } from '../../core/api/agentClient';
import type { BatchItemResult, BatchResult, Entry } from '../../core/api/models';
import type { ListingCache } from '../../core/storage/listingCache';
import { isEntryHidden, type VisibilityPrefs } from '../../core/visibility';
import { invalidNameReason } from './nameValidation';
import { basenameOf, buildPathStackWithinRoot, joinRemotePath, renameDestination } from './paths';
import { rangePaths } from './selectionLogic';
import { sortEntries, type SortOrder } from './sort';
import type { UndoOp } from './undoPlan';

export type ExplorerState = {
  pathStack: string[];
  entries: Entry[];
  loading: boolean;
  loadingMore: boolean;
  /** The last page fetch failed (in memory only); the list stops asking until the user retries or refreshes. */
  loadMoreError: string | null;
  error: string | null;
  /** The agent's error code behind `error` (for example FORBIDDEN), when it gave one. */
  errorCode: string | null;
  selected: Set<string>;
  /** The item a range selection extends from: the last one ticked. */
  anchor: string | null;
  /** Showing cached data; a refresh is in progress or failed. */
  stale: boolean;
  /** The last live fetch failed and the data is from cache only. */
  offline: boolean;
  nextCursor: string | null;
  /** Session-only "show hidden items" override; not persisted. */
  showHidden: boolean;
};

export type ExplorerDeps = {
  hostId: string;
  rootPath: string;
  getClient: () => Promise<AgentClient>;
  cache: ListingCache;
  humanize: (e: unknown) => string;
};

export const currentPath = (s: ExplorerState) => s.pathStack[s.pathStack.length - 1];
export const atRoot = (s: ExplorerState) => s.pathStack.length === 1;
export const multiSelect = (s: ExplorerState) => s.selected.size > 0;

/** Sorted entries and the hidden-path set; the visible list is derived at display time so revealed hidden items keep their sorted position. */
export function deriveDisplay(s: Pick<ExplorerState, 'entries' | 'showHidden'>, sort: SortOrder, vis: VisibilityPrefs) {
  const sorted = sortEntries(s.entries, sort);
  const hidden = new Set(sorted.filter((e) => isEntryHidden(e, vis)).map((e) => e.path));
  return { sorted, hidden, display: s.showHidden ? sorted : sorted.filter((e) => !hidden.has(e.path)), hiddenCount: hidden.size };
}

/**
 * Explorer state machine ported from ExplorerNotifier. Every load takes a generation number so a
 * stale result of an overlapping same-path load (A -> B -> A) can never clobber the fresh one (PR-34).
 */
const errorCodeOf = (e: unknown): string | null => (typeof e === 'object' && e !== null && typeof (e as { code?: unknown }).code === 'string' ? (e as { code: string }).code : null);

export function createExplorer(deps: ExplorerDeps): StoreApi<ExplorerState> & Explorer {
  const store = createStore<ExplorerState>(() => ({
    pathStack: [deps.rootPath],
    entries: [],
    loading: false,
    loadingMore: false,
    loadMoreError: null,
    error: null,
    errorCode: null,
    selected: new Set(),
    anchor: null,
    stale: false,
    offline: false,
    nextCursor: null,
    showHidden: false,
  }));
  let generation = 0;
  /** The folder `entries` belongs to. */
  let shownPath: string | null = null;
  const get = store.getState;
  const set = (p: Partial<ExplorerState>) => store.setState(p);

  const load = async () => {
    const path = currentPath(get());
    const gen = ++generation;
    // The cache only speeds things up: a backend read that fails is treated as a miss, not as a failed navigation.
    const cached = await Promise.resolve()
      .then(() => deps.cache.get(deps.hostId, path))
      .catch(() => undefined);
    if (gen !== generation) return;
    if (cached) shownPath = path;
    if (cached) set({ entries: cached.entries, loading: false, loadingMore: false, stale: true, offline: false, error: null, loadMoreError: null, selected: new Set(), anchor: null, nextCursor: null });
    // A miss for another folder must not leave the previous folder's rows, or its offline flag, under this folder's name (a failed fetch showed the parent's list as the child's).
    else set({ loading: true, loadingMore: false, error: null, errorCode: null, loadMoreError: null, selected: new Set(), anchor: null, nextCursor: null, ...(shownPath !== path ? { entries: [], stale: false, offline: false } : {}) });
    try {
      const client = await deps.getClient();
      if (gen !== generation) return;
      const listing = await client.list(path);
      if (gen !== generation) return;
      shownPath = path;
      set({ loading: false, entries: listing.entries, stale: false, offline: false, error: null, errorCode: null, nextCursor: listing.nextCursor ?? null });
      // A good listing stays on screen even if the local cache cannot be written.
      await deps.cache.put(deps.hostId, path, listing.entries).catch(() => {});
    } catch (e) {
      if (gen !== generation) return;
      if (cached) set({ loading: false, stale: true, offline: true });
      else set({ loading: false, error: deps.humanize(e), errorCode: errorCodeOf(e) });
    }
  };

  const api: Explorer = {
    load,
    refresh: load,
    async loadMore(o) {
      const s = get();
      if (s.loading || s.loadingMore || s.nextCursor === null) return;
      // After a failure the list asks again as soon as it is still at the end; only an explicit retry goes out.
      if (s.loadMoreError !== null && !o?.retry) return;
      const path = currentPath(s);
      const gen = ++generation;
      set({ loadingMore: true, loadMoreError: null });
      try {
        const client = await deps.getClient();
        if (gen !== generation) return;
        const listing = await client.list(path, { cursor: s.nextCursor });
        if (gen !== generation) return;
        const merged = [...get().entries, ...listing.entries];
        set({ entries: merged, loadingMore: false, nextCursor: listing.nextCursor ?? null });
        await deps.cache.put(deps.hostId, path, merged).catch(() => {});
      } catch (e) {
        if (gen === generation) set({ loadingMore: false, loadMoreError: deps.humanize(e) });
      }
    },
    navigate(path) {
      set({ pathStack: [...get().pathStack, path] });
      void load();
    },
    popDirectory() {
      const s = get();
      if (atRoot(s)) return false;
      set({ pathStack: s.pathStack.slice(0, -1) });
      void load();
      return true;
    },
    navigateTo(index) {
      const s = get();
      if (index >= s.pathStack.length) return;
      set({ pathStack: s.pathStack.slice(0, index + 1) });
      void load();
    },
    jumpTo(path) {
      set({ pathStack: buildPathStackWithinRoot(deps.rootPath, path) });
      void load();
    },
    toggleShowHidden: () => set({ showHidden: !get().showHidden }),
    toggleSelect(path) {
      const sel = new Set(get().selected);
      const added = !sel.delete(path);
      if (added) sel.add(path);
      set({ selected: sel, anchor: added ? path : null });
    },
    selectRange(displayed, to) {
      const range = rangePaths(displayed.map((e) => e.path), get().anchor, to);
      if (range.length === 0) return;
      set({ selected: new Set([...get().selected, ...range]), anchor: to });
    },
    clearSelection: () => set({ selected: new Set(), anchor: null }),
    selectAll: (displayed) => set({ selected: new Set(displayed.map((e) => e.path)), anchor: null }),
    invertSelection(displayed) {
      const all = new Set(displayed.map((e) => e.path));
      const cur = get().selected;
      set({ selected: new Set([...all].filter((p) => !cur.has(p))), anchor: null });
    },
    async createFolder(name, o = {}) {
      const target = joinRemotePath(currentPath(get()), name);
      await (await deps.getClient()).createFolder(target);
      if (o.open) {
        set({ pathStack: [...get().pathStack, target] });
      }
      await load();
    },
    async createFile(name) {
      await (await deps.getClient()).createFile(joinRemotePath(currentPath(get()), name));
      await load();
    },
    async rename(oldPath, newName) {
      const bad = invalidNameReason(newName);
      if (bad) throw new Error(bad);
      await (await deps.getClient()).rename(oldPath, renameDestination(oldPath, newName));
      await load();
    },
    async deleteSelected(o = {}) {
      const res = await (await deps.getClient()).delete([...get().selected], { permanent: o.permanent });
      await load();
      return res;
    },
    async moveSelected(destDir, o = {}) {
      const res = await (await deps.getClient()).move(o.sources ?? [...get().selected], destDir, o);
      await load();
      return res;
    },
    async copySelected(destDir, o = {}) {
      const res = await (await deps.getClient()).copy(o.sources ?? [...get().selected], destDir, o);
      await load();
      return res;
    },
    async undo(op) {
      const client = await deps.getClient();
      try {
        if (op.kind === 'rename') await client.rename(op.from, op.to);
        else {
          let refused = 0;
          for (const g of op.groups) refused += (await client.move(g.paths, g.destDir, {})).failed.length;
          if (refused > 0) throw new Error(`${refused} could not go back: a file with that name already exists there, or it was changed since`);
        }
      } finally {
        await load();
      }
    },
    duplicateSelected: () => api.copySelected(currentPath(get()), { duplicate: true }),
    async compressSelected(dest, sources) {
      const entry = await (await deps.getClient()).compress(sources ?? [...get().selected], dest);
      await load();
      return entry;
    },
    async extractArchive(archive, destDir) {
      const entry = await (await deps.getClient()).extract(archive, destDir ?? currentPath(get()));
      await load();
      return entry;
    },
    async batchRename(renames) {
      const client = await deps.getClient();
      const results: BatchItemResult[] = [];
      const pending = new Map<string, { tmp: string; orig: string }>(); // finalPath -> temp name and original path
      const fail = (path: string, e: unknown): BatchItemResult => ({ path, ok: false, errorCode: 'RENAME_FAILED', errorMessage: deps.humanize(e) });
      // Two sources landing on the same final name would strand the first at its temp name (PR-35): reject
      // every duplicate target up front, before any file is touched.
      const counts = new Map<string, number>();
      for (const r of renames) {
        if (invalidNameReason(r.newName)) continue;
        const dst = renameDestination(r.path, r.newName);
        counts.set(dst, (counts.get(dst) ?? 0) + 1);
      }
      // A final name that is already in the folder and is not being vacated by this batch would be refused by the
      // host after other files had moved; refuse it up front instead (a chain a>b, b>c stays allowed).
      // The entries on screen can be stale or only the first page, so a target in a page that was never loaded would be
      // refused by the host midway and could strand a file under its temp name: read the whole folder fresh as well.
      const shown = new Set(get().entries.map((x) => x.path));
      try {
        let cursor: string | undefined;
        do {
          const l = await client.list(currentPath(get()), { cursor });
          for (const x of l.entries) shown.add(x.path);
          cursor = l.nextCursor;
        } while (cursor);
      } catch {
        // No fresh listing: the host still refuses a taken name, and each failed item is put back below.
      }
      // Only a rename that really goes ahead vacates its old name, so refusing one (its target is taken) can in turn
      // leave the rename that wanted the name it would have vacated without a free target: repeat until stable, so no
      // file is moved to a temp name that it then cannot leave again.
      const moving = (i: number) => !invalidNameReason(renames[i].newName) && renameDestination(renames[i].path, renames[i].newName) !== renames[i].path && (counts.get(renameDestination(renames[i].path, renames[i].newName)) ?? 0) <= 1;
      const blocked = new Set<number>();
      for (let changed = true; changed; ) {
        changed = false;
        const vacated = new Set<string>();
        for (let i = 0; i < renames.length; i++) if (moving(i) && !blocked.has(i)) vacated.add(renames[i].path);
        for (let i = 0; i < renames.length; i++) {
          if (!moving(i) || blocked.has(i)) continue;
          const dst = renameDestination(renames[i].path, renames[i].newName);
          if (shown.has(dst) && !vacated.has(dst)) {
            blocked.add(i);
            changed = true;
          }
        }
      }
      for (let i = 0; i < renames.length; i++) {
        const r = renames[i];
        const badName = invalidNameReason(r.newName);
        if (badName) {
          results.push({ path: r.path, ok: false, errorCode: 'INVALID_NAME', errorMessage: badName });
          continue;
        }
        const dst = renameDestination(r.path, r.newName);
        if (dst === r.path) {
          results.push({ path: r.path, ok: true });
          continue;
        }
        if (blocked.has(i)) {
          results.push({ path: r.path, ok: false, errorCode: 'TARGET_EXISTS', errorMessage: `"${r.newName}" already exists in this folder` });
          continue;
        }
        if ((counts.get(dst) ?? 0) > 1) {
          results.push({ path: r.path, ok: false, errorCode: 'DUPLICATE_TARGET', errorMessage: `Another selected item is also being renamed to "${r.newName}"` });
          continue;
        }
        const tmp = renameDestination(r.path, `.rfe-rn-${i}-${r.newName}`);
        try {
          await client.rename(r.path, tmp);
          pending.set(dst, { tmp, orig: r.path });
        } catch (e) {
          results.push(fail(r.path, e));
        }
      }
      for (const [finalPath, { tmp, orig }] of pending) {
        try {
          await client.rename(tmp, finalPath);
          results.push({ path: finalPath, ok: true });
        } catch (e) {
          // The final name was refused (for example taken by an item that was not selected): put the file back
          // under its own name instead of leaving it under the hidden temp name.
          try {
            await client.rename(tmp, orig);
            results.push(fail(orig, e));
          } catch {
            results.push({ path: tmp, ok: false, errorCode: 'RENAME_STRANDED', errorMessage: `Could not rename it, and could not restore the name ${orig}: it was left as ${tmp}. ${deps.humanize(e)}` });
          }
        }
      }
      await load();
      return { results, failed: results.filter((r) => !r.ok) };
    },
    /** Basenames of [sourcePaths] that already exist in [destDir]; pages the whole destination so late collisions are not missed. */
    async collidingBasenames(destDir, sourcePaths) {
      const client = await deps.getClient();
      const names = new Set<string>();
      let cursor: string | undefined;
      do {
        const l = await client.list(destDir, { cursor });
        for (const e of l.entries) names.add(e.name);
        cursor = l.nextCursor;
      } while (cursor);
      return new Set([...sourcePaths].map(basenameOf).filter((n) => names.has(n)));
    },
  };
  return Object.assign(store, api);
}

type CollisionOpts = { sources?: string[]; duplicate?: boolean; overwrite?: boolean };

export interface Explorer {
  load(): Promise<void>;
  refresh(): Promise<void>;
  loadMore(o?: { retry?: boolean }): Promise<void>;
  navigate(path: string): void;
  popDirectory(): boolean;
  navigateTo(index: number): void;
  jumpTo(path: string): void;
  toggleShowHidden(): void;
  toggleSelect(path: string): void;
  /** Adds everything between the last ticked item and [to] (as listed in [displayed]) to the selection. */
  selectRange(displayed: Entry[], to: string): void;
  clearSelection(): void;
  selectAll(displayed: Entry[]): void;
  invertSelection(displayed: Entry[]): void;
  /** [open] navigates into the new folder instead of re-listing the current one. */
  createFolder(name: string, o?: { open?: boolean }): Promise<void>;
  createFile(name: string): Promise<void>;
  rename(oldPath: string, newName: string): Promise<void>;
  deleteSelected(o?: { permanent?: boolean }): Promise<BatchResult>;
  moveSelected(destDir: string, o?: CollisionOpts): Promise<BatchResult>;
  copySelected(destDir: string, o?: CollisionOpts): Promise<BatchResult>;
  /** Reverses a rename or move (the host refuses to replace anything, so a taken name makes it throw). */
  undo(op: UndoOp): Promise<void>;
  /** Copies the selection next to the originals, each under a free name. */
  duplicateSelected(): Promise<BatchResult>;
  compressSelected(dest: string, sources?: string[]): Promise<Entry>;
  extractArchive(archive: string, destDir?: string): Promise<Entry>;
  batchRename(renames: { path: string; newName: string }[]): Promise<BatchResult>;
  collidingBasenames(destDir: string, sourcePaths: Iterable<string>): Promise<Set<string>>;
}
