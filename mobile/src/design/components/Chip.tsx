import type { ReactNode } from 'react';
import { View } from 'react-native';

import { LumenType } from '../lumen';
import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { withAlpha } from './Callouts';
import { Pressable } from './Pressable';
import { Text } from './Text';

/** Nameplate `.chip`: 36 high, radius 8, 14/500 label in a 1px hairline; selected = accent-soft fill with a 45% accent border. */
export function Chip({ label, selected = false, onPress, icon }: { label: string; selected?: boolean; onPress?: () => void; icon?: ReactNode }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityRole="button" accessibilityLabel={label} accessibilityState={{ selected }} style={{ borderRadius: Radii.chip }}>
      <View
        style={{
          height: 36,
          paddingHorizontal: Spacing.md2,
          flexDirection: 'row',
          alignItems: 'center',
          gap: 6,
          borderRadius: Radii.chip,
          borderWidth: 1,
          borderColor: selected ? withAlpha(c.primary, 0.45) : c.rule2,
          backgroundColor: selected ? c.primaryContainer : 'transparent',
        }}
      >
        {icon}
        <Text style={LumenType.pill} color={c.onSurface} numberOfLines={1}>
          {label}
        </Text>
      </View>
    </Pressable>
  );
}
