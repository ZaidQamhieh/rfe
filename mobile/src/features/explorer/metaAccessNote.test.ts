import type { DeviceGrants, Entry } from '../../core/api/models';
import en from '../../i18n/en.json';
import { metaAccessNotes } from './metaAccessNote';

const grants = (over: Partial<DeviceGrants> = {}): DeviceGrants => ({ browse: true, download: false, upload: false, modify: false, delete: false, share: false, ...over });
const file = { name: 'a.txt', path: '/r/a.txt', isDir: false, size: 1 } as Entry;
const dir = { name: 'd', path: '/r/d', isDir: true } as Entry;

describe('metaAccessNotes', () => {
  it('says nothing while the grants are unknown (older agent, not loaded yet)', () => {
    expect(metaAccessNotes(undefined, file)).toEqual([]);
  });

  it('gives one browse-only note for a phone that holds no write or download grant', () => {
    expect(metaAccessNotes(grants(), file)).toEqual(['metaAccessBrowseOnly']);
    expect(metaAccessNotes(grants(), dir)).toEqual(['metaAccessBrowseOnly']);
  });

  it('says nothing when every action in the sheet is allowed', () => {
    expect(metaAccessNotes(grants({ download: true, upload: true, modify: true, delete: true }), file)).toEqual([]);
  });

  it('names each hidden action when only some grants are missing', () => {
    expect(metaAccessNotes(grants({ upload: true, modify: true, delete: true }), file)).toEqual(['metaAccessNoDownload']);
    expect(metaAccessNotes(grants({ download: true, upload: true }), file)).toEqual(['metaAccessNoModify', 'metaAccessNoDelete']);
  });

  it('does not mention downloads for a folder, which the sheet never offers to download', () => {
    expect(metaAccessNotes(grants({ upload: true, modify: true, delete: true }), dir)).toEqual([]);
    expect(metaAccessNotes(grants({ download: true, upload: true, modify: true }), dir)).toEqual(['metaAccessNoDelete']);
  });

  it('has an English string for every note key', () => {
    const keys = ['metaAccessBrowseOnly', 'metaAccessNoDownload', 'metaAccessNoModify', 'metaAccessNoDelete'];
    for (const k of keys) expect((en as Record<string, string>)[k]).toEqual(expect.stringContaining('Ask the owner'));
  });
});
