import type { Entry, SearchResult } from '../../core/api/models';
import { createSearchController } from './searchController';
import { defaultFilters } from './searchLogic';

const e = (name: string): Entry => ({ name, path: `/${name}`, isDir: false, isSymlink: false });

function setup(handler: (o: Record<string, unknown>) => Promise<SearchResult> | SearchResult) {
  const calls: Record<string, unknown>[] = [];
  const recorded: string[] = [];
  const ctl = createSearchController({
    getClient: async () => ({
      async search(o) {
        calls.push(o as never);
        return handler(o as never);
      },
    }),
    humanize: (x) => (x as Error).message,
    onSearched: (q) => recorded.push(q),
    now: () => new Date(2026, 5, 15, 12),
  });
  return { ctl, calls, recorded };
}

const ok = (...names: string[]): SearchResult => ({ entries: names.map(e), truncated: false, timeBudgetHit: false });

describe('search controller', () => {
  it('maps filters to the request and sorts by relevance', async () => {
    const { ctl, calls, recorded } = setup(() => ok('zed foo', 'foo bar'));
    await ctl.run('  foo ', { ...defaultFilters(), categories: ['image'], size: 'mb10', date: 'last24h', mode: 'regex' }, '/here');
    expect(calls[0]).toEqual({ q: '*foo*', root: '/here', types: ['image'], minSize: 10 * 1024 * 1024, modifiedAfter: new Date(2026, 5, 14, 12) });
    expect(ctl.getState().raw.map((x) => x.name)).toEqual(['foo bar', 'zed foo']);
    expect(ctl.getState().query).toBe('foo');
    expect(recorded).toEqual(['foo']);
  });

  it('searches every root when not restricted to the current folder', async () => {
    const { ctl, calls } = setup(() => ok());
    await ctl.run('x', { ...defaultFilters(), fromHere: false }, '/here');
    expect(calls[0].root).toBeUndefined();
    expect(calls[0].types).toBeUndefined();
  });

  it('carries truncation flags and clears on an empty query', async () => {
    const { ctl } = setup(() => ({ entries: [e('a')], truncated: true, timeBudgetHit: true }));
    await ctl.run('a', defaultFilters(), '/');
    expect(ctl.getState()).toMatchObject({ truncated: true, timeBudgetHit: true, loading: false });
    await ctl.run('   ', defaultFilters(), '/');
    expect(ctl.getState()).toMatchObject({ query: '', raw: [], truncated: false, loading: false });
  });

  it('reports errors without recording the query', async () => {
    const { ctl, recorded } = setup(() => {
      throw new Error('boom');
    });
    await ctl.run('a', defaultFilters(), '/');
    expect(ctl.getState()).toMatchObject({ error: 'boom', loading: false, raw: [] });
    expect(recorded).toEqual([]);
  });

  it('ignores a slower older response after a newer query', async () => {
    let release: (r: SearchResult) => void = () => {};
    const slow = new Promise<SearchResult>((r) => (release = r));
    const { ctl, recorded } = setup((o) => (o.q === 'old' ? slow : ok('new-hit')));
    const first = ctl.run('old', defaultFilters(), '/');
    await ctl.run('new', defaultFilters(), '/');
    release(ok('old-hit'));
    await first;
    expect(ctl.getState().raw.map((x) => x.name)).toEqual(['new-hit']);
    expect(recorded).toEqual(['new']);
  });
});
