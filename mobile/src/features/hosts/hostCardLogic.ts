import type { Host, HostRoute } from '../../core/models/host';
import { routeForAddress } from '../../core/models/host';
import { basenameOf } from '../explorer/paths';

/** Which quick actions of a host card are usable right now. Open stays available offline (cached browsing). */
export function hostCardActions({ online, checking }: { online: boolean; checking: boolean }) {
  return {
    open: !checking,
    search: online && !checking,
    apps: online && !checking,
    transfers: true,
  };
}

/** What the Home route strip (Phone --- LAN . active) says, from the real reachability and the address that answered. */
export type RouteStripState = { kind: 'checking' } | { kind: 'offline' } | { kind: 'active'; route: HostRoute };

export function routeStripState(o: { online: boolean; checking: boolean; activeAddress: string | null }, host: Host): RouteStripState {
  if (o.checking) return { kind: 'checking' };
  if (!o.online) return { kind: 'offline' };
  return { kind: 'active', route: routeForAddress(host, o.activeAddress ?? host.address) };
}

/** "Projects . 2.4 MB": the containing folder's name and the size, whichever the listing actually returned. */
export function recentMeta(entry: { path: string; size?: number }, formatSize: (n: number) => string): string {
  const parts = entry.path.split(/[/\\]/).filter(Boolean);
  const parent = parts.length >= 2 ? basenameOf(parts.slice(0, -1).join('/')) : '';
  return [parent, typeof entry.size === 'number' ? formatSize(entry.size) : ''].filter(Boolean).join(' · ');
}
