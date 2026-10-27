import type { KeyValueStore } from '../../core/storage/hostStore';

const two = (n: number) => String(n).padStart(2, '0');

/**
 * `<destRoot>/[deviceSegment/]YYYY/YYYY-MM/<name>` using the photo's capture date; `/` separators throughout and
 * no leading double slash when [destRoot] is the filesystem root. The device segment keeps several phones that
 * back up to one shared folder from interleaving.
 */
export function backupRemotePath(destRoot: string, created: Date, name: string, deviceSegment = ''): string {
  const year = String(created.getFullYear());
  const month = `${year}-${two(created.getMonth() + 1)}`;
  let root = destRoot;
  while (root.length > 1 && root.endsWith('/')) root = root.slice(0, -1);
  if (root === '/') root = '';
  const dates = deviceSegment === '' ? `${year}/${month}` : `${deviceSegment}/${year}/${month}`;
  return `${root}/${dates}/${name}`;
}

/** Photos not yet backed up, in their original order. */
export const pendingIds = (all: readonly string[], backedUp: ReadonlySet<string>) => all.filter((id) => !backedUp.has(id));

/** Albums to scan: an empty selection means every album; a selected album that no longer exists is dropped. */
export const albumsToScan = (available: readonly string[], selected: ReadonlySet<string>) => (selected.size === 0 ? [...available] : available.filter((id) => selected.has(id)));

/**
 * True once two reads of the length taken [intervalMs] apart agree and are non-zero. A file that a sync client is
 * still writing has a growing length, and uploading it would ship a truncated photo.
 */
export async function isFileStable(readLength: () => number | Promise<number>, wait: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms)), intervalMs = 300): Promise<boolean> {
  const first = await readLength();
  if (first <= 0) return false;
  await wait(intervalMs);
  return (await readLength()) === first;
}

