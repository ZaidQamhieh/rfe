import type { BottomTabBarProps } from 'expo-router/tabs';
import type { LucideIcon } from 'lucide-react-native';
import { Pressable as RNPressable, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { t } from '../../i18n';
import { LumenSize, LumenType } from '../lumen';
import { useScheme } from '../theme';
import { DuoIcon } from './DuoIcon';
import { Text } from './Text';
import { Spacing } from '../tokens';

export type NavDestination = {
  name: string;
  label: string;
  Icon: LucideIcon;
  /** A plain number written beside the label (active transfers); hidden when absent or zero. */
  count?: number;
  /** Other routes that belong under this destination, so it stays selected while one of them is open. */
  also?: string[];
};

/**
 * Nameplate `.nav`: a flat 76 high bar on the sheet colour with a hairline above it. Every destination is an icon and a
 * label; the selected one has a 60x30 pill in the accent-soft colour behind its icon and its label in ink. A count is a
 * mono number beside the label, never a badge. Destinations are matched to routes by name, so hidden routes in the
 * tab navigator do not shift them. The pill stays mounted and only its opacity changes, because Android drops a view's
 * radius when its background is swapped or mounted late.
 */
export function BottomNav({ state, navigation, destinations }: BottomTabBarProps & { destinations: NavDestination[] }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const current = state.routes[state.index]?.name;

  return (
    <View style={{ backgroundColor: c.surfaceContainerLowest, borderTopWidth: 1, borderTopColor: c.rule, paddingBottom: insets.bottom }}>
      <View style={{ height: LumenSize.dockHeight, flexDirection: 'row', alignItems: 'center' }}>
        {destinations.map((d) => {
          const selected = current === d.name || (d.also?.includes(current ?? '') ?? false);
          const route = state.routes.find((r) => r.name === d.name);
          const labelColor = selected ? c.onSurface : c.onSurfaceVariant;
          const count = d.count && d.count > 0 ? d.count : 0;
          return (
            <RNPressable
              key={d.name}
              accessibilityRole="tab"
              accessibilityState={{ selected }}
              accessibilityLabel={count ? t('navTransfersActive', { label: d.label, count }) : d.label}
              onPress={() => {
                if (!route) return;
                const ev = navigation.emit({ type: 'tabPress', target: route.key, canPreventDefault: true });
                if (!ev.defaultPrevented) navigation.navigate(route.name);
              }}
              style={{ flex: 1, height: '100%', alignItems: 'center', justifyContent: 'center', gap: Spacing.xs }}
            >
              <View style={{ width: 60, height: 30, borderRadius: LumenSize.dockItemRadius, alignItems: 'center', justifyContent: 'center' }}>
                <View style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, borderRadius: LumenSize.dockItemRadius, backgroundColor: c.primaryContainer, opacity: selected ? 1 : 0 }} />
                <DuoIcon icon={d.Icon} size={24} color={selected ? c.onPrimaryContainer : c.onSurfaceVariant} />
              </View>
              <View style={{ flexDirection: 'row', alignItems: 'center' }}>
                <Text style={LumenType.dock} color={labelColor} numberOfLines={1}>
                  {d.label}
                </Text>
                {count ? (
                  <Text style={[LumenType.monoSmall, { marginLeft: Spacing.xs }]} color={c.onSurface}>
                    {count}
                  </Text>
                ) : null}
              </View>
            </RNPressable>
          );
        })}
      </View>
    </View>
  );
}
