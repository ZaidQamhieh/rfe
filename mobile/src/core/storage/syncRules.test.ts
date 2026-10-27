import { MemoryKeyValueStore } from './hostStore';
import { SYNC_RULES_KEY, SyncRuleStore } from './syncRules';

const rule = { id: 'r1', hostId: 'h1', remotePath: '/docs', localPath: 'content://tree/x', enabled: true };

test('saves, replaces by id, toggles and removes rules', async () => {
  const s = new SyncRuleStore(new MemoryKeyValueStore());
  await s.save(rule);
  await s.save({ ...rule, enabled: false, lastSync: '2026-09-30T10:00:00.000Z' });
  await s.save({ ...rule, id: 'r2' });
  const list = await s.list();
  expect(list).toHaveLength(2);
  expect(list.find((r) => r.id === 'r1')).toMatchObject({ enabled: false, lastSync: '2026-09-30T10:00:00.000Z' });
  await s.remove('r1');
  expect((await s.list()).map((r) => r.id)).toEqual(['r2']);
});

test('reads the Flutter list of JSON strings and skips a corrupt entry', async () => {
  const kv = new MemoryKeyValueStore();
  await kv.set(SYNC_RULES_KEY, JSON.stringify([JSON.stringify({ id: 'a', hostId: 'h', remotePath: '/p', localPath: '/storage/emulated/0/x', lastSync: '2026-01-02T03:04:05.000' }), '{broken', 5, { id: 'b', hostId: 'h', remotePath: '/q', localPath: 'l', enabled: false }]));
  const list = await new SyncRuleStore(kv).list();
  expect(list.map((r) => r.id)).toEqual(['a', 'b']);
  expect(list[0].enabled).toBe(true);
  expect(list[1].enabled).toBe(false);
});

test('writes the Flutter encoding and drops a forgotten computer\'s rules', async () => {
  const kv = new MemoryKeyValueStore();
  const s = new SyncRuleStore(kv);
  await s.save(rule);
  await s.save({ ...rule, id: 'r2', hostId: 'h2' });
  expect(JSON.parse((await kv.get(SYNC_RULES_KEY))!).every((e: unknown) => typeof e === 'string')).toBe(true);
  await s.removeForHost('h1');
  expect((await s.list()).map((r) => r.id)).toEqual(['r2']);
});
