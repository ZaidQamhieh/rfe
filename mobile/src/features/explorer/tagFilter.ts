import type { Entry } from '../../core/api/models';

/**
 * The entries the list shows once a bookmark tag is active. Select all, invert and every other action on "what is on
 * screen" must use this list, not the unfiltered one, or they reach rows the user cannot see.
 */
export function filterByTag(display: Entry[], activeTag: string | null, taggedPaths: ReadonlySet<string>): Entry[] {
  return activeTag ? display.filter((e) => taggedPaths.has(e.path)) : display;
}
