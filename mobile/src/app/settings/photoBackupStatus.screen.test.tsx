import { act } from 'react';
import { Linking } from 'react-native';
import { create, type ReactTestRenderer, type ReactTestInstance } from 'react-test-renderer';

import { t } from '../../i18n';
import type { BackupResult } from '../../features/photoBackup/photoBackupService';
import PhotoBackup from './photo-backup';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mockPrefs = { enabled: true, hostId: 'h1' as string | null };
const mockRun = jest.fn();
const mockAccess = jest.fn();
let mockBackedUp = 0;
const mockToast = { info: jest.fn(), error: jest.fn(), success: jest.fn() };

// Boundaries only: icons, navigation, native modules, the backup service, and the design/settings building blocks.
jest.mock('lucide-react-native', () => new Proxy({}, { get: () => () => null }));
jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));
jest.mock('../../core/native', () => ({ transfers: { subscribe: () => () => undefined } }));
jest.mock('../../features/hosts/useHosts', () => ({ useHosts: () => [{ id: 'h1', label: 'Desk' }] }));
jest.mock('../../features/photoBackup/useBackupHostStates', () => ({ useBackupHostStates: () => null }));
jest.mock('../../features/photoBackup/AlbumPickerSheet', () => ({ AlbumPickerSheet: () => null }));
jest.mock('../../features/photoBackup/photoBackupService', () => ({
  listAlbums: jest.fn(),
  requestPhotoAccess: (...a: unknown[]) => mockAccess(...a),
  runPhotoBackup: (...a: unknown[]) => mockRun(...a),
  resetPhotoBackupRecord: jest.fn(),
  photoBackupStore: {
    load: async () => ({ ...mockPrefs, deviceName: null, wifiOnly: true, chargingOnly: false, albumIds: [], scheduled: false }),
    save: jest.fn(),
    doneCountFor: async () => mockBackedUp,
  },
}));
jest.mock('../../design/components', () => {
  const { createElement } = jest.requireActual('react');
  return {
    Button: (p: object) => createElement('Button', { accessibilityLabel: (p as { label: string }).label, ...p }),
    Text: ({ children }: { children?: unknown }) => createElement('Text', null, children),
    useDialogs: () => ({ confirm: jest.fn(), prompt: jest.fn() }),
    useToast: () => mockToast,
  };
});
jest.mock('../../features/settings/parts', () => {
  const { createElement } = jest.requireActual('react');
  const row = (name: string) => (p: { title: string; subtitle?: string; onPress?: () => void; onChange?: (v: boolean) => void }) =>
    createElement(name, { accessibilityLabel: p.title, subtitle: p.subtitle, onPress: p.onPress, onChange: p.onChange });
  const box = (name: string) => ({ children }: { children?: unknown }) => createElement(name, null, children);
  return { SettingsPage: box('Page'), SettingsSection: box('Section'), InfoRow: row('InfoRow'), NavRow: row('NavRow'), ToggleRow: row('ToggleRow'), ValueRow: row('ValueRow') };
});

async function mount(): Promise<ReactTestRenderer> {
  let r: ReactTestRenderer | undefined;
  await act(async () => {
    r = create(<PhotoBackup />);
  });
  return r as ReactTestRenderer;
}

const byLabel = (r: ReactTestRenderer, type: string, label: string): ReactTestInstance[] =>
  r.root.findAll((n) => n.type === type && n.props.accessibilityLabel === label);

/** Taps Back up now and lets the run's result settle. */
async function backUpNow(r: ReactTestRenderer, result: BackupResult) {
  mockRun.mockResolvedValue(result);
  await act(async () => {
    byLabel(r, 'Button', t('backUpNow'))[0].props.onPress();
  });
}

const allText = (r: ReactTestRenderer) => r.root.findAllByType('Text' as never).map((n) => String(n.props.children));

beforeEach(() => {
  mockPrefs.enabled = true;
  mockPrefs.hostId = 'h1';
  mockBackedUp = 0;
  mockRun.mockReset();
  mockAccess.mockReset();
  Object.values(mockToast).forEach((f) => f.mockReset());
});

