import type { Host } from '../../core/models/host';
import { pairAgainAddress, switcherRowState } from './switcherLogic';

const host = (over: Partial<Host> = {}): Host => ({ id: 'h1', label: 'Studio PC', address: '192.168.1.20:8765', ...over });
const status = (over: Partial<Parameters<typeof switcherRowState>[0]> = {}) => ({ online: true, checking: false, untrusted: false, refused: false, activeAddress: '192.168.1.20:8765', lastSeen: null, ...over });
const NOW = new Date('2026-11-10T12:00:00Z');

describe('Pair again on a refused computer', () => {
  it('a refused computer offers the Pair again action, with the refused phrase', () => {
    expect(switcherRowState(status({ refused: true }), host(), NOW)).toMatchObject({ kind: 'refused', action: 'pair', off: false, phrase: 'No longer accepts this phone' });
  });

  it('an online computer that has not refused gets no action', () => {
    expect(switcherRowState(status({ refused: false }), host(), NOW)).toMatchObject({ kind: 'online', action: null });
  });

  it('a changed identity still offers Review, not Pair again', () => {
    expect(switcherRowState(status({ refused: true, untrusted: true, online: false }), host(), NOW)).toMatchObject({ kind: 'untrusted', action: 'review' });
  });

  it('a computer that is still being checked offers no action', () => {
    expect(switcherRowState(status({ refused: true, checking: true }), host(), NOW)).toMatchObject({ kind: 'checking', action: null });
  });

  it('the address comes from the one that answered this phone', () => {
    expect(pairAgainAddress({ activeAddress: '100.64.0.7:8765' }, host())).toBe('100.64.0.7:8765');
  });

  it('falls back to the saved address when no address answered', () => {
    expect(pairAgainAddress({ activeAddress: null }, host({ address: '10.0.0.5:8765' }))).toBe('10.0.0.5:8765');
  });
});
