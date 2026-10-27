import type { Host } from '../../core/models/host';
import { buildRouteRows, pickCurrent, routeSubtitle, stateTone, summarize } from './connectLogic';
import type { ProbeResult } from './diagnostics';

const host: Host = { id: 'h', label: 'PC', address: '192.168.1.2:8765', tailscaleAddress: '100.1.2.3:8765' };
const ok = (route: ProbeResult['route'], address: string, latencyMs = 12, auth: ProbeResult['auth'] = 'accepted'): ProbeResult => ({ route, address, latencyMs, failure: 'none', auth });
const bad = (route: ProbeResult['route'], address: string, failure: ProbeResult['failure']): ProbeResult => ({ route, address, failure, auth: 'notChecked' });

describe('pickCurrent', () => {
  it('prefers the address the client is using when it answered', () => {
    expect(pickCurrent([ok('lan', 'a'), ok('tailscale', 'b')], 'b')).toBe('b');
  });
  it('falls back to the first reachable route', () => {
    expect(pickCurrent([bad('lan', 'a', 'unreachable'), ok('tailscale', 'b')], 'a')).toBe('b');
    expect(pickCurrent([bad('lan', 'a', 'unreachable')], null)).toBeNull();
    expect(pickCurrent(null, 'a')).toBeNull();
  });
});

describe('buildRouteRows', () => {
  it('always lists LAN, Tailscale and direct HTTPS in priority order', () => {
    const rows = buildRouteRows({ id: 'h', label: 'PC', address: 'lan:1' }, null, null);
    expect(rows.map((r) => r.route)).toEqual(['lan', 'tailscale', 'directHttps']);
    expect(rows.map((r) => r.state)).toEqual(['checking', 'notConfigured', 'notConfigured']);
  });

  it('marks the current route and keeps latency only for reachable ones', () => {
    const rows = buildRouteRows(host, [ok('lan', host.address, 9), bad('tailscale', host.tailscaleAddress!, 'unreachable')], host.address);
    expect(rows[0]).toMatchObject({ state: 'current', latencyMs: 9 });
    expect(rows[1]).toMatchObject({ state: 'noResponse', latencyMs: undefined });
    expect(rows[2].state).toBe('notConfigured');
  });

  it('maps pin, auth and dns outcomes', () => {
    const h: Host = { ...host, internetAddress: 'files.example.com' };
    const rows = buildRouteRows(h, [bad('lan', h.address, 'pinMismatch'), ok('tailscale', h.tailscaleAddress!, 30, 'denied'), bad('directHttps', 'files.example.com', 'dns')], null);
    expect(rows.map((r) => r.state)).toEqual(['mismatch', 'denied', 'dns']);
  });

  it('shows a reachable non-current route as ready', () => {
    const rows = buildRouteRows(host, [ok('lan', host.address), ok('tailscale', host.tailscaleAddress!)], host.address);
    expect(rows.map((r) => r.state)).toEqual(['current', 'ready', 'notConfigured']);
  });
});

describe('summarize and labels', () => {
  it('summarises the route list', () => {
    expect(summarize(buildRouteRows(host, null, null))).toBe('checking');
    expect(summarize(buildRouteRows(host, [ok('lan', host.address), bad('tailscale', host.tailscaleAddress!, 'unreachable')], null))).toBe('online');
    expect(summarize(buildRouteRows(host, [bad('lan', host.address, 'pinMismatch'), bad('tailscale', host.tailscaleAddress!, 'unreachable')], null))).toBe('attention');
    expect(summarize(buildRouteRows(host, [bad('lan', host.address, 'unreachable'), bad('tailscale', host.tailscaleAddress!, 'unreachable')], null))).toBe('offline');
  });

  it('writes the subtitle from real values only', () => {
    const [lan, ts, direct] = buildRouteRows(host, [ok('lan', host.address, 14)], host.address);
    expect(routeSubtitle(lan)).toBe('192.168.1.2:8765 · 14 ms');
    expect(routeSubtitle({ ...ts, address: null, state: 'notConfigured' })).toBe('Not found on this computer');
    expect(routeSubtitle(direct)).toBe('Not configured');
    expect(stateTone('current')).toBe('safe');
    expect(stateTone('mismatch')).toBe('warn');
    expect(stateTone('ready')).toBe('muted');
  });
});
