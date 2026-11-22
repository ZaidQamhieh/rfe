import { hostCardActions, routeStripState } from './hostCardLogic';

describe('hostCardActions', () => {
  it('enables everything when online', () => {
    expect(hostCardActions({ online: true, checking: false })).toEqual({ open: true, search: true, apps: true, transfers: true });
  });

  it('keeps Open and Transfers offline but disables Search and Apps', () => {
    expect(hostCardActions({ online: false, checking: false })).toEqual({ open: true, search: false, apps: false, transfers: true });
  });

  it('disables Open, Search and Apps while checking', () => {
    expect(hostCardActions({ online: true, checking: true })).toEqual({ open: false, search: false, apps: false, transfers: true });
  });
});

describe('routeStripState', () => {
  const host = { id: 'h', label: 'PC', address: '192.168.1.2:8765', tailscaleAddress: '100.1.2.3:8765' };
  it('reports checking first', () => expect(routeStripState({ online: false, checking: true, activeAddress: null }, host)).toEqual({ kind: 'checking' }));
  it('reports offline', () => expect(routeStripState({ online: false, checking: false, activeAddress: null }, host)).toEqual({ kind: 'offline' }));
  it('names the route that answered', () => {
    expect(routeStripState({ online: true, checking: false, activeAddress: '100.1.2.3:8765' }, host)).toEqual({ kind: 'active', route: 'tailscale' });
    expect(routeStripState({ online: true, checking: false, activeAddress: null }, host)).toEqual({ kind: 'active', route: 'lan' });
  });
});
