import { computerMenuEntries } from './computerMenuLogic';

describe('computer menu for a phone that is not the owner', () => {
  it('leaves out Phones with access, keeping the other entries in order', () => {
    const keys = computerMenuEntries('Studio PC', true, false).map((x) => x.key);
    expect(keys).toEqual(['trash', 'apps', 'about', 'rule', 'settings']);
  });
  it('keeps Phones with access for the owner', () => {
    const keys = computerMenuEntries('Studio PC', true, true).map((x) => x.key);
    expect(keys).toContain('phones');
  });
  it('still greys Apps out while the computer is unreachable', () => {
    expect(computerMenuEntries('Studio PC', false, false).find((x) => x.key === 'apps')).toMatchObject({ disabled: true });
  });
});
