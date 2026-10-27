import { Stack, useLocalSearchParams } from 'expo-router';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import type { Host } from '../../../core/models/host';
import { Loading, Text } from '../../../design/components';
import { HostSettingsScreen } from '../../../features/settings/HostSettingsScreen';
import { hostStore } from '../../../services';

/** Settings for one paired computer. */
export default function HostSettings() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const [host, setHost] = useState<Host | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    void hostStore.listHosts().then((l) => live && setHost(l.find((h) => h.id === id) ?? null));
    return () => {
      live = false;
    };
  }, [id]);
  if (host === undefined) return <Loading />;
  if (host === null) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Stack.Screen options={{ title: '' }} />
        <Text muted>This computer is no longer paired.</Text>
      </View>
    );
  }
  return <HostSettingsScreen host={host} />;
}
