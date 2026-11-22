import { routeAddresses, type Host, type HostRoute } from '../../core/models/host';
import { t } from '../../i18n';
import type { ProbeResult } from './diagnostics';
import { relativeLabel } from './relative';

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

const TITLE: Record<RouteRow['route'], string> = { lan: t('routeLocalNetworkName'), tailscale: t('routeTailscaleName'), directHttps: t('routeInternetName') };
const ROUTE_ORDER: RouteRow['route'][] = ['lan', 'tailscale', 'directHttps'];

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

export type ConnectHeadline = {
  /** Lamp and short phrase for the nameplate. */
  lamp: 'go' | 'wait';
  phrase: string;
  /** The sentence above the buttons, and an optional second sentence under it. */
  title: string;
  body: string | null;
  /** True when no route works right now (nothing answers, or one answers but is refused). */
  lost: boolean;
  retryLabel: string;
  detailsLabel: string;
};

/**
 * What the top of the Connect screen says, in plain words, from the one-word [summary] of the routes: ready, still
 * checking, needing attention, or lost (with when the computer last answered, if ever). Never states a time it does not know.
 */
export function connectHeadline(summary: ConnectionSummary, name: string, lastSeen: Date | null, now: Date = new Date()): ConnectHeadline {
  if (summary === 'online') return { lamp: 'go', phrase: t('connectReady'), title: t('connectReadyTitle', { name }), body: null, lost: false, retryLabel: t('connectCheckAgain'), detailsLabel: t('detailsButton') };
  if (summary === 'checking') return { lamp: 'wait', phrase: t('checkingStatus'), title: t('connectCheckingTitle', { name }), body: null, lost: false, retryLabel: t('connectCheckAgain'), detailsLabel: t('detailsButton') };
  const buttons = { lost: true, retryLabel: t('giTryNow'), detailsLabel: t('giCheckConnection') };
  if (summary === 'attention') return { lamp: 'wait', phrase: t('connectAttention'), title: t('connectAttentionTitle', { name }), body: t('connectAttentionBody'), ...buttons };
  const since = lastSeen ? relativeLabel(lastSeen, now) : null;
  return {
    lamp: 'wait',
    phrase: since ? t('statusOfflineLastSeen', { relative: since }) : t('offlineStatus'),
    title: since ? t('connectLostSince', { name, relative: since }) : t('connectStopped', { name }),
    body: t('giLostBody'),
    ...buttons,
  };
}

export const STATE_LABEL: Record<RouteState, string> = {
  checking: t('trustChecking'),
  current: t('routeInUse'),
  ready: t('routeReady'),
  denied: t('routeAccessDenied'),
  mismatch: t('routeIdentityChanged'),
  noPin: t('routeNoIdentityPin'),
  noResponse: t('probeNoResponseBadge'),
  dns: t('probeDnsFailedBadge'),
  error: t('probeError'),
  notConfigured: t('routeSetUp'),
};

export type StateTone = 'safe' | 'warn' | 'muted';
export const stateTone = (s: RouteState): StateTone => (s === 'current' ? 'safe' : s === 'denied' || s === 'mismatch' || s === 'noPin' || s === 'error' ? 'warn' : 'muted');

/** The row subtitle: the address (or why there is none) plus the measured round trip when there is one. */
export function routeSubtitle(row: RouteRow): string {
  if (row.address === null) return row.route === 'tailscale' ? t('routeNotFoundOnComputer') : t('routeNotConfigured');
  return row.latencyMs !== undefined ? `${row.address} · ${row.latencyMs} ms` : row.address;
}
