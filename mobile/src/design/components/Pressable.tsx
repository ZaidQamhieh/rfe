import { useState, type ReactNode } from 'react';
import { Animated, Pressable as RNPressable, StyleSheet, type PressableProps, type StyleProp, type ViewStyle } from 'react-native';

type Props = Omit<PressableProps, 'style' | 'children'> & {
  children: ReactNode;
  style?: StyleProp<ViewStyle>;
  pressedScale?: number;
};

/** Port of core/ui/pressable.dart: opaque hit area, 100ms scale to 0.985 on press. */
const OUTER_KEYS = ['flex', 'flexGrow', 'flexShrink', 'flexBasis', 'alignSelf', 'width', 'minWidth', 'maxWidth', 'margin', 'marginTop', 'marginBottom', 'marginLeft', 'marginRight', 'marginHorizontal', 'marginVertical', 'position', 'top', 'left', 'right', 'bottom'] as const;

/** Layout props (flex, margin, position…) belong to the touchable so it lays out in its parent; the rest style the scaled inner view. */
function splitStyle(style: StyleProp<ViewStyle>) {
  const flat = (StyleSheet.flatten(style) ?? {}) as Record<string, unknown>;
  const outer: Record<string, unknown> = {};
  const inner: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(flat)) {
    if ((OUTER_KEYS as readonly string[]).includes(k)) outer[k] = v;
    else inner[k] = v;
  }
  return { outer: outer as ViewStyle, inner: inner as ViewStyle };
}

export function Pressable({ children, style, pressedScale = 0.985, onPressIn, onPressOut, ...rest }: Props) {
  const { outer, inner } = splitStyle(style);
  const scale = useState(() => new Animated.Value(1))[0];
  const to = (v: number) => Animated.timing(scale, { toValue: v, duration: 100, useNativeDriver: true }).start();
  return (
    <RNPressable
      accessibilityRole="button"
      {...rest}
      style={outer}
      onPressIn={(e) => {
        to(pressedScale);
        onPressIn?.(e);
      }}
      onPressOut={(e) => {
        to(1);
        onPressOut?.(e);
      }}
    >
      <Animated.View style={[{ transform: [{ scale }] }, inner]}>{children}</Animated.View>
    </RNPressable>
  );
}
