import type { Entry } from './api/models';

// Port of core/ui/entry_leading.dart (category resolution + extension tables).
export type EntryCategory = 'folder' | 'image' | 'video' | 'audio' | 'document' | 'archive' | 'other';

export const imageExtensions = new Set(['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'heic', 'heif', 'svg', 'tiff', 'ico']);
export const videoExtensions = new Set(['mp4', 'mov', 'mkv', 'avi', 'webm', 'm4v', '3gp', 'flv', 'wmv']);
export const audioExtensions = new Set(['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a', 'wma', 'opus']);
export const archiveExtensions = new Set(['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'tgz']);
export const docExtensions = new Set(['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp', 'txt', 'md', 'rtf', 'csv']);

/** Category from isDir + MIME prefix, same rules as the explorer/search icon selection. */
export function categoryOf(e: Pick<Entry, 'isDir' | 'mimeType'>): EntryCategory {
  if (e.isDir) return 'folder';
  const mime = e.mimeType ?? '';
  if (mime.startsWith('image/')) return 'image';
  if (mime.startsWith('video/')) return 'video';
  if (mime.startsWith('audio/')) return 'audio';
  if (mime.includes('pdf')) return 'document';
  if (mime.includes('zip') || mime.includes('archive')) return 'archive';
  if (mime.startsWith('text/') || mime.includes('json')) return 'document';
  return 'other';
}

/** Icon tint/background per category (mockup file-type palette). `bg` is the tonal chip behind the glyph. */
export type IconKind = { icon: 'folder' | 'image' | 'video' | 'music' | 'fileArchive' | 'fileText' | 'file'; color: string | null; bg: string | null };

export function iconKindFor(e: Pick<Entry, 'isDir' | 'mimeType'>): IconKind {
  const FOLDER = { color: '#4C8DFF', bg: 'rgba(76,141,255,0.14)' };
  const VIOLET = { color: '#9B87F5', bg: 'rgba(155,135,245,0.14)' };
  const RED = { color: '#F1596B', bg: 'rgba(241,89,107,0.14)' };
  const AMBER = { color: '#F3A73F', bg: 'rgba(243,167,63,0.14)' };
  const mime = e.mimeType ?? '';
  switch (categoryOf(e)) {
    case 'folder':
      return { icon: 'folder', ...FOLDER };
    case 'image':
      return { icon: 'image', ...VIOLET };
    case 'video':
      return { icon: 'video', ...VIOLET };
    case 'audio':
      return { icon: 'music', color: null, bg: null };
    case 'archive':
      return { icon: 'fileArchive', ...AMBER };
    case 'document':
      if (mime.includes('pdf')) return { icon: 'fileText', ...RED };
      if (mime.startsWith('text/')) return { icon: 'fileText', color: null, bg: null };
      return { icon: 'fileText', ...FOLDER };
    default:
      return { icon: 'file', color: null, bg: null };
  }
}

/** Colour role of an entry (design/tokens `Roles`); `null` means neutral, unknown types are never coloured. */
export type EntryRole = 'folder' | 'doc' | 'photo' | 'route' | 'warn';

export function roleOf(e: Pick<Entry, 'isDir' | 'mimeType'> & { name?: string }): EntryRole | null {
  switch (categoryOf(e)) {
    case 'folder':
      return 'folder';
    case 'image':
    case 'video':
      return 'photo';
    case 'audio':
      return 'route';
    case 'document':
      return 'doc';
    case 'archive':
      return 'warn';
  }
  const ext = (e.name ?? '').split('.').pop()?.toLowerCase() ?? '';
  if (imageExtensions.has(ext) || videoExtensions.has(ext)) return 'photo';
  if (audioExtensions.has(ext)) return 'route';
  if (docExtensions.has(ext)) return 'doc';
  if (archiveExtensions.has(ext)) return 'warn';
  return null;
}
