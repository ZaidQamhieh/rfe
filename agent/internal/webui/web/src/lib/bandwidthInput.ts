// Parses a bandwidth limit field. Empty means "unlimited" (0); anything that is not a
// non-negative whole number is rejected (null) instead of silently becoming 0 = unlimited.
export function parseBandwidthLimit(input: string): number | null {
  const text = input.trim();
  if (text === '') return 0;
  if (!/^\d+$/.test(text)) return null;
  const value = Number(text);
  return Number.isSafeInteger(value) ? value : null;
}
