import { defaultVisibility } from '../../core/visibility';
import { addExtension, addName, customExtensions, normalizeExtension, removeExtension, removeName, setHideDotfiles, withVisibilityOverride } from './visibilityEdit';

describe('visibility edits', () => {
  it('normalizes extensions', () => {
    expect(normalizeExtension(' .JPG ')).toBe('jpg');
    expect(normalizeExtension('..tar.gz')).toBe('tar.gz');
    expect(normalizeExtension('   ')).toBe('');
  });
  it('adds and removes extensions without touching the original', () => {
    const p = defaultVisibility();
    const a = addExtension(p, '.LOG');
    expect([...a.hiddenExtensions]).toEqual(['log']);
    expect(p.hiddenExtensions.size).toBe(0);
    expect(addExtension(a, '  ')).toBe(a);
    expect(removeExtension(a, 'LOG').hiddenExtensions.size).toBe(0);
  });
  it('matches exact names case-insensitively', () => {
    const a = addName(addName(defaultVisibility(), 'Thumbs.db'), 'thumbs.DB');
    expect([...a.hiddenNames]).toEqual(['thumbs.DB']);
    expect(removeName(a, 'THUMBS.db').hiddenNames.size).toBe(0);
  });
  it('toggles dotfiles', () => {
    expect(setHideDotfiles(defaultVisibility(), false).hideDotfiles).toBe(false);
  });
  it('lists only hand-typed extensions as custom', () => {
    const p = addExtension(addExtension(defaultVisibility(), 'zzz'), 'log');
    expect(customExtensions(p)).toEqual(['zzz']);
  });
});

describe('withVisibilityOverride', () => {
  it('turning on copies the effective rules; turning off drops only visibility', () => {
    const eff = addExtension(defaultVisibility(), 'tmp');
    const on = withVisibilityOverride({ gridView: true }, eff, true);
    expect(on.gridView).toBe(true);
    expect([...on.visibility!.hiddenExtensions]).toEqual(['tmp']);
    expect(on.visibility).not.toBe(eff);
    expect(withVisibilityOverride(on, eff, false)).toEqual({ gridView: true });
    expect(withVisibilityOverride(undefined, eff, false)).toEqual({});
  });
});
