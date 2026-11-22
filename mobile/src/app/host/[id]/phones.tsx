import { useLocalSearchParams } from 'expo-router';

import { Loading } from '../../../design/components';
import { PhonesWithAccessScreen } from '../../../features/hosts/PhonesWithAccessScreen';
import { HostGone } from '../../../features/hosts/HostGone';
import { useHostById } from '../../../features/hosts/useHostById';

/** Phones with access: which phones may use this computer and what each may do. */
export default function PhonesWithAccess() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) return <HostGone />;
  return <PhonesWithAccessScreen host={host} />;
}
