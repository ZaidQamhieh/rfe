import type { AgentClient } from '../../core/api/agentClient';
import { listAllEntries } from '../../core/api/listAll';
import type { Entry } from '../../core/api/models';
import { extractFolderName } from './metaLogic';
import { joinRemotePath, parentDirOf } from './paths';

/**
 * Extracts [archive] into a new folder beside it. Extraction replaces same-name files in an existing folder, so the
 * folder gets a name nothing uses yet. Returns that folder's name.
 */
export async function extractToFreshFolder(cl: AgentClient, archive: Entry): Promise<string> {
  const parent = parentDirOf(archive.path);
  const taken = new Set((await listAllEntries((cursor) => cl.list(parent, { cursor }))).map((e) => e.name));
  const folder = extractFolderName(archive.name, taken);
  await cl.extract(archive.path, joinRemotePath(parent, folder));
  return folder;
}
