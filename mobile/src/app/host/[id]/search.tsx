import { Stack, useLocalSearchParams } from 'expo-router';
import { useEffect, useState } from 'react';
import { View } from 'react-native';

import { Loading, Text } from '../../../design/components';
import type { Host } from '../../../core/models/host';
import { HostSearchScreen } from '../../../features/search/HostSearchScreen';
import { hostStore } from '../../../services';

/** Search one host. `path` (the folder the search was opened from) scopes it; without it the search covers every shared root. */
export default function HostSearch() {
  const { id, path } = useLocalSearchParams<{ id: string; path?: string }>();
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
        <Text muted>This computer is no longer paired.</Text>
      </View>
    );
  }
  return (
    <>
      <Stack.Screen options={{ headerShown: false }} />
      <HostSearchScreen host={host} currentPath={path ?? '/'} onlyEverywhere={!path} />
    </>
  );
}
