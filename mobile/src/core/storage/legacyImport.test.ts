import { HOSTS_KEY, MemoryKeyValueStore } from './hostStore';
import { IMPORT_DONE_KEY, importLegacyState } from './legacyImport';
import { MemorySecureStore, SecureKeys } from '../security/secureStore';

const PIN = 'ef'.repeat(32);
const h1 = { id: 'h1', label: 'A', address: 'a:1', certFingerprint: 'a'.repeat(64) };
const h2 = { id: 'h2', label: 'B', address: 'b:1' };
const legacy = { [HOSTS_KEY]: JSON.stringify([JSON.stringify(h1), JSON.stringify(h2), 'garbage']) };

describe('importLegacyState without saved computers', () => {
  it('still carries over settings and favorites', async () => {
    const kv = new MemoryKeyValueStore();
    const prefs = { 'app.themeMode': '"dark"', bookmarks_v1: '["{}"]', unrelated: '1' };
    const r = await importLegacyState(prefs, kv, new MemorySecureStore());
    expect(r).toEqual({ status: 'imported', hosts: 0, needRepair: [], skippedRecords: 0 });
    expect(kv.data.get('app.themeMode')).toBe('"dark"');
    expect(kv.data.get('bookmarks_v1')).toBe('["{}"]');
    expect(kv.data.has('unrelated')).toBe(false);
    expect(kv.data.has(HOSTS_KEY)).toBe(false);
  });
});

describe('importLegacyState', () => {
  it('imports hosts, flags those without secure pin/token for re-pair, never trusts the metadata mirror', async () => {
    const kv = new MemoryKeyValueStore();
    const secure = new MemorySecureStore();
    await secure.write(SecureKeys.fingerprint('h1'), PIN);
    await secure.write(SecureKeys.token('h1'), 'tok');
    const r = await importLegacyState(legacy, kv, secure);
    expect(r).toEqual({ status: 'imported', hosts: 2, needRepair: ['h2'], skippedRecords: 1 });
    expect(JSON.parse(kv.data.get(HOSTS_KEY)!).map((h: { id: string }) => h.id)).toEqual(['h1', 'h2']);
  });

  it('is idempotent and does not overwrite existing RN hosts', async () => {
    const kv = new MemoryKeyValueStore();
    const secure = new MemorySecureStore();
    await kv.set(HOSTS_KEY, JSON.stringify([{ id: 'h1', label: 'RN-owned', address: 'x:9' }]));
    await importLegacyState(legacy, kv, secure);
    const hosts = JSON.parse(kv.data.get(HOSTS_KEY)!);
    expect(hosts.find((h: { id: string }) => h.id === 'h1').label).toBe('RN-owned');
    expect((await importLegacyState(legacy, kv, secure)).status).toBe('already-done');
  });

  it.each(['{not json', '{"a":1}', 'null'])('a corrupt stored host list (%s) still lets the import finish and mark itself done (F-137)', async (corrupt) => {
    const kv = new MemoryKeyValueStore();
    await kv.set(HOSTS_KEY, corrupt);
    const r = await importLegacyState(legacy, kv, new MemorySecureStore());
    expect(r.status).toBe('imported');
    expect(JSON.parse(kv.data.get(HOSTS_KEY)!).map((h: { id: string }) => h.id)).toEqual(['h1', 'h2']);
    expect(kv.data.get(IMPORT_DONE_KEY)).toBe('1');
  });

  it('a null entry in the stored list does not abort the import', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(HOSTS_KEY, JSON.stringify([null, { id: 'h9', label: 'Z', address: 'z:1' }]));
    expect((await importLegacyState(legacy, kv, new MemorySecureStore())).status).toBe('imported');
    expect(kv.data.get(IMPORT_DONE_KEY)).toBe('1');
  });

  it('handles a fresh install with nothing to import', async () => {
    const kv = new MemoryKeyValueStore();
    expect((await importLegacyState({}, kv, new MemorySecureStore())).status).toBe('nothing-to-import');
    expect(kv.data.get(IMPORT_DONE_KEY)).toBe('1');
  });

  it('copies no secrets anywhere', async () => {
    const kv = new MemoryKeyValueStore();
    const secure = new MemorySecureStore();
    await secure.write(SecureKeys.token('h1'), 'SECRET-TOKEN');
    await secure.write(SecureKeys.fingerprint('h1'), PIN);
    await importLegacyState(legacy, kv, secure);
    expect(JSON.stringify([...kv.data.values()])).not.toContain('SECRET-TOKEN');
  });

  it('carries over numeric last-seen timestamps and ignores junk', async () => {
    const kv = new MemoryKeyValueStore();
    await importLegacyState({ ...legacy, rfe_last_seen_h1: '1780000000000', rfe_last_seen_h2: '"x"' }, kv, new MemorySecureStore());
    expect(kv.data.get('rfe_last_seen_h1')).toBe('1780000000000');
    expect(kv.data.has('rfe_last_seen_h2')).toBe(false);
  });

  it('carries over settings and feature keys under the owned prefixes verbatim, without overwriting', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set('app.gridView', 'false');
    await importLegacyState(
      { ...legacy, 'app.gridView': 'true', 'app.sortField': '"size"', 'settings.deviceOverrides.v1': '"{}"', 'host.x.y': '1', 'other.key': '1', rfe_favorites_v1: '["a"]' },
      kv,
      new MemorySecureStore(),
    );
    expect(kv.data.get('app.gridView')).toBe('false');
    expect(kv.data.get('app.sortField')).toBe('"size"');
    expect(kv.data.get('settings.deviceOverrides.v1')).toBe('"{}"');
    expect(kv.data.get('rfe_favorites_v1')).toBe('["a"]');
    expect(kv.data.has('other.key')).toBe(false);
  });
});
