import { normalizeFingerprint } from '../api/pin';
import type { Host } from './host';

const MERGEABLE_FIELDS = ['tailscaleName', 'tailscaleAddress', 'internetAddress', 'macAddress', 'note'] as const;
const PLAIN_ADDRESS = /^\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?$/;

/** True when a label is only an address (what a computer is named before the owner renames it), so a real name is nicer. */
function isAddressLabel(label: string, host: Host): boolean {
  const l = label.trim().toLowerCase();
  if (l === '') return true;
  if (PLAIN_ADDRESS.test(l)) return true;
  return [host.address, host.tailscaleAddress, host.internetAddress].some((a) => a !== undefined && a.trim().toLowerCase() === l);
}

/**
 * Folds saved entries that are the same computer (same certificate) into one. The newest entry, the one listed last,
 * survives with its id (its login token is stored under that id) and address; it takes the nicest name among the copies
 * (a real name over a bare address, else its own) and fills any field it lacks from the older copies. Entries without a
 * certificate are never merged. Returns the new list in the original order and the ids that were dropped.
 */
export function mergeDuplicateHosts(hosts: readonly Host[]): { hosts: Host[]; dropped: string[] } {
  const groups = new Map<string, Host[]>();
  for (const h of hosts) {
    const fp = normalizeFingerprint(h.certFingerprint);
    if (fp === null) continue;
    groups.set(fp, [...(groups.get(fp) ?? []), h]);
  }
  const survivors = new Map<string, Host>();
  const dropped: string[] = [];
  for (const group of groups.values()) {
    if (group.length < 2) continue;
    const newestFirst = [...group].reverse();
    const keep = { ...newestFirst[0] };
    for (const f of MERGEABLE_FIELDS) {
      const from = newestFirst.find((h) => h[f] !== undefined);
      if (keep[f] === undefined && from) keep[f] = from[f];
    }
    if (isAddressLabel(keep.label, keep)) {
      const nicer = newestFirst.find((h) => !isAddressLabel(h.label, h));
      if (nicer) keep.label = nicer.label;
    }
    survivors.set(keep.id, keep);
    for (const h of group) if (h.id !== keep.id) dropped.push(h.id);
  }
  if (dropped.length === 0) return { hosts: [...hosts], dropped };
  const gone = new Set(dropped);
  return { hosts: hosts.filter((h) => !gone.has(h.id)).map((h) => survivors.get(h.id) ?? h), dropped };
}
