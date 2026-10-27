import { type VisibilityPrefs, visibilityPresets } from '../../core/visibility';
import type { DeviceOverrides } from '../../core/settings/settings';

/** Lowercase extension without dots or spaces: ` .JPG ` becomes `jpg`. Empty when nothing usable is left. */
export const normalizeExtension = (raw: string) => raw.trim().replace(/^\.+/, '').toLowerCase().replace(/\s+/g, '');

const withSet = (s: Set<string>, edit: (n: Set<string>) => void) => {
  const next = new Set(s);
  edit(next);
  return next;
};

export const setHideDotfiles = (p: VisibilityPrefs, hide: boolean): VisibilityPrefs => ({ ...p, hideDotfiles: hide });

export const addExtension = (p: VisibilityPrefs, raw: string): VisibilityPrefs => {
  const ext = normalizeExtension(raw);
  return ext === '' ? p : { ...p, hiddenExtensions: withSet(p.hiddenExtensions, (s) => s.add(ext)) };
};

export const removeExtension = (p: VisibilityPrefs, ext: string): VisibilityPrefs => ({ ...p, hiddenExtensions: withSet(p.hiddenExtensions, (s) => s.delete(ext.toLowerCase())) });

/** Exact names match case-insensitively, so adding replaces any differently cased copy. */
export const addName = (p: VisibilityPrefs, name: string): VisibilityPrefs => {
  const trimmed = name.trim();
  return trimmed === '' ? p : { ...p, hiddenNames: withSet(p.hiddenNames, (s) => (removeCased(s, trimmed), s.add(trimmed))) };
};

export const removeName = (p: VisibilityPrefs, name: string): VisibilityPrefs => ({ ...p, hiddenNames: withSet(p.hiddenNames, (s) => removeCased(s, name)) });

function removeCased(s: Set<string>, name: string) {
  const lower = name.toLowerCase();
  for (const n of [...s]) if (n.toLowerCase() === lower) s.delete(n);
}

/** Hidden extensions no preset category accounts for: the ones typed by hand. */
export function customExtensions(p: VisibilityPrefs): string[] {
  const preset = new Set(visibilityPresets.flatMap((x) => [...x.extensions]));
  return [...p.hiddenExtensions].filter((e) => !preset.has(e)).sort();
}

/**
 * Turns a device's own file-visibility rules on (starting as a copy of the current effective rules, so nothing
 * changes until edited) or off (falls back to the app default).
 */
export function withVisibilityOverride(o: DeviceOverrides | undefined, effective: VisibilityPrefs, on: boolean): DeviceOverrides {
  const { visibility: _drop, ...rest } = o ?? {};
  return on ? { ...rest, visibility: { hideDotfiles: effective.hideDotfiles, hiddenExtensions: new Set(effective.hiddenExtensions), hiddenNames: new Set(effective.hiddenNames) } } : rest;
}

