import type { Host } from '../../core/models/host';
import type { KeyValueStore } from '../../core/storage/hostStore';

/** The one piece of persisted Home state: the host that was open last, so the same computer is shown after a restart. */
export const LAST_HOST_KEY = 'rfe_last_host_v1';

export const readLastHostId = (kv: KeyValueStore): Promise<string | null> => kv.get(LAST_HOST_KEY);
export const writeLastHostId = (kv: KeyValueStore, id: string): Promise<void> => kv.set(LAST_HOST_KEY, id);

/**
 * The host Home and Workspaces treat as selected: the in-memory active host if it is still paired, else the last one
 * opened, else the first paired host. Null only when nothing is paired.
 */
export function pickHomeHost(hosts: readonly Host[], activeId: string | null | undefined, lastId: string | null | undefined): Host | null {
  return hosts.find((h) => h.id === activeId) ?? hosts.find((h) => h.id === lastId) ?? hosts[0] ?? null;
}
