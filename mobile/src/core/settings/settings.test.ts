import { MemoryKeyValueStore } from '../storage/hostStore';
import { K, SettingsRepo, defaultAppDefaults, loadSettings, resolveVisibility, resolveView } from './settings';

describe('settings compatible with the Flutter storage layout', () => {
  it('reads Flutter-written values (JSON text; string sets are JSON lists inside strings)', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(K.gridView, 'true');
    await kv.set(K.sortField, '"size"');
    await kv.set(K.sortAscending, 'false');
    await kv.set(K.density, '"compact"');
    await kv.set(K.visHideDotfiles, 'false');
    await kv.set(K.visHiddenExtensions, JSON.stringify('["log","tmp"]'));
    await kv.set(K.lowDiskThreshold, '5368709120');
    await kv.set(K.themeMode, '"dark"');
    await kv.set(K.overrides, JSON.stringify(JSON.stringify({ h1: { gridView: true, sortField: 'date', sortAscending: false, visibility: { hideDotfiles: false, hiddenExtensions: ['x'], hiddenNames: [] } }, h2: {} })));
    const s = await loadSettings(kv);
    expect(s.app).toMatchObject({ gridView: true, density: 'compact', sort: { field: 'size', ascending: false }, themeMode: 'dark', lowDiskThresholdBytes: 5368709120 });
    expect([...s.app.visibility.hiddenExtensions]).toEqual(['log', 'tmp']);
    expect(s.app.visibility.hideDotfiles).toBe(false);
    expect(Object.keys(s.overrides)).toEqual(['h1']);
    expect(resolveView(s, 'h1')).toMatchObject({ gridView: true, sort: { field: 'date', ascending: false }, density: 'compact' });
    expect(resolveView(s, 'other').sort.field).toBe('size');
    expect(resolveVisibility(s, 'h1').hideDotfiles).toBe(false);
    expect([...resolveVisibility(s, 'h1').hiddenExtensions]).toEqual(['x']);
  });

  it('falls back to defaults for missing, corrupt or mistyped values', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(K.gridView, '"yes"');
    await kv.set(K.sortField, '"weird"');
    await kv.set(K.overrides, 'not json');
    await kv.set(K.lowDiskThreshold, '"x"');
    const s = await loadSettings(kv);
    const d = defaultAppDefaults();
    expect(s.app.gridView).toBe(d.gridView);
    expect(s.app.sort.field).toBe('name');
    expect(s.app.lowDiskThresholdBytes).toBe(d.lowDiskThresholdBytes);
    expect(s.overrides).toEqual({});
  });

  it('writes round-trip through the same encodings', async () => {
    const kv = new MemoryKeyValueStore();
    const repo = new SettingsRepo(kv);
    await repo.setApp('gridView', true);
    await repo.setApp('sort', { field: 'type', ascending: false });
    await repo.setApp('visibility', { hideDotfiles: false, hiddenExtensions: new Set(['a']), hiddenNames: new Set(['B']) });
    await repo.setApp('themeMode', 'light');
    await repo.setOverrides({ h: { gridView: false, sort: { field: 'size', ascending: true } }, empty: {} });
    const s = await repo.load();
    expect(s.app).toMatchObject({ gridView: true, sort: { field: 'type', ascending: false }, themeMode: 'light' });
    expect([...s.app.visibility.hiddenNames]).toEqual(['B']);
    expect(s.overrides).toEqual({ h: { gridView: false, sort: { field: 'size', ascending: true } } });
  });

  it('still loads storage written with the removed AMOLED and accent keys, and ignores them', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set('app.amoledDark', 'true');
    await kv.set('app.seedColor', String(0xff4c8dff));
    await kv.set('app.dynamicColor', 'true');
    await kv.set('app.notificationsEnabled', 'false');
    await kv.set('app.watchedFolders.v1', JSON.stringify('["/a"]'));
    await kv.set('app.compressDownloadsOnCellular', 'false');
    await kv.set('app.weeklyDigestEnabled', 'true');
    await kv.set(K.themeMode, '"dark"');
    await kv.set(K.density, '"compact"');
    const { app } = await loadSettings(kv);
    expect(app).toMatchObject({ themeMode: 'dark', density: 'compact' });
    expect(app).not.toHaveProperty('amoledDark');
    expect(app).not.toHaveProperty('seedColor');
    for (const gone of ['dynamicColor', 'notificationsEnabled', 'watchedFolders', 'compressDownloadsOnCellular', 'weeklyDigestEnabled']) {
      expect(app).not.toHaveProperty(gone);
    }
  });

  it('falls back to the system theme for a stored theme that no longer exists', async () => {
    const kv = new MemoryKeyValueStore();
    await kv.set(K.themeMode, '"amoled"');
    expect((await loadSettings(kv)).app.themeMode).toBe('system');
  });
});
