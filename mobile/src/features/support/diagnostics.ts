import { formatSize } from '../../core/format';
import type { Host } from '../../core/models/host';
import type { AppDefaults } from '../../core/settings/settings';

export type DiagnosticsInput = {
  appName: string;
  version: string;
  build: string | number | undefined;
  platform: string;
  osVersion: string | number;
  locale: string;
  app: Pick<AppDefaults, 'themeMode' | 'dynamicColor' | 'notificationsEnabled' | 'lowDiskThresholdBytes' | 'gridView' | 'density' | 'sort' | 'appLockEnabled'>;
  hosts: readonly Pick<Host, 'label' | 'address' | 'tailscaleAddress' | 'macAddress'>[];
  now: Date;
};

/** Plain-text summary a user can paste into a bug report. Holds no tokens, pins or file names. */
export function buildDiagnostics(d: DiagnosticsInput): string {
  const lines = [
    '=== RFE Diagnostics ===',
    `App: ${d.appName} ${d.version}${d.build === undefined ? '' : `+${d.build}`}`,
    `Platform: ${d.platform} ${d.osVersion}`,
    `Locale: ${d.locale}`,
    '',
    '--- Settings ---',
    `Theme: ${d.app.themeMode}`,
    `Dynamic color: ${d.app.dynamicColor}`,
    `Notifications: ${d.app.notificationsEnabled}`,
    `Low-disk threshold: ${formatSize(d.app.lowDiskThresholdBytes)}`,
    `Grid view: ${d.app.gridView}`,
    `Density: ${d.app.density}`,
    `Sort: ${d.app.sort.field} ${d.app.sort.ascending ? 'asc' : 'desc'}`,
    `App lock: ${d.app.appLockEnabled}`,
    '',
    `--- Hosts (${d.hosts.length}) ---`,
  ];
  for (const h of d.hosts) {
    lines.push(`  ${h.label}`, `    Address: ${h.address}`, `    Tailscale: ${h.tailscaleAddress ?? 'none'}`, `    MAC: ${h.macAddress ?? 'none'}`);
  }
  lines.push('', `Generated: ${d.now.toISOString()}`);
  return lines.join('\n');
}
