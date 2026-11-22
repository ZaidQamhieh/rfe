import * as Battery from 'expo-battery';
import { Directory, File, Paths } from 'expo-file-system';
import * as MediaLibrary from 'expo-media-library';

import { AgentApiError, HTTP_UNAUTHORIZED } from '../../core/api/agentClient';
import { ignoreError } from '../../core/log';
import { deviceIdNative, networkTransports, transfers, type TransferRecord } from '../../core/native';
import { t } from '../../i18n';
import { clientForHost, hostStore, keyValue } from '../../services';
import { transferErrorMessage } from '../transfers/transferLogic';
import { albumsToScan, assetIdFromTransferId, assetKey, backupRemotePath, backupTransferId, classifyPrior, isAlreadyOnHost, isFileStable, isInScope, isRefused, PhotoBackupStore, safeExtension, sanitizeSegment, singleFlight, stagingBudget, type DoneScope } from './photoBackupLogic';

export const photoBackupStore = new PhotoBackupStore(keyValue);

export type BackupResult =
  | { kind: 'enqueued'; count: number }
  | { kind: 'upToDate' }
  | { kind: 'incomplete'; count: number }
  /** Nothing new started, but [count] photos are still queued or uploading. */
  | { kind: 'waiting'; count: number }
  /** [count] photos were refused by the computer; [reason] is the plain-language cause. */
  | { kind: 'refused'; count: number; reason: string }
  | { kind: 'disabled' | 'notConfigured' | 'permissionDenied' | 'serverNotConfigured' | 'albumsGone' }
  | { kind: 'skipped'; reason: 'wifi' | 'charging' | 'unreachable' | 'refused' | 'policy'; host?: string };

export type AlbumInfo = { id: string; title: string; count: number };

const imagesIn = (album: MediaLibrary.Album) => new MediaLibrary.Query().album(album).eq(MediaLibrary.AssetField.MEDIA_TYPE, MediaLibrary.MediaType.IMAGE).exe();

export async function requestPhotoAccess(): Promise<boolean> {
  const p = await MediaLibrary.requestPermissionsAsync(false, ['photo']);
  return p.granted;
}

/** The current photo permission without asking; a scheduled run has no screen to ask on. */
async function hasPhotoAccess(): Promise<boolean> {
  return (await MediaLibrary.getPermissionsAsync(false, ['photo'])).granted;
}

/** Device albums that hold at least one photo. */
export async function listAlbums(): Promise<AlbumInfo[]> {
  const out: AlbumInfo[] = [];
  for (const album of await MediaLibrary.Album.getAll()) {
    try {
      const count = (await imagesIn(album)).length;
      if (count === 0) continue;
      // Files at the storage root sit in a bucket with no name, which getTitle rejects.
      const title = await album.getTitle().catch(() => t('albumUnnamed'));
      out.push({ id: album.id, title, count });
    } catch {
      // An album that vanished while listing is left out.
    }
  }
  return out.sort((a, b) => a.title.localeCompare(b.title));
}

const toPath = (uri: string) => decodeURIComponent(uri.replace('file://', ''));

/** Runs one backup and records whether it was allowed to ask for photo access, so a caller that joins it can tell. */
const runPhotoBackupShared = singleFlight(async (o: { interactive?: boolean } = {}) => ({ result: await runPhotoBackupOnce(o), interactive: o.interactive ?? true }));

/**
 * A caller that joins a run already in flight gets that run's result. If that run was a background one that could
 * not ask for photo access and this caller can, it asks now instead of reporting the background run's refusal.
 */
export async function runPhotoBackup(o: { interactive?: boolean } = {}): Promise<BackupResult> {
  const { result, interactive: ranInteractive } = await runPhotoBackupShared(o);
  // Retry only a background run's refusal: an interactive run's denial is the owner's answer to the dialog just shown.
  if (result.kind === 'permissionDenied' && !ranInteractive && (o.interactive ?? true)) return (await runPhotoBackupShared(o)).result;
  return result;
}

/** The owner's Wi-Fi-only or charging-only choice that does not hold right now, or null when the backup may use the network. */
async function unmetConstraint(prefs: { wifiOnly: boolean; chargingOnly: boolean }): Promise<'wifi' | 'charging' | null> {
  if (prefs.wifiOnly) {
    const nets = await networkTransports();
    if (!nets.includes('wifi') && !nets.includes('ethernet')) return 'wifi';
  }
  if (prefs.chargingOnly) {
    const s = await Battery.getBatteryStateAsync();
    if (s !== Battery.BatteryState.CHARGING && s !== Battery.BatteryState.FULL) return 'charging';
  }
  return null;
}

