import { sleep } from '../../core/sleep';
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
  const dates = deviceSegment === '' ? `${year}/${month}` : `${deviceSegment}/${year}/${month}`;
  return `${backupRootPrefix(destRoot)}${dates}/${name}`;
}

/** [destRoot] without trailing slashes; the filesystem root becomes the empty string so paths never start with `//`. */
function normalRoot(destRoot: string): string {
  let root = destRoot;
  while (root.length > 1 && root.endsWith('/')) root = root.slice(0, -1);
  return root === '/' ? '' : root;
}

/** The start every backup path under [destRoot] shares (the root and one `/`). */
export const backupRootPrefix = (destRoot: string) => `${normalRoot(destRoot)}/`;

/**
 * Albums to scan: an empty selection means every album; a selected album that no longer exists is dropped. When every
 * selected album is gone the result is empty on purpose: widening it to every album would upload photos the owner
 * left out, so the caller reports that instead.
 */
export const albumsToScan = (available: readonly string[], selected: ReadonlySet<string>) => (selected.size === 0 ? [...available] : available.filter((id) => selected.has(id)));

/** True until a computer is chosen as the backup destination; the settings screen shows a notice while this holds. */
export const needsDestination = (hostId: string | null | undefined): boolean => !hostId;

/** What set-up still needs: a paired computer to exist, one to be chosen, backup to be switched on, or nothing (done). */
export type SetupStep = 'pairComputer' | 'chooseComputer' | 'enable' | 'done';

/**
 * The first thing photo backup still lacks, given the saved choices and the ids of the computers that are paired now.
 * A chosen computer that has since been forgotten counts as not chosen.
 */
export function nextSetupStep(prefs: Pick<PhotoBackupPrefs, 'enabled' | 'hostId'>, hostIds: readonly string[]): SetupStep {
  if (hostIds.length === 0) return 'pairComputer';
  if (!prefs.hostId || !hostIds.includes(prefs.hostId)) return 'chooseComputer';
  return prefs.enabled ? 'done' : 'enable';
}

/** The computer to back up to when only one is paired (no question to ask); null when there is a real choice or none. */
export const soleHostId = (hostIds: readonly string[]): string | null => (hostIds.length === 1 ? hostIds[0] : null);

/** What a photo-backup destination check learned about one paired computer. */
export type BackupHostState = {
  id: string;
  /** It answered a health check just now. */
  online: boolean;
  /** It answered with a different certificate than the one this phone pinned (its identity changed). */
  untrusted: boolean;
  /** It removed this phone's access. */
  refused: boolean;
};

/**
 * Which computer "Back up to" should name. A computer that changed identity or removed this phone can never receive
 * photos, so it is skipped. Among the rest the saved choice wins while it is answering, then any answering computer, then
 * the saved choice even though it is silent; null when nothing is usable (the person must choose or pair again).
 */
export function defaultBackupHostId(savedId: string | null, hosts: readonly BackupHostState[]): string | null {
  const usable = hosts.filter((h) => !h.untrusted && !h.refused);
  const saved = usable.find((h) => h.id === savedId);
  if (saved?.online) return saved.id;
  const connected = usable.find((h) => h.online);
  if (connected) return connected.id;
  return saved ? saved.id : null;
}

const STABILITY_CHECK_INTERVAL_MS = 300;

/**
 * True once two reads of the length taken [intervalMs] apart agree and are non-zero. A file that a sync client is
 * still writing has a growing length, and uploading it would ship a truncated photo.
 */
export async function isFileStable(readLength: () => number | Promise<number>, wait: (ms: number) => Promise<void> = sleep, intervalMs = STABILITY_CHECK_INTERVAL_MS): Promise<boolean> {
  const first = await readLength();
  if (first <= 0) return false;
  await wait(intervalMs);
  return (await readLength()) === first;
}

const MAX_SEGMENT_LENGTH = 64;
const DEVICE_SEGMENT_FALLBACK = 'device';
const RESERVED_SEGMENT_PREFIX = '_';
/** Windows device names, refused as a file or folder name with or without an extension, in any case. */
const WINDOWS_DEVICE_NAME = /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$/i;

/** Windows drops trailing dots and spaces from a name, so a segment must not end in them (nor start with them). */
const trimDotsAndSpaces = (s: string): string => s.replace(/^[.\s]+|[.\s]+$/g, '');

