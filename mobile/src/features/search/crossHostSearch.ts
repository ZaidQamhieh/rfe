import { createStore, type StoreApi } from 'zustand/vanilla';

import type { AgentClient } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';

export type CrossHostResult = { host: Host; entry: Entry };
export type CrossHostState = { searching: boolean; results: CrossHostResult[]; failed: string[] };

export type CrossHostDeps = {
  hosts: Host[];
  getClient: (host: Host) => Promise<Pick<AgentClient, 'search'>>;
  /** A host slower than this counts as failed instead of holding back everyone else's results. */
  perHostTimeoutMs?: number;
};

export const MIN_CROSS_QUERY = 2;

const withTimeout = <T,>(p: Promise<T>, ms: number): Promise<T> =>
  new Promise((resolve, reject) => {
    const id = setTimeout(() => reject(new Error('timeout')), ms);
    p.then(
      (v) => (clearTimeout(id), resolve(v)),
      (e) => (clearTimeout(id), reject(e)),
    );
  });

/**
 * Searches every host at once and streams each host's hits in as it answers. A run is tagged with a generation,
 * so a slow answer for an old query never lands in newer results (PR-32). Queries under two characters clear.
 */
export function createCrossHostSearch(deps: CrossHostDeps): StoreApi<CrossHostState> & { run(query: string): Promise<void> } {
  const store = createStore<CrossHostState>(() => ({ searching: false, results: [], failed: [] }));
  let generation = 0;
  return Object.assign(store, {
    async run(query: string) {
      const q = query.trim();
      const gen = ++generation;
      if (q.length < MIN_CROSS_QUERY) {
        store.setState({ searching: false, results: [], failed: [] });
        return;
      }
      store.setState({ searching: true, results: [], failed: [] });
      await Promise.all(
        deps.hosts.map(async (host) => {
          try {
            const client = await deps.getClient(host);
            const res = await withTimeout(client.search({ q }), deps.perHostTimeoutMs ?? 10_000);
            if (gen !== generation) return;
            store.setState((s) => ({ results: [...s.results, ...res.entries.map((entry) => ({ host, entry }))] }));
          } catch {
            if (gen === generation) store.setState((s) => ({ failed: [...s.failed, host.id] }));
          }
        }),
      );
      if (gen === generation) store.setState({ searching: false });
    },
  });
}

/** Results grouped per host, keeping first-seen host order. */
export function groupByHost(results: CrossHostResult[]): { host: Host; entries: Entry[] }[] {
  const groups = new Map<string, { host: Host; entries: Entry[] }>();
  for (const r of results) {
    const g = groups.get(r.host.id) ?? { host: r.host, entries: [] };
    g.entries.push(r.entry);
    groups.set(r.host.id, g);
  }
  return [...groups.values()];
}
