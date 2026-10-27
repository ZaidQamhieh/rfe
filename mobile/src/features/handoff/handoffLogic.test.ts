import type { Host } from '../../core/models/host';
import { encodeHandoff, isSafeLocalName, matchHandoffHost, parseHandoff } from './handoffLogic';

const fp = 'ab'.repeat(32);
const host = (id: string): Host => ({ id, label: id, address: '10.0.0.1:8765' });

describe('isSafeLocalName', () => {
  it('accepts plain names and rejects traversal, separators, controls and reserved characters', () => {
    expect(isSafeLocalName('report.pdf')).toBe(true);
    for (const bad of ['', '.', '..', '../x', 'a/b', 'a\\b', 'a:b', 'a*b', 'a\u0007b', 'x'.repeat(256)]) expect(isSafeLocalName(bad)).toBe(false);
  });
});

describe('handoff payload', () => {
  it('round-trips', () => {
    const p = { certFingerprint: fp, path: '/srv/a.txt', name: 'a.txt' };
    expect(parseHandoff(encodeHandoff(p))).toEqual(p);
  });
  it('rejects junk, missing fields and unsafe names', () => {
    expect(parseHandoff('nope')).toBeNull();
    expect(parseHandoff('null')).toBeNull();
    expect(parseHandoff(JSON.stringify({ certFingerprint: fp, path: '/a' }))).toBeNull();
    expect(parseHandoff(JSON.stringify({ certFingerprint: fp, path: '/a', name: '../evil' }))).toBeNull();
  });
});

describe('matchHandoffHost', () => {
  it('matches by the secure-store pin, ignoring case and colons', () => {
    const colon = fp.toUpperCase().match(/../g)!.join(':');
    const hosts = [{ host: host('a'), pin: 'cd'.repeat(32) }, { host: host('b'), pin: fp }];
    expect(matchHandoffHost(hosts, colon)?.id).toBe('b');
  });
  it('returns null for no match, a missing pin, or a malformed fingerprint', () => {
    expect(matchHandoffHost([{ host: host('a'), pin: null }], fp)).toBeNull();
    expect(matchHandoffHost([{ host: host('a'), pin: fp }], 'short')).toBeNull();
  });
});
