import type { BottomTabBarProps } from 'expo-router/tabs';
import type { LucideIcon } from 'lucide-react-native';
import { Pressable as RNPressable, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { mix } from '../color';
import { LumenSize, LumenType } from '../lumen';
import { useScheme } from '../theme';
import { Text } from './Text';

export type NavDestination = { name: string; label: string; Icon: LucideIcon; SelectedIcon?: LucideIcon };

/**
 * Lumen `.bottom`: a floating rounded dock (8 px inset, 12 px radius, raised surface, soft shadow) whose selected item is a
 * 9 px-radius pill tinted 13% with the host colour. Every pill background stays mounted and only its opacity changes,
 * because Android drops a view's radius when its background is swapped or mounted late.
 */
export function BottomNav({ state, navigation, destinations }: BottomTabBarProps & { destinations: NavDestination[] }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const pill = mix(c.primary, c.surfaceContainer, 0.13);

  return (
    <View style={{ backgroundColor: c.surface, paddingBottom: insets.bottom + LumenSize.dockInsetBottom, paddingHorizontal: LumenSize.dockInsetX, paddingTop: 4 }}>
      <View
        style={{
          height: LumenSize.dockHeight,
          padding: 6,
          flexDirection: 'row',
          alignItems: 'center',
          borderRadius: LumenSize.dockRadius,
          backgroundColor: c.surfaceContainer,
          elevation: 6,
          shadowColor: '#000',
          shadowOpacity: 0.2,
          shadowRadius: 14,
          shadowOffset: { width: 0, height: 4 },
        }}
      >
        {destinations.map((d, i) => {
          const selected = state.index === i;
          const tint = selected ? c.primary : c.onSurfaceVariant;
          const Icon = selected ? (d.SelectedIcon ?? d.Icon) : d.Icon;
          return (
            <RNPressable
              key={d.name}
              accessibilityRole="tab"
              accessibilityState={{ selected }}
              accessibilityLabel={d.label}
              onPress={() => {
                const route = state.routes[i];
                const ev = navigation.emit({ type: 'tabPress', target: route.key, canPreventDefault: true });
                if (!selected && !ev.defaultPrevented) navigation.navigate(route.name);
              }}
              style={{ flex: 1, height: '100%', alignItems: 'center', justifyContent: 'center' }}
            >
              <View style={{ position: 'absolute', top: 0, left: 2, right: 2, bottom: 0, borderRadius: LumenSize.dockItemRadius, backgroundColor: pill, opacity: selected ? 1 : 0 }} />
              <View style={{ alignItems: 'center', gap: 4 }}>
                <Icon size={26} color={tint} />
                <Text style={[LumenType.dock, selected && { fontFamily: 'Lato_700Bold' }]} color={tint} numberOfLines={1}>
                  {d.label}
                </Text>
              </View>
            </RNPressable>
          );
        })}
      </View>
    </View>
  );
}
