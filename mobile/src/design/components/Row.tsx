import type { ReactNode } from 'react';
import { View } from 'react-native';

import type { EntryDensity } from '../../core/models/sort';
import { rowDensity } from '../density';
import { LumenType } from '../lumen';
import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { Pressable } from './Pressable';
import { Text } from './Text';

type Props = {
  title: string;
  subtitle?: string;
  /** Colour of the subtitle line (default ink-2), e.g. the success green for an online computer. */
  subtitleColor?: string;
  /** 40x40 leading slot: an Emoji, a DuoIcon, a thumbnail or a Checkbox. */
  lead?: ReactNode;
  /** Right side: a figure, a switch, a chevron. */
  trailing?: ReactNode;
  selected?: boolean;
  onPress?: () => void;
  onLongPress?: () => void;
  /** Bold (500) title, as `.t1.b`. */
  strong?: boolean;
  accessibilityLabel?: string;
  /** Comfortable (default) or Compact: sets the row's least height and vertical padding. */
  density?: EntryDensity;
};

/**
 * Nameplate `.row`, flat: min 64 high, 16 gap, 16 side padding, a 40x40 lead slot, a 16/22 title and a 14/20 ink-2
 * subtitle. Long names are cut in the middle so the extension stays visible. Selected rows get the fill colour, a 1px
 * `--sel-bd` border and an 8px side margin with radius 8 (the 8px padding keeps the content where it was).
 */
export function Row({ title, subtitle, subtitleColor, lead, trailing, selected = false, onPress, onLongPress, strong = false, accessibilityLabel, density = 'comfortable' }: Props) {
  const c = useScheme();
  const size = rowDensity(density);
  return (
    <Pressable onPress={onPress} onLongPress={onLongPress} disabled={!onPress && !onLongPress} accessibilityLabel={accessibilityLabel ?? title} accessibilityState={{ selected }} pressedScale={1}>
      <View
        style={{
          minHeight: size.rowMinHeight,
          flexDirection: 'row',
          alignItems: 'center',
          gap: Spacing.md,
          paddingVertical: size.rowPaddingY,
          paddingHorizontal: selected ? 7 : 16,
          marginHorizontal: selected ? 8 : 0,
          borderRadius: Radii.control,
          borderWidth: selected ? 1 : 0,
          borderColor: c.selBd,
          backgroundColor: selected ? c.surfaceContainer : 'transparent',
        }}
      >
        {lead ? <View style={{ width: 40, height: 40, alignItems: 'center', justifyContent: 'center' }}>{lead}</View> : null}
        <View style={{ flex: 1, minWidth: 0 }}>
          <Text numberOfLines={1} ellipsizeMode="middle" style={strong ? LumenType.rowTitle : LumenType.name}>
            {title}
          </Text>
          {subtitle ? (
            <Text numberOfLines={1} muted={!subtitleColor} color={subtitleColor} style={LumenType.meta}>
              {subtitle}
            </Text>
          ) : null}
        </View>
        {trailing}
      </View>
    </Pressable>
  );
}
