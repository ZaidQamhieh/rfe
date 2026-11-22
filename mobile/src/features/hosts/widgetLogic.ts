import type { Drive } from '../../core/api/models';
import { formatSize } from '../../core/format';

export type WidgetSnapshot = { title: string; detail: string; percent: number };

/** What the home-screen storage widget shows for one computer: the sum over its drives with known sizes; "not reported" when it has drives but none with a size; null with no drives at all. */
export function widgetSnapshot(label: string, drives: Drive[]): WidgetSnapshot | null {
  let total = 0;
  let free = 0;
  for (const d of drives) {
    if (typeof d.totalBytes !== 'number' || typeof d.freeBytes !== 'number' || d.totalBytes <= 0) continue;
    total += d.totalBytes;
    free += Math.min(d.freeBytes, d.totalBytes);
  }
  if (total <= 0) return drives.length > 0 ? { title: label, detail: 'Storage size not reported', percent: 0 } : null;
  const percent = Math.round(((total - free) / total) * 100);
  return { title: label, detail: `${formatSize(free)} free of ${formatSize(total)}`, percent };
}
