import type { Drive } from '../api/models';

/** Used fraction 0..1, or null when the drive has no usable capacity (skip the gauge, never show a misleading bar). */
export function usedFraction(d: Drive): number | null {
  const { totalBytes: total, freeBytes: free } = d;
  if (total == null || total <= 0 || free == null) return null;
  return Math.min(Math.max((total - free) / total, 0), 1);
}

/** Aggregate over drives with real capacity; free is capped at each drive's total (bad agent data can't inflate it). */
export function aggregateUsage(drives: Drive[]): { totalBytes: number; freeBytes: number; usedFraction: number } | null {
  let total = 0;
  let free = 0;
  let any = false;
  for (const d of drives) {
    if (d.totalBytes == null || d.totalBytes <= 0 || d.freeBytes == null) continue;
    any = true;
    total += d.totalBytes;
    free += Math.min(d.freeBytes, d.totalBytes);
  }
  if (!any || total <= 0) return null;
  return { totalBytes: total, freeBytes: free, usedFraction: Math.min(Math.max((total - free) / total, 0), 1) };
}
