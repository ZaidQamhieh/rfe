import { AgentApiError, AgentClient, type Transport } from '../../core/api/agentClient';
import type { Health } from '../../core/api/models';
import { CertPinMismatch, MissingCertPin, normalizeFingerprint } from '../../core/api/pin';
import { routeAddresses, type Host, type HostRoute } from '../../core/models/host';

export type ProbeFailure = 'none' | 'missingPin' | 'pinMismatch' | 'dns' | 'unreachable' | 'other';
export type AuthOutcome = 'notChecked' | 'accepted' | 'denied';

export type ProbeResult = { route: HostRoute; address: string; latencyMs?: number; health?: Health; failure: ProbeFailure; auth: AuthOutcome };

const DNS_HINTS = ['unable to resolve host', 'no address associated with hostname', 'failed host lookup', 'name or service not known', 'nodename nor servname', 'unknownhost'];

/** Maps a failed request to the layer that broke: pin, DNS, reachability, or something else. */
export function classifyFailure(e: unknown): ProbeFailure {
  if (e instanceof CertPinMismatch) return 'pinMismatch';
  if (e instanceof MissingCertPin) return 'missingPin';
  if (e instanceof AgentApiError && e.code === 'CONNECTION') {
    const detail = String((e.cause as { message?: unknown } | undefined)?.message ?? '').toLowerCase();
    if (DNS_HINTS.some((h) => detail.includes(h))) return 'dns';
    return 'unreachable';
  }
  return 'other';
}

export type ProbeDeps = { transport: Transport; deviceToken: string | null; fingerprint: string | null; timeoutMs?: number; now?: () => number };

/**
 * Checks one address on its own: is the host reachable, does its certificate match the pin (nothing is sent to
 * a host that does not), does it accept this device's token, and how long did the round trip take. The route
 * is isolated by giving the client a host record that has only that address.
 */
export async function probeRoute(d: ProbeDeps, host: Host, route: HostRoute, address: string): Promise<ProbeResult> {
  const pin = normalizeFingerprint(d.fingerprint);
  if (pin === null) return { route, address, failure: 'missingPin', auth: 'notChecked' };
  const single: Host = { id: host.id, label: host.label, address, certFingerprint: pin };
  const client = new AgentClient(single, { transport: d.transport, pinnedFingerprint: pin, deviceToken: d.deviceToken ?? undefined, timeoutMs: d.timeoutMs ?? 5000 });
  const now = d.now ?? Date.now;
  try {
    const started = now();
    const health = await client.health();
    const latencyMs = now() - started;
    let auth: AuthOutcome = 'notChecked';
    try {
      await client.status();
      auth = 'accepted';
    } catch (e) {
      if (e instanceof CertPinMismatch || e instanceof MissingCertPin) throw e;
      // Reachability and pinned TLS are already established; an unexpected auth-check failure stays "unknown".
      if (e instanceof AgentApiError && (e.statusCode === 401 || e.statusCode === 403)) auth = 'denied';
    }
    return { route, address, latencyMs, health, failure: 'none', auth };
  } catch (e) {
    return { route, address, failure: classifyFailure(e), auth: 'notChecked' };
  }
}

/** Probes every route of [host] in parallel, in route-priority order. */
export function probeAll(d: ProbeDeps, host: Host): Promise<ProbeResult[]> {
  return Promise.all(routeAddresses(host).map((r) => probeRoute(d, host, r.route, r.address)));
}
