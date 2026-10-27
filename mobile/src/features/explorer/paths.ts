// Path helpers ported from explorer_state.dart. Hosts can be POSIX (`/`) or Windows (`C:\`); the
// separator of the path in hand is always preserved (PR-66).

const SEP_RE = /[/\\]/;

/** Final path component (works for both separators). */
export function basenameOf(path: string): string {
  const parts = path.split(SEP_RE).filter(Boolean);
  return parts[parts.length - 1] ?? '';
}

/** `photo.jpg` -> `photo (1).jpg` -> `photo (2).jpg`; dotfiles/extensionless names get the suffix at the end. */
export function dedupedName(name: string, existing: ReadonlySet<string>): string {
  if (!existing.has(name)) return name;
  const dot = name.lastIndexOf('.');
  const hasExt = dot > 0 && dot < name.length - 1;
  const base = hasExt ? name.slice(0, dot) : name;
  const ext = hasExt ? name.slice(dot) : '';
  let n = 1;
  let candidate: string;
  do {
    candidate = `${base} (${n})${ext}`;
    n++;
  } while (existing.has(candidate));
  return candidate;
}

/** Display name for a folder path, or "Root" for a filesystem root. */
export function folderLabel(path: string): string {
  if (path === '/' || /^[A-Za-z]:\\?$/.test(path)) return 'Root';
  const name = basenameOf(path);
  return name === '' ? path : name;
}

/** Breadcrumb label: the root crumb shows the root folder's name (never a lone "/"), the rest their last segment. */
export const crumbLabel = (stack: string[], index: number): string => {
  if (index === 0) return stack[0] ? folderLabel(stack[0]) : '/';
  return stack[index].split(/[/\\]/).pop() ?? '';
};

export function renameDestination(oldPath: string, newName: string): string {
  const sep = oldPath.includes('\\') ? '\\' : '/';
  const idx = oldPath.lastIndexOf(sep);
  const parent = idx <= 0 ? sep : oldPath.slice(0, idx);
  return parent === sep ? `${sep}${newName}` : `${parent}${sep}${newName}`;
}

/** Parent directory of [path], keeping its separator style; the root's parent is the root. */
export function parentDirOf(path: string): string {
  const sep = path.includes('\\') ? '\\' : '/';
  const idx = path.lastIndexOf(sep);
  if (idx <= 0) return sep;
  const parent = path.slice(0, idx);
  // The parent of C:\x is the drive root C:\ (a bare C: means the current folder on that drive).
  return /^[A-Za-z]:$/.test(parent) ? `${parent}${sep}` : parent;
}

export function joinRemotePath(dir: string, name: string): string {
  const sep = dir.includes('\\') ? '\\' : '/';
  if (dir === '' || dir === sep) return `${sep}${name}`;
  return dir.endsWith(sep) ? `${dir}${name}` : `${dir}${sep}${name}`;
}

/** `/home/x/Storage` -> ['/', '/home', '/home/x', '/home/x/Storage']; `C:\a\b` -> ['C:\', 'C:\a', 'C:\a\b']. */
export function buildPathStack(path: string): string[] {
  const win = /^[A-Za-z]:/.exec(path);
  if (win) {
    const parts = path.replaceAll('/', '\\').split('\\').filter(Boolean);
    const stack = [`${parts[0]}\\`];
    let cur = parts[0];
    for (const p of parts.slice(1)) {
      cur = `${cur}\\${p}`;
      stack.push(cur);
    }
    return stack;
  }
  const stack = ['/'];
  let cur = '';
  for (const p of path.split('/').filter(Boolean)) {
    cur = `${cur}/${p}`;
    stack.push(cur);
  }
  return stack;
}

/** Deep-link stack that starts at [rootPath]; a path outside the jail stays at the jail root. */
export function buildPathStackWithinRoot(rootPath: string, path: string): string[] {
  const pathStack = buildPathStack(path);
  const rootStack = buildPathStack(rootPath);
  const root = rootStack[rootStack.length - 1];
  const win = root.includes('\\');
  const idx = pathStack.findIndex((c) => (win ? c.toLowerCase() === root.toLowerCase() : c === root));
  return idx < 0 ? [rootPath] : [rootPath, ...pathStack.slice(idx + 1)];
}
