import type { BatchItemResult } from '../../core/api/models';
import { basenameOf, joinRemotePath, parentDirOf } from './paths';

/** How to reverse a rename or move with the calls the app already has: no agent-side journal, never an overwrite. */
export type UndoOp =
  | { kind: 'rename'; from: string; to: string }
  | { kind: 'move'; groups: { paths: string[]; destDir: string }[] };

/** Renaming [newPath] back to [oldPath]; null when the name did not change. */
export function renameUndo(oldPath: string, newPath: string): UndoOp | null {
  return oldPath === newPath ? null : { kind: 'rename', from: newPath, to: oldPath };
}

/**
 * Moving what moved from [sources] into [destDir] back to the folder each came from. Only items the host reported
 * as moved, and only ones that really changed folder. Only for a plain move: one that kept both or overwrote
 * changed names or removed files, which cannot be put back.
 */
export function moveUndo(sources: readonly string[], destDir: string, results: readonly BatchItemResult[]): UndoOp | null {
  const moved = new Set(results.filter((r) => r.ok).map((r) => r.path));
  const groups = new Map<string, string[]>();
  for (const src of sources) {
    const from = parentDirOf(src);
    if (!moved.has(src) || from === destDir) continue;
    groups.set(from, [...(groups.get(from) ?? []), joinRemotePath(destDir, basenameOf(src))]);
  }
  return groups.size === 0 ? null : { kind: 'move', groups: [...groups].map(([dir, paths]) => ({ paths, destDir: dir })) };
}
