import { Stack, useLocalSearchParams } from 'expo-router';

import { t } from '../../../i18n';
import { AppsScreen } from '../../../features/apps/AppsScreen';

/** Apps the computer chose to expose; the same screen as the Apps tab, under the stack header. */
export default function HostApps() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return (
    <>
      <Stack.Screen options={{ title: t('hostAppsTitle') }} />
      <AppsScreen hostId={id} chrome={false} />
    </>
  );
}
