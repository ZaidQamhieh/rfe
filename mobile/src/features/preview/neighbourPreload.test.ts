import type { Entry } from '../../core/api/models';
import type { Host } from '../../core/models/host';
import { isCellularOnly, neighbourImages, preloadNeighbours } from './neighbourPreload';

const host = { id: 'h', label: 'PC', address: 'a' } as Host;
const img = (n: string): Entry => ({ name: `${n}.jpg`, path: `/${n}.jpg`, isDir: false, isSymlink: false, mimeType: 'image/jpeg' });
const vid = (n: string): Entry => ({ name: `${n}.mp4`, path: `/${n}.mp4`, isDir: false, isSymlink: false, mimeType: 'video/mp4' });

describe('neighbour selection', () => {
  it('takes only images directly beside the current page', () => {
    const list = [img('a'), vid('b'), img('c'), img('d'), img('e')];
    expect(neighbourImages(list, 2).map((e) => e.name)).toEqual(['d.jpg']);
    expect(neighbourImages(list, 3).map((e) => e.name)).toEqual(['c.jpg', 'e.jpg']);
    expect(neighbourImages(list, 0)).toEqual([]);
    expect(neighbourImages(list, 4).map((e) => e.name)).toEqual(['d.jpg']);
  });
});

describe('isCellularOnly', () => {
  it('needs cellular without Wi-Fi or Ethernet', () => {
    expect(isCellularOnly(['cellular'])).toBe(true);
    expect(isCellularOnly(['cellular', 'vpn'])).toBe(true);
    expect(isCellularOnly(['wifi', 'cellular'])).toBe(false);
    expect(isCellularOnly(['ethernet', 'cellular'])).toBe(false);
    expect(isCellularOnly(['wifi'])).toBe(false);
    expect(isCellularOnly([])).toBe(false);
  });
});

describe('preloadNeighbours', () => {
  const entries = [img('a'), img('b'), img('c')];
  const run = (transports: () => Promise<string[]>, allowCellular = false) => {
    const fetched: string[] = [];
    const p = preloadNeighbours({ host, entries, index: 1, allowCellular, transports, fetchFile: async (_, e) => void fetched.push(e.name) });
    return { p, fetched };
  };

  it('fetches both neighbours on Wi-Fi', async () => {
    const { p, fetched } = run(async () => ['wifi']);
    await p;
    expect(fetched.sort()).toEqual(['a.jpg', 'c.jpg']);
  });
  it('fetches nothing on cellular unless allowed', async () => {
    const off = run(async () => ['cellular']);
    await off.p;
    expect(off.fetched).toEqual([]);
    const on = run(async () => ['cellular'], true);
    await on.p;
    expect(on.fetched.length).toBe(2);
  });
  it('preloads as usual when the network state is unavailable, and swallows fetch failures', async () => {
    const { p, fetched } = run(async () => {
      throw new Error('no api');
    });
    await expect(p).resolves.toHaveLength(2);
    expect(fetched.length).toBe(2);
    await expect(preloadNeighbours({ host, entries, index: 1, allowCellular: false, transports: async () => [], fetchFile: async () => Promise.reject(new Error('x')) })).resolves.toHaveLength(2);
  });
});
