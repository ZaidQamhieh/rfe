import type { Entry } from '../../core/api/models';
import { ListingCache, MemoryListingBackend } from '../../core/storage/listingCache';
import type { AgentClient } from '../../core/api/agentClient';
import { createExplorer } from './explorerStore';
import { filterByTag } from './tagFilter';

const e = (name: string): Entry => ({ name, path: `/r/${name}`, isDir: false, isSymlink: false });

describe('filterByTag', () => {
  const display = [e('a'), e('b'), e('c')];

  it('keeps everything when no tag is active', () => {
    expect(filterByTag(display, null, new Set())).toBe(display);
  });

  it('keeps only the tagged entries, in order, when a tag is active', () => {
    expect(filterByTag(display, 'work', new Set(['/r/c', '/r/a'])).map((x) => x.name)).toEqual(['a', 'c']);
  });

  it('select all and invert only reach rows the user can see under a tag filter', async () => {
    const cache = new ListingCache(new MemoryListingBackend(), 3);
    const client = { list: jest.fn().mockResolvedValue({ path: '/r', entries: display }) };
    const ex = createExplorer({ hostId: 'h', rootPath: '/r', getClient: async () => client as unknown as AgentClient, cache, humanize: String });
    await ex.load();
    const visible = filterByTag(display, 'work', new Set(['/r/a', '/r/c']));
    ex.selectAll(visible);
    expect([...ex.getState().selected].sort()).toEqual(['/r/a', '/r/c']);
    ex.toggleSelect('/r/a');
    ex.invertSelection(visible);
    expect([...ex.getState().selected]).toEqual(['/r/a']);
    expect(ex.getState().selected.has('/r/b')).toBe(false);
  });
});
