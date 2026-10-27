import type { AuditEntry } from '../../core/api/models';
import { AUDIT_PAGE, appendOlder, olderCursor } from './auditPaging';

const a = (id: number): AuditEntry => ({ id, at: new Date(id * 1000), action: 'login', actor: '', target: '', detail: '' }) as AuditEntry;
const page = (from: number, n: number) => Array.from({ length: n }, (_, i) => a(from - i)); // newest first

describe('olderCursor', () => {
  it('is the oldest id of a full page', () => {
    expect(olderCursor(page(500, AUDIT_PAGE))).toBe(500 - AUDIT_PAGE + 1);
  });
  it('is null when the last page was short or empty: nothing older exists', () => {
    expect(olderCursor(page(50, AUDIT_PAGE - 1))).toBeNull();
    expect(olderCursor([])).toBeNull();
  });
});

describe('appendOlder', () => {
  it('appends the older page after the loaded entries and drops repeats', () => {
    const merged = appendOlder([a(5), a(4), a(3)], [a(3), a(2), a(1)]);
    expect(merged.map((e) => e.id)).toEqual([5, 4, 3, 2, 1]);
  });
});
