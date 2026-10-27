import { AgentApiError } from '../../core/api/agentClient';
import { parseHostAppCatalog } from '../../core/api/models';
import { appIconKey, appMeta, filterApps, launchFailure, platformKey, safeIcon } from './appsLogic';

const apps = [
  { id: 'a', name: 'Firefox', launchable: true, description: 'Web browser' },
  { id: 'b', name: 'Terminal', launchable: true },
];

describe('apps logic', () => {
  it('filters by name or description, ignoring case and padding', () => {
    expect(filterApps(apps, ' fire ').map((a) => a.id)).toEqual(['a']);
    expect(filterApps(apps, 'BROWSER').map((a) => a.id)).toEqual(['a']);
    expect(filterApps(apps, '')).toBe(apps);
    expect(filterApps(apps, 'zzz')).toEqual([]);
  });

  it('maps platforms and icons through allowlists', () => {
    expect(platformKey('darwin')).toBe('hostAppsPlatformMacOS');
    expect(platformKey('plan9')).toBe('hostAppsPlatformUnknown');
    expect(safeIcon('terminal')).toBe('terminal');
    expect(safeIcon('../../etc')).toBe('monitor');
    expect(safeIcon(undefined)).toBe('monitor');
  });

  it('explains launch failures and flags stale entries for a reload', () => {
    expect(launchFailure(new AgentApiError(403, 'FORBIDDEN', 'x'))).toEqual({ key: 'hostAppsLaunchDenied', refresh: false });
    expect(launchFailure(new AgentApiError(404, 'APP_NOT_FOUND', 'x'))).toEqual({ key: 'hostAppNoLongerAvailable', refresh: true });
    expect(launchFailure(new AgentApiError(400, 'BAD_APP_ID', 'x')).refresh).toBe(true);
    expect(launchFailure(new AgentApiError(429, 'APP_LAUNCH_RATE_LIMITED', 'x')).key).toBe('hostAppLaunchRateLimited');
    expect(launchFailure(new AgentApiError(500, 'WHATEVER', 'x')).key).toBe('hostAppLaunchFailed');
    expect(launchFailure(new Error('boom')).key).toBe('hostAppLaunchFailed');
  });

  it('parses a catalog with safe defaults for older agents', () => {
    const c = parseHostAppCatalog({ platform: 'linux', apps: [{ id: 'x', name: 'Gimp' }] });
    expect(c).toEqual({ platform: 'linux', launchAllowed: false, apps: [{ id: 'x', name: 'Gimp', launchable: true, description: undefined, icon: undefined }] });
    expect(() => parseHostAppCatalog({ apps: [{ id: '', name: 'x' }] })).toThrow();
    expect(() => parseHostAppCatalog({})).toThrow();
  });
});

describe('appIconKey and appMeta', () => {
  it('keeps a drawn icon hint and falls back to the category', () => {
    expect(appIconKey({ icon: 'terminal', category: 'Development' })).toBe('terminal');
    expect(appIconKey({ icon: 'firefox', category: 'Internet' })).toBe('browser');
    expect(appIconKey({ category: 'Development' })).toBe('code');
    expect(appIconKey({})).toBe('monitor');
    expect(appIconKey({ category: 'Nonsense' })).toBe('monitor');
  });

  it('writes Category · ready only when the app can be started now', () => {
    expect(appMeta({ launchable: true, category: 'Development' }, true)).toBe('Development · ready');
    expect(appMeta({ launchable: true, category: 'Development' }, false)).toBe('Development');
    expect(appMeta({ launchable: false, category: 'System' }, true)).toBe('System · Launch not supported');
    expect(appMeta({ launchable: false }, true)).toBe('Launch not supported');
    expect(appMeta({ launchable: true }, false)).toBeUndefined();
    expect(appMeta({ launchable: true, category: 'Media', description: 'Plays things' }, true)).toBe('Plays things');
  });
});
