import type { Entry, Listing } from '../../core/api/models';
import { defaultVisibility } from '../../core/visibility';
import { createDestinationPicker, pickerPath } from './destinationPicker';

const dir = (path: string): Entry => ({ name: path.split('/').pop()!, path, isDir: true, isSymlink: false });
const file = (path: string): Entry => ({ name: path.split('/').pop()!, path, isDir: false, isSymlink: false });

function setup(pages: Record<string, Listing[]>) {
  const calls: string[] = [];
  const gates = new Map<string, () => void>();
  const created: string[] = [];
  const picker = createDestinationPicker({
    hostId: 'h',
    startPath: '/a/b',
    visibility: defaultVisibility,
    humanize: (e) => (e as Error).message,
    getClient: async () => ({
      async list(path, o = {}) {
        calls.push(`${path}@${o.cursor ?? ''}`);
        const gate = gates.get(path);
        if (gate === undefined && gates.has(path)) await new Promise<void>((r) => gates.set(path, r));
        const p = pages[path];
        if (!p) throw new Error(`no ${path}`);
        return p[o.cursor ? Number(o.cursor) : 0];
      },
      async createFolder(path) {
        created.push(path);
        return dir(path);
      },
    }),
  });
  return { picker, calls, created, gates };
}

describe('destination picker', () => {
  it('starts on the given path and lists only visible folders', async () => {
    const { picker } = setup({ '/a/b': [{ path: '/a/b', entries: [dir('/a/b/x'), file('/a/b/f'), dir('/a/b/.hidden')] }] });
    expect(picker.getState().pathStack).toEqual(['/', '/a', '/a/b']);
    await picker.load();
    expect(picker.getState().folders.map((f) => f.name)).toEqual(['x']);
  });

  it('pages, navigates and jumps back through the breadcrumb', async () => {
    const { picker, calls } = setup({
      '/a/b': [
        { path: '/a/b', entries: [dir('/a/b/x')], nextCursor: '1' },
        { path: '/a/b', entries: [dir('/a/b/y')] },
      ],
      '/a/b/x': [{ path: '/a/b/x', entries: [] }],
      '/a': [{ path: '/a', entries: [dir('/a/b')] }],
    });
    await picker.load();
    await picker.loadMore();
    expect(picker.getState().folders.map((f) => f.name)).toEqual(['x', 'y']);
    expect(picker.getState().nextCursor).toBeNull();
    picker.navigate('/a/b/x');
    await new Promise((r) => setTimeout(r, 0));
    expect(pickerPath(picker.getState())).toBe('/a/b/x');
    picker.navigateTo(1);
    await new Promise((r) => setTimeout(r, 0));
    expect(pickerPath(picker.getState())).toBe('/a');
    expect(calls).toEqual(['/a/b@', '/a/b@1', '/a/b/x@', '/a@']);
  });

  it('a stale response for a folder the user left never overwrites the current one', async () => {
    const { picker, gates } = setup({
      '/a/b': [{ path: '/a/b', entries: [dir('/a/b/old')] }],
      '/a': [{ path: '/a', entries: [dir('/a/fresh')] }],
    });
    gates.set('/a/b', undefined as never);
    const slow = picker.load();
    picker.navigateTo(1);
    await new Promise((r) => setTimeout(r, 0));
    gates.get('/a/b')?.();
    await slow;
    expect(picker.getState().folders.map((f) => f.name)).toEqual(['fresh']);
  });

  it('creates a folder in the current directory and reloads', async () => {
    const { picker, created } = setup({ '/a/b': [{ path: '/a/b', entries: [] }] });
    await picker.createFolder('new');
    expect(created).toEqual(['/a/b/new']);
  });

  it('reports a load failure without dropping the path', async () => {
    const { picker } = setup({});
    await picker.load();
    expect(picker.getState().error).toBe('no /a/b');
    expect(pickerPath(picker.getState())).toBe('/a/b');
  });
});
