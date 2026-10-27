import { createStore, type StoreApi } from 'zustand/vanilla';

import type { AgentClient } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import { minBytesFor, queryForMode, resolveDatePreset, sortByRelevance, type SearchFilters } from './searchLogic';

export type SearchState = { query: string; loading: boolean; error: string | null; raw: Entry[]; truncated: boolean; timeBudgetHit: boolean };

export type SearchDeps = {
  getClient: () => Promise<Pick<AgentClient, 'search'>>;
  humanize: (e: unknown) => string;
  /** Called with the query after a search that returned results successfully (feeds the recent-searches list). */
  onSearched: (q: string) => void;
  now?: () => Date;
};

export interface SearchController {
  /** Runs [value] under [filters]; `currentPath` is the search root when `filters.fromHere`. An empty value clears results. */
  run(value: string, filters: SearchFilters, currentPath: string): Promise<void>;
}

const EMPTY: SearchState = { query: '', loading: false, error: null, raw: [], truncated: false, timeBudgetHit: false };

/**
 * Search state machine. Each run takes a generation number, so an older, slower response can never replace the
 * results of a newer query (the transport has no cancellation, so this is the guard).
 */
export function createSearchController(deps: SearchDeps): StoreApi<SearchState> & SearchController {
  const store = createStore<SearchState>(() => EMPTY);
  let generation = 0;
  const api: SearchController = {
    async run(value, filters, currentPath) {
      const q = value.trim();
      const gen = ++generation;
      if (q === '') {
        store.setState(EMPTY);
        return;
      }
      store.setState({ query: q, loading: true, error: null });
      try {
        const client = await deps.getClient();
        const res = await client.search({
          q: queryForMode(q, filters.mode),
          root: filters.fromHere ? currentPath : undefined,
          types: filters.categories.length ? [...filters.categories] : undefined,
          minSize: minBytesFor(filters.size),
          modifiedAfter: resolveDatePreset(filters.date, (deps.now ?? (() => new Date()))()),
        });
        if (gen !== generation) return;
        store.setState({ loading: false, raw: sortByRelevance(res.entries, q), truncated: res.truncated, timeBudgetHit: res.timeBudgetHit });
        deps.onSearched(q);
      } catch (e) {
        if (gen === generation) store.setState({ loading: false, error: deps.humanize(e), raw: [], truncated: false, timeBudgetHit: false });
      }
    },
  };
  return Object.assign(store, api);
}
