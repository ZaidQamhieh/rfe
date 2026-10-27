// Audit 2026-10-20 (F-67): overlapping collection mutations must not lose updates.
/* eslint-disable @typescript-eslint/no-require-imports -- jest.mock factories and lazy require are the supported pattern here */

jest.mock('../services', () => {
  const kv = new (require('../core/storage/hostStore').MemoryKeyValueStore)();
  const set = kv.set.bind(kv);
  kv.set = async (k: string, v: string) => {
    await new Promise((r) => setTimeout(r, 20)); // a real store is asynchronous
    return set(k, v);
  };
  return { keyValue: kv, listingCache: { clear: jest.fn() } };
});

const run = it;

describe('audit F-67', () => {
  run('two overlapping toggleFavorite calls both persist', async () => {
    const { useCollections } = require('./collections');
    await Promise.all([
      useCollections.getState().toggleFavorite({ hostId: 'h', path: '/a', label: 'A' }),
      useCollections.getState().toggleFavorite({ hostId: 'h', path: '/b', label: 'B' }),
    ]);
    expect(useCollections.getState().favorites.map((f: { path: string }) => f.path).sort()).toEqual(['/a', '/b']);
  });
});
