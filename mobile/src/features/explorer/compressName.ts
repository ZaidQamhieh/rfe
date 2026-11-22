import { splitNameExt } from './batchRename';

/** The extension of the archive Compress makes. */
const ZIP_SUFFIX = '.zip';
const ZIP_SUFFIX_RE = /\.zip$/i;
/** Default archive name when the selection is in a filesystem root and holds several items. */
export const ROOT_ARCHIVE_STEM = 'Archive';

/** The name with `.zip` added once: a name that already ends in `.zip` (any case) is kept as typed. */
export function zipFileName(name: string): string {
  return ZIP_SUFFIX_RE.test(name) ? name : `${name}${ZIP_SUFFIX}`;
}

/**
 * The name the prompt starts with: one item gives its own name (a file without its extension, a folder whole);
 * several items give the folder they sit in, or [ROOT_ARCHIVE_STEM] when that folder is a filesystem root (null).
 */
export function compressDefaultStem(single: { name: string; isDir: boolean } | null, folderName: string | null): string {
  if (single) return single.isDir ? single.name : splitNameExt(single.name).stem;
  return folderName ?? ROOT_ARCHIVE_STEM;
}

/**
 * [zipName] if no name in [taken] matches it (compared without case, as hosts may be case-insensitive), else
 * "stem (n).zip" with the first n that is free, so an existing archive or folder is never replaced.
 */
export function freeZipName(zipName: string, taken: Iterable<string>): string {
  const used = new Set([...taken].map((n) => n.toLowerCase()));
  if (!used.has(zipName.toLowerCase())) return zipName;
  const { stem, ext } = splitNameExt(zipName);
  for (let n = 1; ; n++) {
    const candidate = `${stem} (${n})${ext}`;
    if (!used.has(candidate.toLowerCase())) return candidate;
  }
}
