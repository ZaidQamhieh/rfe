export type PaletteItem = { id: string; label: string };

/** Case-insensitive substring filter on the label; an empty query keeps every action in order. */
export function filterPalette<T extends PaletteItem>(items: readonly T[], query: string): T[] {
  const q = query.trim().toLowerCase();
  return q === '' ? [...items] : items.filter((i) => i.label.toLowerCase().includes(q));
}

/** Cleans a typed path for navigation: trims, and returns null when nothing usable is left. */
export function normalizeTypedPath(input: string): string | null {
  const p = input.trim();
  return p === '' ? null : p;
}
