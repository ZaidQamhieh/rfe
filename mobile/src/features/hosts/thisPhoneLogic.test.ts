import { keptFolders } from './thisPhoneLogic';

describe('keptFolders', () => {
  it('names each folder by its last part and the computer it came from', () => {
    const rows = keptFolders([{ hostId: 'h1', remotePath: '/Music/Band recordings' }, { hostId: 'gone', remotePath: 'C:\\Docs' }], new Map([['h1', 'Studio PC']]));
    expect(rows).toEqual([
      { key: 'h1:/Music/Band recordings', name: 'Band recordings', computer: 'Studio PC' },
      { key: 'gone:C:\\Docs', name: 'Docs', computer: '' },
    ]);
  });

  it('keeps the whole path for a root and returns nothing for no pins', () => {
    expect(keptFolders([{ hostId: 'h', remotePath: '/' }], new Map())[0].name).toBe('/');
    expect(keptFolders([], new Map())).toEqual([]);
  });
});
