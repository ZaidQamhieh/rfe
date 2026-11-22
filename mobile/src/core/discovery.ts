// Port of features/pairing/lan_discovery.dart validation. Discovery output is
// an untrusted hint: only IPv4 authorities with sane names and ports survive.

import { MAX_PORT, MIN_PORT, validIpv4 } from './models/host';

const MAX_DISCOVERED_NAME_LENGTH = 128;

export type DiscoveredAgent = { name: string; address: string; port: number; authority: string };

export function parseDiscovered(value: unknown): DiscoveredAgent | null {
  if (typeof value !== 'object' || value === null) return null;
  const v = value as Record<string, unknown>;
  if (typeof v.name !== 'string' || typeof v.address !== 'string' || typeof v.port !== 'number') return null;
  const name = v.name.trim();
  const address = v.address.trim();
  if (
    name === '' ||
    name.length > MAX_DISCOVERED_NAME_LENGTH ||
    !validIpv4(address) ||
    !Number.isInteger(v.port) ||
    v.port < MIN_PORT ||
    v.port > MAX_PORT
  ) {
    return null;
  }
  return { name, address, port: v.port, authority: `${address}:${v.port}` };
}

/**
 * Adds one computer that has just been found to the list shown so far. A hint that fails validation leaves the very same
 * list; a repeat of an address and port replaces its entry in place; the input list is never changed.
 */
export function mergeDiscovered(current: readonly DiscoveredAgent[], incoming: unknown): DiscoveredAgent[] {
  const a = parseDiscovered(incoming);
  if (!a) return current as DiscoveredAgent[];
  const i = current.findIndex((x) => x.authority === a.authority);
  if (i < 0) return [...current, a];
  return current.map((x, j) => (j === i ? a : x));
}

export function normalizeDiscovered(raw: unknown[]): DiscoveredAgent[] {
  const byAuthority = new Map<string, DiscoveredAgent>();
  for (const item of raw) {
    const a = parseDiscovered(item);
    if (a) byAuthority.set(a.authority, a);
  }
  return [...byAuthority.values()];
}
