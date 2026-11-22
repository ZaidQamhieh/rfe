import { routeAddresses, type Host } from '../../core/models/host';

/** The saved computer that already uses [authority] (host:port) as its main, Tailscale or direct address, if any. Case and spaces are ignored. */
export function savedHostAt(hosts: readonly Host[], authority: string): Host | undefined {
  const a = authority.trim().toLowerCase();
  return hosts.find((h) => routeAddresses(h).some((r) => r.address.trim().toLowerCase() === a));
}
