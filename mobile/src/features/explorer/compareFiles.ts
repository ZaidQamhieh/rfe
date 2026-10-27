import type { Entry } from '../../core/api/models';

export type Verdict = 'same' | 'different' | 'unknown';

/** 'different' when the known sizes differ (no hashing needed); null when only the contents can tell. */
export function sizeVerdict(a: Entry, b: Entry): 'different' | null {
  return a.size != null && b.size != null && a.size !== b.size ? 'different' : null;
}

/** Verdict from a path-to-SHA-256 map. A missing or empty hash is unknown, never a match. */
export function compareVerdict(hashes: Readonly<Record<string, string>>, pathA: string, pathB: string): Verdict {
  const x = hashes[pathA]?.toLowerCase();
  const y = hashes[pathB]?.toLowerCase();
  if (!x || !y) return 'unknown';
  return x === y ? 'same' : 'different';
}
