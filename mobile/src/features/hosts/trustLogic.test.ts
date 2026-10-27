import type { AgentStatus, Device } from '../../core/api/models';
import { buildPermissions, identityState } from './trustLogic';

type S = Pick<AgentStatus, 'fileCapabilities' | 'readOnly' | 'allowSharing' | 'accessDenied'>;
const all = (v: boolean) => ({ browse: v, download: v, upload: v, modify: v, delete: v, share: v });
const status = (o: Partial<S> = {}): S => ({ fileCapabilities: all(true), readOnly: false, allowSharing: true, accessDenied: false, ...o });
const device = (o: Partial<Device> = {}): Device => ({ id: 'd', label: 'Phone', created: 0, lastSeen: 0, revoked: false, current: true, viaLogin: false, lastAddress: '', lastVersion: '', jailRoot: '', ...o });
const map = (rows: ReturnType<typeof buildPermissions>) => Object.fromEntries(rows.map((r) => [r.key, r.allowed]));

describe('buildPermissions', () => {
  it('lists the six file grants and both app grants', () => {
    expect(buildPermissions(status(), device({ viewApps: true, launchApps: true })).map((r) => r.key)).toEqual(['browse', 'download', 'upload', 'modify', 'delete', 'share', 'viewApps', 'launchApps']);
  });

  it('uses the host answer for this device', () => {
    const m = map(buildPermissions(status({ fileCapabilities: { ...all(false), browse: true, download: true } }), device({ viewApps: false, launchApps: false })));
    expect(m).toMatchObject({ browse: true, download: true, upload: false, delete: false, viewApps: false, launchApps: false });
  });

  it('falls back to the device record, and owners signed in by password may do everything', () => {
    expect(map(buildPermissions(status({ fileCapabilities: undefined }), device({ browse: true, download: false, upload: false, modify: false, delete: false, share: false })))).toMatchObject({ browse: true, download: false });
    expect(map(buildPermissions(status({ fileCapabilities: undefined }), device({ viaLogin: true })))).toMatchObject({ upload: true, share: true });
  });

  it('reports unknown when the host did not say', () => {
    const m = map(buildPermissions(status({ fileCapabilities: undefined }), device()));
    expect(m).toMatchObject({ browse: null, viewApps: null, launchApps: null });
  });

  it('uses the apps endpoint answer when the device record has no app grants, and ignores it when it has', () => {
    expect(map(buildPermissions(status(), device(), { view: true, launch: false }))).toMatchObject({ viewApps: true, launchApps: false });
    expect(map(buildPermissions(status(), device(), { view: false, launch: false }))).toMatchObject({ viewApps: false, launchApps: false });
    expect(map(buildPermissions(status(), device(), null))).toMatchObject({ viewApps: null, launchApps: null });
    expect(map(buildPermissions(status(), device({ viewApps: false, launchApps: false }), { view: true, launch: true }))).toMatchObject({ viewApps: false, launchApps: false });
  });

  it('never shows launching as allowed without the app list', () => {
    expect(map(buildPermissions(status(), device({ viewApps: false, launchApps: true }))).launchApps).toBe(false);
  });

  it('lets read-only mode, disabled share links and a folder lock-out override a grant', () => {
    const ro = buildPermissions(status({ readOnly: true }), device());
    expect(ro.find((r) => r.key === 'modify')).toMatchObject({ allowed: false, note: expect.stringContaining('read-only') });
    expect(ro.find((r) => r.key === 'browse')?.allowed).toBe(true);
    expect(buildPermissions(status({ allowSharing: false }), device()).find((r) => r.key === 'share')?.allowed).toBe(false);
    expect(map(buildPermissions(status({ accessDenied: true }), device()))).toMatchObject({ browse: false, download: false });
  });
});

describe('identityState', () => {
  it('combines the stored pin with the outcome of the last call', () => {
    expect(identityState({ pinned: false, outcome: 'ok' })).toBe('unpinned');
    expect(identityState({ pinned: null, outcome: 'loading' })).toBe('checking');
    expect(identityState({ pinned: true, outcome: 'ok' })).toBe('verified');
    expect(identityState({ pinned: true, outcome: 'pinMismatch' })).toBe('mismatch');
    expect(identityState({ pinned: true, outcome: 'failed' })).toBe('unreachable');
  });
});
