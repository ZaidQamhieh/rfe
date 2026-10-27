import type { FileCapability } from '../../core/api/models';
import { footerActions, FOOTER_LIST_PADDING } from './footerLogic';

const caps = (...on: FileCapability[]): Record<FileCapability, boolean> => ({ browse: true, download: false, upload: false, modify: false, delete: false, share: false, ...Object.fromEntries(on.map((k) => [k, true])) });
const keys = (a: ReturnType<typeof footerActions>) => a.map((x) => `${x.key}${x.primary ? '*' : ''}`);

describe('footerActions', () => {
  it('unknown capabilities (older agent) behave as full grants', () => {
    expect(keys(footerActions({ caps: undefined, showPaste: false }))).toEqual(['upload*', 'new']);
  });

  it('full grants: Upload primary, New secondary', () => {
    expect(keys(footerActions({ caps: caps('upload', 'modify'), showPaste: false }))).toEqual(['upload*', 'new']);
  });

  it('upload-only: a lone Upload', () => {
    expect(keys(footerActions({ caps: caps('upload'), showPaste: false }))).toEqual(['upload*']);
  });

  it('modify-only: a lone New, which is then primary', () => {
    expect(keys(footerActions({ caps: caps('modify'), showPaste: false }))).toEqual(['new*']);
  });

  it('read-only: no footer', () => {
    expect(footerActions({ caps: caps(), showPaste: false })).toEqual([]);
  });

  it('clipboard makes Paste primary when the device may modify', () => {
    expect(keys(footerActions({ caps: caps('upload', 'modify'), showPaste: true }))).toEqual(['paste*', 'new']);
    expect(keys(footerActions({ caps: caps('modify'), showPaste: true }))).toEqual(['paste*', 'new']);
  });

  it('never offers Paste without modify (the agent would refuse it)', () => {
    expect(keys(footerActions({ caps: caps('upload'), showPaste: true }))).toEqual(['upload*']);
    expect(footerActions({ caps: caps(), showPaste: true })).toEqual([]);
  });

  it('list padding clears the footer by 8 dp', () => {
    expect(FOOTER_LIST_PADDING).toBe(76);
  });
});
