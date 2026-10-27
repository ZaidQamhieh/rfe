import { Hammer } from 'lucide-react-native';
import { View } from 'react-native';

import { useScheme } from '../theme';
import { Text } from './Text';

/** Placeholder for a route whose feature is still being ported; names the tracked task so nothing looks finished when it is not. */
export function NotYet({ feature, task }: { feature: string; task: string }) {
  const c = useScheme();
  return (
    <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: 24, gap: 12 }}>
      <Hammer size={48} color={c.outline} />
      <Text variant="titleMedium" style={{ textAlign: 'center' }}>{feature}</Text>
      <Text muted style={{ textAlign: 'center' }}>This screen has not been ported from the Flutter app yet ({task}).</Text>
    </View>
  );
}
