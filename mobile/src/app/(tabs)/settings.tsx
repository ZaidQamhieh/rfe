import { useRouter } from 'expo-router';
import { ArchiveRestore, ArrowLeftRight, FolderSync, EyeOff, HardDrive, Images, Info, Palette } from 'lucide-react-native';
import { View } from 'react-native';

import { PageHead, TopBar } from '../../design/components';
import { useRoles, useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { NavRow, SettingsPage, SettingsSection } from '../../features/settings/parts';
import { useActiveHost } from '../../state/activeHost';

/** App-wide settings hub (reached from the Home overflow menu); each row opens a stack screen. Per-computer settings live under a host. */
export default function Settings() {
  const router = useRouter();
  const c = useScheme();
  const r = useRoles();
  const active = useActiveHost((s) => s.active);
  return (
    <View style={{ flex: 1 }}>
      <TopBar context={active ? `${active.host.label} · Settings` : 'Settings'} />
      <PageHead title={t('settingsTitle')} />
      <SettingsPage>
        <SettingsSection title={t('preferencesSection')}>
          <NavRow icon={Palette} tint={c.primary} title={t('appearanceSection')} subtitle={t('appearanceSubtitle')} onPress={() => router.push('/settings/appearance')} />
          <NavRow icon={EyeOff} tint={r.folder} title={t('fileVisibilityTitle')} subtitle={t('fileVisibilitySubtitle')} onPress={() => router.push('/settings/visibility')} />
          <NavRow icon={ArrowLeftRight} tint={r.transfer} title={t('transfersSettingsTitle')} subtitle={t('transfersSettingsSubtitle')} onPress={() => router.push('/settings/transfers')} />
        </SettingsSection>
        <SettingsSection title={t('dataSection')}>
          <NavRow icon={Images} tint={r.photo} title={t('photoBackupTitle')} subtitle={t('copyPhonePhotos')} onPress={() => router.push('/settings/photo-backup')} />
          <NavRow icon={FolderSync} tint={r.route} title={t('syncRulesTitle')} subtitle={t('syncRulesSubtitle')} onPress={() => router.push('/settings/sync')} />
          <NavRow icon={ArchiveRestore} tint={r.warn} title={t('backupRestoreSection')} subtitle={t('exportConfigSubtitle')} onPress={() => router.push('/settings/backup')} />
          <NavRow icon={HardDrive} tint={r.safe} title={t('storageSecurityTitle')} subtitle={t('storageSecuritySubtitle')} onPress={() => router.push('/settings/storage')} />
        </SettingsSection>
        <SettingsSection title={t('supportSection')}>
          <NavRow icon={Info} tint={r.doc} title={t('aboutSupportTitle')} subtitle={t('aboutSupportSubtitle')} onPress={() => router.push('/settings/about')} />
        </SettingsSection>
      </SettingsPage>
    </View>
  );
}
