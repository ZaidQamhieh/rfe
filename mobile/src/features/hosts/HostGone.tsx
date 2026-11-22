import { Stack } from 'expo-router';
import { View } from 'react-native';

import { Text } from '../../design/components';
import { Spacing } from '../../design/tokens';
import { t } from '../../i18n';
import { StackTopBar } from './LumenRows';

/**
 * What a host route shows when its computer is no longer paired. With `context` it draws the screen's own top bar
 * (for routes that hide the stack header); without it the stack header is left blank.
 */
export function HostGone({ context }: { context?: string }) {
  if (context === undefined) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
        <Stack.Screen options={{ title: '' }} />
        <Text muted>{t('hostNoLongerPaired')}</Text>
      </View>
    );
  }
  return (
    <View style={{ flex: 1 }}>
      <Stack.Screen options={{ headerShown: false }} />
      <StackTopBar context={context} />
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', padding: Spacing.lg }}>
        <Text muted>{t('hostNoLongerPaired')}</Text>
      </View>
    </View>
  );
}
