import { Directory, File, Paths } from 'expo-file-system';

import type { Host } from '../../core/models/host';
import { transfers } from '../../core/native';
import { clientForHost } from '../../services';

/** Local, non-secret id for a transfer journal entry. */
const newTransferId = () => `d${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;

/**
 * Queues durable native downloads of [paths] into the app's downloads folder (public Downloads via
 * MediaStore is rfe-bvw.7). Uses the route the client last reached the host on.
 */
export async function enqueueDownloads(host: Host, paths: string[]): Promise<void> {
  const dir = new Directory(Paths.document, 'downloads');
  dir.create({ idempotent: true, intermediates: true });
  const address = (await clientForHost(host)).activeAddress ?? host.address;
  for (const p of paths) {
    const name = p.split(/[/\\]/).pop() || 'file';
    // A folder per transfer keeps the file's own name while two downloads of one file never share a partial file.
    const id = newTransferId();
    const own = new Directory(dir, id);
    own.create({ idempotent: true, intermediates: true });
    const rawDest = new File(own, name).uri.replace('file://', '');
    let dest = rawDest;
    try {
      dest = decodeURIComponent(rawDest);
    } catch {
      // A stray '%' in the name: use the path as given.
    }
    await transfers.enqueue(id, host.id, address, p, dest);
  }
}
