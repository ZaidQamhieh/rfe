import type { KeyValueStore } from '../storage/hostStore';
import { defaultVisibility, type VisibilityPrefs } from '../visibility';
import { defaultSort, type EntryDensity, type SortField, type SortOrder } from '../models/sort';

// Port of core/settings/{app_settings,settings_controller}.dart. Key names and value encodings are
// identical to the Flutter app (values are JSON text in the kv store) so an in-place upgrade keeps
// every setting.

export type ThemeMode = 'system' | 'light' | 'dark';

export type AppDefaults = {
  gridView: boolean;
  density: EntryDensity;
  sort: SortOrder;
  visibility: VisibilityPrefs;
  themeMode: ThemeMode;
  dynamicColor: boolean;
  notificationsEnabled: boolean;
  lowDiskThresholdBytes: number;
  appLockEnabled: boolean;
  amoledDark: boolean;
  seedColor: number | null;
  watchedFolders: Set<string>;
  compressDownloadsOnCellular: boolean;
  preloadPreviewOnCellular: boolean;
  weeklyDigestEnabled: boolean;
};

export type DeviceOverrides = { gridView?: boolean; density?: EntryDensity; sort?: SortOrder; visibility?: VisibilityPrefs };

export type SettingsState = { app: AppDefaults; overrides: Record<string, DeviceOverrides> };

export const defaultAppDefaults = (): AppDefaults => ({
  gridView: false,
  density: 'comfortable',
  sort: defaultSort(),
  visibility: defaultVisibility(),
  themeMode: 'system',
  dynamicColor: false,
  notificationsEnabled: true,
  lowDiskThresholdBytes: 1024 * 1024 * 1024,
  appLockEnabled: false,
  amoledDark: false,
  seedColor: null,
  watchedFolders: new Set(),
  compressDownloadsOnCellular: true,
  preloadPreviewOnCellular: false,
  weeklyDigestEnabled: false,
});

export const K = {
  gridView: 'app.gridView',
  density: 'app.density',
  sortField: 'app.sortField',
  sortAscending: 'app.sortAscending',
  visHideDotfiles: 'app.visibility.hideDotfiles',
  visHiddenExtensions: 'app.visibility.hiddenExtensions',
  visHiddenNames: 'app.visibility.hiddenNames',
  themeMode: 'app.themeMode',
  dynamicColor: 'app.dynamicColor',
  notifications: 'app.notificationsEnabled',
  lowDiskThreshold: 'app.lowDiskThresholdBytes',
  appLock: 'app.appLockEnabled',
  amoled: 'app.amoledDark',
  seedColor: 'app.seedColor',
  watchedFolders: 'app.watchedFolders.v1',
  compressCellular: 'app.compressDownloadsOnCellular',
  preloadCellular: 'app.preloadPreviewOnCellular',
  weeklyDigest: 'app.weeklyDigestEnabled',
  overrides: 'settings.deviceOverrides.v1',
} as const;

const SORT_FIELDS: SortField[] = ['name', 'size', 'date', 'type'];
const DENSITIES: EntryDensity[] = ['comfortable', 'compact'];
const THEMES: ThemeMode[] = ['system', 'light', 'dark'];

/** Typed reads over JSON-encoded kv values; any unreadable/mistyped value falls back to the default. */
class Reader {
  constructor(private readonly kv: KeyValueStore) {}
  private async raw(key: string): Promise<unknown> {
    const s = await this.kv.get(key);
    if (s === null) return undefined;
    try {
      return JSON.parse(s);
    } catch {
      return undefined;
    }
  }
  async bool(key: string, d: boolean) {
    const v = await this.raw(key);
    return typeof v === 'boolean' ? v : d;
  }
  async int(key: string, d: number): Promise<number>;
  async int(key: string, d: null): Promise<number | null>;
  async int(key: string, d: number | null) {
    const v = await this.raw(key);
    return typeof v === 'number' && Number.isFinite(v) ? v : d;
  }
  async oneOf<T extends string>(key: string, allowed: readonly T[], d: T): Promise<T> {
    const v = await this.raw(key);
    return typeof v === 'string' && (allowed as readonly string[]).includes(v) ? (v as T) : d;
  }
  /** Flutter stores string sets as a JSON list *inside a string*. */
  async stringSet(key: string): Promise<Set<string>> {
    const v = await this.raw(key);
    return new Set(decodeStringSet(v));
  }
  async json(key: string): Promise<unknown> {
    const v = await this.raw(key);
    return typeof v === 'string' ? safeParse(v) : v;
  }
}

