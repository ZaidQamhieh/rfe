import type { Entry } from './api/models';
import { archiveExtensions, audioExtensions, docExtensions, imageExtensions, videoExtensions } from './entryCategory';

export type VisibilityPrefs = { hideDotfiles: boolean; hiddenExtensions: Set<string>; hiddenNames: Set<string> };

export const defaultVisibility = (): VisibilityPrefs => ({ hideDotfiles: true, hiddenExtensions: new Set(), hiddenNames: new Set() });

export type VisibilityPreset = { label: string; extensions: Set<string>; names: Set<string> };
const preset = (label: string, extensions: Iterable<string> = [], names: Iterable<string> = []): VisibilityPreset => ({ label, extensions: new Set(extensions), names: new Set(names) });

export const visibilityPresets: VisibilityPreset[] = [
  preset('System junk', ['tmp', 'bak', 'swp', 'lock', 'ini'], ['.DS_Store', 'Thumbs.db', 'desktop.ini']),
  preset('Logs', ['log', 'old']),
  preset('Archives', archiveExtensions),
  preset('Audio', audioExtensions),
  preset('Images', imageExtensions),
  preset('Videos', videoExtensions),
  preset('Docs', docExtensions),
];

/** Lowercase extension without the dot; '' for no extension, trailing dot, or a dotfile like `.bashrc`. */
export function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.');
  if (dot <= 0 || dot === name.length - 1) return '';
  return name.slice(dot + 1).toLowerCase();
}

export const isDotfile = (name: string) => name.startsWith('.');

export function isEntryHidden(e: Pick<Entry, 'name' | 'isDir'>, p: VisibilityPrefs): boolean {
  if (p.hideDotfiles && isDotfile(e.name)) return true;
  if (!e.isDir) {
    const ext = extensionOf(e.name);
    if (ext && [...p.hiddenExtensions].some((x) => x.toLowerCase() === ext)) return true;
  }
  const lower = e.name.toLowerCase();
  return [...p.hiddenNames].some((n) => n.toLowerCase() === lower);
}

/** The destination picker only shows folders, so only the dotfolder rule applies. */
export const isEntryHiddenInPicker = (e: Pick<Entry, 'name'>, p: VisibilityPrefs) => p.hideDotfiles && isDotfile(e.name);
