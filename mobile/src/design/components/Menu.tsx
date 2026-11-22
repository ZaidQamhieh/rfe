import { useState, type ReactNode } from 'react';
import { Modal, Pressable as RNPressable, View } from 'react-native';

import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { insetPadding } from '../safeInsets';
import { useScheme } from '../theme';
import { Radii, Spacing } from '../tokens';
import { Text } from './Text';

export type MenuItem = { label: string; icon?: ReactNode; destructive?: boolean; onPress: () => void };

/** The popup card's edge: a 1 px ring in the theme's rule colour and a soft drop shadow (shared with ComputerMenu). */
export const menuBoxShadow = (rule: string) => `0 0 0 1px ${rule}, 0 8px 24px rgba(0,0,0,0.08)`;

/** Popup menu anchored near the top-right of the screen (PopupMenuButton equivalent). */
export function Menu({ trigger, items, accessibilityLabel }: { trigger: ReactNode; items: MenuItem[]; accessibilityLabel: string }) {
  const c = useScheme();
  const insets = useSafeAreaInsets();
  const [open, setOpen] = useState(false);
  return (
    <>
      <RNPressable accessibilityRole="button" accessibilityLabel={accessibilityLabel} onPress={() => setOpen(true)} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
        {trigger}
      </RNPressable>
      <Modal visible={open} transparent animationType="fade" onRequestClose={() => setOpen(false)} statusBarTranslucent>
        <RNPressable style={{ flex: 1, backgroundColor: c.scrim, justifyContent: 'center', ...insetPadding(insets, Spacing.xl) }} onPress={() => setOpen(false)}>
          <View accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainerLowest, borderRadius: Radii.control, paddingVertical: Spacing.sm, boxShadow: menuBoxShadow(c.rule) }}>
            {items.map((it) => (
              <RNPressable
                key={it.label}
                accessibilityRole="menuitem"
                onPress={() => {
                  setOpen(false);
                  it.onPress();
                }}
                style={{ flexDirection: 'row', alignItems: 'center', gap: Spacing.md, paddingHorizontal: Spacing.md, minHeight: 48 }}
              >
                {it.icon}
                <Text variant="bodyLarge" style={{ fontSize: 15 }} color={it.destructive ? c.stop : c.onSurface}>{it.label}</Text>
              </RNPressable>
            ))}
          </View>
        </RNPressable>
      </Modal>
    </>
  );
}