function safeParse(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return undefined;
  }
}

function decodeStringSet(v: unknown): string[] {
  const arr = typeof v === 'string' ? safeParse(v) : v;
  return Array.isArray(arr) ? arr.filter((x): x is string => typeof x === 'string') : [];
}

const visibilityFromJson = (m: Record<string, unknown>): VisibilityPrefs => ({
  hideDotfiles: typeof m.hideDotfiles === 'boolean' ? m.hideDotfiles : true,
  hiddenExtensions: new Set(decodeStringSet(m.hiddenExtensions)),
  hiddenNames: new Set(decodeStringSet(m.hiddenNames)),
});

const visibilityToJson = (v: VisibilityPrefs) => ({ hideDotfiles: v.hideDotfiles, hiddenExtensions: [...v.hiddenExtensions], hiddenNames: [...v.hiddenNames] });

export async function loadSettings(kv: KeyValueStore): Promise<SettingsState> {
  const r = new Reader(kv);
  const d = defaultAppDefaults();
  const app: AppDefaults = {
    gridView: await r.bool(K.gridView, d.gridView),
    density: await r.oneOf(K.density, DENSITIES, d.density),
    sort: { field: await r.oneOf(K.sortField, SORT_FIELDS, 'name'), ascending: await r.bool(K.sortAscending, true) },
    visibility: {
      hideDotfiles: await r.bool(K.visHideDotfiles, true),
      hiddenExtensions: await r.stringSet(K.visHiddenExtensions),
      hiddenNames: await r.stringSet(K.visHiddenNames),
    },
    themeMode: await r.oneOf(K.themeMode, THEMES, 'system'),
    dynamicColor: await r.bool(K.dynamicColor, d.dynamicColor),
    notificationsEnabled: await r.bool(K.notifications, d.notificationsEnabled),
    lowDiskThresholdBytes: await r.int(K.lowDiskThreshold, d.lowDiskThresholdBytes),
    appLockEnabled: await r.bool(K.appLock, d.appLockEnabled),
    amoledDark: await r.bool(K.amoled, d.amoledDark),
    seedColor: await r.int(K.seedColor, null),
    watchedFolders: await r.stringSet(K.watchedFolders),
    compressDownloadsOnCellular: await r.bool(K.compressCellular, d.compressDownloadsOnCellular),
    preloadPreviewOnCellular: await r.bool(K.preloadCellular, d.preloadPreviewOnCellular),
    weeklyDigestEnabled: await r.bool(K.weeklyDigest, d.weeklyDigestEnabled),
  };
  const overrides: Record<string, DeviceOverrides> = {};
  const blob = await r.json(K.overrides);
  if (typeof blob === 'object' && blob !== null && !Array.isArray(blob)) {
    for (const [hostId, raw] of Object.entries(blob as Record<string, unknown>)) {
      if (typeof raw !== 'object' || raw === null) continue;
      const m = raw as Record<string, unknown>;
      const o: DeviceOverrides = {};
      if (typeof m.gridView === 'boolean') o.gridView = m.gridView;
      if (typeof m.density === 'string' && (DENSITIES as string[]).includes(m.density)) o.density = m.density as EntryDensity;
      if ('sortField' in m) {
        const f = typeof m.sortField === 'string' && (SORT_FIELDS as string[]).includes(m.sortField) ? (m.sortField as SortField) : 'name';
        o.sort = { field: f, ascending: typeof m.sortAscending === 'boolean' ? m.sortAscending : true };
      }
      if (typeof m.visibility === 'object' && m.visibility !== null) o.visibility = visibilityFromJson(m.visibility as Record<string, unknown>);
      if (Object.keys(o).length > 0) overrides[hostId] = o;
    }
  }
  return { app, overrides };
}

