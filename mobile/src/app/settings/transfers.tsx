import { useRouter } from 'expo-router';
import { Activity, Layers } from 'lucide-react-native';

import { useScheme } from '../../design/theme';
import { t } from '../../i18n';
import { InfoRow, NavRow, SettingsPage, SettingsSection } from '../../features/settings/parts';

/** Uploads use the agent's 4 MB chunks; history is the Transfers tab. */
export default function TransferSettings() {
  const router = useRouter();
  const c = useScheme();
  return (
    <SettingsPage>
      <SettingsSection title={t('transfersSettingsTitle')}>
        <InfoRow icon={Layers} tint={c.primary} title={t('transferChunkSize')} subtitle={t('transferChunkSizeSubtitle')} />
        <NavRow icon={Activity} tint={c.primary} title={t('transferHistoryOpen')} subtitle={t('transferHistoryOpenSubtitle')} onPress={() => router.replace('/transfers')} />
      </SettingsSection>
    </SettingsPage>
  );
}
