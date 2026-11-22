import { t } from '../../i18n';
import type { Host } from '../../core/models/host';
import { nextRefused } from './accessState';
import { homeStateView, switcherRowState } from './switcherLogic';

const host: Host = { id: 'h', label: 'Studio', address: '10.0.0.1:8765' };
const st = (o: object = {}) => ({ online: false, checking: false, untrusted: false, refused: false, activeAddress: null, lastSeen: null, ...o });

describe('a computer that removed this phone', () => {
  it('has its own Home state, whether or not it still answers, not the generic offline one', () => {
    expect(homeStateView(st({ refused: true, online: true }), host)).toBe('refused');
    expect(homeStateView(st({ refused: true, online: false }), host)).toBe('refused');
    expect(homeStateView(st({ refused: false }), host)).toBe('lost');
  });
  it('still offers Pair again in the switcher', () => {
    expect(switcherRowState(st({ refused: true }), host)).toMatchObject({ kind: 'refused', action: 'pair' });
  });
  it('says so in plain words', () => {
    expect(t('stateRefusedTitle')).toBe("This computer removed this phone's access");
    expect(t('stateRefusedBody').length).toBeGreaterThan(0);
  });
});

describe('nextRefused', () => {
  it('turns on at a refusal and stays on while the computer cannot be asked', () => {
    expect(nextRefused(false, 'unauthorized')).toBe(true);
    expect(nextRefused(true, 'unreachable')).toBe(true);
    expect(nextRefused(true, 'error')).toBe(true);
  });
  it('turns off only when the computer accepted the phone', () => {
    expect(nextRefused(true, 'accepted')).toBe(false);
    expect(nextRefused(false, 'unreachable')).toBe(false);
  });
});
