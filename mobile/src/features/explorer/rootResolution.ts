import type { AgentStatus, Health } from '../../core/api/models';

/** Deepest allowed root containing [path] (separator- and, on Windows, case-insensitive), or null. Port of allowedRootForPath. */
export function allowedRootForPath(roots: string[], path: string, windows = false): string | null {
  const normalize = (value: string) => {
    let r = value.replaceAll('\\', '/');
    while (r.length > 1 && r.endsWith('/') && !/^[A-Za-z]:\/$/.test(r)) r = r.slice(0, -1);
    return windows ? r.toLowerCase() : r;
  };
  const candidate = normalize(path);
  let best: string | null = null;
  let bestLen = -1;
  for (const root of roots) {
    const nr = normalize(root);
    const prefix = nr.endsWith('/') ? nr : `${nr}/`;
    const inside = candidate === nr || (nr === '/' ? candidate.startsWith('/') : candidate.startsWith(prefix));
    if (inside && nr.length > bestLen) {
      best = root;
      bestLen = nr.length;
    }
  }
  return best;
}

const windowsDriveRoot = (path: string) => {
  const m = /^([A-Za-z]:)[\\/]/.exec(path);
  return m ? `${m[1]}\\` : null;
};

export type RootResolution =
  | { kind: 'denied' }
  | { kind: 'select'; rootPath: string; initialPath?: string }
  | { kind: 'roots'; roots: string[]; unavailablePath?: string }
  | { kind: 'drives' };

/** What the Files tab should show for a host: no access, straight into a root, a shared-folder picker, or a Windows drive list. */
export function resolveRoots(settings: Pick<AgentStatus, 'roots' | 'accessDenied'>, health: Pick<Health, 'os'> | null, initialPath?: string): RootResolution {
  if (settings.accessDenied) return { kind: 'denied' };
  const os = health?.os.toLowerCase() ?? '';
  const winPath = (p: string) => /^[A-Za-z]:[\\/]/.test(p);
  const isWindows = os === 'windows' || (initialPath !== undefined && winPath(initialPath)) || settings.roots.some(winPath);

  if (initialPath !== undefined) {
    const root = settings.roots.length === 0 ? (isWindows ? windowsDriveRoot(initialPath) : '/') : allowedRootForPath(settings.roots, initialPath, isWindows);
    if (root !== null) return { kind: 'select', rootPath: root, initialPath };
  }
  if (settings.roots.length > 0) return { kind: 'roots', roots: settings.roots, unavailablePath: initialPath };
  if (isWindows) return { kind: 'drives' };
  return { kind: 'select', rootPath: '/', initialPath };
}
