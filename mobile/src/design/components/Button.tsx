import type { ReactNode } from 'react';
import { ActivityIndicator, View, type StyleProp, type ViewStyle } from 'react-native';

import { LumenSize, LumenType } from '../lumen';
import { useScheme } from '../theme';
import { withAlpha } from './Callouts';
import { Pressable } from './Pressable';
import { Text } from './Text';
import { Spacing } from '../tokens';

/** `gradient` is kept for old call sites and renders as `filled`. */
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
  /** `md` is 48 high (`.btn`), `s` is 36 (`.btn.s`); `lg` is an alias of `md` kept for old call sites. */
  size?: 'md' | 's' | 'lg';
};

/**
 * Nameplate `.btn`: `filled` is the raised accent button (accent fill with the `--raise` edge and highlight);
 * `tonal`, `neutral` and `outlined` are transparent with a 1px hairline border; `text` is a bare label. A destructive
 * button uses the stop colour for its label and (when bordered) its border. Radius 8, 15/500 label, 40% opacity when off.
 */
export function Button({ label, onPress, kind = 'filled', icon, renderIcon, busy, disabled, style, destructive, size = 'md' }: Props) {
  const c = useScheme();
  const off = disabled || busy;
  const k: Kind = kind === 'gradient' ? 'filled' : kind;
  const small = size === 's';
  const bordered = k === 'tonal' || k === 'neutral' || k === 'outlined';
  const fg = k === 'filled' ? c.onPrimary : destructive ? c.stop : c.onSurface;
  const borderColor = destructive ? withAlpha(c.stop, 0.45) : c.rule2;
  return (
    <Pressable
      onPress={off ? undefined : onPress}
      disabled={off}
      accessibilityState={{ disabled: !!off, busy: !!busy }}
      accessibilityLabel={label}
      pressedScale={0.97}
      style={[{ opacity: off ? 0.4 : 1, borderRadius: LumenSize.buttonRadius }, style]}
    >
      <View
        style={[
          {
            minHeight: small ? LumenSize.actionHeightSmall : LumenSize.actionHeight,
            paddingHorizontal: k === 'text' ? 12 : small ? 14 : 20,
            borderRadius: LumenSize.buttonRadius,
            backgroundColor: k === 'filled' ? c.primary : 'transparent',
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: 'center',
            gap: Spacing.sm,
          },
          k === 'filled' && { boxShadow: c.raise },
          bordered && { borderWidth: 1, borderColor },
        ]}
      >
        {busy ? <ActivityIndicator size="small" color={fg} /> : renderIcon ? renderIcon(fg) : icon}
        <Text color={fg} numberOfLines={1} style={[LumenType.action, small && { fontSize: 14 }, { textAlign: 'center' }]}>
          {label}
        </Text>
      </View>
    </Pressable>
  );
}
