import type { Pin } from '../../core/storage/collections';
import { basenameOf } from '../explorer/paths';

/** One folder kept for offline use, ready to draw: its name, and which computer it came from (empty if that one is gone). */
export type KeptFolder = { key: string; name: string; computer: string };

/** The kept folders as rows, named by their last path part (the whole path for a root), in the order they were kept. */
export function keptFolders(pins: readonly Pin[], computerNames: ReadonlyMap<string, string>): KeptFolder[] {
  return pins.map((p) => ({
    key: `${p.hostId}:${p.remotePath}`,
    name: basenameOf(p.remotePath) || p.remotePath,
    computer: computerNames.get(p.hostId) ?? '',
  }));
}
