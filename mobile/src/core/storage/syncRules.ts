import type { KeyValueStore } from './hostStore';

/** A rule mapping a remote folder on a computer to a local folder that mirrors it (downloads only). */
export type SyncRule = {
  id: string;
  hostId: string;
  remotePath: string;
  /** A `content://` tree URI from the folder picker, or a plain path imported from the Flutter app. */
  localPath: string;
  enabled: boolean;
  lastSync?: string;
};

// The Flutter app's key: a list of JSON strings (the RN store reads both that and a list of objects).
export const SYNC_RULES_KEY = 'rfe_sync_rules_v1';

export function parseRule(raw: unknown): SyncRule | null {
  let o = raw;
  if (typeof raw === 'string') {
    try {
      o = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  const r = o as Partial<Record<keyof SyncRule, unknown>> | null;
  if (typeof r !== 'object' || r === null) return null;
  if (typeof r.id !== 'string' || typeof r.hostId !== 'string' || typeof r.remotePath !== 'string' || typeof r.localPath !== 'string') return null;
  return {
    id: r.id,
    hostId: r.hostId,
    remotePath: r.remotePath,
    localPath: r.localPath,
    enabled: typeof r.enabled === 'boolean' ? r.enabled : true,
    lastSync: typeof r.lastSync === 'string' && !Number.isNaN(Date.parse(r.lastSync)) ? r.lastSync : undefined,
  };
}

export class SyncRuleStore {
  constructor(private readonly kv: KeyValueStore) {}

  /** One corrupt or legacy entry is skipped rather than hiding every rule. */
  async list(): Promise<SyncRule[]> {
    const raw = await this.kv.get(SYNC_RULES_KEY);
    if (!raw) return [];
    let arr: unknown;
    try {
      arr = JSON.parse(raw);
    } catch {
      return [];
    }
    return Array.isArray(arr) ? arr.map(parseRule).filter((r): r is SyncRule => r !== null) : [];
  }

  private write(rules: SyncRule[]) {
    return this.kv.set(SYNC_RULES_KEY, JSON.stringify(rules.map((r) => JSON.stringify(r))));
  }

  async save(rule: SyncRule): Promise<void> {
    const rules = (await this.list()).filter((r) => r.id !== rule.id);
    rules.push(rule);
    await this.write(rules);
  }

  async remove(id: string): Promise<void> {
    await this.write((await this.list()).filter((r) => r.id !== id));
  }

  /** Drops the rules of a computer that was forgotten. */
  async removeForHost(hostId: string): Promise<void> {
    await this.write((await this.list()).filter((r) => r.hostId !== hostId));
  }
}
