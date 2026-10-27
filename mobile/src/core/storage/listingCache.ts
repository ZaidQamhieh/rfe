import type { Entry } from '../api/models';

export type CachedListing = { entries: Entry[]; fetchedAt: Date };

/** Persistence for cached listings: one JSON blob per (host, path). */
export interface ListingBackend {
  get(host: string, path: string): Promise<{ json: string; fetchedAt: number } | null>;
  put(host: string, path: string, json: string, fetchedAt: number): Promise<void>;
  /** Paths for a host with their fetch time, oldest first. */
  list(host: string): Promise<{ path: string; fetchedAt: number }[]>;
  remove(host: string, path: string): Promise<void>;
  removeHost(host: string): Promise<void>;
}

export class MemoryListingBackend implements ListingBackend {
  readonly rows = new Map<string, { json: string; fetchedAt: number }>();
  private k = (h: string, p: string) => `${h}\u0000${p}`;
  async get(h: string, p: string) {
    return this.rows.get(this.k(h, p)) ?? null;
  }
  async put(h: string, p: string, json: string, fetchedAt: number) {
    this.rows.set(this.k(h, p), { json, fetchedAt });
  }
  async list(h: string) {
    return [...this.rows.entries()]
      .filter(([k]) => k.startsWith(`${h}\u0000`))
      .map(([k, v]) => ({ path: k.slice(h.length + 1), fetchedAt: v.fetchedAt }))
      .sort((a, b) => a.fetchedAt - b.fetchedAt);
  }
  async remove(h: string, p: string) {
    this.rows.delete(this.k(h, p));
  }
  async removeHost(h: string) {
    for (const k of [...this.rows.keys()]) if (k.startsWith(`${h}\u0000`)) this.rows.delete(k);
  }
}

/**
 * Recent directory listings per host so navigation is instant and the explorer stays browsable
 * (read-only) while the host is unreachable. Capped per host with the oldest non-pinned evicted first.
 * Writes are serialised per host (PR-49) so concurrent puts cannot clobber each other.
 */
export class ListingCache {
  private pinned = new Set<string>();
  private chains = new Map<string, Promise<void>>();

  constructor(
    private readonly backend: ListingBackend,
    private readonly maxEntries = 200,
    private readonly now: () => number = Date.now,
  ) {}

  setPinned(key: string, pinned: boolean) {
    if (pinned) this.pinned.add(key);
    else this.pinned.delete(key);
  }

  /** Replaces the pinned-path set for [hostId]; re-hydrated from the pin store because it is lost on restart. */
  syncPinned(hostId: string, paths: Iterable<string>) {
    for (const k of [...this.pinned]) if (k.startsWith(`${hostId}:`)) this.pinned.delete(k);
    for (const p of paths) this.pinned.add(`${hostId}:${p}`);
  }

  put(hostId: string, path: string, entries: Entry[]): Promise<void> {
    const prior = this.chains.get(hostId) ?? Promise.resolve();
    const next = prior.then(() => this.putLocked(hostId, path, entries));
    this.chains.set(hostId, next.catch(() => undefined));
    return next;
  }

  private async putLocked(hostId: string, path: string, entries: Entry[]) {
    await this.backend.put(hostId, path, JSON.stringify(entries), this.now());
    const all = await this.backend.list(hostId);
    let toEvict = all.length - this.maxEntries;
    for (const row of all) {
      if (toEvict <= 0) break;
      if (this.pinned.has(`${hostId}:${row.path}`) || row.path === path) continue;
      await this.backend.remove(hostId, row.path);
      toEvict--;
    }
  }

  async get(hostId: string, path: string): Promise<CachedListing | null> {
    const row = await this.backend.get(hostId, path);
    if (!row) return null;
    try {
      const entries = JSON.parse(row.json);
      return Array.isArray(entries) ? { entries, fetchedAt: new Date(row.fetchedAt) } : null;
    } catch {
      return null;
    }
  }

  /** Cached listings across [hostIds], for the storage screen. */
  async count(hostIds: readonly string[]): Promise<number> {
    let n = 0;
    for (const id of hostIds) n += (await this.backend.list(id)).length;
    return n;
  }

  async evictHost(hostId: string) {
    await this.backend.removeHost(hostId);
    for (const k of [...this.pinned]) if (k.startsWith(`${hostId}:`)) this.pinned.delete(k);
  }
}
