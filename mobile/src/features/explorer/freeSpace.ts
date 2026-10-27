import type { Drive } from '../../core/api/models';

const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();

/** The drive whose mount path is the longest prefix of [path], or undefined when none covers it. */
export function driveFor(drives: readonly Drive[], path: string): Drive | undefined {
  const target = norm(path);
  let best: Drive | undefined;
  let bestLen = -1;
  for (const d of drives) {
    const m = norm(d.path);
    const covers = m === '' || target === m || target.startsWith(`${m}/`);
    if (covers && m.length > bestLen) {
      best = d;
      bestLen = m.length;
    }
  }
  return best;
}

/** How much is needed versus free on the drive holding [destDir], or null when the files fit or the free space is unknown. */
export function uploadSpaceShortfall(drives: readonly Drive[], destDir: string, neededBytes: number): { needed: number; free: number } | null {
  if (neededBytes <= 0) return null;
  const drive = driveFor(drives, destDir);
  const free = drive?.freeBytes;
  // 0 free of 0 total is how the agent reports a drive it could not read.
  if (free === undefined || (free === 0 && !drive?.totalBytes) || neededBytes <= free) return null;
  return { needed: neededBytes, free };
}
