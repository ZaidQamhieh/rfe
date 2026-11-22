import { Directory, File, Paths } from 'expo-file-system';

import type { Host } from '../../core/models/host';
import { transfers } from '../../core/native';
import { clientForHost } from '../../services';
import { joinRemotePath } from '../explorer/paths';
import { newTransferId } from './transferId';
import { cleanUploadName, type UploadItem } from './uploadPlan';

export const toPath = (uri: string) => {
  const raw = uri.replace('file://', '');
  try {
    return decodeURIComponent(raw);
  } catch {
    // A stray '%' in a name: the raw path is what the file system has.
    return raw;
  }
};

/** The address the client last reached [host] on, falling back to the saved one. */
export const currentAddress = async (host: Host) => (await clientForHost(host)).activeAddress ?? host.address;

/** Some files of a batch were queued before one failed: [queued] are on their way, [remaining] can still be queued, [failure] is what went wrong. */
export class PartialUploadError extends Error {
  constructor(
    readonly queued: number,
    readonly remaining: UploadItem[],
    readonly failure: unknown,
  ) {
    super(failure instanceof Error ? failure.message : String(failure));
    this.name = 'PartialUploadError';
  }
}

/** True while the picked file is still where the picker left it (a failed move leaves it there; a move that went through and was then undone does not). */
function stillThere(item: UploadItem): boolean {
  try {
    return new File(item.source.uri).exists;
  } catch {
    return false;
  }
}

/**
 * Queues durable native uploads of [items] into [destDir]. Each picked file is moved from the picker's cache into
 * app-private storage first so it survives a cleared cache and a restart, and the engine deletes it when the
 * upload is done or cancelled. Returns how many were queued. When a later file fails after earlier ones were queued it
 * throws a [PartialUploadError] naming the files still to do, so a retry does not queue the first ones twice; a failure
 * before anything was queued is thrown as it is.
 */
export async function enqueueUploads(host: Host, destDir: string, items: UploadItem[]): Promise<number> {
  const address = await currentAddress(host);
  let queued = 0;
  for (const [index, item] of items.entries()) {
    const id = newTransferId('u');
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
      if (queued === 0) throw e;
      // The failed file is offered again only when its source survived (a move that went through and then failed to enqueue consumed it).
      throw new PartialUploadError(queued, [...(stillThere(item) ? [item] : []), ...items.slice(index + 1)], e);
    }
    queued++;
  }
  return queued;
}
