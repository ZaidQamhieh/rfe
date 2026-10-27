import { externalLinkTarget } from '../core/externalLink';

// Runs for every link another app opens us with; see externalLink.ts for the allow-list.
export function redirectSystemPath({ path }: { path: string; initial: boolean }): string {
  return externalLinkTarget(path);
}
