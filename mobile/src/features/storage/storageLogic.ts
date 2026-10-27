import type { Drive } from '../../core/api/models';
import { usedFraction } from '../../core/storage/usage';

// Port of explorer/type_treemap_screen.dart (aggregation and buckets) and hosts/storage_insights_screen.dart (ring segments).

export type FileCategory = 'image' | 'video' | 'audio' | 'document' | 'archive' | 'code' | 'other';

const CATEGORY_EXTENSIONS: Record<Exclude<FileCategory, 'other'>, Set<string>> = {
  image: new Set(['.jpg', '.jpeg', '.png', '.gif', '.webp', '.svg', '.bmp', '.ico', '.tiff']),
  video: new Set(['.mp4', '.mkv', '.avi', '.mov', '.wmv', '.flv', '.webm']),
  audio: new Set(['.mp3', '.flac', '.wav', '.aac', '.ogg', '.wma', '.m4a']),
  document: new Set(['.pdf', '.doc', '.docx', '.txt', '.md', '.rtf', '.odt', '.xls', '.xlsx', '.ppt', '.pptx', '.csv']),
  archive: new Set(['.zip', '.tar', '.gz', '.rar', '.7z', '.bz2', '.xz', '.tgz']),
  code: new Set(['.dart', '.go', '.py', '.js', '.ts', '.java', '.kt', '.c', '.cpp', '.h', '.rs', '.rb', '.swift', '.html', '.css', '.json', '.yaml', '.yml', '.xml', '.sh', '.bat', '.sql']),
};

export function categoryFor(ext: string): FileCategory {
  const lower = ext.toLowerCase();
  for (const [cat, set] of Object.entries(CATEGORY_EXTENSIONS)) if (set.has(lower)) return cat as FileCategory;
  return 'other';
}

export const CATEGORY_COLOR: Record<FileCategory, string> = {
  image: '#42A5F5',
  video: '#AB47BC',
  audio: '#66BB6A',
  document: '#FFA726',
  archive: '#8D6E63',
  code: '#26C6DA',
  other: '#9E9E9E',
};

/** Extension with its dot, or '' for names without one (a leading dot alone is a dotfile, not an extension). */
export function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot <= 0 || dot === name.length - 1 ? '' : name.slice(dot);
}

export type TypeAggregation = { sizeByExt: Map<string, number>; countByExt: Map<string, number>; totalSize: number; totalFiles: number };

export function aggregateByExtension(files: readonly { ext: string; size: number }[]): TypeAggregation {
  const sizeByExt = new Map<string, number>();
  const countByExt = new Map<string, number>();
  let totalSize = 0;
  for (const f of files) {
    const ext = f.ext === '' ? '(no ext)' : f.ext.toLowerCase();
    sizeByExt.set(ext, (sizeByExt.get(ext) ?? 0) + f.size);
    countByExt.set(ext, (countByExt.get(ext) ?? 0) + 1);
    totalSize += f.size;
  }
  return { sizeByExt, countByExt, totalSize, totalFiles: files.length };
}

/** Extensions by total size, largest first (ties by name so the order is stable). */
export function sortedBySize(a: TypeAggregation): { ext: string; bytes: number; count: number }[] {
  return [...a.sizeByExt.entries()]
    .map(([ext, bytes]) => ({ ext, bytes, count: a.countByExt.get(ext) ?? 0 }))
    .sort((x, y) => y.bytes - x.bytes || x.ext.localeCompare(y.ext));
}

const BUCKET: Record<FileCategory, string> = {
  image: 'Photos & Video',
  video: 'Photos & Video',
  document: 'Documents',
  code: 'Documents',
  archive: 'Archives',
  audio: 'Other',
  other: 'Other',
};

/** The four visual blocks of the map, biggest first. */
export function bucketBySize(rows: readonly { ext: string; bytes: number }[]): { label: string; bytes: number }[] {
  const sizes = new Map<string, number>();
  for (const r of rows) {
    const label = BUCKET[categoryFor(r.ext)];
    sizes.set(label, (sizes.get(label) ?? 0) + r.bytes);
  }
  return [...sizes.entries()].map(([label, bytes]) => ({ label, bytes })).sort((a, b) => b.bytes - a.bytes);
}

export type RingSegment = { key: string; fraction: number; drive?: Drive };

/** One segment per drive with a usable capacity, then the free remainder. Empty when no drive reports capacity. */
export function ringSegments(drives: readonly Drive[]): RingSegment[] {
  const withCapacity = drives.filter((d) => usedFraction(d) !== null);
  let total = 0;
  let free = 0;
  for (const d of withCapacity) {
    total += d.totalBytes!;
    free += Math.min(d.freeBytes!, d.totalBytes!);
  }
  if (total <= 0) return [];
  return [
    ...withCapacity.map((d) => ({ key: d.path, drive: d, fraction: (d.totalBytes! - Math.min(d.freeBytes!, d.totalBytes!)) / total })),
    { key: 'free', fraction: free / total },
  ];
}
