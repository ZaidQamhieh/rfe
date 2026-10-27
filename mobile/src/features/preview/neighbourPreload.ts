import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { previewKindOf, shouldPreloadOnCellular } from './previewKind';

/** Cellular means no Wi-Fi or Ethernet is up alongside it (a phone on both is treated as unmetered). */
export const isCellularOnly = (transports: string[]) => !transports.some((t) => t === 'wifi' || t === 'ethernet') && transports.includes('cellular');

/** The images directly before and after [index]: the only pages worth warming, since video and PDF are too heavy to fetch speculatively. */
export function neighbourImages(entries: Entry[], index: number): Entry[] {
  return [index - 1, index + 1].filter((i) => i >= 0 && i < entries.length).map((i) => entries[i]).filter((e) => previewKindOf(e) === 'image');
}

export type PreloadDeps = {
  host: Host;
  entries: Entry[];
  index: number;
  /** Settings > preload previews on cellular. */
  allowCellular: boolean;
  transports: () => Promise<string[]>;
  fetchFile: (host: Host, entry: Entry) => Promise<unknown>;
};

/**
 * Warms the ±1 neighbouring images so the next swipe shows instantly. Skipped on cellular unless the user opted
 * in. Failures are silent: the viewer reports its own error when the page is actually reached.
 */
export async function preloadNeighbours(d: PreloadDeps): Promise<Entry[]> {
  const targets = neighbourImages(d.entries, d.index);
  if (targets.length === 0) return [];
  let cellular = false;
  try {
    cellular = isCellularOnly(await d.transports());
  } catch {
    // Unknown network: preload as usual.
  }
  if (!shouldPreloadOnCellular(cellular, d.allowCellular)) return [];
  await Promise.all(targets.map((e) => d.fetchFile(d.host, e).catch(() => undefined)));
  return targets;
}