const isBackupUpload = (r: TransferRecord) => r.direction === 'UPLOAD' && assetIdFromTransferId(r.id) !== null;

/**
 * Backups the app paused because a constraint stopped holding, so the next run that finds it holding again lets them
 * go on. A backup the owner paused by hand is not in the held set and stays paused. Returns the ids it resumed.
 */
async function resumeHeldBackups(): Promise<Set<string>> {
  const held = await photoBackupStore.heldUploads();
  const resumed = new Set<string>();
  if (held.size === 0) return resumed;
  for (const r of await transfers.list()) {
    if (!isBackupUpload(r) || r.state !== 'PAUSED' || !held.has(r.id)) continue;
    // Released before the resume so a system pause that follows it can hold the upload again and keep that hold.
    await photoBackupStore.releaseUploads([r.id]);
    await transfers.resume(r.id);
    resumed.add(r.id);
  }
  // The rest are running again (the owner resumed them) or gone. One that is paused now was re-held by a system pause
  // during the loop above and stays held.
  const stillPaused = new Set((await transfers.list()).filter((r) => r.state === 'PAUSED').map((r) => r.id));
  await photoBackupStore.releaseUploads([...held].filter((id) => !resumed.has(id) && !stillPaused.has(id)));
  return resumed;
}

const CONSTRAINT_RECHECK_MS = 15_000;

/**
 * One-way photo backup: uploads photos that are not yet recorded as backed up to the folder the computer's owner chose.
 * Each photo is copied into app storage and queued as a durable native upload named `pb-<asset id>`; the asset is
 * recorded as done only when that upload finishes (see [watchPhotoBackup]), so a failed upload is retried next run.
 */
