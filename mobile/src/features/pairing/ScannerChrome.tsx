import { useEffect, useState } from 'react';
import { Animated, Easing, View } from 'react-native';

import { Pressable } from '../../design/components';
import { useScheme } from '../../design/theme';

/** The moving scan line; [color] defaults to the classic blue for the screens that still draw their own chrome. */
export function Scanline({ height, color = '#4C8DFF' }: { height: number; color?: string }) {
  const v = useState(() => new Animated.Value(0))[0];
  useEffect(() => {
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(v, { toValue: 1, duration: 2200, easing: Easing.inOut(Easing.ease), useNativeDriver: true }),
        Animated.timing(v, { toValue: 0, duration: 2200, easing: Easing.inOut(Easing.ease), useNativeDriver: true }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [v]);
  const travel = height / 2 - 8;
  return (
    <Animated.View
      style={{ position: 'absolute', left: 8, right: 8, top: height / 2, height: 2, backgroundColor: color, shadowColor: color, shadowOpacity: 0.6, shadowRadius: 8, transform: [{ translateY: v.interpolate({ inputRange: [0, 1], outputRange: [-travel, travel] }) }] }}
    />
  );
}

export const DarkIconButton = ({ children, onPress, label }: { children: React.ReactNode; onPress: () => void; label: string }) => (
  <Pressable onPress={onPress} accessibilityLabel={label}>
    <View style={{ width: 34, height: 34, borderRadius: 17, backgroundColor: 'rgba(255,255,255,0.08)', alignItems: 'center', justifyContent: 'center' }}>{children}</View>
  </Pressable>
);


/** Flat Lumen icon button for the scanner's top bar: a 48 dp target around a raised 40 dp tile. */
export function ScanIconButton({ children, onPress, label, active }: { children: React.ReactNode; onPress: () => void; label: string; active?: boolean }) {
  const c = useScheme();
  return (
    <Pressable onPress={onPress} accessibilityLabel={label} accessibilityState={{ selected: !!active }} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 40, height: 40, borderRadius: 14, backgroundColor: active ? c.primaryContainer : c.surfaceContainerHigh, alignItems: 'center', justifyContent: 'center' }}>{children}</View>
    </Pressable>
  );
}
