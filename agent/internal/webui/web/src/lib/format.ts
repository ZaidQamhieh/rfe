// Display formatters shared by the pages. No imports: tests/format.test.ts runs this file under Node directly.

/** Bytes as B, KB, MB, GB or TB; one decimal above bytes, 0 shows as "0 B". A value that rounds up to 1024 moves to the next unit. */
export function formatBytes(n: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  // Compare the number as it will be shown, so 1048575 B reads "1.0 MB" and never "1024.0 KB".
  const shown = () => (i === 0 ? v : Number(v.toFixed(1)));
  while (shown() >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

/** A unix time as "just now", "5m ago", "3h ago" or "2d ago". */
export function formatAgo(unixSeconds: number): string {
  const s = Date.now() / 1000 - unixSeconds;
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

/** A transfer rate as MB/s or KB/s. `base` is 1000 for decimal units (Overview) or 1024 for binary (Settings). */
export function rateParts(bytesPerSec: number, base: 1000 | 1024): { value: string; unit: 'MB/s' | 'KB/s' } {
  const mega = base * base;
  if (bytesPerSec >= mega) return { value: (bytesPerSec / mega).toFixed(1), unit: 'MB/s' };
  return { value: (bytesPerSec / base).toFixed(0), unit: 'KB/s' };
}

/** Bytes as decimal gigabytes (1 GB = 1e9 bytes); callers pick the number of decimals. */
export const bytesToGB = (n: number): number => n / 1e9;

/** Bytes as whole decimal gigabytes, the form the Overview and Devices pages show. */
export const wholeGB = (n: number): string => bytesToGB(n).toFixed(0);

/** Bytes in use on a disk: total minus free. */
export const usedBytesOf = (status: { totalBytes: number; freeBytes: number }): number => status.totalBytes - status.freeBytes;

/** A log line's timestamp as the local time of day; an unparseable timestamp is shown as sent. */
export function formatLogTime(ts: string): string {
  const t = new Date(ts);
  return Number.isNaN(t.getTime()) ? ts : t.toLocaleTimeString();
}
