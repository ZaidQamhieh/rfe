import type { Host } from '../../core/models/host';
import { useActiveHost } from '../../state/activeHost';
import { buildPathStack } from './paths';

/** Folder that contains [entryPath] (the path itself when it has no parent). Search and Recents reveal results this way. */
export function parentPathOf(entryPath: string): string {
  const stack = buildPathStack(entryPath);
  return stack.length >= 2 ? stack[stack.length - 2] : entryPath;
}

/**
 * Shows [entryPath]'s folder in the Files tab. With a known [rootPath] the open explorer keeps its root;
 * without one the host root view resolves which shared root contains the folder.
 */
/** The one router method used; `useRouter()` satisfies it. */
type Dismiss = { dismissTo(href: string): void };

export function revealInExplorer(router: Dismiss, host: Host, entryPath: string, rootPath?: string) {
  useActiveHost.getState().setActive({ host, health: null, rootPath, initialPath: parentPathOf(entryPath) });
  router.dismissTo('/files');
}
