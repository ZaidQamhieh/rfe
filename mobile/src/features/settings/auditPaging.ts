import type { AuditEntry } from '../../core/api/models';

/** Entries asked for per request; the agent returns up to this many, newest first. */
export const AUDIT_PAGE = 100;

/** The `before` id that fetches the next older page, or null when the last page was short (nothing older). */
export function olderCursor(lastPage: readonly AuditEntry[], pageSize = AUDIT_PAGE): number | null {
  return lastPage.length >= pageSize ? lastPage[lastPage.length - 1].id : null;
}

/** [older] appended after [loaded], skipping ids already shown. */
export function appendOlder(loaded: readonly AuditEntry[], older: readonly AuditEntry[]): AuditEntry[] {
  const seen = new Set(loaded.map((e) => e.id));
  return [...loaded, ...older.filter((e) => !seen.has(e.id))];
}
