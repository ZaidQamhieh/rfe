import type { Host } from './host';
import { mergeDuplicateHosts } from './hostMerge';

const F1 = 'ab'.repeat(32);
const F2 = 'cd'.repeat(32);
const host = (over: Partial<Host> & { id: string }): Host => ({ label: 'PC', address: '10.0.0.1:8765', ...over });

describe('mergeDuplicateHosts', () => {
  it('keeps one entry per certificate: the newest (last listed) one, with the nicer name', () => {
    const older = host({ id: 'a', label: 'Studio PC', address: '10.0.0.1:8765', certFingerprint: F1, note: 'Home office' });
    const newer = host({ id: 'b', label: '10.0.0.7:8765', address: '10.0.0.7:8765', certFingerprint: F1.toUpperCase(), tailscaleAddress: '100.64.0.7:8765' });
    const r = mergeDuplicateHosts([older, newer]);
    expect(r.dropped).toEqual(['a']);
    expect(r.hosts).toEqual([{ id: 'b', label: 'Studio PC', address: '10.0.0.7:8765', certFingerprint: F1.toUpperCase(), tailscaleAddress: '100.64.0.7:8765', note: 'Home office' }]);
  });

  it('keeps the newest name when both names are real', () => {
    const r = mergeDuplicateHosts([host({ id: 'a', label: 'Old name', certFingerprint: F1 }), host({ id: 'b', label: 'New name', certFingerprint: F1 })]);
    expect(r.hosts.map((h) => h.label)).toEqual(['New name']);
  });

  it('leaves different certificates and entries without a certificate alone, in order', () => {
    const list = [host({ id: 'a', certFingerprint: F1 }), host({ id: 'b', certFingerprint: F2 }), host({ id: 'c' }), host({ id: 'd' })];
    const r = mergeDuplicateHosts(list);
    expect(r.dropped).toEqual([]);
    expect(r.hosts).toEqual(list);
  });

  it('merges a group of three and keeps the position of the survivor', () => {
    const r = mergeDuplicateHosts([host({ id: 'a', certFingerprint: F1 }), host({ id: 'x', certFingerprint: F2 }), host({ id: 'b', certFingerprint: F1 }), host({ id: 'c', certFingerprint: F1 })]);
    expect(r.dropped.sort()).toEqual(['a', 'b']);
    expect(r.hosts.map((h) => h.id)).toEqual(['x', 'c']);
  });
});
