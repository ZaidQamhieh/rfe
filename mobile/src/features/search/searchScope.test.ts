import { isTopLevel, parentFolderName, scopeLabels, scopePhrase } from './searchScope';

describe('parentFolderName', () => {
  it('names only the folder the result sits in', () => {
    expect(parentFolderName('/home/a/Documents/Invoice 0412.pdf')).toBe('Documents');
    expect(parentFolderName('C:\\Users\\a\\x.txt')).toBe('a');
    expect(parentFolderName('/home/a/Documents')).toBe('a');
  });
  it('is empty for a file at the top level', () => {
    expect(parentFolderName('/x.txt')).toBe('');
    expect(parentFolderName('C:\\x.txt')).toBe('');
    expect(parentFolderName('')).toBe('');
  });
});

describe('scopeLabels', () => {
  it('uses the "any" words when nothing is filtered', () => {
    expect(scopeLabels({ categories: [], size: 'any' }, 'Studio PC', 'Documents', '/home/a/Documents')).toEqual({ computer: 'Studio PC', folder: 'Documents', type: 'Any type', size: 'Any size' });
  });
  it('names one type and counts several', () => {
    expect(scopeLabels({ categories: ['image'], size: 'mb10' }, 'PC', 'x', '/x').type).toBe('Images');
    expect(scopeLabels({ categories: ['image', 'video'], size: 'mb10' }, 'PC', 'x', '/x').type).toBe('2 types');
    expect(scopeLabels({ categories: [], size: 'mb10' }, 'PC', 'x', '/x').size).toBe('> 10 MB');
  });
  it('has no folder chip at a top level', () => {
    expect(scopeLabels({ categories: [], size: 'any' }, 'PC', 'Root', '/').folder).toBeNull();
    expect(scopeLabels({ categories: [], size: 'any' }, 'PC', 'Root', 'C:\\').folder).toBeNull();
    expect(isTopLevel('/home')).toBe(false);
  });
});

describe('scopePhrase', () => {
  const labels = { computer: 'Studio PC', folder: 'Documents', type: 'Any type', size: 'Any size' };
  it('names the folder only while searching from it', () => {
    expect(scopePhrase(true, labels)).toBe('Documents');
    expect(scopePhrase(false, labels)).toBe('Studio PC');
    expect(scopePhrase(true, { ...labels, folder: null })).toBe('Studio PC');
  });
});
