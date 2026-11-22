import { dedupedName } from '../explorer/paths';

export type Picked = { name: string; uri: string; size?: number };
export type UploadItem = { source: Picked; targetName: string; overwrite: boolean };
export type Resolution = 'keepBoth' | 'overwrite' | 'skip';

/** A picked file name that cannot climb out of the destination folder. */
export function cleanUploadName(name: string): string {
  const base = name.split(/[/\\]/).pop() ?? '';
  const cleaned = base.replace(/[\u0000-\u001f]/g, '').replace(/^\.+$/, '').trim();
  return cleaned === '' ? 'file' : cleaned.slice(-200);
}

/**
 * Decides the host-side name and overwrite flag for each picked file. Names that already exist in the folder follow
 * [resolution]; names repeated within the pick are always kept apart so two picks never race for one target (only the first
 * of several picks with an overwritten name replaces it).
 */
export function planUploads(picked: Picked[], colliding: ReadonlySet<string>, resolution: Resolution): UploadItem[] {
  const taken = new Set<string>(colliding);
  // Colliding names already given to one pick for overwriting: a later pick of the same name must not overwrite it too.
  const overwritten = new Set<string>();
  const out: UploadItem[] = [];
  for (const source of picked) {
    const name = cleanUploadName(source.name);
    const exists = colliding.has(name);
    if (exists && resolution === 'skip') continue;
    if (exists && resolution === 'overwrite' && !overwritten.has(name)) {
      overwritten.add(name);
      out.push({ source, targetName: name, overwrite: true });
      continue;
    }
    // Not colliding with the folder, or keep both: pick a free name, counting names already handed out.
    const targetName = exists || taken.has(name) ? dedupedName(name, taken) : name;
    taken.add(targetName);
    out.push({ source, targetName, overwrite: false });
  }
  return out;
}
