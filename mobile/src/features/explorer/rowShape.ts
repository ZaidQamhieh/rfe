/** Where a listing row sits in the Lumen card layout: a folder is its own card, consecutive files share one (`.filelist`). */
export type RowShape = { first: boolean; last: boolean };

export function rowShape(entries: readonly { isDir: boolean }[], i: number): RowShape {
  const e = entries[i];
  if (!e || e.isDir) return { first: true, last: true };
  const prev = entries[i - 1];
  const next = entries[i + 1];
  return { first: !prev || prev.isDir, last: !next || next.isDir };
}
