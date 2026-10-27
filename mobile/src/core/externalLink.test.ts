import { externalLinkTarget } from './externalLink';

describe('externalLinkTarget', () => {
  it('lets a link prefill the Add-workspace address and nothing more', () => {
    expect(externalLinkTarget('rfe://pair?address=192.168.1.5:8765')).toBe('/pair?address=192.168.1.5%3A8765');
    expect(externalLinkTarget('/pair?address=pc.local')).toBe('/pair?address=pc.local');
    expect(externalLinkTarget('rfe://pair')).toBe('/pair');
  });

  it('never reaches the screen that pairs on open, or any other route', () => {
    for (const p of [
      'rfe://pair/request?address=evil:8765',
      '/pair/request?address=evil:8765',
      'rfe://pair/login?address=evil',
      'rfe://pair/register?address=evil',
      'rfe://edit?path=/etc/passwd',
      'rfe://host/h1/settings',
      'rfe://receive',
      'rfe://dataUrl=rfeShareKey',
      'rfe://expo-sharing-intent?x=1',
      'https://example.com/pair/request',
    ]) {
      expect(externalLinkTarget(p)).toBe('/');
    }
  });

  it('falls back to home for junk and refuses odd addresses', () => {
    expect(externalLinkTarget('')).toBe('/');
    expect(externalLinkTarget('rfe://pair?address=' + 'a'.repeat(300))).toBe('/pair');
    expect(externalLinkTarget('rfe://pair?address=a%0Ab')).toBe('/pair');
  });
});
