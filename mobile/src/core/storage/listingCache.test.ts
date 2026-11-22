import { ListingCache, MemoryListingBackend } from './listingCache';

/** A backend whose put() waits until [release] is called, to hold a write in flight. */
class GatedBackend extends MemoryListingBackend {
  release!: () => void;
  private gate = new Promise<void>((r) => (this.release = r));
  async put(h: string, p: string, json: string, fetchedAt: number) {
    await this.gate;
    await super.put(h, p, json, fetchedAt);
  }
}

describe('ListingCache.evictHost ordering', () => {
  it('a put already queued for a host cannot write a row after the host is evicted', async () => {
    const backend = new GatedBackend();
    const cache = new ListingCache(backend);
    const pending = cache.put('a', '/x', []);
    const evicted = cache.evictHost('a');
    backend.release();
    await Promise.all([pending, evicted]);
    expect(await cache.count(['a'])).toBe(0);
  });
});

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
