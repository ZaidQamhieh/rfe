import type { Entry, SearchResult } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { createCrossHostSearch, groupByHost } from './crossHostSearch';

const host = (id: string): Host => ({ id, label: id.toUpperCase(), address: id });
const e = (name: string): Entry => ({ name, path: `/${name}`, isDir: false, isSymlink: false });
const res = (...n: string[]): SearchResult => ({ entries: n.map(e), truncated: false, timeBudgetHit: false });

describe('cross-host search', () => {
  it('merges hits as hosts answer and marks unreachable ones', async () => {
    const s = createCrossHostSearch({
      hosts: [host('a'), host('b'), host('c')],
      getClient: async (h) => ({
        async search() {
          if (h.id === 'b') throw new Error('offline');
          return res(`${h.id}-file`);
        },
      }),
    });
    await s.run('file');
    const st = s.getState();
    expect(st.searching).toBe(false);
    expect(st.results.map((r) => r.entry.name).sort()).toEqual(['a-file', 'c-file']);
    expect(st.failed).toEqual(['b']);
    expect(groupByHost(st.results).map((g) => g.host.id).sort()).toEqual(['a', 'c']);
  });

  it('does not search for a single character', async () => {
    let calls = 0;
    const s = createCrossHostSearch({ hosts: [host('a')], getClient: async () => ({ search: async () => (calls++, res('x')) }) });
    await s.run('a');
    expect(calls).toBe(0);
    expect(s.getState()).toEqual({ searching: false, results: [], failed: [] });
  });

  it('treats a slow host as failed without blocking the others', async () => {
    const s = createCrossHostSearch({
      hosts: [host('slow'), host('fast')],
      perHostTimeoutMs: 20,
      getClient: async (h) => ({ search: () => (h.id === 'slow' ? new Promise<SearchResult>(() => {}) : Promise.resolve(res('quick'))) }),
    });
    await s.run('quick');
    expect(s.getState().results.map((r) => r.host.id)).toEqual(['fast']);
    expect(s.getState().failed).toEqual(['slow']);
  });

  it('drops answers that arrive after a newer query', async () => {
    let releaseOld: (r: SearchResult) => void = () => {};
    const old = new Promise<SearchResult>((r) => (releaseOld = r));
    const s = createCrossHostSearch({ hosts: [host('a')], getClient: async () => ({ search: (o) => (o.q === 'old' ? old : Promise.resolve(res('new-hit'))) }) });
    const first = s.run('old');
    await s.run('new');
    releaseOld(res('old-hit'));
    await first;
    expect(s.getState().results.map((r) => r.entry.name)).toEqual(['new-hit']);
    expect(s.getState().searching).toBe(false);
  });
});
