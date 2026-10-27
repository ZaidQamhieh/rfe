import type { ReactNode } from 'react';
import { View } from 'react-native';

import { useScheme } from '../theme';
import { Pressable } from './Pressable';
import { Text } from './Text';

/** Lumen `.space`: a flat card tile (surfaceContainer, 16 dp radius) with a role-coloured icon above a 14 sp bold label (12 sp when `compact`). Shown at 40% when disabled. */
export function ActionTile({ label, onPress, disabled, renderIcon, compact }: { label: string; onPress?: () => void; disabled?: boolean; renderIcon: (color: string) => ReactNode; /** 12 sp label, for rows of four or more tiles. */ compact?: boolean }) {
  const c = useScheme();
  return (
    <Pressable onPress={disabled ? undefined : onPress} disabled={disabled} accessibilityLabel={label} accessibilityState={{ disabled: !!disabled }} style={{ flex: 1, opacity: disabled ? 0.4 : 1 }}>
      <View style={{ minHeight: 72, borderRadius: 16, backgroundColor: c.surfaceContainer, alignItems: 'center', justifyContent: 'center', gap: 6, paddingVertical: 8, paddingHorizontal: 4 }}>
        {renderIcon(c.onSurface)}
        <Text style={{ fontSize: compact ? 12 : 14, lineHeight: compact ? 16 : 18, fontFamily: 'Lato_700Bold' }} numberOfLines={1}>
          {label}
        </Text>
      </View>
    </Pressable>
  );
}
