import { Directory, File, Paths } from 'expo-file-system';

import type { AgentClient } from '../../core/api/agentClient';
import type { Host } from '../../core/models/host';
import { transfers, type TransferRecord } from '../../core/native';
import { clientForHost } from '../../services';
import { basenameOf, parentDirOf } from '../explorer/paths';
import { enqueueUploads } from './enqueueUploads';
import { newTransferId } from './transferId';
import { planClashResolution } from './transferSections';
import { cleanUploadName, type Resolution } from './uploadPlan';

/** Listing pages read when looking for free names; a folder with more files than this is treated as having no more names. */
const MAX_LISTING_PAGES = 10;

/** `file://` address of a plain on-device path, each part escaped so spaces, `#` and `%` in a name survive. */
export const fileUriOf =(path: string) => `file://${path.split('/').map(encodeURIComponent).join('/')}`;

/** Every name in the computer's folder [dir], read page by page, so Keep both can choose a name that is free. */
async function namesIn(client: AgentClient, dir: string): Promise<Set<string>> {
  const names = new Set<string>();
  let cursor: string | undefined;
  for (let page = 0; page < MAX_LISTING_PAGES; page++) {
    const l = await client.list(dir, { cursor });
    for (const e of l.entries) names.add(e.name);
    if (!l.nextCursor) break;
    cursor = l.nextCursor;
  }
  return names;
}

/** Records whose choice is being carried out right now, so a double tap does not start a second run. */
const inFlight = new Set<string>();
/** Records whose replacement upload is already queued but whose old row could not be removed yet; a retry only removes. */
const queuedFor = new Set<string>();

/**
 * Carries out the owner's choice for an upload the computer refused because the name was taken. Skip drops the transfer
 * (and its staged copy). Keep both and Replace copy the phone-side file into a fresh staging folder, queue a new upload
 * from it, and only then forget the old one, so a failure at any step leaves the original waiting and nothing lost.
 * A second call for the same record while one runs does nothing, and a retry after the new upload was queued (but the
 * old record could not be removed) only removes the old record instead of queuing another copy.
 * Resolves true when the choice was carried out (the record is gone or going), false when there was nothing to upload and
 * the record still waits for another choice.
 */
export async function resolveClash(host: Host, r: TransferRecord, resolution: Resolution): Promise<boolean> {
  if (inFlight.has(r.id)) return true;
  inFlight.add(r.id);
  try {
    if (resolution === 'skip') {
      await transfers.remove(r.id);
      return true;
    }
    if (!queuedFor.has(r.id)) {
      if (!(await queueReplacement(host, r, resolution))) return false;
      queuedFor.add(r.id);
    }
    await transfers.remove(r.id);
    queuedFor.delete(r.id);
    return true;
  } finally {
    inFlight.delete(r.id);
  }
}

/** Copies the phone-side file aside and queues the new upload; false when the plan says there is nothing to upload. */
async function queueReplacement(host: Host, r: TransferRecord, resolution: Resolution): Promise<boolean> {
  const name = cleanUploadName(basenameOf(r.remotePath));
  const dir = parentDirOf(r.remotePath);
  const client = await clientForHost(host);
  const plan = planClashResolution(name, resolution, await namesIn(client, dir));
  if (!plan) return false;
  const stage = new Directory(Paths.cache, 'clash', newTransferId('u'));
  stage.create({ idempotent: true, intermediates: true });
  try {
    const copy = new File(stage, name);
    await new File(fileUriOf(r.destPath)).copy(copy);
    await enqueueUploads(host, dir, [{ source: { name, uri: copy.uri }, targetName: plan.targetName, overwrite: plan.overwrite }]);
  } finally {
    try {
      // The upload moved the copy out of here; whatever is left (a failed step) goes with the folder.
      stage.delete();
    } catch {
      // Already gone: nothing to clean.
    }
  }
  return true;
}
