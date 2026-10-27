import type { Entry } from '../../core/api/models';
import { t } from '../../i18n';

/** The paths from [anchor] to [target] (inclusive, either direction) in on-screen [order]; just [target] without a usable anchor. */
export function rangePaths(order: readonly string[], anchor: string | null, target: string): string[] {
  const to = order.indexOf(target);
  if (to < 0) return [];
  const from = anchor === null ? -1 : order.indexOf(anchor);
  if (from < 0) return [target];
  return order.slice(Math.min(from, to), Math.max(from, to) + 1);
}

/** Total size of the selected files; folders and entries without a size add nothing. */
export function selectedBytes(entries: readonly Entry[], selected: ReadonlySet<string>): number {
  let sum = 0;
  for (const e of entries) if (selected.has(e.path) && !e.isDir) sum += e.size ?? 0;
  return sum;
}

/** The selected paths, one per line, in listing order. */
export function pathsText(entries: readonly Entry[], selected: ReadonlySet<string>): string {
  return entries.filter((e) => selected.has(e.path)).map((e) => e.path).join('\n');
}

/** Subtitle of the delete dialog: what is deleted and on which host (one item is named). */
export function deleteSubtitle(o: { count: number; hostLabel: string; name?: string }): string {
  const what = o.count === 1 && o.name ? t('deleteOneOnHost', { name: o.name, host: o.hostLabel }) : t('deleteManyOnHost', { count: o.count, host: o.hostLabel });
  return `${what} ${t('canRestoreFromTrash', { count: o.count })}`;
}
