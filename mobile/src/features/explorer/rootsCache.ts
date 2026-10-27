import type { Drive } from '../../core/api/models';
import type { KeyValueStore } from '../../core/storage/hostStore';

/** What a host last told us it lets this device browse; lets the Files tab open cached listings while the host is offline. */
export type CachedRoots = { roots: string[]; os?: string; drives?: Drive[] };

const key = (hostId: string) => `rfe_roots_v1_${hostId}`;

export async function saveRoots(kv: KeyValueStore, hostId: string, value: CachedRoots): Promise<void> {
  await kv.set(key(hostId), JSON.stringify(value));
}

/** The saved roots, or null when none were saved or the record is unusable. Access-denied hosts are never saved. */
export async function loadRoots(kv: KeyValueStore, hostId: string): Promise<CachedRoots | null> {
  const raw = await kv.get(key(hostId));
  if (!raw) return null;
  try {
    const j = JSON.parse(raw) as Partial<CachedRoots>;
    if (!Array.isArray(j.roots) || !j.roots.every((r) => typeof r === 'string')) return null;
    return {
      roots: j.roots,
      os: typeof j.os === 'string' ? j.os : undefined,
      drives: Array.isArray(j.drives) ? j.drives.filter((d): d is Drive => typeof d === 'object' && d !== null && typeof (d as Drive).path === 'string') : undefined,
    };
  } catch {
    return null;
  }
}

export const forgetRoots = (kv: KeyValueStore, hostId: string) => kv.remove(key(hostId));
