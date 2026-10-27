import { AgentApiError } from '../../core/api/agentClient';
import type { HostApp } from '../../core/api/models';
import type { StringKey } from '../../i18n';

/** Case-insensitive match on name or description. */
export function filterApps(apps: HostApp[], query: string): HostApp[] {
  const q = query.trim().toLowerCase();
  if (q === '') return apps;
  return apps.filter((a) => a.name.toLowerCase().includes(q) || (a.description?.toLowerCase().includes(q) ?? false));
}

export function platformKey(platform: string): StringKey {
  switch (platform) {
    case 'windows':
      return 'hostAppsPlatformWindows';
    case 'linux':
      return 'hostAppsPlatformLinux';
    case 'darwin':
      return 'hostAppsPlatformMacOS';
    default:
      return 'hostAppsPlatformUnknown';
  }
}

export type IconKey = 'browser' | 'code' | 'office' | 'media' | 'terminal' | 'system' | 'monitor';
const ICON_KEYS: ReadonlySet<string> = new Set<IconKey>(['browser', 'code', 'office', 'media', 'terminal', 'system']);

/** The agent's icon hint goes through an allowlist; anything else is the generic monitor. */
export function safeIcon(icon: string | undefined): IconKey {
  return icon !== undefined && ICON_KEYS.has(icon) ? (icon as IconKey) : 'monitor';
}

/** Why a launch failed, and whether the catalog should be reloaded because the entry is stale. */
export function launchFailure(error: unknown): { key: StringKey; refresh: boolean } {
  if (!(error instanceof AgentApiError)) return { key: 'hostAppLaunchFailed', refresh: false };
  if (error.statusCode === 403) return { key: 'hostAppsLaunchDenied', refresh: false };
  switch (error.code) {
    case 'APP_NOT_LAUNCHABLE':
      return { key: 'hostAppCannotRun', refresh: false };
    case 'APP_LAUNCH_BUSY':
      return { key: 'hostAppLaunchBusy', refresh: false };
    case 'APP_LAUNCH_RATE_LIMITED':
      return { key: 'hostAppLaunchRateLimited', refresh: false };
    case 'NO_INTERACTIVE_SESSION':
      return { key: 'hostAppNoInteractiveSession', refresh: false };
    case 'APP_LAUNCH_UNAVAILABLE':
      return { key: 'hostAppLauncherUnavailable', refresh: false };
    case 'APP_NOT_FOUND':
      return { key: 'hostAppNoLongerAvailable', refresh: true };
    case 'BAD_APP_ID':
      return { key: 'hostAppInvalidEntry', refresh: true };
    case 'APP_CATALOG_UNSUPPORTED':
      return { key: 'hostAppsUnsupported', refresh: false };
    default:
      return { key: 'hostAppLaunchFailed', refresh: false };
  }
}

/** The tile icon: the agent's icon hint when it is one we draw, else one chosen from the app's category, else the generic monitor. */
export function appIconKey(app: Pick<HostApp, 'icon' | 'category'>): IconKey {
  const hinted = safeIcon(app.icon);
  if (hinted !== 'monitor') return hinted;
  switch (app.category) {
    case 'Development':
      return 'code';
    case 'Internet':
      return 'browser';
    case 'Office':
      return 'office';
    case 'Media':
    case 'Graphics':
    case 'Games':
      return 'media';
    case 'System':
    case 'Utilities':
      return 'system';
    default:
      return 'monitor';
  }
}

/** Second line of an app row: its description, else "Category · ready" (ready means it can be started now). */
export function appMeta(app: Pick<HostApp, 'description' | 'category' | 'launchable'>, launchAllowed: boolean): string | undefined {
  if (app.description) return app.description;
  if (!app.launchable) return app.category ? `${app.category} · Launch not supported` : 'Launch not supported';
  return [app.category, launchAllowed ? 'ready' : undefined].filter(Boolean).join(' · ') || undefined;
}
