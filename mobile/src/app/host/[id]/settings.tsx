import { useLocalSearchParams } from 'expo-router';

import { Loading } from '../../../design/components';
import { HostGone } from '../../../features/hosts/HostGone';
import { useHostById } from '../../../features/hosts/useHostById';
import { HostSettingsScreen } from '../../../features/settings/HostSettingsScreen';

/** Settings for one paired computer. */
export default function HostSettings() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) return <HostGone />;
  return <HostSettingsScreen host={host} />;
}
