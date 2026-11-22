import type { Entry } from '../../core/api/models';
import { parentDirOf } from '../explorer/paths';

/** Copy within one computer is the agent's own copy; between two computers it passes through the phone (relay). */
export const copyRoute = (srcHostId: string, dstHostId: string): 'same' | 'relay' => (srcHostId === dstHostId ? 'same' : 'relay');

/** Whether the selected entries can be relayed: only files, since a folder has no single download. */
export const relayable = (entries: Entry[], selected: ReadonlySet<string>): boolean => {
  const picked = entries.filter((e) => selected.has(e.path));
  return picked.length > 0 && picked.every((e) => !e.isDir);
};

/** Entries shown in a pane: folders first, then files, each by name. */
export const paneOrder = (entries: Entry[]): Entry[] =>
  [...entries].sort((a, b) => Number(b.isDir) - Number(a.isDir) || a.name.localeCompare(b.name));

/** The folder one level up, or null at the top of the pane's browse root. */
export const paneUp = (path: string, top: string): string | null => (path === top ? null : parentDirOf(path));
