import type { BatchResult } from '../../core/api/models';
import { restoreOutcome } from './trashRestore';

const ok = (path: string): BatchResult => ({ results: [{ path, ok: true }], failed: [] });

describe('restoreOutcome', () => {
  it('reports a plain restore under its own name', () => {
    expect(restoreOutcome('notes.txt', ok('/home/u/notes.txt'))).toEqual({ ok: true, name: 'notes.txt', renamed: false });
  });

  it('reports the new name when the agent renamed on a collision', () => {
    expect(restoreOutcome('notes.txt', ok('/home/u/notes (2).txt'))).toEqual({ ok: true, name: 'notes (2).txt', renamed: true });
  });

  it('understands Windows separators', () => {
    expect(restoreOutcome('a.txt', ok('C:\\Users\\u\\a (2).txt'))).toEqual({ ok: true, name: 'a (2).txt', renamed: true });
  });

  it('is an error, never a success, when the item failed', () => {
    const failed = { path: 'abc', ok: false, errorCode: 'PATH_NOT_FOUND', errorMessage: 'trash payload missing' };
    const r = restoreOutcome('notes.txt', { results: [failed], failed: [failed] });
    expect(r).toEqual({ ok: false, error: 'trash payload missing' });
  });

  it('falls back to the code, then a generic message, when the agent gave no text', () => {
    const coded = { path: 'abc', ok: false, errorCode: 'RESTORE_FAILED' };
    expect(restoreOutcome('x', { results: [coded], failed: [coded] })).toEqual({ ok: false, error: 'RESTORE_FAILED' });
    const bare = { path: 'abc', ok: false };
    expect(restoreOutcome('x', { results: [bare], failed: [bare] })).toEqual({ ok: false, error: 'Restore failed' });
  });

  it('is an error when the agent returned no result at all', () => {
    expect(restoreOutcome('x', { results: [], failed: [] })).toEqual({ ok: false, error: 'Restore failed' });
  });
});
