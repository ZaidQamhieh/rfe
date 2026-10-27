import type { AgentClient } from '../../core/api/agentClient';

export type DupGroup = { hash: string; paths: string[] };

/** Slice of the client the scan needs (lets tests fake it). */
export type DupClient = Pick<AgentClient, 'list' | 'batchChecksums'>;

const CHECKSUM_CHUNK = 500;

/**
 * Every file under [root], paging through each directory fully so a folder larger than one page is not
 * silently truncated (PR-33). Files come back in walk order with their listed sizes.
 */
export async function collectFiles(client: DupClient, root: string, onFile?: (count: number) => void): Promise<{ paths: string[]; sizes: Record<string, number> }> {
  const paths: string[] = [];
  const sizes: Record<string, number> = {};
  const walk = async (dir: string): Promise<void> => {
    let cursor: string | undefined;
    do {
      const listing = await client.list(dir, { cursor });
      for (const e of listing.entries) {
        if (e.isDir) await walk(e.path);
        else {
          paths.push(e.path);
          if (e.size != null) sizes[e.path] = e.size;
          onFile?.(paths.length);
        }
      }
      cursor = listing.nextCursor;
    } while (cursor);
  };
  await walk(root);
  return { paths, sizes };
}

/** Groups of two or more paths sharing a hash, largest file first; paths keep the order of [order]. */
export function groupDuplicates(hashes: Record<string, string>, sizes: Record<string, number>, order: string[] = Object.keys(hashes)): DupGroup[] {
  const byHash = new Map<string, string[]>();
  for (const p of order) {
    const h = hashes[p];
    if (h === undefined) continue;
    byHash.set(h, [...(byHash.get(h) ?? []), p]);
  }
  return [...byHash.entries()]
    .filter(([, paths]) => paths.length > 1)
    .map(([hash, paths]) => ({ hash, paths }))
    .sort((a, b) => (sizes[b.paths[0]] ?? 0) - (sizes[a.paths[0]] ?? 0));
}

/** Bytes reclaimable by keeping one copy per group. */
export function computeWaste(groups: DupGroup[], sizes: Record<string, number>): number {
  return groups.reduce((sum, g) => sum + (sizes[g.paths[0]] ?? 0) * (g.paths.length - 1), 0);
}

/** Lists, hashes in chunks of 500 and groups. Files the agent cannot hash are simply not compared. */
export async function scanForDuplicates(client: DupClient, root: string, onFile?: (count: number) => void): Promise<{ groups: DupGroup[]; sizes: Record<string, number> }> {
  const { paths, sizes } = await collectFiles(client, root, onFile);
  const hashes: Record<string, string> = {};
  for (let i = 0; i < paths.length; i += CHECKSUM_CHUNK) Object.assign(hashes, await client.batchChecksums(paths.slice(i, i + CHECKSUM_CHUNK)));
  return { groups: groupDuplicates(hashes, sizes, paths), sizes };
}

/** Everything but the first copy of each group starts marked for deletion. */
export const defaultDeletions = (groups: DupGroup[]): Set<string> => new Set(groups.flatMap((g) => g.paths.slice(1)));

/** Removes deleted paths from the groups and drops groups left with a single copy. */
export function withoutPaths(groups: DupGroup[], deleted: ReadonlySet<string>): DupGroup[] {
  return groups.map((g) => ({ ...g, paths: g.paths.filter((p) => !deleted.has(p)) })).filter((g) => g.paths.length > 1);
}
