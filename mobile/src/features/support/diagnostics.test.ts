import { buildDiagnostics, type DiagnosticsInput } from './diagnostics';

const base: DiagnosticsInput = {
  appName: 'RFE',
  version: '2.0.0',
  build: 81,
  platform: 'android',
  osVersion: 35,
  locale: 'en-US',
  app: { themeMode: 'dark', dynamicColor: false, notificationsEnabled: true, lowDiskThresholdBytes: 1024 * 1024 * 1024, gridView: false, density: 'comfortable', sort: { field: 'name', ascending: true } as never, appLockEnabled: true },
  hosts: [{ label: 'pc', address: '10.0.0.2:8765', tailscaleAddress: undefined, macAddress: 'aa:bb' }],
  now: new Date('2026-01-02T03:04:05Z'),
};

test('summarises app, settings and hosts', () => {
  const out = buildDiagnostics(base);
  expect(out).toContain('App: RFE 2.0.0+81');
  expect(out).toContain('Sort: name asc');
  expect(out).toContain('--- Hosts (1) ---');
  expect(out).toContain('    Tailscale: none');
  expect(out).toContain('    MAC: aa:bb');
  expect(out).toContain('Generated: 2026-01-02T03:04:05.000Z');
});

test('omits the build when unknown and never prints secrets', () => {
  const out = buildDiagnostics({ ...base, build: undefined, hosts: [{ ...base.hosts[0], certFingerprint: 'deadbeef', token: 'secret' } as never] });
  expect(out).toContain('App: RFE 2.0.0\n');
  expect(out).not.toMatch(/deadbeef|secret/);
});
