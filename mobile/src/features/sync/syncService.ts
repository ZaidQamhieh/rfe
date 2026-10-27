import { Directory, File, Paths } from 'expo-file-system';

import type { Entry } from '../../core/api/models';
import { fetchCancelNative, fetchToFileNative } from '../../core/native';
import { SyncRuleStore, type SyncRule } from '../../core/storage/syncRules';
import { clientForHost, hostStore, keyValue } from '../../services';
import { fileUriFromPath, pathFromFileUri } from './syncLogic';
import { runSync,type LocalFolder, type LocalInfo, type SyncProgress, type SyncResult } from './syncRunner';

export const syncRules = new SyncRuleStore(keyValue);

const nativePath = pathFromFileUri;

/** Opens the system folder picker. The app keeps read and write access to the chosen tree across restarts. */
export async function pickLocalFolder(): Promise<string | null> {
  try {
    return (await Directory.pickDirectoryAsync()).uri;
  } catch {
    return null; // cancelled
  }
}

/** A readable name for a rule's local folder (`content://…/tree/primary%3ADocuments%2Fx` becomes `Documents/x`). */
export function folderLabel(localPath: string): string {
  if (!localPath.startsWith('content://')) return localPath;
  const tree = /\/tree\/([^/?#]+)/.exec(localPath)?.[1];
  if (!tree) return localPath;
  const id = decodeURIComponent(tree);
  const colon = id.indexOf(':');
  return colon >= 0 && id.slice(colon + 1) !== '' ? id.slice(colon + 1) : id;
}

/** The mirror folder as files: a picked tree URI (Storage Access Framework) or a plain path from the Flutter app. */
export function openLocalFolder(localPath: string): LocalFolder {
  const dir = () => new Directory(localPath.startsWith('content://') ? localPath : fileUriFromPath(localPath));
  return {
    async list() {
      const out = new Map<string, LocalInfo>();
      for (const item of dir().list()) {
        if (item instanceof File) out.set(item.name, { size: item.size ?? 0, modified: item.modificationTime ?? null });
      }
      return out;
    },
    async put(_name, stagedPath) {
      // Copying into the folder lets the file system create the document, named after the staged file, and replace any
      // existing one. Overwriting a document made beforehand with createFile deletes it before writing.
      await new File(fileUriFromPath(stagedPath)).copy(dir(), { overwrite: true });
    },
  };
}

async function listAll(hostId: string, remotePath: string): Promise<Entry[]> {
  const host = (await hostStore.listHosts()).find((h) => h.id === hostId);
  if (!host) throw new Error('This computer is no longer paired.');
  const client = await clientForHost(host);
  const out: Entry[] = [];
  let cursor: string | undefined;
  do {
    const page = await client.list(remotePath, { cursor });
    out.push(...page.entries);
    cursor = page.nextCursor;
  } while (cursor);
  return out;
}

/** Runs one rule and records the time when every file made it. */
export async function syncRule(rule: SyncRule, onProgress?: (p: SyncProgress) => void, cancelled?: () => boolean): Promise<SyncResult> {
  const host = (await hostStore.listHosts()).find((h) => h.id === rule.hostId);
  if (!host) throw new Error('This computer is no longer paired.');
  const client = await clientForHost(host);
  const stage = new Directory(Paths.cache, 'sync', rule.id);
  stage.create({ idempotent: true, intermediates: true });
  const result = await runSync(
    rule.remotePath,
    {
      listRemote: (p) => listAll(rule.hostId, p),
      async fetch(entry, dest) {
        const spec = client.downloadSpec(entry.path);
        const id = `s${rule.id}${Date.now().toString(36)}`;
        // A cancel stops the download in flight instead of waiting for a large file to finish (audit F-112).
        const watch = cancelled ? setInterval(() => cancelled() && void fetchCancelNative(id).catch(() => undefined), 500) : undefined;
        try {
          const r = await fetchToFileNative(id, spec.url, spec.headers, spec.pin, dest, 600_000);
          if (r.status < 200 || r.status >= 300) throw new Error(`HTTP ${r.status}`);
        } finally {
          if (watch !== undefined) clearInterval(watch);
        }
      },
      staging: (name) => nativePath(new File(stage, name).uri),
      discard: (p) => {
        try {
          new File(fileUriFromPath(p)).delete();
        } catch {
          // Already gone.
        }
      },
      local: openLocalFolder(rule.localPath),
    },
    onProgress,
    cancelled,
  );
  if (result.failed.length === 0 && !cancelled?.()) await syncRules.save({ ...rule, lastSync: new Date().toISOString() });
  return result;
}
