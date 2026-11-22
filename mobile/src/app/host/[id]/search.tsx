import { Stack, useLocalSearchParams } from 'expo-router';

import { Loading } from '../../../design/components';
import { HostGone } from '../../../features/hosts/HostGone';
import { useHostById } from '../../../features/hosts/useHostById';
import { HostSearchScreen } from '../../../features/search/HostSearchScreen';

/** Search one host. `path` (the folder the search was opened from) scopes it; without it the search covers every shared root. */
export default function HostSearch() {
  const { id, path } = useLocalSearchParams<{ id: string; path?: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) return <HostGone />;
  return (
    <>
      <Stack.Screen options={{ headerShown: false }} />
      <HostSearchScreen host={host} currentPath={path ?? '/'} onlyEverywhere={!path} />
    </>
  );
}
