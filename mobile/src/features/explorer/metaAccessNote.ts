import { can, type DeviceGrants, type Entry } from '../../core/api/models';

/** i18n keys of the notes the file sheet can show about actions it hides. */
export type MetaAccessNote = 'metaAccessBrowseOnly' | 'metaAccessNoDownload' | 'metaAccessNoModify' | 'metaAccessNoDelete';

/**
 * Why the file sheet is missing actions: the notes (as i18n keys) for what this phone's grants on the host deny.
 * A phone with no download, upload, modify or delete grant gets one browse-only note; otherwise each hidden action
 * the sheet would offer for [entry] gets its own. Unknown grants (older agent, not loaded yet) give no notes,
 * matching `can`, which hides nothing then.
 */
export function metaAccessNotes(caps: DeviceGrants | undefined, entry: Entry): MetaAccessNote[] {
  if (caps === undefined) return [];
  if (!can(caps, 'download') && !can(caps, 'upload') && !can(caps, 'modify') && !can(caps, 'delete')) return ['metaAccessBrowseOnly'];
  const notes: MetaAccessNote[] = [];
  if (!entry.isDir && !can(caps, 'download')) notes.push('metaAccessNoDownload');
  if (!can(caps, 'modify')) notes.push('metaAccessNoModify');
  if (!can(caps, 'delete')) notes.push('metaAccessNoDelete');
  return notes;
}
