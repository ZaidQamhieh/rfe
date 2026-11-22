import { useLocalSearchParams } from 'expo-router';

import { Loading } from '../../../design/components';
import { AboutComputerScreen } from '../../../features/hosts/AboutComputerScreen';
import { HostGone } from '../../../features/hosts/HostGone';
import { useHostById } from '../../../features/hosts/useHostById';

/** About this computer: its less common settings and technical details. */
export default function AboutComputer() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const host = useHostById(id);
  if (host === undefined) return <Loading />;
  if (host === null) return <HostGone />;
  return <AboutComputerScreen host={host} />;
}
