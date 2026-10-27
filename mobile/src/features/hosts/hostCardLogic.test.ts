import { hostCardActions, recentMeta, routeStripState } from './hostCardLogic';

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

describe('recentMeta', () => {
  const size = (n: number) => `${n} B`;
  it('joins parent folder and size', () => expect(recentMeta({ path: 'C:\\Work\\Projects\\plan.pdf', size: 5 }, size)).toBe('Projects · 5 B'));
  it('drops what the listing lacks', () => {
    expect(recentMeta({ path: '/plan.pdf' }, size)).toBe('');
    expect(recentMeta({ path: '/a/plan.pdf' }, size)).toBe('a');
  });
});
