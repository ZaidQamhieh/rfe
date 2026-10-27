import { Directory, File, Paths } from 'expo-file-system';

import type { Host } from '../../core/models/host';
import { transfers } from '../../core/native';
import { clientForHost } from '../../services';
import { joinRemotePath } from '../explorer/paths';
import { cleanUploadName, type UploadItem } from './uploadPlan';

const newTransferId = () => `u${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

const toPath = (uri: string) => {
  const raw = uri.replace('file://', '');
  try {
    return decodeURIComponent(raw);
  } catch {
    // A stray '%' in a name: the raw path is what the file system has.
    return raw;
  }
};

/**
 * Queues durable native uploads of [items] into [destDir]. Each picked file is moved from the picker's cache into
 * app-private storage first so it survives a cleared cache and a restart, and the engine deletes it when the
 * upload is done or cancelled. Returns how many were queued.
 */
export async function enqueueUploads(host: Host, destDir: string, items: UploadItem[]): Promise<number> {
  const address = (await clientForHost(host)).activeAddress ?? host.address;
  let queued = 0;
  for (const item of items) {
    const id = newTransferId();
    const dir = new Directory(Paths.document, 'uploads', id);
    dir.create({ idempotent: true, intermediates: true });
    // Planned names are already clean; clean again so a caller that skips the plan cannot leave the staging
    // directory or the chosen remote folder.
    const targetName = cleanUploadName(item.targetName);
    const staged = new File(dir, targetName);
    try {
      await new File(item.source.uri).move(staged);
      await transfers.enqueueUpload(id, host.id, address, toPath(staged.uri), joinRemotePath(destDir, targetName), item.overwrite, true);
    } catch (e) {
      // A full phone (or any failed move/enqueue) must not leave a half-copied file behind in app storage.
      try {
        dir.delete();
      } catch {
        // Already gone.
      }
      throw e;
    }
    queued++;
  }
  return queued;
}
