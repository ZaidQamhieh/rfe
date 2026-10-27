import type { ReactNode } from 'react';
import { ActivityIndicator, View, type StyleProp, type ViewStyle } from 'react-native';

import { LumenSize, LumenType } from '../lumen';
import { useScheme } from '../theme';
import { Pressable } from './Pressable';
import { Text } from './Text';

/** `gradient` is kept for old call sites and renders as `filled` (Lumen buttons are flat fills without outlines). */
type Kind = 'gradient' | 'filled' | 'tonal' | 'neutral' | 'outlined' | 'text';
type Props = {
  label: string;
  onPress?: () => void;
  kind?: Kind;
  icon?: ReactNode;
  busy?: boolean;
  disabled?: boolean;
  style?: StyleProp<ViewStyle>;
  destructive?: boolean;
  /** Render the icon in the button's foreground color. */
  renderIcon?: (color: string) => ReactNode;
  /** `lg` is the footer action (mockup `.action-footer .action`: 36 px high, 9 px radius, 9 px text). */
  size?: 'md' | 'lg';
};

/**
 * Lumen `.action`: a flat, filled rectangle with centred icon and label. `filled` uses the host colour with dark-on-light
 * text; `tonal`, `neutral` and `outlined` use the raised surface (`.action.secondary`); `text` is a bare label.
 */
export function Button({ label, onPress, kind = 'filled', icon, renderIcon, busy, disabled, style, destructive, size = 'md' }: Props) {
  const c = useScheme();
  const off = disabled || busy;
  const k: Kind = kind === 'gradient' ? 'filled' : kind;
  const raised = k === 'tonal' || k === 'neutral' || k === 'outlined';
  const fg = k === 'filled' ? c.onPrimary : raised ? (destructive ? c.error : c.onSurface) : destructive ? c.error : c.primary;
  const lg = size === 'lg';
  const radius = lg ? LumenSize.footerButtonRadius : LumenSize.buttonRadius;
  const bg = k === 'filled' ? c.primary : raised ? c.surfaceContainerHigh : 'transparent';
  return (
    <Pressable
      onPress={off ? undefined : onPress}
      disabled={off}
      accessibilityState={{ disabled: !!off, busy: !!busy }}
      accessibilityLabel={label}
      style={[{ opacity: off ? 0.5 : 1, borderRadius: radius }, style]}
    >
      <View
        style={{
          minHeight: lg ? LumenSize.footerActionHeight : LumenSize.actionHeight,
          paddingHorizontal: lg ? 18 : 18,
          borderRadius: radius,
          backgroundColor: bg,
          flexDirection: 'row',
          alignItems: 'center',
          justifyContent: 'center',
          gap: lg ? 10 : 8,
        }}
      >
        {busy ? <ActivityIndicator size="small" color={fg} /> : renderIcon ? renderIcon(fg) : icon}
        <Text color={fg} numberOfLines={1} style={[lg ? LumenType.action : LumenType.pill, { textAlign: 'center' }]}>
          {label}
        </Text>
      </View>
    </Pressable>
  );
}
