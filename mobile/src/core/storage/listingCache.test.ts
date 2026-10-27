import { ListingCache, MemoryListingBackend } from './listingCache';

describe('ListingCache.count and evictHost', () => {
  it('counts listings across hosts and clears them per host', async () => {
    const cache = new ListingCache(new MemoryListingBackend());
    await cache.put('a', '/x', []);
    await cache.put('a', '/y', []);
    await cache.put('b', '/x', []);
    expect(await cache.count(['a', 'b', 'missing'])).toBe(3);
    await cache.evictHost('a');
    expect(await cache.count(['a', 'b'])).toBe(1);
  });
});
