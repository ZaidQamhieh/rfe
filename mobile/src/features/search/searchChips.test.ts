import { defaultFilters } from './searchLogic';
import { scopeChips, scopeLabels } from './searchScope';

const labels = scopeLabels({ categories: [], size: 'any' }, 'Studio PC', 'Documents', '/home/a/Documents');

describe('scopeChips', () => {
  it('has no chip for the computer: it is chosen elsewhere, not filtered', () => {
    const chips = scopeChips(defaultFilters(), labels, { canSearchContent: true });
    expect(chips.map((c) => c.key)).not.toContain('computer');
    expect(chips.map((c) => c.label)).not.toContain('Studio PC');
  });
  it('keeps the all-computers, folder, content, type, size and more filters in order', () => {
    expect(scopeChips(defaultFilters(), labels, { canSearchContent: true }).map((c) => c.key)).toEqual(['all', 'folder', 'content', 'type', 'size', 'more']);
  });
  it('leaves out the folder chip at a top level and the content chip when unsupported', () => {
    const top = scopeLabels({ categories: [], size: 'any' }, 'Studio PC', '', '/');
    expect(scopeChips(defaultFilters(), top, { canSearchContent: false }).map((c) => c.key)).toEqual(['all', 'type', 'size', 'more']);
  });
  it('lets the folder chip switch the search back to the whole computer', () => {
    const inFolder = scopeChips({ ...defaultFilters(), fromHere: true }, labels, { canSearchContent: false }).find((c) => c.key === 'folder');
    expect(inFolder?.selected).toBe(true);
    expect(inFolder?.action).toEqual({ kind: 'filters', patch: { fromHere: false } });
    const whole = scopeChips({ ...defaultFilters(), fromHere: false }, labels, { canSearchContent: false }).find((c) => c.key === 'folder');
    expect(whole?.selected).toBe(false);
    expect(whole?.action).toEqual({ kind: 'filters', patch: { fromHere: true } });
  });
  it('marks type, size and more as selected only when they filter something', () => {
    const f = { ...defaultFilters(), size: 'mb10' as const };
    const by = Object.fromEntries(scopeChips(f, scopeLabels(f, 'PC', 'D', '/a/D'), { canSearchContent: false }).map((c) => [c.key, c]));
    expect(by.size.selected).toBe(true);
    expect(by.type.selected).toBe(false);
    expect(by.more.selected).toBe(true);
  });
});
