import type { Entry } from '../../core/api/models';
import { archiveExtensions, audioExtensions, categoryOf, imageExtensions, videoExtensions } from '../../core/entryCategory';
import type { EmojiName } from '../../design/components';

/** Extensions of plain-text notes, drawn as a memo rather than a printed page. */
const NOTE_EXTENSIONS = new Set(['txt', 'md', 'rtf', 'log']);

/** Lower-case extension of a file name, or '' when it has none (a leading dot alone is not an extension). */
function extensionOf(name: string | undefined): string {
  const n = name ?? '';
  const dot = n.lastIndexOf('.');
  return dot > 0 && dot < n.length - 1 ? n.slice(dot + 1).toLowerCase() : '';
}

/**
 * The Fluent emoji that stands for an entry in a listing: folders are file folders (open while selected), pictures a
 * framed picture, video film frames, audio musical notes, archives a package, text notes a memo, and every other
 * file (documents, unknown types) a printed page. The MIME type decides first; the extension decides when the type is generic.
 */
export function emojiNameFor(e: Pick<Entry, 'isDir' | 'mimeType' | 'name'>, opts: { open?: boolean } = {}): EmojiName {
  const category = categoryOf(e);
  if (category === 'folder') return opts.open ? 'open_file_folder' : 'file_folder';
  const ext = extensionOf(e.name);
  if (category === 'image' || imageExtensions.has(ext)) return 'framed_picture';
  if (category === 'video' || videoExtensions.has(ext)) return 'film_frames';
  if (category === 'audio' || audioExtensions.has(ext)) return 'musical_notes';
  if (category === 'archive' || archiveExtensions.has(ext)) return 'package';
  if ((e.mimeType ?? '').startsWith('text/') || NOTE_EXTENSIONS.has(ext)) return 'memo';
  return 'page_facing_up';
}
