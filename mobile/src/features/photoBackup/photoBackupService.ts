import * as Battery from 'expo-battery';
import { Directory, File, Paths } from 'expo-file-system';
import * as MediaLibrary from 'expo-media-library';

import { deviceIdNative, networkTransports, transfers, type TransferRecord } from '../../core/native';
import { t } from '../../i18n';
import { clientForHost, hostStore, keyValue } from '../../services';
import { albumsToScan, assetIdFromTransferId, assetKey, backupRemotePath, backupTransferId, isAlreadyOnHost, isFileStable, PhotoBackupStore, safeExtension, sanitizeSegment, singleFlight, stagingBudget } from './photoBackupLogic';

export const photoBackupStore = new PhotoBackupStore(keyValue);

export type BackupResult =
  | { kind: 'enqueued'; count: number }
  | { kind: 'upToDate' }
  | { kind: 'incomplete'; count: number }
  | { kind: 'disabled' | 'notConfigured' | 'permissionDenied' | 'serverNotConfigured' }
  | { kind: 'skipped'; reason: 'wifi' | 'charging' | 'unreachable' | 'policy'; host?: string };

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

/**
 * One-way photo backup: uploads photos that are not yet recorded as backed up to the folder the computer's owner chose.
 * Each photo is copied into app storage and queued as a durable native upload named `pb-<asset id>`; the asset is
 * recorded as done only when that upload finishes (see [watchPhotoBackup]), so a failed upload is retried next run.
 */
const runPhotoBackupShared = singleFlight(runPhotoBackupOnce);

/**
 * A caller that joins a run already in flight gets that run's result. If that run was a background one that could
 * not ask for photo access and this caller can, it asks now instead of reporting the background run's refusal.
 */
export async function runPhotoBackup(o: { interactive?: boolean } = {}): Promise<BackupResult> {
  const result = await runPhotoBackupShared(o);
  if (result.kind === 'permissionDenied' && (o.interactive ?? true)) return runPhotoBackupShared(o);
  return result;
}

async function runPhotoBackupOnce(o: { interactive?: boolean } = {}): Promise<BackupResult> {
  const prefs = await photoBackupStore.load();
  if (!prefs.enabled) return { kind: 'disabled' };
  if (!prefs.hostId) return { kind: 'notConfigured' };
  if (prefs.wifiOnly) {
    const nets = await networkTransports();
    if (!nets.includes('wifi') && !nets.includes('ethernet')) return { kind: 'skipped', reason: 'wifi' };
  }
  if (prefs.chargingOnly) {
    const s = await Battery.getBatteryStateAsync();
    if (s !== Battery.BatteryState.CHARGING && s !== Battery.BatteryState.FULL) return { kind: 'skipped', reason: 'charging' };
  }
  if (!((o.interactive ?? true) ? await requestPhotoAccess() : await hasPhotoAccess())) return { kind: 'permissionDenied' };

  const host = (await hostStore.listHosts()).find((h) => h.id === prefs.hostId);
  if (!host) return { kind: 'notConfigured' };

  // The destination is the computer's call, read fresh each run so a change there applies to the next backup.
  const client = await clientForHost(host);
  let root: string;
  try {
    const s = await client.status();
    root = s.photoBackupRoot;
    if (!(s.photoBackupConfigured || root !== '')) return { kind: 'serverNotConfigured' };
    if (!(s.photoBackupAvailable ?? root !== '') || root === '') return { kind: 'skipped', reason: 'policy', host: host.label };
  } catch {
    return { kind: 'skipped', reason: 'unreachable', host: host.label };
  }

  const albums = await MediaLibrary.Album.getAll();
  const wanted = new Set(albumsToScan(albums.map((a) => a.id), new Set(prefs.albumIds)));
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
  let copyBudget = stagingBudget(records);
  let count = 0;
  let failed = 0;
  let deferred = false;
  for (const asset of pending) {
    let stagedDir: Directory | null = null;
    try {
      const id = backupTransferId(asset.id);
      const prior = existing.get(id);
      if (prior?.state === 'DONE' || (prior && isAlreadyOnHost(prior))) {
        await photoBackupStore.markDone([assetKey(asset.id)]);
        continue;
      }
      if (prior && prior.state !== 'FAILED' && prior.state !== 'PAUSED' && prior.state !== 'CANCELLED') continue;
      // A failed or paused upload (the app was closed mid-way) keeps its session on the computer and its staged copy
      // here, so it picks up where it stopped. Starting a new session instead would leave the old one open, and the
      // computer allows only four per device.
      if ((prior?.state === 'FAILED' || prior?.state === 'PAUSED') && prior.address === address && new File(`file://${prior.destPath}`).exists) {
        await transfers.resume(id);
        count++;
        continue;
      }
      if (copyBudget <= 0) {
        deferred = true;
        break;
      }
      if (prior) await transfers.remove(id);
      const src = new File(await asset.getUri());
      if (!(await isFileStable(() => src.size))) continue;
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
  return failed > 0 ? { kind: 'incomplete', count: failed } : { kind: 'upToDate' };
}

/** Records finished photo uploads in the backed-up set; runs for the life of the app. Returns the unsubscribe. */
export function watchPhotoBackup(): () => void {
  const note = (records: TransferRecord[]) => {
    const ids = records.filter((r) => (r.state === 'DONE' || isAlreadyOnHost(r)) && r.direction === 'UPLOAD').map((r) => assetIdFromTransferId(r.id)).filter((x): x is string => x !== null);
    if (ids.length > 0) void photoBackupStore.markDone(ids).catch(() => {});
  };
  void transfers.list().then(note).catch(() => {});
  return transfers.subscribe((r) => note([r]));
}