async function runPhotoBackupOnce(o: { interactive?: boolean } = {}): Promise<BackupResult> {
  const prefs = await photoBackupStore.load();
  if (!prefs.enabled) return { kind: 'disabled' };
  if (!prefs.hostId) return { kind: 'notConfigured' };
  const unmet = await unmetConstraint(prefs);
  if (unmet) return { kind: 'skipped', reason: unmet };
  const resumed = await resumeHeldBackups();
  if (!((o.interactive ?? true) ? await requestPhotoAccess() : await hasPhotoAccess())) return { kind: 'permissionDenied' };

  const host = (await hostStore.listHosts()).find((h) => h.id === prefs.hostId);
  if (!host) return { kind: 'notConfigured' };

  // The destination is the computer's call, read fresh each run so a change there applies to the next backup.
  const client = await clientForHost(host);
  let root: string;
  try {
    const s = await client.status();
    root = s.photoBackupRoot;
    // A status without the field at all (an older or odd computer) counts as no folder, like an empty one.
    const hasRoot = typeof root === 'string' && root !== '';
    if (!(s.photoBackupConfigured || hasRoot)) return { kind: 'serverNotConfigured' };
    if (!(s.photoBackupAvailable ?? hasRoot) || !hasRoot) return { kind: 'skipped', reason: 'policy', host: host.label };
  } catch (e) {
    // A 401 means the computer answered and no longer accepts this phone: that is not "could not reach".
    const refused = e instanceof AgentApiError && e.statusCode === HTTP_UNAUTHORIZED;
    return { kind: 'skipped', reason: refused ? 'refused' : 'unreachable', host: host.label };
  }

  // What was backed up belongs to one computer and folder: a different one starts from nothing.
  const scope: DoneScope = { hostId: host.id, root };
  await photoBackupStore.enterScope(scope);

  const albums = await MediaLibrary.Album.getAll();
  const wanted = new Set(albumsToScan(albums.map((a) => a.id), new Set(prefs.albumIds)));
  // Albums were chosen but none exists any more: that is not "nothing to do".
  if (prefs.albumIds.length > 0 && wanted.size === 0) return { kind: 'albumsGone' };
  const seen = new Set<string>();
  const assets: MediaLibrary.Asset[] = [];
  for (const album of albums) {
    if (!wanted.has(album.id)) continue;
    for (const a of await imagesIn(album)) {
      if (seen.has(a.id)) continue;
      seen.add(a.id);
      assets.push(a);
    }
  }

  const done = await photoBackupStore.doneIds();
  const pending = assets.filter((a) => !done.has(assetKey(a.id)));
  if (pending.length === 0) return { kind: 'upToDate' };

  const nickname = prefs.deviceName?.trim();
  const segment = nickname ? sanitizeSegment(nickname) : ((await deviceIdNative())?.slice(0, 8) ?? 'phone');
  const address = client.activeAddress ?? host.address;
  const records = await transfers.list();
  const existing = new Map(records.map((r) => [r.id, r]));
  // Each queued photo keeps a private copy until it uploads: copy only as many as the cap allows, the next run takes the rest.
  // Only uploads to this computer and folder take a place: a record for another target is replaced, not added to.
  let copyBudget = stagingBudget(records.filter((r) => isInScope(r, scope)));
  let count = 0;
  let waiting = 0;
  let refused = 0;
  let refusal: string | null = null;
  let failed = 0;
  let notReady = 0;
  let deferred = false;
  for (const asset of pending) {
    let stagedDir: Directory | null = null;
    try {
      const id = backupTransferId(asset.id);
      const prior = existing.get(id);
      // A failed or paused upload (the app was closed mid-way) keeps its session on the computer and its staged copy
      // here, so it picks up where it stopped. Starting a new session instead would leave the old one open, and the
      // computer allows only four per device.
      const action = classifyPrior(prior, { hostId: host.id, address, root }, () => new File(`file://${prior?.destPath}`).exists);
      if (action === 'done') {
        await photoBackupStore.markDone(scope, [assetKey(asset.id)]);
        continue;
      }
      if (action === 'waiting') {
        waiting++;
        continue;
      }
      // Still uploading to another computer or folder: it cannot be replaced yet, so this photo waits for the next run.
      if (action === 'busy') {
        notReady++;
        continue;
      }
      if (action === 'refused') {
        refused++;
        refusal ??= prior?.error ?? null;
        continue;
      }
      if (action === 'resume') {
        // A paused upload the app did not pause is the owner's own pause: it waits for the owner.
        if (prior?.state === 'PAUSED' && !resumed.has(id)) {
          waiting++;
          continue;
        }
        if (!resumed.has(id)) await transfers.resume(id);
        count++;
        continue;
      }
      if (copyBudget <= 0) {
        deferred = true;
        break;
      }
      if (prior) await transfers.remove(id);
      const src = new File(await asset.getUri());
      // A file still being written is not copied yet; it counts as not ready so the run does not claim it is up to date.
      if (!(await isFileStable(() => src.size))) {
        notReady++;
        continue;
      }
      const filename = await asset.getFilename();
      const name = `${assetKey(asset.id)}${safeExtension(filename)}`;
      const created = new Date((await asset.getCreationTime()) ?? Date.now());
      const dir = new Directory(Paths.document, 'uploads', id);
      dir.create({ idempotent: true, intermediates: true });
      stagedDir = dir;
      const staged = new File(dir, name);
      await src.copy(staged);
      await transfers.enqueueUpload(id, host.id, address, toPath(staged.uri), backupRemotePath(root, created, name, segment), false, true);
      copyBudget--;
      count++;
    } catch {
      // An asset that cannot be read or copied (removed meanwhile, limited access, storage full) is skipped and retried next run.
      // Its half-made staged copy goes too, or the next run's copy would collide with it and the space would leak.
      try {
        stagedDir?.delete();
      } catch {
        // Already gone.
      }
      failed++;
    }
  }
  if (count > 0) return { kind: 'enqueued', count };
  // Photos are waiting behind a full staging area: that is not "up to date".
  if (deferred) return { kind: 'incomplete', count: pending.length };
  if (failed + notReady > 0) return { kind: 'incomplete', count: failed + notReady };
  // Up to date only when every photo has arrived: refused or still-uploading ones are reported as what they are.
  if (refused > 0) return { kind: 'refused', count: refused, reason: transferErrorMessage(refusal) };
  if (waiting > 0) return { kind: 'waiting', count: waiting };
  return { kind: 'upToDate' };
}

/**
 * Records finished photo uploads in the backed-up set; runs for the life of the app. Only uploads to the computer and
 * folder the set describes count; the next run sorts out the rest. Returns the unsubscribe.
 */
