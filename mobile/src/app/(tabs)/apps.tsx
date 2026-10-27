import { useRouter } from 'expo-router';
import { View } from 'react-native';

import { EmptyState, PageHead, TopBar } from '../../design/components';
import { AppsScreen } from '../../features/apps/AppsScreen';
import { useActiveHost } from '../../state/activeHost';

/** Apps tab: the installed apps of the active computer (the one picked on Home). */
export default function AppsTab() {
  const router = useRouter();
  const active = useActiveHost((s) => s.active);
  if (!active) {
    return (
      <View style={{ flex: 1 }}>
        <TopBar context="Apps" />
        <PageHead title="Applications" />
        <EmptyState message="Pair a computer to see and launch its apps" action={{ label: 'Add computer', onPress: () => router.push('/pair') }} />
      </View>
    );
  }
  return <AppsScreen key={active.host.id} hostId={active.host.id} />;
}