/** A user-typed nickname made safe as one path segment; falls back to `device` when nothing is left. */
export function sanitizeSegment(name: string): string {
  let s = name.replace(/[\u0000-\u001f\\/:*?"<>|]/g, '_');
  s = s.replace(/^[.\s]+|[.\s]+$/g, '');
  if (s.length > 64) s = s.slice(0, 64);
  return s === '' ? 'device' : s;
}

/** `.` plus up to five alphanumerics from a file name, or `.jpg`; never the name itself (untrusted input). */
export function safeExtension(title: string): string {
  const m = /\.([A-Za-z0-9]{1,5})$/.exec(title);
  return m ? `.${m[1]}` : '.jpg';
}

const ID_PREFIX = 'pb';
const SAFE_ID = /^[A-Za-z0-9_-]+$/;

/**
 * Stable key for a media asset: the MediaStore row id. The media library reports assets as content URIs
 * (`content://media/external/images/media/20`), which cannot be a file name or transfer id, so the trailing number
 * is used; it is also what the Flutter app recorded, so an imported backup record still matches.
 */
export function assetKey(assetId: string): string {
  const m = /(\d+)$/.exec(assetId);
  return m ? m[1] : assetId.replace(/[^A-Za-z0-9_-]/g, '_');
}

/** Transfer id for a photo, so a finished upload can be traced back to its asset without a side table. */
export const backupTransferId = (assetId: string) => `${ID_PREFIX}-${assetKey(assetId)}`;

/** The asset key encoded by [backupTransferId], or null for any other transfer. */
export function assetIdFromTransferId(id: string): string | null {
  return id.startsWith(`${ID_PREFIX}-`) && SAFE_ID.test(id) ? id.slice(ID_PREFIX.length + 1) : null;
}

/**
 * True when an upload ended because the computer already holds a file at that path. The path is built from the
 * device, the capture date and the MediaStore id, so this is the same photo backed up earlier (typically before a
 * record reset) and counts as backed up.
 */
export const isAlreadyOnHost = (r: { state: string; error: string | null }) => r.state === 'FAILED' && r.error === 'CONFLICT';

/** Most staged photo copies (queued, running, paused or failed uploads) held in app storage at once. */
export const MAX_STAGED_PHOTOS = 100;

/**
 * How many more photos a run may copy into app storage: every queued upload keeps its own copy until it finishes, so a
 * first run over a large library would otherwise copy the whole library. The rest is picked up by later runs.
 */
export function stagingBudget(records: readonly { id: string; state: string; error: string | null }[], max = MAX_STAGED_PHOTOS): number {
  const held = records.filter((r) => assetIdFromTransferId(r.id) !== null && r.state !== 'DONE' && r.state !== 'CANCELLED' && !isAlreadyOnHost(r)).length;
  return Math.max(0, max - held);
}

/** Wraps [run] so calls made while one is in flight share that run instead of starting a second one. */
export function singleFlight<A extends unknown[], T>(run: (...args: A) => Promise<T>): (...args: A) => Promise<T> {
  let current: Promise<T> | null = null;
  return (...args) => {
    if (current === null) {
      const p: Promise<T> = run(...args).finally(() => {
        if (current === p) current = null;
      });
      current = p;
    }
    return current;
  };
}

export type PhotoBackupPrefs = {
  enabled: boolean;
  hostId: string | null;
  deviceName: string | null;
  wifiOnly: boolean;
  chargingOnly: boolean;
  albumIds: string[];
  /** Run in the background about every six hours (new in the RN app; the Flutter app only backed up on request). */
  scheduled: boolean;
};

// Same keys and JSON encoding the Flutter app wrote, so the one-time import carries the settings and the record over.
const K = {
  enabled: 'rfe_photo_backup_enabled',
  host: 'rfe_photo_backup_host',
  deviceName: 'rfe_photo_backup_device_name',
  wifiOnly: 'rfe_photo_backup_wifi_only',
  chargingOnly: 'rfe_photo_backup_charging_only',
  albums: 'rfe_photo_backup_albums',
  scheduled: 'rfe_photo_backup_scheduled',
  done: 'rfe_photo_backup_done',
} as const;

const parse = (raw: string | null): unknown => {
  if (raw === null) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
};
const asBool = (v: unknown, d: boolean) => (typeof v === 'boolean' ? v : v === 'true' ? true : v === 'false' ? false : d);
const asString = (v: unknown) => (typeof v === 'string' && v !== '' ? v : null);
const asStrings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);

export class PhotoBackupStore {
  constructor(private readonly kv: KeyValueStore) {}

  async load(): Promise<PhotoBackupPrefs> {
    const get = async (k: string) => parse(await this.kv.get(k));
    return {
      enabled: asBool(await get(K.enabled), false),
      hostId: asString(await get(K.host)),
      deviceName: asString(await get(K.deviceName)),
      wifiOnly: asBool(await get(K.wifiOnly), true),
      chargingOnly: asBool(await get(K.chargingOnly), false),
      albumIds: asStrings(await get(K.albums)),
      scheduled: asBool(await get(K.scheduled), false),
    };
  }

  async save(p: PhotoBackupPrefs): Promise<void> {
    const set = (k: string, v: unknown) => this.kv.set(k, JSON.stringify(v));
    await set(K.enabled, p.enabled);
    await set(K.wifiOnly, p.wifiOnly);
    await set(K.chargingOnly, p.chargingOnly);
    await set(K.albums, p.albumIds);
    await set(K.scheduled, p.scheduled);
    if (p.hostId) await set(K.host, p.hostId);
    else await this.kv.remove(K.host);
    if (p.deviceName) await set(K.deviceName, p.deviceName);
    else await this.kv.remove(K.deviceName);
  }

  async doneIds(): Promise<Set<string>> {
    return new Set(asStrings(parse(await this.kv.get(K.done))));
  }

  async markDone(ids: readonly string[]): Promise<void> {
    if (ids.length === 0) return;
    const done = await this.doneIds();
    const before = done.size;
    for (const id of ids) done.add(id);
    if (done.size !== before) await this.kv.set(K.done, JSON.stringify([...done]));
  }

  async clearDone(): Promise<void> {
    await this.kv.remove(K.done);
  }
}
