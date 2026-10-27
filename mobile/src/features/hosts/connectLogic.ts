import { routeAddresses, type Host, type HostRoute } from '../../core/models/host';
import type { ProbeResult } from './diagnostics';

/** What one connection route looks like right now. `current` is the route requests are going through. */
export type RouteState = 'checking' | 'current' | 'ready' | 'denied' | 'mismatch' | 'noPin' | 'noResponse' | 'dns' | 'error' | 'notConfigured';

export type RouteRow = {
  route: Exclude<HostRoute, 'custom'>;
  title: string;
  /** The authority for this route, or null when the host has none configured. */
  address: string | null;
  state: RouteState;
  latencyMs?: number;
};

const TITLE: Record<RouteRow['route'], string> = { lan: 'Local network', tailscale: 'Tailscale', directHttps: 'Direct HTTPS' };
export const ROUTE_ORDER: RouteRow['route'][] = ['lan', 'tailscale', 'directHttps'];

const isReachable = (r: ProbeResult) => r.failure === 'none';

/** Which address counts as the current route: the one the client is using if it answered, else the first route that did. */
export function pickCurrent(results: ProbeResult[] | null, clientAddress: string | null): string | null {
  if (!results) return null;
  const ok = results.filter(isReachable);
  if (clientAddress && ok.some((r) => r.address === clientAddress)) return clientAddress;
  return ok[0]?.address ?? null;
}

function stateOf(r: ProbeResult, current: string | null): RouteState {
  switch (r.failure) {
    case 'none':
      return r.auth === 'denied' ? 'denied' : r.address === current ? 'current' : 'ready';
    case 'missingPin':
      return 'noPin';
    case 'pinMismatch':
      return 'mismatch';
    case 'dns':
      return 'dns';
    case 'unreachable':
      return 'noResponse';
    default:
      return 'error';
  }
}

/**
 * The three route slots of a host, in connection-priority order (LAN, Tailscale, direct HTTPS), each with what the last probe
 * found. Slots the host has no address for stay in the list as `notConfigured` so the owner can see what is missing.
 * [results] is null while the first probe is running.
 */
export function buildRouteRows(host: Host, results: ProbeResult[] | null, clientAddress: string | null): RouteRow[] {
  const configured = new Map(routeAddresses(host).map((r) => [r.route, r.address]));
  const current = pickCurrent(results, clientAddress);
  return ROUTE_ORDER.map((route): RouteRow => {
    const address = configured.get(route) ?? null;
    const base = { route, title: TITLE[route], address };
    if (address === null) return { ...base, state: 'notConfigured' };
    const r = results?.find((p) => p.address === address);
    if (!r) return { ...base, state: 'checking' };
    return { ...base, state: stateOf(r, current), latencyMs: isReachable(r) ? r.latencyMs : undefined };
  });
}

export type ConnectionSummary = 'checking' | 'online' | 'attention' | 'offline';

/** One word for the top bar: some route works, one works but needs attention, none does, or still checking. */
export function summarize(rows: RouteRow[]): ConnectionSummary {
  if (rows.some((r) => r.state === 'checking')) return 'checking';
  if (rows.some((r) => r.state === 'current' || r.state === 'ready')) return 'online';
  if (rows.some((r) => r.state === 'denied' || r.state === 'mismatch' || r.state === 'noPin')) return 'attention';
  return 'offline';
}

export const STATE_LABEL: Record<RouteState, string> = {
  checking: 'Checking',
  current: 'In use',
  ready: 'Ready',
  denied: 'Access denied',
  mismatch: 'Identity changed',
  noPin: 'No identity pin',
  noResponse: 'No response',
  dns: 'DNS failed',
  error: 'Error',
  notConfigured: 'Set up',
};

export type StateTone = 'safe' | 'warn' | 'muted';
export const stateTone = (s: RouteState): StateTone => (s === 'current' ? 'safe' : s === 'denied' || s === 'mismatch' || s === 'noPin' || s === 'error' ? 'warn' : 'muted');

/** The row subtitle: the address (or why there is none) plus the measured round trip when there is one. */
export function routeSubtitle(row: RouteRow): string {
  if (row.address === null) return row.route === 'tailscale' ? 'Not found on this computer' : 'Not configured';
  return row.latencyMs !== undefined ? `${row.address} · ${row.latencyMs} ms` : row.address;
}
