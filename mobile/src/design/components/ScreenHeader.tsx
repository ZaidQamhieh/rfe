import { View } from 'react-native';

import { Text } from './Text';

export function ScreenHeader({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <View>
      <Text variant="screenTitle" accessibilityRole="header">
        {title}
      </Text>
      {subtitle ? (
        <Text variant="screenSubtitle" muted>
          {subtitle}
        </Text>
      ) : null}
    </View>
  );
}