/** Effective view for a host: device override, else app default. */
export function resolveView(s: SettingsState, hostId: string) {
  const o = s.overrides[hostId] ?? {};
  return { gridView: o.gridView ?? s.app.gridView, density: o.density ?? s.app.density, sort: o.sort ?? s.app.sort };
}

export function resolveVisibility(s: SettingsState, hostId: string): VisibilityPrefs {
  return s.overrides[hostId]?.visibility ?? s.app.visibility;
}

const enc = (v: unknown) => JSON.stringify(v);

/** Persists one change (same keys/encodings as Flutter) and returns the next in-memory state. */
export class SettingsRepo {
  constructor(private readonly kv: KeyValueStore) {}

  load() {
    return loadSettings(this.kv);
  }

  async setApp<T extends keyof AppDefaults>(key: T, value: AppDefaults[T]): Promise<void> {
    const kv = this.kv;
    switch (key) {
      case 'gridView': return kv.set(K.gridView, enc(value));
      case 'density': return kv.set(K.density, enc(value));
      case 'sort': {
        const v = value as SortOrder;
        await kv.set(K.sortField, enc(v.field));
        return kv.set(K.sortAscending, enc(v.ascending));
      }
      case 'visibility': {
        const v = value as VisibilityPrefs;
        await kv.set(K.visHideDotfiles, enc(v.hideDotfiles));
        await kv.set(K.visHiddenExtensions, enc(JSON.stringify([...v.hiddenExtensions])));
        return kv.set(K.visHiddenNames, enc(JSON.stringify([...v.hiddenNames])));
      }
      case 'themeMode': return kv.set(K.themeMode, enc(value));
      case 'dynamicColor': return kv.set(K.dynamicColor, enc(value));
      case 'notificationsEnabled': return kv.set(K.notifications, enc(value));
      case 'lowDiskThresholdBytes': return kv.set(K.lowDiskThreshold, enc(value));
      case 'appLockEnabled': return kv.set(K.appLock, enc(value));
      case 'amoledDark': return kv.set(K.amoled, enc(value));
      case 'seedColor': return value === null ? kv.remove(K.seedColor) : kv.set(K.seedColor, enc(value));
      case 'watchedFolders': return kv.set(K.watchedFolders, enc(JSON.stringify([...(value as Set<string>)])));
      case 'compressDownloadsOnCellular': return kv.set(K.compressCellular, enc(value));
      case 'preloadPreviewOnCellular': return kv.set(K.preloadCellular, enc(value));
      case 'weeklyDigestEnabled': return kv.set(K.weeklyDigest, enc(value));
    }
  }

  /** Sparse per-device overrides (a host is present only if it overrides something). */
  async setOverrides(overrides: Record<string, DeviceOverrides>): Promise<void> {
    const blob: Record<string, unknown> = {};
    for (const [id, o] of Object.entries(overrides)) {
      if (Object.keys(o).length === 0) continue;
      blob[id] = {
        ...(o.gridView !== undefined ? { gridView: o.gridView } : {}),
        ...(o.density !== undefined ? { density: o.density } : {}),
        ...(o.sort ? { sortField: o.sort.field, sortAscending: o.sort.ascending } : {}),
        ...(o.visibility ? { visibility: visibilityToJson(o.visibility) } : {}),
      };
    }
    await this.kv.set(K.overrides, enc(JSON.stringify(blob)));
  }
}