/** A user-typed nickname made safe as one path segment; falls back to `device` when nothing is left. */
export function sanitizeSegment(name: string): string {
  let s = trimDotsAndSpaces(name.replace(/[\u0000-\u001f\\/:*?"<>|]/g, '_'));
  if (WINDOWS_DEVICE_NAME.test(s.split('.')[0].trimEnd())) s = `${RESERVED_SEGMENT_PREFIX}${s}`;
  // Cut by code point, not UTF-16 unit, so an emoji at the limit is not split into a lone surrogate.
  s = trimDotsAndSpaces(Array.from(s).slice(0, MAX_SEGMENT_LENGTH).join(''));
  return s === '' ? DEVICE_SEGMENT_FALLBACK : s;
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

/**
 * Upload error codes where the computer said no and trying again cannot change that until someone changes a setting
 * there (no permission, read-only, file too large). Same codes the transfer list marks as not retryable.
 */
export const REFUSAL_CODES: ReadonlySet<string> = new Set(['CAPABILITY_DENIED', 'FORBIDDEN', 'READ_ONLY', 'PAYLOAD_TOO_LARGE']);

/** True when an upload failed because the computer refused it (see [REFUSAL_CODES]); such a photo is not waiting to upload. */
export const isRefused = (r: { state: string; error: string | null }) => r.state === 'FAILED' && r.error !== null && REFUSAL_CODES.has(r.error.split(':')[0].trim());

/** Most staged photo copies (queued, running, paused or failed uploads) held in app storage at once. */
export const MAX_STAGED_PHOTOS = 100;

/**
 * How many more photos a run may copy into app storage: every queued upload keeps its own copy until it finishes, so a
 * first run over a large library would otherwise copy the whole library. The rest is picked up by later runs. Refused
 * uploads are not retried by runs, so they do not take a place.
 */
export function stagingBudget(records: readonly { id: string; state: string; error: string | null }[], max = MAX_STAGED_PHOTOS): number {
  const held = records.filter((r) => assetIdFromTransferId(r.id) !== null && r.state !== 'DONE' && r.state !== 'CANCELLED' && !isAlreadyOnHost(r) && !isRefused(r)).length;
  return Math.max(0, max - held);
}

/** True when an upload record was made for [scope]: the same computer and a target path inside the same folder. */
export const isInScope = (r: { hostId: string; remotePath: string }, scope: DoneScope) => r.hostId === scope.hostId && r.remotePath.startsWith(backupRootPrefix(scope.root));

/** The fields of a transfer record that decide what a backup run does with an earlier upload of the same photo. */
export type PriorUpload = { state: string; error: string | null; hostId: string; address: string; remotePath: string };

/**
 * What a run does with a photo that already has an upload record: `done` (it arrived), `waiting` (still queued or
 * running towards this computer and folder), `busy` (queued or running towards another computer or folder: it cannot
 * be replaced until it ends, so the photo is tried again next run), `refused` (the computer said no; left until the
 * owner resets the record), `resume` (a failed or paused upload that keeps its session and staged copy on the same
 * computer) or `copy` (no usable record: copy the photo and start over). The target is checked first: a record made
 * for another computer or destination folder never counts as done, waiting, refused or resumable for this one.
 */
export function classifyPrior(prior: PriorUpload | undefined, where: { hostId: string; address: string; root: string }, stagedCopyExists: () => boolean): 'done' | 'waiting' | 'busy' | 'refused' | 'resume' | 'copy' {
  if (!prior) return 'copy';
  const inFlight = prior.state === 'QUEUED' || prior.state === 'RUNNING';
  if (!isInScope(prior, { hostId: where.hostId, root: where.root })) return inFlight ? 'busy' : 'copy';
  if (inFlight) return 'waiting';
  if (prior.state === 'DONE' || isAlreadyOnHost(prior)) return 'done';
  if (isRefused(prior)) return 'refused';
  if ((prior.state === 'FAILED' || prior.state === 'PAUSED') && prior.address === where.address && stagedCopyExists()) return 'resume';
  return 'copy';
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
  // Which computer and folder `done` describes; absent in a record written before scopes existed.
  doneScope: 'rfe_photo_backup_done_scope',
  // Ids of the uploads the app paused itself (a Wi-Fi or charging limit stopped holding), as opposed to ones the owner paused.
  held: 'rfe_photo_backup_held_uploads',
} as const;

/** The computer (host id) and destination folder a set of backed-up photos reached. */
export type DoneScope = { hostId: string; root: string };

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

  /** Saves the settings. Choosing another computer drops a done record that has no scope yet: it described the old one. */
  async save(p: PhotoBackupPrefs): Promise<void> {
    const previousHost = asString(parse(await this.kv.get(K.host)));
    if (previousHost !== null && previousHost !== p.hostId) {
      await this.serial(async () => {
        if ((await this.activeScope()) === null) await this.kv.remove(K.done);
      });
    }
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

  /** Runs [op] after every earlier write to the done record, so a read-modify-write never overlaps a clear or a scope change. */
  private serial<T>(op: () => Promise<T>): Promise<T> {
    const result = this.queue.then(op, op);
    this.queue = result.then(() => undefined, () => undefined);
    return result;
  }
  private queue: Promise<void> = Promise.resolve();

  /** The computer and folder the done record belongs to, or null when none was recorded yet (a record from before scopes). */
  async activeScope(): Promise<DoneScope | null> {
    const v = parse(await this.kv.get(K.doneScope));
    if (typeof v !== 'object' || v === null) return null;
    const { hostId, root } = v as Record<string, unknown>;
    return typeof hostId === 'string' && typeof root === 'string' ? { hostId, root } : null;
  }

  /**
   * Makes [scope] the computer and folder the done record describes. A different computer or folder starts an empty
   * record, because what reached the old one is not on the new one. A record from before scopes existed has no scope: it
   * is kept and taken to belong to the first scope that asks, the one the owner was using.
   */
  enterScope(scope: DoneScope): Promise<void> {
    return this.serial(async () => {
      const next: DoneScope = { hostId: scope.hostId, root: normalRoot(scope.root) };
      const current = await this.activeScope();
      if (current !== null && current.hostId === next.hostId && current.root === next.root) return;
      if (current !== null) await this.kv.remove(K.done);
      await this.kv.set(K.doneScope, JSON.stringify(next));
    });
  }

  /** Photos recorded as backed up for the current scope. */
  async doneIds(): Promise<Set<string>> {
    return new Set(asStrings(parse(await this.kv.get(K.done))));
  }

  /**
   * How many photos count as backed up for the computer [hostId]: zero for no computer or for any computer other than
   * the one the record belongs to. It follows the computer only; a changed destination folder is noticed by the next
   * run. A record from before scopes has no scope yet and belongs to the computer chosen when it was made, which
   * [save] keeps true by dropping it when another computer is chosen.
   */
  async doneCountFor(hostId: string | null): Promise<number> {
    if (hostId === null) return 0;
    const scope = await this.activeScope();
    if (scope !== null && scope.hostId !== hostId) return 0;
    return (await this.doneIds()).size;
  }

  /** Adds [ids] to the done record, but only while [scope] is still the record's scope (a late finish for the old computer is dropped). */
  markDone(scope: DoneScope, ids: readonly string[]): Promise<void> {
    if (ids.length === 0) return Promise.resolve();
    return this.serial(async () => {
      const current = await this.activeScope();
      if (current === null || current.hostId !== scope.hostId || current.root !== normalRoot(scope.root)) return;
      const done = await this.doneIds();
      const before = done.size;
      for (const id of ids) done.add(id);
      if (done.size !== before) await this.kv.set(K.done, JSON.stringify([...done]));
    });
  }

  /** The ids of the uploads the app paused on its own, which a later run may resume (an owner's own pause is never in here). */
  async heldUploads(): Promise<Set<string>> {
    return new Set(asStrings(parse(await this.kv.get(K.held))));
  }

  /** Records that the app paused the uploads [ids]. */
  holdUploads(ids: readonly string[]): Promise<void> {
    if (ids.length === 0) return Promise.resolve();
    return this.serial(async () => {
      const held = await this.heldUploads();
      for (const id of ids) held.add(id);
      await this.kv.set(K.held, JSON.stringify([...held]));
    });
  }

  /** Forgets the uploads [ids] as held, once they were resumed or are gone. */
  releaseUploads(ids: readonly string[]): Promise<void> {
    if (ids.length === 0) return Promise.resolve();
    return this.serial(async () => {
      const held = await this.heldUploads();
      for (const id of ids) held.delete(id);
      if (held.size > 0) await this.kv.set(K.held, JSON.stringify([...held]));
      else await this.kv.remove(K.held);
    });
  }

  clearDone(): Promise<void> {
    return this.serial(() => this.kv.remove(K.done));
  }
}