export function watchPhotoBackup(): () => void {
  const note = async (records: TransferRecord[]) => {
    const scope = await photoBackupStore.activeScope();
    if (scope === null) return;
    const ids = records
      .filter((r) => (r.state === 'DONE' || isAlreadyOnHost(r)) && r.direction === 'UPLOAD' && isInScope(r, scope))
      .map((r) => assetIdFromTransferId(r.id))
      .filter((x): x is string => x !== null);
    await photoBackupStore.markDone(scope, ids);
  };
  // Where each backup upload stands with the app's own pause. 'armed': the app has just paused it and the engine has not
  // confirmed yet, so events still arriving are leftovers. 'paused': a PAUSED state was seen, so a later sign of life
  // from it means the owner resumed it by hand.
  const phase = new Map<string, 'armed' | 'paused'>();
  // A counter that only grows, and the tick at which each upload was last touched by a live event or by the app's own
  // pause. A startup snapshot is older than anything touched after it began, so it loses to those.
  let tick = 0;
  const touchedAt = new Map<string, number>();
  const touch = (id: string) => touchedAt.set(id, ++tick);
  const snapshotTick = tick;
  void transfers
    .list()
    .then((records) => {
      for (const r of records) {
        if (r.state === 'PAUSED' && isBackupUpload(r) && (touchedAt.get(r.id) ?? 0) <= snapshotTick) phase.set(r.id, 'paused');
      }
      return note(records);
    })
    .catch(ignoreError('photo backup: transfer list'));
  /**
   * Keeps the held set to pauses the app made and nobody has undone: an upload the owner resumed by hand is no longer
   * the app's to resume, so a pause the owner makes afterwards stays paused. Only a state seen after a PAUSED one counts,
   * so the events that still arrive while the app's own pause is taking effect are ignored, and every new pause by the
   * app starts that wait over (see [enforce]).
   */
  const trackOwnerResume = (r: TransferRecord) => {
    if (!isBackupUpload(r)) return;
    touch(r.id);
    if (r.state === 'PAUSED') {
      phase.set(r.id, 'paused');
    } else if (r.state === 'RUNNING' || r.state === 'QUEUED') {
      if (phase.get(r.id) !== 'paused') return;
      phase.delete(r.id);
      void photoBackupStore.releaseUploads([r.id]).catch(ignoreError('photo backup: release held upload'));
    } else {
      phase.delete(r.id);
    }
  };
  let lastCheck = 0;
  // Uploads already queued keep going after the run that queued them: pause them when Wi-Fi or charging stops holding.
  const enforce = async (r: TransferRecord) => {
    if (r.state !== 'RUNNING' || !isBackupUpload(r)) return;
    const now = Date.now();
    if (now - lastCheck < CONSTRAINT_RECHECK_MS) return;
    lastCheck = now;
    if (!(await unmetConstraint(await photoBackupStore.load()))) return;
    const running = (await transfers.list()).filter((x) => x.state === 'RUNNING' && isBackupUpload(x));
    // A new pause by the app: whatever was seen of these uploads before (a pause the owner has since undone, a startup
    // snapshot) no longer counts, so the owner's resume is only recognised after the engine confirms this pause.
    for (const x of running) {
      phase.set(x.id, 'armed');
      touch(x.id);
    }
    // Written down first, so a later run knows these pauses are the app's to undo.
    await photoBackupStore.holdUploads(running.map((x) => x.id));
    for (const x of running) await transfers.pause(x.id);
  };
  return transfers.subscribe((r) => {
    trackOwnerResume(r);
    void note([r]).catch(ignoreError('photo backup: mark done'));
    void enforce(r).catch(ignoreError('photo backup: constraint recheck'));
  });
}

/**
 * Forgets what was backed up so everything is uploaded again. The finished upload records go too: they stay in the transfer
 * list, and [watchPhotoBackup] and the enqueue loop would otherwise mark those photos done again without uploading them.
 * Refused uploads go as well: runs leave them alone, so this is how the owner has them sent again after fixing the cause.
 */
export async function resetPhotoBackupRecord(): Promise<void> {
  const records = await transfers.list();
  for (const r of records) {
    if (r.direction !== 'UPLOAD' || assetIdFromTransferId(r.id) === null) continue;
    if (r.state === 'DONE' || isAlreadyOnHost(r) || isRefused(r)) await transfers.remove(r.id);
  }
  await photoBackupStore.clearDone();
}