describe('photo backup: the persistent status row', () => {
  it('off: no status row, the enable-first helper shows and Back up now is disabled', async () => {
    mockPrefs.enabled = false;
    const r = await mount();
    expect(byLabel(r, 'InfoRow', t('backupStatusOn'))).toHaveLength(0);
    expect(allText(r)).toContain(t('enableBackupFirst'));
    expect(byLabel(r, 'Button', t('backUpNow'))[0].props.disabled).toBe(true);
  });

  it('on and idle with nothing backed up: says On and where new photos go', async () => {
    const r = await mount();
    const row = byLabel(r, 'InfoRow', t('backupStatusOn'))[0];
    expect(row.props.subtitle).toBe(t('backupStatusOnDetail', { computer: 'Desk' }));
  });

  it('running: stays on screen after the run starts, with the count and the leave-this-screen detail', async () => {
    const r = await mount();
    await backUpNow(r, { kind: 'enqueued', count: 3 });
    const row = byLabel(r, 'InfoRow', t('backingUpPhotos', { count: 3 }))[0];
    expect(row.props.subtitle).toBe(t('backupStatusRunningDetail'));
    expect(byLabel(r, 'Button', t('tryAgainButton'))).toHaveLength(0);
  });

  it('paused for Wi-Fi: says what it waits for and how to change that', async () => {
    const r = await mount();
    await backUpNow(r, { kind: 'skipped', reason: 'wifi' });
    expect(byLabel(r, 'InfoRow', t('backupWaitingWifi'))[0].props.subtitle).toBe(t('backupStatusWifiDetail'));
  });

  it('error: names the computer and offers Try again, which runs the backup again', async () => {
    const r = await mount();
    await backUpNow(r, { kind: 'skipped', reason: 'unreachable', host: 'Desk' });
    expect(byLabel(r, 'InfoRow', t('backupStatusUnreachable', { computer: 'Desk' }))).toHaveLength(1);
    mockRun.mockClear();
    mockRun.mockResolvedValue({ kind: 'upToDate' });
    await act(async () => {
      byLabel(r, 'Button', t('tryAgainButton'))[0].props.onPress();
    });
    expect(mockRun).toHaveBeenCalledTimes(1);
  });

  it('permission denied: shows the photo-access row and an Open settings action that opens system settings', async () => {
    const open = jest.spyOn(Linking, 'openSettings').mockResolvedValue(undefined);
    const r = await mount();
    await backUpNow(r, { kind: 'permissionDenied' });
    expect(byLabel(r, 'InfoRow', t('backupStatusPhotoAccessOff'))[0].props.subtitle).toBe(t('backupStatusPhotoAccessDetail'));
    await act(async () => {
      byLabel(r, 'Button', t('openSettingsButton'))[0].props.onPress();
    });
    expect(open).toHaveBeenCalledTimes(1);
    open.mockRestore();
  });

  it('permission denied when turning the toggle on: the toggle stays off and the same row appears', async () => {
    mockPrefs.enabled = false;
    mockAccess.mockResolvedValue(false);
    const r = await mount();
    await act(async () => {
      byLabel(r, 'ToggleRow', t('enablePhotoBackup'))[0].props.onChange(true);
    });
    expect(byLabel(r, 'InfoRow', t('backupStatusPhotoAccessOff'))).toHaveLength(1);
    expect(byLabel(r, 'Button', t('openSettingsButton'))).toHaveLength(1);
    expect(mockToast.error).toHaveBeenCalledWith(t('backupStatusPhotoAccessOff'));
  });
});

describe('photo backup: page copy', () => {
  it('empty count reads "No photos backed up yet", not "0 photos"', async () => {
    const r = await mount();
    expect(byLabel(r, 'InfoRow', t('noPhotosBackedUp'))).toHaveLength(1);
    expect(byLabel(r, 'InfoRow', t('photosBackedUp', { count: 0 }))).toHaveLength(0);
  });

  it('a non-empty count keeps the plural line', async () => {
    mockBackedUp = 7;
    const r = await mount();
    expect(byLabel(r, 'InfoRow', t('photosBackedUp', { count: 7 }))).toHaveLength(1);
  });

  it('says computer, never PC', async () => {
    const r = await mount();
    const labels = r.root.findAll((n) => typeof n.type === 'string').map((n) => String(n.props.accessibilityLabel ?? ''));
    expect(labels.join(' | ')).not.toMatch(/\bPCs?\b/);
    expect(t('pickPcFirst')).toBe('Pick a computer first');
    expect(t('choosePc')).toBe('Choose a computer');
    expect(t('photoBackupSubtitle')).toBe('One-way: copies new photos to your computer');
  });
});
