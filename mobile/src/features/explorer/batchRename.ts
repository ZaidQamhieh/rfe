// Pure helpers for renaming a multi-selection; ported from batch_rename.dart.

export type BatchRenameMode = 'pattern' | 'findReplace';

const NUMBER_PLACEHOLDER = '{n}';

/** Splits a name into stem and extension (with its dot). A leading or trailing dot belongs to the stem. */
export function splitNameExt(name: string): { stem: string; ext: string } {
  const dot = name.lastIndexOf('.');
  if (dot <= 0 || dot === name.length - 1) return { stem: name, ext: '' };
  return { stem: name.slice(0, dot), ext: name.slice(dot) };
}

/**
 * New basenames for [names], same length and order. Pattern mode numbers items with zero padding to the
 * width of the largest index and keeps each extension; `{n}` in the base marks where the number goes, otherwise
 * it is appended after a space. Find/replace substitutes every occurrence in the whole name; an empty find is a no-op.
 */
export function computeBatchRenames(o: { names: string[]; mode: BatchRenameMode; base?: string; startNumber?: number; find?: string; replace?: string }): string[] {
  const { names, mode, base = '', startNumber = 1, find = '', replace = '' } = o;
  if (mode === 'findReplace') return find === '' ? [...names] : names.map((n) => n.split(find).join(replace));
  const width = String(startNumber + names.length - 1).length;
  return names.map((name, i) => {
    const number = String(startNumber + i).padStart(width, '0');
    const stem = base.includes(NUMBER_PLACEHOLDER) ? base.split(NUMBER_PLACEHOLDER).join(number) : `${base === '' ? 'file' : base} ${number}`;
    return `${stem}${splitNameExt(name).ext}`;
  });
}
