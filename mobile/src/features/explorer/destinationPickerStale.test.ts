import type { Entry, Listing } from '../../core/api/models';
import { defaultVisibility } from '../../core/visibility';
import { createDestinationPicker } from './destinationPicker';

const dir = (path: string): Entry => ({ name: path.split('/').pop()!, path, isDir: true, isSymlink: false });

/** A picker whose list calls the test releases by hand: `gate(path)` returns the function that lets that call finish. */
function setup(pages: Record<string, Listing[] | Error>) {
  const waiting: { path: string; cursor?: string; release: () => void }[] = [];
  const picker = createDestinationPicker({
    hostId: 'h',
    startPath: '/a',
    visibility: defaultVisibility,
    humanize: (x) => (x as Error).message,
    getClient: async () => ({
      async list(path, o = {}) {
        await new Promise<void>((release) => waiting.push({ path, cursor: o.cursor, release }));
        const p = pages[path];
        if (p instanceof Error) throw p;
        return p[o.cursor ? Number(o.cursor) : 0];
      },
      async createFolder(path) {
        return dir(path);
      },
    }),
  });
  const finish = async (path: string, cursor?: string) => {
    const find = () => waiting.findIndex((w) => w.path === path && w.cursor === cursor);
    for (let n = 0; n < 20 && find() < 0; n++) await new Promise((r) => setTimeout(r, 0));
    const i = find();
    waiting.splice(i, 1)[0].release();
    await new Promise((r) => setTimeout(r, 0));
  };
  return { picker, finish };
}

describe('destination picker navigation', () => {
  it('does not show the previous folder\'s rows under the new path, so a load error is visible', async () => {
    const { picker, finish } = setup({ '/a': [{ path: '/a', entries: [dir('/a/x')] }], '/a/x': new Error('nope') });
    const first = picker.load();
    await finish('/a');
    await first;
    expect(picker.getState().folders.map((f) => f.name)).toEqual(['x']);

    picker.navigate('/a/x');
    expect(picker.getState().folders).toEqual([]);
    await finish('/a/x');
    expect(picker.getState()).toMatchObject({ folders: [], error: 'nope', loading: false });
  });

  it('clears loadingMore when navigation supersedes a page fetch, so the next folder can page again', async () => {
    const { picker, finish } = setup({
      '/a': [
        { path: '/a', entries: [dir('/a/x')], nextCursor: '1' },
        { path: '/a', entries: [dir('/a/y')] },
      ],
      '/a/x': [
        { path: '/a/x', entries: [dir('/a/x/p')], nextCursor: '1' },
        { path: '/a/x/q', entries: [dir('/a/x/q')] },
      ],
    });
    const first = picker.load();
    await finish('/a');
    await first;
    const more = picker.loadMore();
    expect(picker.getState().loadingMore).toBe(true);

    picker.navigate('/a/x');
    await finish('/a', '1');
    await more;
    await finish('/a/x');
    expect(picker.getState().loadingMore).toBe(false);

    const again = picker.loadMore();
    await finish('/a/x', '1');
    await again;
    expect(picker.getState().folders.map((f) => f.name)).toEqual(['p', 'q']);
  });
});
