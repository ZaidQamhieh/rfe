import type { Entry } from '../../core/api/models';
import { formatDate, formatSize } from '../../core/format';

/** Second line of a row: size and date for files, "Folder", its item count and modified date for folders (only what the listing returns). */
export function entryMeta(e: Entry): string {
  const parts: string[] = [];
  if (e.isDir) {
    parts.push('Folder');
    if (e.childCount !== undefined) parts.push(e.childCount >= 1000 ? '1000+ items' : e.childCount === 1 ? '1 item' : `${e.childCount} items`);
  } else {
    const size = formatSize(e.size);
    if (size) parts.push(size);
  }
  if (e.modified) parts.push(formatDate(new Date(e.modified)));
  return parts.join('  ·  ');
}
