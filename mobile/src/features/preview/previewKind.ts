import type { Entry } from '../../core/api/models';

// Port of the kind detection in features/preview/preview.dart. MIME wins; the extension is the fallback.

export type PreviewKind = 'image' | 'pdf' | 'video' | 'audio' | 'markdown' | 'csv' | 'archive' | 'text' | 'none';

const MARKDOWN = new Set(['md', 'markdown']);
const CSV = new Set(['csv']);
const ARCHIVE = new Set(['zip', 'tar', 'gz', 'tgz', 'bz2', '7z', 'rar']);
const TEXT = new Set([
  'txt', 'json', 'yaml', 'yml', 'xml', 'tsv', 'log', 'ini', 'cfg', 'conf', 'toml', 'env',
  'dart', 'go', 'py', 'js', 'jsx', 'ts', 'tsx', 'java', 'kt', 'kts', 'c', 'h', 'cpp', 'hpp', 'cc', 'cs', 'rs', 'rb', 'php', 'swift', 'sh', 'bash', 'zsh',
  'sql', 'gradle', 'properties', 'gitignore', 'dockerfile', 'makefile', 'html', 'htm', 'css', 'scss', 'less', 'vue', 'svelte',
]);
const IMAGE = new Set(['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'heic', 'heif']);
const VIDEO = new Set(['mp4', 'mov', 'mkv', 'avi', 'webm', 'm4v', '3gp']);
const AUDIO = new Set(['mp3', 'm4a', 'aac', 'wav', 'flac', 'ogg', 'oga', 'opus', 'wma', 'aiff', 'aif']);

/** Extension for preview purposes: '' when absent or trailing dot (unlike visibility's extensionOf, a leading-dot name has one). */
export function previewExtension(name: string): string {
  const dot = name.lastIndexOf('.');
  if (dot < 0 || dot === name.length - 1) return '';
  return name.slice(dot + 1).toLowerCase();
}

export function previewKindOf(e: Pick<Entry, 'name' | 'mimeType'>): PreviewKind {
  // Parameters are dropped: Go's mime.TypeByExtension reports text types as `text/markdown; charset=utf-8`.
  const mime = e.mimeType?.split(';')[0].trim().toLowerCase();
  const ext = previewExtension(e.name);
  if (mime) {
    if (mime.startsWith('image/')) return 'image';
    if (mime === 'application/pdf') return 'pdf';
    if (mime.startsWith('video/')) return 'video';
    if (mime.startsWith('audio/')) return 'audio';
    if (mime === 'text/markdown') return 'markdown';
    if (mime === 'text/csv') return 'csv';
    if (mime.startsWith('text/')) return 'text';
    if (mime === 'application/json' || mime === 'application/xml' || mime === 'application/x-yaml' || mime.endsWith('+json') || mime.endsWith('+xml')) return 'text';
  }
  if (IMAGE.has(ext)) return 'image';
  if (ext === 'pdf') return 'pdf';
  if (VIDEO.has(ext)) return 'video';
  if (AUDIO.has(ext)) return 'audio';
  if (MARKDOWN.has(ext)) return 'markdown';
  if (CSV.has(ext)) return 'csv';
  if (ARCHIVE.has(ext)) return 'archive';
  if (TEXT.has(ext)) return 'text';
  return 'none';
}

export const isPreviewable = (e: Pick<Entry, 'name' | 'mimeType' | 'isDir'>) => !e.isDir && previewKindOf(e) !== 'none';

/** Previewable siblings in listing order plus the start index (-1 when the entry is not among them). */
export function previewableSiblings(siblings: Entry[], entry: Pick<Entry, 'path'>): { entries: Entry[]; index: number } {
  const entries = siblings.filter(isPreviewable);
  return { entries, index: entries.findIndex((e) => e.path === entry.path) };
}

/** Preload neighbours on Wi-Fi/ethernet/unknown; on cellular only when the user opted in. */
export const shouldPreloadOnCellular = (isCellular: boolean, settingEnabled: boolean) => !isCellular || settingEnabled;

/** Fallback MIME for "Open with" when the agent reports none. */
export function mimeFromExtension(fileName: string): string {
  const map: Record<string, string> = {
    png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', bmp: 'image/bmp', heic: 'image/heif', heif: 'image/heif',
    mp4: 'video/mp4', mov: 'video/quicktime', mkv: 'video/x-matroska', avi: 'video/x-msvideo', webm: 'video/webm',
    mp3: 'audio/mpeg', aac: 'audio/mp4', m4a: 'audio/mp4', wav: 'audio/wav', flac: 'audio/flac', ogg: 'audio/ogg', oga: 'audio/ogg',
    pdf: 'application/pdf', doc: 'application/msword', docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    xls: 'application/vnd.ms-excel', xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    ppt: 'application/vnd.ms-powerpoint', pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
    zip: 'application/zip', gz: 'application/gzip', tgz: 'application/gzip',
    txt: 'text/plain', log: 'text/plain', md: 'text/plain', csv: 'text/plain', html: 'text/html', htm: 'text/html', json: 'application/json', xml: 'text/xml',
    apk: 'application/vnd.android.package-archive',
  };
  return map[previewExtension(fileName)] ?? 'application/octet-stream';
}
