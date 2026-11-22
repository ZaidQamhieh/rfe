import { useEffect, useState } from 'react';
import { Animated, View } from 'react-native';

import { useScheme } from '../theme';

const TRACK_W = 44;
const TRACK_H = 26;
const THUMB = 16;
const BORDER = 1.5;
const THUMB_OFF_X = 5;
const THUMB_ON_X = 23;

/**
 * Nameplate `.tg`: 44x26 track with a 16px thumb. Off: translucent fill, 1.5px hairline, ink-2 thumb at the left.
 * On: accent track, on-accent thumb at the right (animated 150 ms).
 */
export function MockupSwitch({ value }: { value: boolean }) {
  const c = useScheme();
  const x = useState(() => new Animated.Value(value ? 1 : 0))[0];
  useEffect(() => {
    Animated.timing(x, { toValue: value ? 1 : 0, duration: 150, useNativeDriver: true }).start();
  }, [value, x]);
  return (
    <View
      style={{ width: TRACK_W, height: TRACK_H, borderRadius: TRACK_H / 2, backgroundColor: value ? c.primary : c.fill2, borderWidth: BORDER, borderColor: value ? c.primary : c.rule2 }}
      accessibilityRole="switch"
      accessibilityState={{ checked: value }}
    >
      <Animated.View
        style={{
          position: 'absolute',
          top: (TRACK_H - 2 * BORDER - THUMB) / 2,
          left: 0,
          width: THUMB,
          height: THUMB,
          borderRadius: THUMB / 2,
          backgroundColor: value ? c.onPrimary : c.onSurfaceVariant,
          // The thumb is positioned inside the border, so the mockup's 5px / 23px offsets lose the border width.
          transform: [{ translateX: x.interpolate({ inputRange: [0, 1], outputRange: [THUMB_OFF_X - BORDER, THUMB_ON_X - BORDER] }) }],
        }}
      />
    </View>
  );
}
