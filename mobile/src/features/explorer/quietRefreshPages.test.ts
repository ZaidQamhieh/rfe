import type { AgentClient } from '../../core/api/agentClient';
import type { Entry } from '../../core/api/models';
import { ListingCache, MemoryListingBackend } from '../../core/storage/listingCache';
import { createExplorer } from './explorerStore';

const e = (name: string): Entry => ({ name, path: `/r/${name}`, isDir: false, isSymlink: false });

/** A folder of four files served two per page; the cursor is the index of the next page. */
const pagedClient = () =>
  ({
    list: jest.fn(async (_path: string, o?: { cursor?: string }) =>
      o?.cursor === 'p2' ? { path: '/r', entries: [e('c'), e('d')] } : { path: '/r', entries: [e('a'), e('b')], nextCursor: 'p2' },
    ),
  }) as unknown as AgentClient & { list: jest.Mock };

function setup(client: AgentClient) {
  const cache = new ListingCache(new MemoryListingBackend(), 3);
  const ex = createExplorer({ hostId: 'h', rootPath: '/r', getClient: async () => client, cache, humanize: (x) => (x as Error).message });
  return { ex, cache };
}

describe('quietRefresh after more pages were loaded', () => {
  it('keeps the rows of the loaded pages instead of falling back to page 1', async () => {
    const client = pagedClient();
    const { ex, cache } = setup(client);
    await ex.load();
    await ex.loadMore();
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a', 'b', 'c', 'd']);

    await ex.quietRefresh();

    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a', 'b', 'c', 'd']);
    expect(ex.getState().nextCursor).toBeNull();
    expect((await cache.get('h', '/r'))?.entries.map((x) => x.name)).toEqual(['a', 'b', 'c', 'd']);
  });

  it('still fetches only page 1 when no further page was loaded', async () => {
    const client = pagedClient();
    const { ex } = setup(client);
    await ex.load();
    client.list.mockClear();
    await ex.quietRefresh();
    expect(client.list).toHaveBeenCalledTimes(1);
    expect(ex.getState().entries.map((x) => x.name)).toEqual(['a', 'b']);
    expect(ex.getState().nextCursor).toBe('p2');
  });
});
