// Port of app/lib/core/models/host.dart. Field names and JSON shape are kept
// identical because the same persisted `rfe_hosts_v1` records are read after a
// same-package upgrade from the Flutter app.

export type HostRoute = 'lan' | 'tailscale' | 'directHttps' | 'custom';

export type HostRouteAddress = { route: HostRoute; address: string };

export type Host = {
  id: string;
  label: string;
  /** Primary authority without scheme, e.g. `192.168.1.20:8765` (LAN). */
  address: string;
  /** Metadata mirror only; trust decisions use the secure-store pin. */
  certFingerprint?: string;
  tailscaleName?: string;
  tailscaleAddress?: string;
  /** Owner-configured public HTTPS authority; scheme is always added by the app. */
  internetAddress?: string;
  macAddress?: string;
  /** Owner-written label shown under the name (e.g. "Home office"); kept on this phone only. */
  note?: string;
};

const OPTIONAL_FIELDS = [
  'certFingerprint',
  'tailscaleName',
  'tailscaleAddress',
  'internetAddress',
  'macAddress',
  'note',
] as const;

/** Parses one stored record; returns null for corrupt entries so one bad record never hides the list. */
export function hostFromJson(json: unknown): Host | null {
  if (typeof json !== 'object' || json === null) return null;
  const j = json as Record<string, unknown>;
  if (typeof j.id !== 'string' || typeof j.label !== 'string' || typeof j.address !== 'string') {
    return null;
  }
  const host: Host = { id: j.id, label: j.label, address: j.address };
  for (const f of OPTIONAL_FIELDS) {
    const v = j[f];
    if (typeof v === 'string') host[f] = v;
  }
  return host;
}

export function hostToJson(host: Host): Record<string, string> {
  const out: Record<string, string> = { id: host.id, label: host.label, address: host.address };
  for (const f of OPTIONAL_FIELDS) {
    const v = host[f];
    if (v !== undefined) out[f] = v;
  }
  return out;
}

function validPort(port: string | undefined): boolean {
  if (port === undefined) return true;
  const n = Number.parseInt(port, 10);
  return Number.isInteger(n) && n >= 1 && n <= 65535 && /^[0-9]+$/.test(port);
}

function validIpv4(address: string): boolean {
  const octets = address.split('.');
  return (
    octets.length === 4 &&
    octets.every((o) => {
      if (o.length === 0 || o.length > 3 || !/^[0-9]+$/.test(o)) return false;
      const v = Number.parseInt(o, 10);
      return v >= 0 && v <= 255;
    })
  );
}

function validIpv6(address: string): boolean {
  if (!address.includes(':')) return false;
  const at = address.indexOf('::');
  const compressed = at >= 0;
  if (compressed && address.indexOf('::', at + 2) >= 0) return false;
  const segments = compressed
    ? [
        ...(at > 0 ? address.slice(0, at).split(':') : []),
        ...(at + 2 < address.length ? address.slice(at + 2).split(':') : []),
      ]
    : address.split(':');
  if (segments.some((s) => s.length === 0)) return false;
  let units = 0;
  for (let i = 0; i < segments.length; i++) {
    const s = segments[i];
    if (s.includes('.')) {
      if (i !== segments.length - 1 || !validIpv4(s)) return false;
      units += 2;
    } else {
      if (s.length > 4 || !/^[0-9A-Fa-f]{1,4}$/.test(s)) return false;
      units += 1;
    }
  }
  return compressed ? units < 8 : units === 8;
}

/**
 * Authority-only syntax for a direct HTTPS route: no scheme, path, credentials
 * or URL components, so the value cannot redirect API requests elsewhere.
 */
export function isValidInternetAddress(value: string): boolean {
  if (value.length === 0 || value !== value.trim()) return false;
  if (/[\s\x00-\x1f\x7f]/.test(value) || /[/\\?#@]/.test(value)) return false;

  const v6 = /^\[([0-9A-Fa-f:.]+)\](?::([0-9]{1,5}))?$/.exec(value);
  if (v6) return validIpv6(v6[1]) && validPort(v6[2]);

  const auth = /^([^:[\]]+)(?::([0-9]{1,5}))?$/.exec(value);
  if (!auth) return false;
  const hostname = auth[1];
  if (hostname.length > 253) return false;
  if (/^[0-9]+(?:\.[0-9]+){3}$/.test(hostname) && !validIpv4(hostname)) return false;
  const labels = hostname.split('.');
  if (
    labels.some(
      (l) => l.length === 0 || l.length > 63 || !/^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(l),
    )
  ) {
    return false;
  }
  return validPort(auth[2]);
}

/** Connection-attempt order: LAN, Tailscale, then direct HTTPS; duplicates keep the highest-priority route. */
export function routeAddresses(host: Host): HostRouteAddress[] {
  const seen = new Set<string>();
  const candidates: HostRouteAddress[] = [{ route: 'lan', address: host.address }];
  if (host.tailscaleAddress) candidates.push({ route: 'tailscale', address: host.tailscaleAddress });
  if (host.internetAddress && isValidInternetAddress(host.internetAddress)) {
    candidates.push({ route: 'directHttps', address: host.internetAddress });
  }
  return candidates.filter((c) => {
    if (c.address.length === 0 || seen.has(c.address)) return false;
    seen.add(c.address);
    return true;
  });
}

export function routeForAddress(host: Host, address: string): HostRoute {
  return routeAddresses(host).find((c) => c.address === address)?.route ?? 'custom';
}

export function baseUrl(address: string): string {
  return `https://${address}/v1`;
}
