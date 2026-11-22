import type { ShareIntentFile } from 'expo-share-intent';

import { t, type StringKey } from '../../i18n';
import { cleanUploadName, type Picked } from '../transfers/uploadPlan';

/** Files another app shared with us as upload candidates; entries without a usable path are dropped. */
export function sharedFiles(files: readonly Pick<ShareIntentFile, 'path' | 'fileName' | 'size'>[] | null | undefined): Picked[] {
  const out: Picked[] = [];
  for (const f of files ?? []) {
    if (!f.path) continue;
    const uri = f.path.startsWith('/') ? `file://${f.path}` : f.path;
    if (!uri.startsWith('file://')) continue;
    // The sending app chooses this name: keep only a base name (no `../`, separators or control characters).
    const name = cleanUploadName(f.fileName || safeDecode(uri.split('/').pop() ?? '') || 'file');
    out.push({ name, uri, size: f.size ?? undefined });
  }
  return out;
}

/** `decodeURIComponent` that keeps the raw text when it is not valid percent-encoding. */
function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

/** One expiry choice: its length and its words, read from the string table when asked so they follow the language. */
export type ShareExpiryPreset = { readonly seconds: number; readonly labelKey: StringKey; readonly label: string };

/** Builds a choice whose `label` is looked up in the string table each time it is read. */
const expiryPreset = (seconds: number, labelKey: StringKey): ShareExpiryPreset => ({
  seconds,
  labelKey,
  get label() {
    return t(labelKey);
  },
});

const SHARE_EXPIRY_DAY_SECONDS = 24 * 60 * 60;

/** Expiry choices for a new share link, shortest first; 7 days is the most the agent allows. */
export const SHARE_EXPIRY_PRESETS: readonly ShareExpiryPreset[] = [
  expiryPreset(60 * 60, 'shareExpiryHour'),
  expiryPreset(SHARE_EXPIRY_DAY_SECONDS, 'shareExpiryDay'),
  expiryPreset(7 * 24 * 60 * 60, 'shareExpiryWeek'),
];

/** Index of the preset a new share link starts on (1 day). */
export const SHARE_EXPIRY_DEFAULT_INDEX = SHARE_EXPIRY_PRESETS.findIndex((p) => p.seconds === SHARE_EXPIRY_DAY_SECONDS);

/** Index of the shortest expiry choice that covers [remainingSeconds] (the last one when it is longer than all), for showing which length a minted link has. */
export function presetIndexFor(remainingSeconds: number): number {
  const i = SHARE_EXPIRY_PRESETS.findIndex((p) => p.seconds >= remainingSeconds);
  return i < 0 ? SHARE_EXPIRY_PRESETS.length - 1 : i;
}
