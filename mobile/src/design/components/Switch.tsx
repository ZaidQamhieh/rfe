import { useEffect, useState } from 'react';
import { Animated, View } from 'react-native';

import { useScheme } from '../theme';
import { Radii } from '../tokens';

/** Mockup switch: 42x25 pill, 19px thumb, primary track when on (animated 150 ms). */
export function MockupSwitch({ value }: { value: boolean }) {
  const c = useScheme();
  const x = useState(() => new Animated.Value(value ? 1 : 0))[0];
  useEffect(() => {
    Animated.timing(x, { toValue: value ? 1 : 0, duration: 150, useNativeDriver: true }).start();
  }, [value, x]);
  return (
    <View style={{ width: 42, height: 25, padding: 2, borderRadius: Radii.stadium, backgroundColor: value ? c.primary : c.surfaceContainerHighest, borderWidth: 1, borderColor: value ? c.primary : c.outlineVariant }} accessibilityRole="switch" accessibilityState={{ checked: value }}>
      <Animated.View style={{ width: 19, height: 19, borderRadius: 10, backgroundColor: value ? '#FFFFFF' : c.onSurfaceVariant, transform: [{ translateX: x.interpolate({ inputRange: [0, 1], outputRange: [0, 17] }) }] }} />
    </View>
  );
}
