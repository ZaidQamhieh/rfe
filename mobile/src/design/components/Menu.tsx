import { useState, type ReactNode } from 'react';
import { Modal, Pressable as RNPressable, View } from 'react-native';

import { useScheme } from '../theme';
import { Radii } from '../tokens';
import { Text } from './Text';

export type MenuItem = { label: string; icon?: ReactNode; destructive?: boolean; onPress: () => void };

/** Popup menu anchored near the top-right of the screen (PopupMenuButton equivalent). */
export function Menu({ trigger, items, accessibilityLabel }: { trigger: ReactNode; items: MenuItem[]; accessibilityLabel: string }) {
  const c = useScheme();
  const [open, setOpen] = useState(false);
  return (
    <>
      <RNPressable accessibilityRole="button" accessibilityLabel={accessibilityLabel} onPress={() => setOpen(true)} style={{ width: 48, height: 48, alignItems: 'center', justifyContent: 'center' }}>
        {trigger}
      </RNPressable>
      <Modal visible={open} transparent animationType="fade" onRequestClose={() => setOpen(false)} statusBarTranslucent>
        <RNPressable style={{ flex: 1, backgroundColor: 'rgba(0,0,0,0.25)', justifyContent: 'center', padding: 32 }} onPress={() => setOpen(false)}>
          <View accessibilityViewIsModal style={{ backgroundColor: c.surfaceContainer, borderRadius: Radii.lg, paddingVertical: 8 }}>
            {items.map((it) => (
              <RNPressable
                key={it.label}
                accessibilityRole="menuitem"
                onPress={() => {
                  setOpen(false);
                  it.onPress();
                }}
                style={{ flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 16, minHeight: 48 }}
              >
                {it.icon}
                <Text color={it.destructive ? c.error : c.onSurface}>{it.label}</Text>
              </RNPressable>
            ))}
          </View>
        </RNPressable>
      </Modal>
    </>
  );
}
