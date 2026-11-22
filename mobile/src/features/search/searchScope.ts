import { t } from '../../i18n';
import { basenameOf, parentDirOf } from '../explorer/paths';
import { SIZE_PRESETS, CATEGORY_LABEL, activeFilterCount, type SearchFilters } from './searchLogic';

/** The words on the scope chips of the search screen. `folder` is null when the search was opened at a computer's top level (no folder to narrow to). */
export type ScopeLabels = { computer: string; folder: string | null; type: string; size: string };

/** Whether [path] is a top-level place (empty, "/", or a drive such as "C:\") where a "this folder" chip would only repeat the computer chip. */
export const isTopLevel = (path: string) => path === '' || path === '/' || /^[A-Za-z]:[\\/]?$/.test(path);

/** Chip words for the current filters: the computer's name, the folder's name, "Any type" / the one type / "N types", and the size preset. */
export function scopeLabels(filters: Pick<SearchFilters, 'categories' | 'size'>, hostLabel: string, folderName: string, currentPath: string): ScopeLabels {
  const n = filters.categories.length;
  const type = n === 0 ? t('searchTypeAny') : n === 1 ? t(CATEGORY_LABEL[filters.categories[0]]) : t('searchTypeMany', { count: n });
  const preset = SIZE_PRESETS.find((p) => p.key === filters.size);
  return { computer: hostLabel, folder: isTopLevel(currentPath) ? null : folderName, type, size: preset ? t(preset.label) : t('searchSizeAny') };
}

/** The name of the folder a result sits in ("Documents" for /home/a/Documents/x.pdf); empty when that folder is the top level, where a name would only say "Root". */
export function parentFolderName(path: string): string {
  const parent = parentDirOf(path);
  return isTopLevel(parent) ? '' : basenameOf(parent);
}

/** Where the results come from, as a phrase for "N found on …" and "Nothing called … on …": the folder's name when searching from it, else the computer's. */
export function scopePhrase(fromHere: boolean, labels: ScopeLabels): string {
  return fromHere && labels.folder ? labels.folder : labels.computer;
}

/** What tapping a scope chip does: change the filters, search every computer instead, or open the filter sheet. */
export type ScopeChipAction = { kind: 'filters'; patch: Partial<SearchFilters> } | { kind: 'allComputers' } | { kind: 'openFilters' };
export type ScopeChip = { key: 'all' | 'folder' | 'content' | 'type' | 'size' | 'more'; label: string; selected: boolean; dropdown?: boolean; action: ScopeChipAction };

/**
 * The chips under the search box, in order. They are filters and scopes only: the computer being searched is not one of
 * them (it is chosen with the computer switcher), and the folder chip toggles so the search can return to the whole computer.
 */
export function scopeChips(filters: SearchFilters, labels: ScopeLabels, opts: { canSearchContent: boolean }): ScopeChip[] {
  const inFolder = filters.fromHere && labels.folder !== null;
  const chips: ScopeChip[] = [{ key: 'all', label: t('searchScopeAllComputers'), selected: false, action: { kind: 'allComputers' } }];
  if (labels.folder !== null) chips.push({ key: 'folder', label: labels.folder, selected: inFolder, action: { kind: 'filters', patch: { fromHere: !inFolder } } });
  if (opts.canSearchContent) chips.push({ key: 'content', label: t('searchInsideFiles'), selected: filters.content, action: { kind: 'filters', patch: { content: !filters.content } } });
  chips.push(
    { key: 'type', label: labels.type, selected: filters.categories.length > 0, dropdown: true, action: { kind: 'openFilters' } },
    { key: 'size', label: labels.size, selected: filters.size !== 'any', dropdown: true, action: { kind: 'openFilters' } },
    { key: 'more', label: t('searchFiltersTooltip'), selected: activeFilterCount(filters) > 0, dropdown: true, action: { kind: 'openFilters' } },
  );
  return chips;
}
