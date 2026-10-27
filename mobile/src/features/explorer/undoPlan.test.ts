import type { BatchItemResult } from '../../core/api/models';
import { moveUndo, renameUndo } from './undoPlan';

const ok = (path: string): BatchItemResult => ({ path, ok: true });
const bad = (path: string): BatchItemResult => ({ path, ok: false, errorCode: 'CONFLICT' });

describe('renameUndo', () => {
  it('renames the new path back to the old one', () => {
    expect(renameUndo('/r/a.txt', '/r/b.txt')).toEqual({ kind: 'rename', from: '/r/b.txt', to: '/r/a.txt' });
  });
  it('has nothing to undo when the name did not change', () => {
    expect(renameUndo('/r/a', '/r/a')).toBeNull();
  });
});

describe('moveUndo', () => {
  it('moves each item back to the folder it came from, one move per source folder', () => {
    const op = moveUndo(['/a/x', '/a/y', '/b/z'], '/d', [ok('/a/x'), ok('/a/y'), ok('/b/z')]);
    expect(op).toEqual({
      kind: 'move',
      groups: [
        { paths: ['/d/x', '/d/y'], destDir: '/a' },
        { paths: ['/d/z'], destDir: '/b' },
      ],
    });
  });
  it('undoes only what actually moved', () => {
    const op = moveUndo(['/a/x', '/a/y'], '/d', [ok('/a/x'), bad('/a/y')]);
    expect(op).toEqual({ kind: 'move', groups: [{ paths: ['/d/x'], destDir: '/a' }] });
  });
  it('has nothing to undo when nothing moved or the item was already there', () => {
    expect(moveUndo(['/a/x'], '/d', [bad('/a/x')])).toBeNull();
    expect(moveUndo(['/d/x'], '/d', [ok('/d/x')])).toBeNull();
    expect(moveUndo(['/a/x'], '/d', [])).toBeNull();
  });
  it('works with Windows paths', () => {
    expect(moveUndo(['C:\\a\\x'], 'C:\\d', [ok('C:\\a\\x')])).toEqual({ kind: 'move', groups: [{ paths: ['C:\\d\\x'], destDir: 'C:\\a' }] });
  });
});
