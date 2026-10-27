import { normalizeFingerprint } from '../../core/api/pin';
import type { Host } from '../../core/models/host';

// Port of handoff/qr_scan_screen.dart and qr_generate_screen.dart. The QR carries only {certFingerprint, path, name}: no
// token and no address, because the receiving phone is separately paired to the same computer.

export type HandoffPayload = { certFingerprint: string; path: string; name: string };

/** True when [name] is a single, safe local file name: no separators, control characters or Windows-reserved punctuation. */
export function isSafeLocalName(name: string): boolean {
  if (name.length === 0 || name.length > 255) return false;
  if (name === '.' || name === '..') return false;
  return !/[\x00-\x1f\\/:*?"<>|]/.test(name);
}

export const encodeHandoff = (p: HandoffPayload): string => JSON.stringify({ certFingerprint: p.certFingerprint, path: p.path, name: p.name });

/** Parses scanned QR text, or null when it is not a hand-off payload (bad JSON, missing field, unsafe file name). */
export function parseHandoff(raw: string): HandoffPayload | null {
  try {
    const j = JSON.parse(raw) as Record<string, unknown>;
    if (typeof j !== 'object' || j === null) return null;
    const { certFingerprint, path, name } = j;
    if (typeof certFingerprint !== 'string' || typeof path !== 'string' || typeof name !== 'string') return null;
    return isSafeLocalName(name) ? { certFingerprint, path, name } : null;
  } catch {
    return null;
  }
}

/** The paired host whose secure-store pin equals [certFingerprint], i.e. the computer the sender is paired to. */
export function matchHandoffHost(hosts: readonly { host: Host; pin: string | null }[], certFingerprint: string): Host | null {
  const expected = normalizeFingerprint(certFingerprint);
  if (expected === null) return null;
  return hosts.find((h) => normalizeFingerprint(h.pin) === expected)?.host ?? null;
}
