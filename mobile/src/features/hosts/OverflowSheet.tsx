import type { LucideIcon } from 'lucide-react-native';
import { View } from 'react-native';

import { BottomSheet, Pressable, SheetScroll, Text } from '../../design/components';
import { LumenType } from '../../design/lumen';
import { useScheme } from '../../design/theme';

export type OverflowItem = { key: string; label: string; icon: LucideIcon; onPress: () => void; destructive?: boolean; disabled?: boolean };

/** The "..." menu of Home and Workspaces: a bottom sheet of icon rows (48 dp+ touch targets). Rows close the sheet, then act. */
export function OverflowSheet({ visible, title, items, onClose }: { visible: boolean; title?: string; items: OverflowItem[]; onClose: () => void }) {
  const c = useScheme();
  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <View style={{ paddingTop: 10, paddingBottom: 4, alignItems: 'center' }}>
        <View style={{ width: 36, height: 4, borderRadius: 2, backgroundColor: c.outlineVariant }} />
      </View>
      {title ? (
        <Text style={[LumenType.sectionLabel, { paddingHorizontal: 18, paddingTop: 8, paddingBottom: 4 }]} muted numberOfLines={1} accessibilityRole="header">
          {title}
        </Text>
      ) : null}
      <SheetScroll>
        <View style={{ paddingHorizontal: 8, paddingBottom: 12 }}>
          {items.map((it) => {
            const color = it.destructive ? c.error : c.onSurface;
            const Icon = it.icon;
            return (
              <Pressable
                key={it.key}
                disabled={it.disabled}
                accessibilityLabel={it.label}
                accessibilityState={{ disabled: !!it.disabled }}
                onPress={() => {
                  onClose();
                  it.onPress();
                }}
                style={{ opacity: it.disabled ? 0.4 : 1 }}
              >
                <View style={{ minHeight: 56, flexDirection: 'row', alignItems: 'center', gap: 16, paddingHorizontal: 12 }}>
                  <Icon size={24} color={it.destructive ? c.error : c.onSurfaceVariant} />
                  <Text style={LumenType.rowTitle} color={color} numberOfLines={1}>
                    {it.label}
                  </Text>
                </View>
              </Pressable>
            );
          })}
        </View>
      </SheetScroll>
    </BottomSheet>
  );
}
