// Pure helpers for the Settings page: what a save sends, and how the allowed-folder list changes. Its only import is
// a type, erased when Node runs it directly.
import type { BandwidthSettings } from './api';

/** The limits the user typed; a field is left out when the user did not edit it. */
export interface BandwidthEdit {
  upload?: number;
  download?: number;
}

/**
 * The body of a bandwidth save: the edited limits on top of the agent's current values, so a limit the user never
 * touched keeps whatever it is now (even if another tab changed it). Null when nothing differs from the current values.
 */
export function bandwidthBody(latest: BandwidthSettings, edit: BandwidthEdit): BandwidthSettings | null {
  const body = {
    maxUploadBytesPerSec: edit.upload ?? latest.maxUploadBytesPerSec,
    maxDownloadBytesPerSec: edit.download ?? latest.maxDownloadBytesPerSec,
  };
  const same =
    body.maxUploadBytesPerSec === latest.maxUploadBytesPerSec && body.maxDownloadBytesPerSec === latest.maxDownloadBytesPerSec;
  return same ? null : body;
}

/** A change to the allowed-folder list: add one path or remove one. */
export type RootChange = { add: string } | { remove: string };

/** The folder list after [change], applied to the list as it is now. Adding a folder already listed changes nothing. */
export function applyRootChange(roots: string[], change: RootChange): string[] {
  if ('add' in change) return roots.includes(change.add) ? roots : [...roots, change.add];
  return roots.filter((r) => r !== change.remove);
}
