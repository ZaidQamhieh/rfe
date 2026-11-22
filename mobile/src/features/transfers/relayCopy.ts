import type { Host } from '../../core/models/host';
import { transfers } from '../../core/native';
import { clientForHost, hostStore, keyValue } from '../../services';
import { joinRemotePath } from '../explorer/paths';
import { enqueueDownloads } from './enqueueDownloads';
import { currentAddress } from './enqueueUploads';
import { newTransferId } from './transferId';
import { parseJobs, type RelayJob } from './relayPlan';

const KEY = 'relay.jobs';

export const loadRelayJobs = async (): Promise<RelayJob[]> => parseJobs(await keyValue.get(KEY));
const saveRelayJobs = (jobs: RelayJob[]) => (jobs.length ? keyValue.set(KEY, JSON.stringify(jobs)) : keyValue.remove(KEY));

/** Waits for the change before it, whether that succeeded or failed; never rejects. */
let lastUpdate: Promise<void> = Promise.resolve();

/**
 * Changes the saved job list: reads it fresh, applies [change] and saves, one change at a time. Every writer goes
 * through here, so a slow caller cannot save an old copy of the list over a job another one added meanwhile.
 */
export function updateRelayJobs(change: (jobs: RelayJob[]) => RelayJob[]): Promise<void> {
  const result = lastUpdate.then(async () => saveRelayJobs(change(await loadRelayJobs())));
  lastUpdate = result.then(
    () => undefined,
    () => undefined,
  );
  return result;
}

/**
 * Copies files from [source] to [dest] through this phone with the engines that already exist: each file is downloaded
 * from the source, and when that finishes (see useRelayCopy) uploaded to [destDir] on the destination. No computer
 * ever holds the other's credentials. Returns how many files were queued.
 */
export async function startRelayCopy(source: Host, paths: string[], dest: Host, destDir: string): Promise<number> {
  const ids = await enqueueDownloads(source, paths);
  const added = ids.map((downloadId, i): RelayJob => ({ downloadId, destHostId: dest.id, destDir, name: paths[i].split(/[/\\]/).pop() || 'file' }));
  await updateRelayJobs((jobs) => [...jobs, ...added]);
  return ids.length;
}

/** A folder to open the destination browser in: the first shared folder, else the first drive, else the filesystem root. */
export async function destinationStart(host: Host): Promise<string> {
  const client = await clientForHost(host);
  const status = await client.status();
  if (status.roots.length > 0) return status.roots[0];
  const drives = await client.drives().catch(() => []);
  return drives[0]?.path ?? '/';
}

/** Queues the upload for a finished relay download, then lets go of the download record (the upload deletes the file). */
export async function finishRelay(job: RelayJob, localPath: string): Promise<void> {
  const dest = (await hostStore.listHosts()).find((h) => h.id === job.destHostId);
  if (!dest) {
    await transfers.remove(job.downloadId);
    return;
  }
  await transfers.enqueueUpload(newTransferId('u'), dest.id, await currentAddress(dest), localPath, joinRemotePath(job.destDir, job.name), false, true);
  await transfers.remove(job.downloadId);
}
