import { Directory, File, Paths } from 'expo-file-system';

import type { Host } from '../../core/models/host';
import { transfers } from '../../core/native';
import { newTransferId } from './transferId';
import { currentAddress, toPath } from './enqueueUploads';

/**
 * Queues durable native downloads of [paths] into the app's downloads folder (public Downloads via
 * MediaStore is rfe-bvw.7). Uses the route the client last reached the host on.
 */
export async function enqueueDownloads(host: Host, paths: string[]): Promise<string[]> {
  const dir = new Directory(Paths.document, 'downloads');
  dir.create({ idempotent: true, intermediates: true });
  const address = await currentAddress(host);
  const ids: string[] = [];
  for (const p of paths) {
    const name = p.split(/[/\\]/).pop() || 'file';
    // A folder per transfer keeps the file's own name while two downloads of one file never share a partial file.
    const id = newTransferId('d');
    const own = new Directory(dir, id);
    own.create({ idempotent: true, intermediates: true });
    const dest = toPath(new File(own, name).uri);
    await transfers.enqueue(id, host.id, address, p, dest);
    ids.push(id);
  }
  return ids;
}
