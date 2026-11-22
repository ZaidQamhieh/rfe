import type { QueryClient } from '@tanstack/react-query';

/**
 * Marks the cached folder listing stale after this browser changed a folder (new folder, upload). With a path only
 * that folder's listing is refreshed; without one every cached listing is. Search results are refreshed too,
 * because they may now be missing the new entry.
 */
export function invalidateFolderViews(qc: QueryClient, path?: string): void {
  void qc.invalidateQueries({ queryKey: path === undefined ? ['listing'] : ['listing', path] });
  void qc.invalidateQueries({ queryKey: ['fsearch'] });
}
