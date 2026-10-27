import { hostFromJson, hostToJson, type Host } from '../models/host';
import { normalizeFingerprint } from '../api/pin';
import { SecureKeys, type SecureStore } from '../security/secureStore';

/** Non-secret key/value storage (host metadata, settings). Never used for tokens, pins or keys. */
export interface KeyValueStore {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
}

export class MemoryKeyValueStore implements KeyValueStore {
  readonly data = new Map<string, string>();
  async get(key: string) {
    return this.data.get(key) ?? null;
  }
  async set(key: string, value: string) {
    this.data.set(key, value);
  }
  async remove(key: string) {
    this.data.delete(key);
  }
}

// Same key as the Flutter app (`rfe_hosts_v1`). The value here is one JSON
// array of host records; the Flutter StringList form is converted on import.
export const HOSTS_KEY = 'rfe_hosts_v1';
const lastSeenKey = (id: string) => `rfe_last_seen_${id}`;

export class HostStore {
  constructor(
    private readonly kv: KeyValueStore,
    private readonly secure: SecureStore,
    /** Runs after a host is forgotten, to drop everything cached for it (listings, offline file bodies). */
    private readonly onRemoved: (id: string) => Promise<void> = async () => {},
  ) {}

  async listHosts(): Promise<Host[]> {
    const raw = await this.kv.get(HOSTS_KEY);
    if (!raw) return [];
    let arr: unknown;
    try {
      arr = JSON.parse(raw);
    } catch {
      return [];
    }
    if (!Array.isArray(arr)) return [];
    // One corrupt record must never hide the rest of the list (PR-54).
    return arr.map((e) => hostFromJson(typeof e === 'string' ? safeParse(e) : e)).filter((h): h is Host => h !== null);
  }

  private save(hosts: Host[]) {
    return this.kv.set(HOSTS_KEY, JSON.stringify(hosts.map(hostToJson)));
  }

  /** Mutations of the host list read, change and write it back: run one at a time so none overwrites another (F-29). */
  private queue: Promise<unknown> = Promise.resolve();
  private serial<T>(fn: () => Promise<T>): Promise<T> {
    const run = this.queue.then(fn, fn);
    this.queue = run.catch(() => undefined);
    return run;
  }

  addHost(host: Host) {
    return this.serial(async () => {
      const hosts = (await this.listHosts()).filter((h) => h.id !== host.id);
      hosts.push(host);
      await this.save(hosts);
    });
  }

  /** Replaces a listed host's record. A host that is no longer listed (forgotten meanwhile) is not added back (F-28). */
  updateHost(host: Host) {
    return this.serial(async () => {
      const hosts = await this.listHosts();
      const i = hosts.findIndex((h) => h.id === host.id);
      if (i < 0) return;
      hosts[i] = host;
      await this.save(hosts);
    });
  }

  /**
   * Changes only the given fields of the stored record, so a background learn (address, MAC) cannot revert an edit made
   * meanwhile. Returns false, writing nothing, when the host is no longer listed.
   */
  patchHost(id: string, patch: Partial<Omit<Host, 'id'>>): Promise<boolean> {
    return this.serial(async () => {
      const hosts = await this.listHosts();
      const i = hosts.findIndex((h) => h.id === id);
      if (i < 0) return false;
      hosts[i] = { ...hosts[i], ...patch };
      await this.save(hosts);
      return true;
    });
  }

  /**
   * Commits a freshly paired host, its token and pin as one unit. Secrets are
   * written first (pin before token) and the visible host record last, so a
   * failure at any step can never leave a listed host without its pin or token;
   * on failure everything written so far is removed.
   */
  async commitPairing(host: Host, o: { token: string; fingerprint: string }) {
    const pin = normalizeFingerprint(o.fingerprint);
    if (pin === null) throw new Error('refusing to commit pairing without a valid certificate pin');
    // Re-pairing a host that is still listed: a failure must put its previous pin and token back, not delete them
    // and leave a listed host with no login (audit F-49).
    const before = new Map<string, string | null>();
    for (const k of [SecureKeys.fingerprint(host.id), SecureKeys.token(host.id)]) before.set(k, await this.secure.read(k).catch(() => null));
    const done: string[] = [];
    try {
      await this.secure.write(SecureKeys.fingerprint(host.id), pin);
      done.push(SecureKeys.fingerprint(host.id));
      await this.secure.write(SecureKeys.token(host.id), o.token);
      done.push(SecureKeys.token(host.id));
      await this.addHost({ ...host, certFingerprint: pin });
    } catch (e) {
      for (const k of done) {
        const prev = before.get(k) ?? null;
        await (prev === null ? this.secure.delete(k) : this.secure.write(k, prev)).catch(() => undefined);
      }
      throw e;
    }
  }

  touchHost(id: string) {
    return this.serial(async () => {
      const hosts = await this.listHosts();
      const i = hosts.findIndex((h) => h.id === id);
      if (i <= 0) return;
      hosts.unshift(...hosts.splice(i, 1));
      await this.save(hosts);
    });
  }

  removeHost(id: string) {
    return this.serial(async () => {
      // Secrets first: if a keystore delete throws, the host is still listed and can be forgotten again, instead of
      // vanishing from the list while its bearer token stays in the keystore and in backups (audit F-75).
      await this.secure.delete(SecureKeys.token(id));
      await this.secure.delete(SecureKeys.fingerprint(id));
      await this.save((await this.listHosts()).filter((h) => h.id !== id));
      await this.kv.remove(lastSeenKey(id));
      // A forgotten host must not keep serving its cached listings and files. Cleanup is best effort: the
      // host, token and pin are already gone, so a failure here cannot resurrect access.
      await this.onRemoved(id).catch(() => {});
    });
  }

  /** Last successful /health, shown on offline hosts. */
  async getLastSeen(id: string): Promise<Date | null> {
    const raw = await this.kv.get(lastSeenKey(id));
    const ms = raw === null ? NaN : Number(raw);
    return Number.isFinite(ms) ? new Date(ms) : null;
  }

  /** Records a successful /health; ignored for a host that is no longer listed, so a late ping cannot resurrect it. */
  setLastSeen(id: string, at: Date = new Date()) {
    return this.serial(async () => {
      if (!(await this.listHosts()).some((h) => h.id === id)) return;
      await this.kv.set(lastSeenKey(id), String(at.getTime()));
    });
  }

  getToken(id: string) {
    return this.secure.read(SecureKeys.token(id));
  }

  /** The secure-store pin is authoritative; the host record's mirror is never used for trust. */
  async getPin(id: string): Promise<string | null> {
    return normalizeFingerprint(await this.secure.read(SecureKeys.fingerprint(id)));
  }
}

function safeParse(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return null;
  }
}
