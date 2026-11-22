import { act } from 'react';
import { create, type ReactTestRenderer, type ReactTestInstance } from 'react-test-renderer';

import { t } from '../../i18n';
import PhotoBackup from './photo-backup';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const mockConfirm = jest.fn();
const mockReset = jest.fn();

// Boundaries only: icons, navigation, native modules, the backup service, and the design/settings building blocks,
// which are replaced by host elements that expose the props the screen passes them.
jest.mock('lucide-react-native', () => new Proxy({}, { get: () => () => null }));
jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));
jest.mock('../../core/native', () => ({ transfers: { subscribe: () => () => undefined } }));
jest.mock('../../features/hosts/useHosts', () => ({ useHosts: () => [] }));
jest.mock('../../features/photoBackup/useBackupHostStates', () => ({ useBackupHostStates: () => null }));
jest.mock('../../features/photoBackup/AlbumPickerSheet', () => ({ AlbumPickerSheet: () => null }));
jest.mock('../../features/photoBackup/photoBackupService', () => ({
  listAlbums: jest.fn(),
  requestPhotoAccess: jest.fn(),
  runPhotoBackup: jest.fn(),
  resetPhotoBackupRecord: (...a: unknown[]) => mockReset(...a),
  photoBackupStore: {
    load: async () => ({ enabled: true, hostId: null, deviceName: null, wifiOnly: true, chargingOnly: false, albumIds: [], scheduled: false }),
    save: jest.fn(),
    doneCountFor: async () => 3,
  },
}));
jest.mock('../../design/components', () => {
  const { createElement } = jest.requireActual('react');
  return {
    Button: (p: object) => createElement('Button', { accessibilityLabel: (p as { label: string }).label, ...p }),
    Text: ({ children }: { children?: unknown }) => createElement('Text', null, children),
    useDialogs: () => ({ confirm: mockConfirm, prompt: jest.fn() }),
    useToast: () => ({ info: jest.fn(), error: jest.fn(), success: jest.fn() }),
  };
});
jest.mock('../../features/settings/parts', () => {
  const { createElement } = jest.requireActual('react');
  const row = (name: string) => (p: { title: string; onPress?: () => void }) => createElement(name, { accessibilityLabel: p.title, onPress: p.onPress });
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

function resetControl(r: ReactTestRenderer): ReactTestInstance {
  return r.root.findAll((n) => typeof n.type === 'string' && n.props.accessibilityLabel === t('resetBackupRecord'))[0];
}

beforeEach(() => {
  mockConfirm.mockReset();
  mockReset.mockReset();
});

describe('photo backup: reset backup record', () => {
  it('is a destructive button, not a plain row that looks like the primary action', async () => {
    const control = resetControl(await mount());
    expect(control.type).toBe('Button');
    expect(control.props.destructive).toBe(true);
  });

  it('does not reset until the person confirms', async () => {
    mockConfirm.mockResolvedValue(false);
    const control = resetControl(await mount());
    await act(async () => {
      control.props.onPress();
    });
    expect(mockConfirm).toHaveBeenCalledTimes(1);
    expect(mockConfirm.mock.calls[0][0]).toMatchObject({ title: t('resetBackupConfirmTitle'), destructive: true });
    expect(mockReset).not.toHaveBeenCalled();
  });

  it('resets once the person confirms', async () => {
    mockConfirm.mockResolvedValue(true);
    mockReset.mockResolvedValue(undefined);
    const control = resetControl(await mount());
    await act(async () => {
      control.props.onPress();
    });
    expect(mockReset).toHaveBeenCalledTimes(1);
  });
});
