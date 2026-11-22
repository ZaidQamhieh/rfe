import { useLocalSearchParams } from 'expo-router';

import { Loading } from '../../../design/components';
import { StorageUsageScreen } from '../../../features/explorer/StorageUsageScreen';
import { HostGone } from '../../../features/hosts/HostGone';
import { useHostById } from '../../../features/hosts/useHostById';

/** What is using space under a folder of one computer. */
export default function HostUsage() {
  const { id, path } = useLocalSearchParams<{ id: string; path?: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) return <HostGone />;
  return <StorageUsageScreen host={host} path={path ?? '/'} />;
}
