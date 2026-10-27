import Constants from 'expo-constants';
import * as Clipboard from 'expo-clipboard';
import { FileText, History, Info, Scale, Share2 } from 'lucide-react-native';
import { Platform } from 'react-native';

import { useScheme } from '../../design/theme';
import { useToast } from '../../design/components';
import { t } from '../../i18n';
import { useRouter } from 'expo-router';
import { InfoRow, NavRow, SettingsPage, SettingsSection } from '../../features/settings/parts';
import { UpdateSection } from '../../features/update/UpdateSection';
import { buildDiagnostics } from '../../features/support/diagnostics';
import { hostStore } from '../../services';
import { useSettings } from '../../state/settings';

export default function AboutSettings() {
  const c = useScheme();
  const router = useRouter();
  const toast = useToast();
  const version = Constants.expoConfig?.version ?? '';
  const build = Constants.expoConfig?.android?.versionCode;

  async function exportDiagnostics() {
    try {
      const hosts = await hostStore.listHosts();
      const text = buildDiagnostics({
        appName: t('appNameLabel'),
        version,
        build,
        platform: Platform.OS,
        osVersion: Platform.Version,
        locale: Intl.DateTimeFormat().resolvedOptions().locale,
        app: useSettings.getState().state.app,
        hosts,
        now: new Date(),
      });
      await Clipboard.setStringAsync(text);
      toast.success(t('diagnosticsCopied'));
    } catch (e) {
      toast.error(t('operationFailed', { operation: t('diagnosticsExportButton'), error: e instanceof Error ? e.message : String(e) }));
    }
  }

  return (
    <SettingsPage>
      <SettingsSection title={t('aboutSection')}>
        <InfoRow icon={Info} tint={c.primary} title={t('appNameLabel')} subtitle={`${t('appVersionLabel')} ${version}${build ? ` (${build})` : ''}`} />
        <NavRow icon={History} tint={c.primary} title={t('whatsNewTitle')} onPress={() => router.push('/settings/whatsnew')} />
        <NavRow icon={FileText} tint={c.primary} title={t('privacyTitle')} onPress={() => router.push('/settings/privacy')} />
        <NavRow icon={Scale} tint={c.primary} title={t('openSourceLicenses')} onPress={() => router.push('/settings/licenses')} />
      </SettingsSection>
      <UpdateSection />
      <SettingsSection title={t('diagnosticsExportTitle')}>
        <NavRow icon={Share2} tint={c.primary} title={t('diagnosticsExportButton')} subtitle={t('diagnosticsExportSubtitle')} onPress={() => void exportDiagnostics()} />
      </SettingsSection>
    </SettingsPage>
  );
}
