// Port of features/pairing/lan_discovery.dart validation. Discovery output is
// an untrusted hint: only IPv4 authorities with sane names and ports survive.

export type DiscoveredAgent = { name: string; address: string; port: number; authority: string };

function isIpv4(value: string): boolean {
  const parts = value.split('.');
  return (
    parts.length === 4 &&
    parts.every((p) => p.length > 0 && p.length <= 3 && /^[0-9]+$/.test(p) && Number(p) >= 0 && Number(p) <= 255)
  );
}

export function parseDiscovered(value: unknown): DiscoveredAgent | null {
  if (typeof value !== 'object' || value === null) return null;
  const v = value as Record<string, unknown>;
  if (typeof v.name !== 'string' || typeof v.address !== 'string' || typeof v.port !== 'number') return null;
  const name = v.name.trim();
  const address = v.address.trim();
  if (name === '' || name.length > 128 || !isIpv4(address) || !Number.isInteger(v.port) || v.port < 1 || v.port > 65535) {
    return null;
  }
  return { name, address, port: v.port, authority: `${address}:${v.port}` };
}

/** Validates and de-duplicates (by authority) the raw platform list. */
export function normalizeDiscovered(raw: unknown[]): DiscoveredAgent[] {
  const byAuthority = new Map<string, DiscoveredAgent>();
  for (const item of raw) {
    const a = parseDiscovered(item);
    if (a) byAuthority.set(a.authority, a);
  }
  return [...byAuthority.values()];
}
